package promo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/platform/postgres"
)

type Service struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, now: time.Now}
}

const cols = `p.id, p.code, p.name, p.name_ar, p.description, p.description_ar, p.discount_type,
	p.percent_bp, p.amount_minor, p.max_discount_minor, p.currency, p.trial_days, p.applies_to,
	p.audience, p.kitchen_share_bp, p.usage_limit, p.per_customer_limit, p.usage_count,
	p.is_public, p.is_active, p.starts_at, p.ends_at, p.updated_at,
	COALESCE((SELECT array_agg(b.branch_id) FROM promo_branches b WHERE b.promo_id = p.id), '{}')`

func scan(row pgx.Row) (Promo, error) {
	var p Promo
	err := row.Scan(&p.ID, &p.Code, &p.Name, &p.NameAr, &p.Description, &p.DescriptionAr,
		&p.DiscountType, &p.PercentBP, &p.AmountMinor, &p.MaxDiscountMinor, &p.Currency,
		&p.TrialDays, &p.AppliesTo, &p.Audience, &p.KitchenShareBP, &p.UsageLimit,
		&p.PerCustomerLimit, &p.UsageCount, &p.IsPublic, &p.IsActive, &p.StartsAt, &p.EndsAt,
		&p.UpdatedAt, &p.BranchIDs)
	return p, err
}

