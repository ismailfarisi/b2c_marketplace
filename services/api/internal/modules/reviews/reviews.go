// Package reviews holds kitchen ratings from paying subscribers and the
// food-safety verification staff record against a kitchen.
package reviews

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/platform/postgres"
)

const MaxText = 1000

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

type Review struct {
	ID             uuid.UUID  `json:"id"`
	BranchID       uuid.UUID  `json:"branch_id"`
	SubscriptionID uuid.UUID  `json:"subscription_id,omitempty"`
	UserID         *uuid.UUID `json:"user_id,omitempty"`
	KitchenName    string     `json:"kitchen_name,omitempty"`
	Rating         int        `json:"rating"`
	Comment        string     `json:"comment"`
	Status         string     `json:"status,omitempty"`
	HiddenReason   *string    `json:"hidden_reason,omitempty"`
	Reply          *string    `json:"reply"`
	RepliedAt      *time.Time `json:"replied_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func invalid(field, code, msg string) error {
	return &httpx.ValidationError{Fields: []httpx.FieldError{{Field: field, Code: code, Message: msg}}}
}

// Validate checks a rating and comment.
func Validate(rating int, comment string) (string, error) {
	comment = strings.TrimSpace(comment)
	if rating < 1 || rating > 5 {
		return comment, invalid("rating", "out_of_range", "rate from 1 to 5 stars")
	}
	if len([]rune(comment)) > MaxText {
		return comment, invalid("comment", "too_long", "keep it under 1000 characters")
	}
	return comment, nil
}

// Eligibility says whether the customer may review a subscription now.
type Eligibility struct {
	CanReview bool    `json:"can_review"`
	Reason    string  `json:"reason,omitempty"`
	Review    *Review `json:"review"`
}

func (s *Service) eligible(ctx context.Context, q pgx.Tx, userID, subID uuid.UUID) (uuid.UUID, int, error) {
	var branchID uuid.UUID
	var delivered int
	err := q.QueryRow(ctx, `
		SELECT s.branch_id,
		       (SELECT count(*) FROM fulfillments f
		         WHERE f.subscription_id = s.id AND f.status = 'completed')::int
		  FROM subscriptions s
		 WHERE s.id = $1 AND s.user_id = $2`, subID, userID).Scan(&branchID, &delivered)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, 0, httpx.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, 0, fmt.Errorf("reviews: eligibility: %w", err)
	}
	return branchID, delivered, nil
}

const reviewCols = `r.id, r.branch_id, r.subscription_id, r.rating, r.comment, r.status,
	r.hidden_reason, r.reply, r.replied_at, r.created_at, r.updated_at`

func scanReview(row pgx.Row) (Review, error) {
	var r Review
	err := row.Scan(&r.ID, &r.BranchID, &r.SubscriptionID, &r.Rating, &r.Comment, &r.Status,
		&r.HiddenReason, &r.Reply, &r.RepliedAt, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

// Mine returns whether the customer can review the subscription, and their
// review if they already wrote one.
func (s *Service) Mine(ctx context.Context, userID, subID uuid.UUID) (Eligibility, error) {
	var out Eligibility
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		_, delivered, err := s.eligible(ctx, tx, userID, subID)
		if err != nil {
			return err
		}
		r, err := scanReview(tx.QueryRow(ctx, `SELECT `+reviewCols+` FROM kitchen_reviews r
			 WHERE r.subscription_id = $1`, subID))
		if err == nil {
			out.Review = &r
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("reviews: mine: %w", err)
		}
		out.CanReview = delivered > 0
		if !out.CanReview {
			out.Reason = "after_first_delivery"
		}
		return nil
	})
	return out, err
}

// Write creates or updates the customer's review of a subscription's kitchen.
func (s *Service) Write(ctx context.Context, userID, subID uuid.UUID, rating int, comment string) (Review, error) {
	comment, err := Validate(rating, comment)
	if err != nil {
		return Review{}, err
	}
	var out Review
	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		branchID, delivered, err := s.eligible(ctx, tx, userID, subID)
		if err != nil {
			return err
		}
		if delivered == 0 {
			return invalid("rating", "not_yet", "you can rate this kitchen after your first delivery")
		}
		out, err = scanReview(tx.QueryRow(ctx, `
			INSERT INTO kitchen_reviews AS r (branch_id, user_id, subscription_id, rating, comment)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (subscription_id) DO UPDATE
			   SET rating = EXCLUDED.rating, comment = EXCLUDED.comment, updated_at = now()
			RETURNING `+reviewCols, branchID, userID, subID, rating, comment))
		if err != nil {
			return fmt.Errorf("reviews: write: %w", err)
		}
		return recompute(ctx, tx, branchID)
	})
	return out, err
}

// recompute refreshes a kitchen's rating cache from its published reviews.
func recompute(ctx context.Context, tx pgx.Tx, branchID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `
		UPDATE vendor_branches b
		   SET rating = x.avg, rating_count = x.n
		  FROM (SELECT round(avg(rating)::numeric, 2) AS avg, count(*)::int AS n
		          FROM kitchen_reviews WHERE branch_id = $1 AND status = 'published') x
		 WHERE b.id = $1`, branchID); err != nil {
		return fmt.Errorf("reviews: recompute: %w", err)
	}
	return nil
}

// Public lists a kitchen's published reviews, newest first, without who
// wrote them: every one is from a verified subscriber, and that is the claim.
func (s *Service) Public(ctx context.Context, branchID uuid.UUID, limit int) ([]Review, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `SELECT `+reviewCols+` FROM kitchen_reviews r
		 WHERE r.branch_id = $1 AND r.status = 'published'
		 ORDER BY r.created_at DESC LIMIT $2`, branchID, limit)
	if err != nil {
		return nil, fmt.Errorf("reviews: public: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Review, error) {
		x, err := scanReview(r)
		x.SubscriptionID, x.Status, x.HiddenReason = uuid.Nil, "", nil
		return x, err
	})
	if err != nil {
		return nil, fmt.Errorf("reviews: public: %w", err)
	}
	return out, nil
}

// ForKitchen lists every review of a branch for the kitchen's own console,
// including hidden ones so the kitchen sees what staff removed.
func (s *Service) ForKitchen(ctx context.Context, branchID uuid.UUID) ([]Review, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+reviewCols+` FROM kitchen_reviews r
		 WHERE r.branch_id = $1 ORDER BY r.created_at DESC LIMIT 200`, branchID)
	if err != nil {
		return nil, fmt.Errorf("reviews: kitchen: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Review, error) { return scanReview(r) })
}

// Reply sets or replaces the kitchen's public reply. Empty clears it.
func (s *Service) Reply(ctx context.Context, branchID, reviewID uuid.UUID, reply string) (Review, error) {
	reply = strings.TrimSpace(reply)
	if len([]rune(reply)) > MaxText {
		return Review{}, invalid("reply", "too_long", "keep it under 1000 characters")
	}
	r, err := scanReview(s.pool.QueryRow(ctx, `
		UPDATE kitchen_reviews r SET reply = NULLIF($3, ''),
		       replied_at = CASE WHEN $3 = '' THEN NULL ELSE now() END, updated_at = now()
		 WHERE r.id = $1 AND r.branch_id = $2
		RETURNING `+reviewCols, reviewID, branchID, reply))
	if errors.Is(err, pgx.ErrNoRows) {
		return r, httpx.ErrNotFound
	}
	return r, err
}

// Admin lists reviews across kitchens; status filters to published or hidden.
func (s *Service) Admin(ctx context.Context, status string) ([]Review, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+reviewCols+`, r.user_id, b.name FROM kitchen_reviews r
		  JOIN vendor_branches b ON b.id = r.branch_id
		 WHERE ($1 = '' OR r.status = $1)
		 ORDER BY r.created_at DESC LIMIT 200`, status)
	if err != nil {
		return nil, fmt.Errorf("reviews: admin: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Review, error) {
		var r Review
		var uid uuid.UUID
		err := row.Scan(&r.ID, &r.BranchID, &r.SubscriptionID, &r.Rating, &r.Comment, &r.Status,
			&r.HiddenReason, &r.Reply, &r.RepliedAt, &r.CreatedAt, &r.UpdatedAt, &uid, &r.KitchenName)
		r.UserID = &uid
		return r, err
	})
}

// SetHidden hides or restores a review and refreshes the kitchen's rating.
func (s *Service) SetHidden(ctx context.Context, actor, reviewID uuid.UUID, hidden bool, reason string) (Review, error) {
	reason = strings.TrimSpace(reason)
	if hidden && reason == "" {
		return Review{}, invalid("reason", "required", "say why the review is hidden")
	}
	var out Review
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		status := "published"
		if hidden {
			status = "hidden"
		}
		var err error
		out, err = scanReview(tx.QueryRow(ctx, `
			UPDATE kitchen_reviews r SET status = $2,
			       hidden_reason = CASE WHEN $2 = 'hidden' THEN $3 ELSE NULL END,
			       hidden_by = CASE WHEN $2 = 'hidden' THEN $4::uuid ELSE NULL END, updated_at = now()
			 WHERE r.id = $1 RETURNING `+reviewCols, reviewID, status, reason, actor))
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("reviews: hide: %w", err)
		}
		return recompute(ctx, tx, out.BranchID)
	})
	return out, err
}

// FoodSafety is a kitchen's food-safety verification.
type FoodSafety struct {
	Ref        *string    `json:"ref"`
	ExpiresOn  *string    `json:"expires_on"`
	VerifiedAt *time.Time `json:"verified_at"`
	Verified   bool       `json:"verified"`
}

// SetFoodSafety records (verified=true) or clears staff verification of a
// kitchen's food-safety licence. A verification needs a reference and an
// expiry in the future; the badge disappears on its own when it lapses.
func (s *Service) SetFoodSafety(ctx context.Context, actor, branchID uuid.UUID, verified bool, ref, expiresOn string) (FoodSafety, error) {
	ref = strings.TrimSpace(ref)
	var exp any
	if verified {
		if ref == "" {
			return FoodSafety{}, invalid("ref", "required", "enter the licence or certificate number")
		}
		d, err := time.Parse("2006-01-02", expiresOn)
		if err != nil {
			return FoodSafety{}, invalid("expires_on", "invalid_date", "expected YYYY-MM-DD")
		}
		if !d.After(time.Now()) {
			return FoodSafety{}, invalid("expires_on", "expired", "this licence has already expired")
		}
		exp = d
	}
	var out FoodSafety
	var expires *time.Time
	err := s.pool.QueryRow(ctx, `
		UPDATE vendor_branches SET
		       food_safety_ref = CASE WHEN $2 THEN $3 ELSE NULL END,
		       food_safety_expires_on = CASE WHEN $2 THEN $4::date ELSE NULL END,
		       food_safety_verified_at = CASE WHEN $2 THEN now() ELSE NULL END,
		       food_safety_verified_by = CASE WHEN $2 THEN $5::uuid ELSE NULL END,
		       updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL
		RETURNING food_safety_ref, food_safety_expires_on, food_safety_verified_at`,
		branchID, verified, ref, exp, actor).Scan(&out.Ref, &expires, &out.VerifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, httpx.ErrNotFound
	}
	if err != nil {
		return out, fmt.Errorf("reviews: food safety: %w", err)
	}
	if expires != nil {
		d := expires.Format("2006-01-02")
		out.ExpiresOn = &d
	}
	out.Verified = out.VerifiedAt != nil
	return out, nil
}

// GetFoodSafety reads a kitchen's verification for the admin page.
func (s *Service) GetFoodSafety(ctx context.Context, branchID uuid.UUID) (FoodSafety, error) {
	var out FoodSafety
	var expires *time.Time
	err := s.pool.QueryRow(ctx, `SELECT food_safety_ref, food_safety_expires_on, food_safety_verified_at
		  FROM vendor_branches WHERE id = $1 AND deleted_at IS NULL`, branchID).
		Scan(&out.Ref, &expires, &out.VerifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, httpx.ErrNotFound
	}
	if err != nil {
		return out, fmt.Errorf("reviews: food safety: %w", err)
	}
	if expires != nil {
		d := expires.Format("2006-01-02")
		out.ExpiresOn = &d
	}
	out.Verified = out.VerifiedAt != nil && expires != nil && expires.After(time.Now())
	return out, nil
}