func collect(rows pgx.Rows, err error) ([]Promo, error) {
	if err != nil {
		return nil, fmt.Errorf("promo: query: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Promo, error) { return scan(r) })
	if err != nil {
		return nil, fmt.Errorf("promo: scan: %w", err)
	}
	return out, nil
}

// ---- admin --------------------------------------------------------------

func (s *Service) List(ctx context.Context) ([]Promo, error) {
	return collect(s.pool.Query(ctx, `SELECT `+cols+` FROM promos p
		 WHERE p.deleted_at IS NULL ORDER BY p.updated_at DESC`))
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Promo, error) {
	p, err := scan(s.pool.QueryRow(ctx, `SELECT `+cols+` FROM promos p
		 WHERE p.id = $1 AND p.deleted_at IS NULL`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return p, httpx.ErrNotFound
	}
	return p, err
}

func (s *Service) Save(ctx context.Context, id *uuid.UUID, actor uuid.UUID, in Input) (Promo, error) {
	in, err := Normalize(in)
	if err != nil {
		return Promo{}, err
	}
	var saved uuid.UUID
	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		args := []any{in.Code, in.Name, in.NameAr, in.Description, in.DescriptionAr, in.DiscountType,
			in.PercentBP, in.AmountMinor, in.MaxDiscountMinor, in.Currency, in.TrialDays, in.AppliesTo,
			in.Audience, in.KitchenShareBP, in.UsageLimit, in.PerCustomerLimit, in.IsPublic,
			in.IsActive, in.StartsAt, in.EndsAt}
		var qerr error
		if id == nil {
			qerr = tx.QueryRow(ctx, `
				INSERT INTO promos (code, name, name_ar, description, description_ar, discount_type,
					percent_bp, amount_minor, max_discount_minor, currency, trial_days, applies_to,
					audience, kitchen_share_bp, usage_limit, per_customer_limit, is_public,
					is_active, starts_at, ends_at, created_by)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
				RETURNING id`, append(args, actor)...).Scan(&saved)
		} else {
			qerr = tx.QueryRow(ctx, `
				UPDATE promos SET code=$1, name=$2, name_ar=$3, description=$4, description_ar=$5,
					discount_type=$6, percent_bp=$7, amount_minor=$8, max_discount_minor=$9,
					currency=$10, trial_days=$11, applies_to=$12, audience=$13, kitchen_share_bp=$14,
					usage_limit=$15, per_customer_limit=$16, is_public=$17, is_active=$18,
					starts_at=$19, ends_at=$20, updated_at=now()
				 WHERE id = $21 AND deleted_at IS NULL RETURNING id`, append(args, *id)...).Scan(&saved)
		}
		if errors.Is(qerr, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if isUnique(qerr) {
			return invalid("code", "taken", "another promo already uses this code")
		}
		if qerr != nil {
			return fmt.Errorf("promo: save: %w", qerr)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM promo_branches WHERE promo_id = $1`, saved); err != nil {
			return fmt.Errorf("promo: branches: %w", err)
		}
		for _, b := range in.BranchIDs {
			if _, err := tx.Exec(ctx, `INSERT INTO promo_branches (promo_id, branch_id) VALUES ($1, $2)`,
				saved, b); err != nil {
				var pg *pgconn.PgError
				if errors.As(err, &pg) && pg.Code == "23503" {
					return invalid("branch_ids", "unknown", "one of the kitchens does not exist")
				}
				return fmt.Errorf("promo: branches: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return Promo{}, err
	}
	return s.Get(ctx, saved)
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `UPDATE promos SET deleted_at = now(), is_active = false
		 WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("promo: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	return nil
}

type Redemption struct {
	ID             uuid.UUID  `json:"id"`
	UserID         uuid.UUID  `json:"user_id"`
	SubscriptionID *uuid.UUID `json:"subscription_id"`
	RotationID     *uuid.UUID `json:"rotation_id"`
	Reference      string     `json:"reference"`
	DiscountMinor  int64      `json:"discount_minor"`
	PlatformMinor  int64      `json:"platform_funded_minor"`
	Currency       string     `json:"currency"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
}

func (s *Service) Redemptions(ctx context.Context, id uuid.UUID) ([]Redemption, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.user_id, r.subscription_id, r.rotation_id,
		       COALESCE(s.subscription_no, m.rotation_no, ''), r.discount_minor,
		       r.platform_funded_minor, r.currency, r.status, r.created_at
		  FROM promo_redemptions r
		  LEFT JOIN subscriptions s ON s.id = r.subscription_id
		  LEFT JOIN meal_rotations m ON m.id = r.rotation_id
		 WHERE r.promo_id = $1
		 ORDER BY r.created_at DESC LIMIT 200`, id)
	if err != nil {
		return nil, fmt.Errorf("promo: redemptions: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Redemption, error) {
		var x Redemption
		return x, r.Scan(&x.ID, &x.UserID, &x.SubscriptionID, &x.RotationID, &x.Reference,
			&x.DiscountMinor, &x.PlatformMinor, &x.Currency, &x.Status, &x.CreatedAt)
	})
}

// ---- storefront ---------------------------------------------------------

// Offers lists the public promos usable at a kitchen right now.
func (s *Service) Offers(ctx context.Context, branchID uuid.UUID) ([]*Offer, error) {
	all, err := collect(s.pool.Query(ctx, `SELECT `+cols+` FROM promos p
		 WHERE p.deleted_at IS NULL AND p.is_public AND p.is_active
		 ORDER BY p.trial_days NULLS LAST, p.updated_at DESC`))
	if err != nil {
		return nil, err
	}
	now := s.now()
	out := []*Offer{}
	for _, p := range all {
		if p.LiveAt(now) && p.CoversAll([]uuid.UUID{branchID}) {
			out = append(out, p.Offer(false))
		}
	}
	return out, nil
}

// ---- checkout -----------------------------------------------------------

// Ref is how a customer names a promo: a typed code, or the id of a public
// offer they clicked on the storefront.
type Ref struct {
	Code string
	ID   *uuid.UUID
}

func (r Ref) Empty() bool { return r.Code == "" && r.ID == nil }

// Resolve finds a promo and checks the customer may use it for these kitchens.
// target is ForSubscriptions or ForRotations. The per-customer limit is
// checked in Reserve, where stale unpaid checkouts can be released.
func (s *Service) Resolve(ctx context.Context, tx pgx.Tx, ref Ref, userID uuid.UUID,
	branches []uuid.UUID, target string) (*Promo, error) {
	if ref.Empty() {
		return nil, nil
	}
	var row pgx.Row
	if ref.ID != nil {
		row = tx.QueryRow(ctx, `SELECT `+cols+` FROM promos p
			 WHERE p.id = $1 AND p.deleted_at IS NULL AND (p.is_public OR p.code IS NOT NULL)`, *ref.ID)
	} else {
		row = tx.QueryRow(ctx, `SELECT `+cols+` FROM promos p
			 WHERE p.code = upper(btrim($1)) AND p.deleted_at IS NULL`, ref.Code)
	}
	p, err := scan(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, invalid("promo_code", "unknown", "this promo code does not exist")
	}
	if err != nil {
		return nil, fmt.Errorf("promo: resolve: %w", err)
	}
	if ref.ID != nil && !p.IsPublic {
		return nil, invalid("promo_code", "unknown", "this promo code does not exist")
	}
	if !p.LiveAt(s.now()) {
		return nil, invalid("promo_code", "not_live", "this promo has ended or is not running yet")
	}
	if p.AppliesTo != ForBoth && p.AppliesTo != target {
		msg := "this promo is for single-kitchen subscriptions"
		if p.AppliesTo == ForRotations {
			msg = "this promo is for four-kitchen rotations"
		}
		return nil, invalid("promo_code", "wrong_product", msg)
	}
	if !p.CoversAll(branches) {
		return nil, invalid("promo_code", "wrong_kitchen", "this promo does not cover the kitchen you chose")
	}
	if p.Audience == AudienceNew {
		var paid bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM subscription_cycles c
				  JOIN subscriptions s ON s.id = c.subscription_id
				 WHERE s.user_id = $1 AND c.status IN ('paid', 'refunded'))`, userID).Scan(&paid); err != nil {
			return nil, fmt.Errorf("promo: new customer: %w", err)
		}
		if paid {
			return nil, invalid("promo_code", "new_customers_only", "this promo is for first-time subscribers")
		}
	}
	return &p, nil
}

// Target names what a redemption pays for: one subscription or one rotation.
type Target struct {
	SubscriptionID *uuid.UUID
	RotationID     *uuid.UUID
}

// Reserve records a redemption and takes one use of the promo.
//
// If the customer is at their limit only because of checkouts they never
// paid, the oldest of those are cancelled and their uses returned: someone who
// abandons checkout and starts again must not be locked out of their own
// promo. A paid redemption is never released.
func (s *Service) Reserve(ctx context.Context, tx pgx.Tx, p *Promo, userID uuid.UUID, t Target,
	discountMinor, platformMinor int64, currency string) error {
	if p.PerCustomerLimit != nil {
		rows, err := tx.Query(ctx, `
			SELECT id, status, subscription_id, rotation_id FROM promo_redemptions
			 WHERE promo_id = $1 AND user_id = $2 AND status <> 'released'
			 ORDER BY created_at FOR UPDATE`, p.ID, userID)
		if err != nil {
			return fmt.Errorf("promo: count uses: %w", err)
		}
		type held struct {
			id       uuid.UUID
			status   string
			sub, rot *uuid.UUID
		}
		var hs []held
		for rows.Next() {
			var h held
			if err := rows.Scan(&h.id, &h.status, &h.sub, &h.rot); err != nil {
				rows.Close()
				return fmt.Errorf("promo: count uses: %w", err)
			}
			hs = append(hs, h)
		}
		rows.Close()
		used := len(hs)
		for _, h := range hs {
			if used < *p.PerCustomerLimit {
				break
			}
			if h.status != "pending" {
				continue
			}
			if err := releaseOne(ctx, tx, h.id, h.sub, h.rot, true); err != nil {
				return err
			}
			used--
		}
		if used >= *p.PerCustomerLimit {
			return invalid("promo_code", "already_used", "you have already used this promo")
		}
	}

	tag, err := tx.Exec(ctx, `
		UPDATE promos SET usage_count = usage_count + 1
		 WHERE id = $1 AND (usage_limit IS NULL OR usage_count < usage_limit)`, p.ID)
	if err != nil {
		return fmt.Errorf("promo: take use: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return invalid("promo_code", "exhausted", "this promo has been fully used")
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO promo_redemptions (promo_id, user_id, subscription_id, rotation_id,
			discount_minor, platform_funded_minor, currency)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		p.ID, userID, t.SubscriptionID, t.RotationID, discountMinor, platformMinor, currency); err != nil {
		return fmt.Errorf("promo: record redemption: %w", err)
	}
	return nil
}

// ReleaseUnpaid returns the use held by an unpaid subscription or rotation
// that is being cancelled. A no-op once paid.
func ReleaseUnpaid(ctx context.Context, tx pgx.Tx, t Target) error {
	rows, err := tx.Query(ctx, `
		SELECT id FROM promo_redemptions
		 WHERE status = 'pending' AND (subscription_id = $1 OR rotation_id = $2) FOR UPDATE`,
		t.SubscriptionID, t.RotationID)
	if err != nil {
		return fmt.Errorf("promo: release: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return fmt.Errorf("promo: release: %w", err)
	}
	for _, id := range ids {
		if err := releaseOne(ctx, tx, id, nil, nil, false); err != nil {
			return err
		}
	}
	return nil
}

// releaseOne returns a pending use. With cancelCheckout it also cancels the
// unpaid subscription or rotation that held it and voids its quotes, so the
// old discounted checkout can no longer be paid.
func releaseOne(ctx context.Context, tx pgx.Tx, id uuid.UUID, sub, rot *uuid.UUID, cancelCheckout bool) error {
	var promoID uuid.UUID
	if err := tx.QueryRow(ctx, `
		UPDATE promo_redemptions SET status = 'released', released_at = now()
		 WHERE id = $1 AND status = 'pending' RETURNING promo_id`, id).Scan(&promoID); err != nil {
		return fmt.Errorf("promo: release: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE promos SET usage_count = GREATEST(usage_count - 1, 0) WHERE id = $1`,
		promoID); err != nil {
		return fmt.Errorf("promo: return use: %w", err)
	}
	if !cancelCheckout {
		return nil
	}
	if rot != nil {
		if _, err := tx.Exec(ctx, `UPDATE meal_rotations SET status = 'cancelled'
			 WHERE id = $1 AND status = 'pending_payment'`, *rot); err != nil {
			return fmt.Errorf("promo: cancel stale rotation: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		WITH subs AS (
			UPDATE subscriptions SET status = 'cancelled', cancelled_at = now(), updated_at = now()
			 WHERE status = 'pending_payment' AND (id = $1 OR rotation_id = $2)
			RETURNING id
		), cycles AS (
			UPDATE subscription_cycles SET status = 'cancelled'
			 WHERE subscription_id IN (SELECT id FROM subs) AND status = 'pending'
			RETURNING order_id
		)
		UPDATE price_quotes SET state = 'void'
		 WHERE order_id IN (SELECT order_id FROM cycles) AND state = 'estimated'`, sub, rot); err != nil {
		return fmt.Errorf("promo: cancel stale checkout: %w", err)
	}
	return nil
}

func isUnique(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}
