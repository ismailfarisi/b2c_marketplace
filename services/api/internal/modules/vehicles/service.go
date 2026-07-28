package vehicles

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/platform/postgres"
)

// The three things this module needs from elsewhere, expressed as what it needs
// rather than as who provides it.
//
// Without these interfaces the listing flow would import the packages module
// (to spend a credit), the chat module (to open a thread) and the trust module
// (to raise a signal), and the classifieds engine would be a knot rather than a
// layer. With them, `vehicles` depends on "may this seller publish", "open a
// thread", "record this evidence" — three sentences, each replaceable.
type (
	// Entitlements is the package quota, as the listing flow needs it.
	Entitlements interface {
		// CheckListingQuota answers the seller-facing question before any work
		// is done, so a seller out of credit is told at submit rather than
		// after a moderator has spent two minutes on the listing.
		CheckListingQuota(ctx context.Context, sellerID uuid.UUID) error
		// ConsumeListing spends a credit. Called at activation, because a
		// rejected listing must not cost the seller anything.
		ConsumeListing(ctx context.Context, tx pgx.Tx, sellerID uuid.UUID) error
	}

	// Threads opens the buyer↔seller conversation. Chat is not optional in this
	// vertical (SCOPE-EXTENSIONS §3): the phone number never becomes public, so
	// an inquiry that cannot open a thread is an inquiry that cannot happen.
	Threads interface {
		Open(ctx context.Context, tx pgx.Tx, listingID, buyerID, sellerID uuid.UUID) (uuid.UUID, error)
	}

	// Signals records fraud evidence. Fire-and-forget from this module's point
	// of view: a detector that fails must never block a listing, because the
	// failure mode of "no listings" is worse than the failure mode of "one
	// unscored listing".
	Signals interface {
		Raise(ctx context.Context, tx pgx.Tx, subjectType string, subjectID uuid.UUID,
			kind string, severity int, detail map[string]any) error
	}
)

type Service struct {
	pool    *pgxpool.Pool
	repo    *Repository
	quota   Entitlements
	threads Threads
	signals Signals
	nowFn   func() time.Time
}

func NewService(pool *pgxpool.Pool, quota Entitlements, threads Threads, signals Signals) *Service {
	return &Service{
		pool:    pool,
		repo:    NewRepository(pool),
		quota:   quota,
		threads: threads,
		signals: signals,
		nowFn:   time.Now,
	}
}

func (s *Service) Repo() *Repository { return s.repo }

// ─────────────────────────────────────────────────────────────────────────────
// Authoring
// ─────────────────────────────────────────────────────────────────────────────

type CreateListingInput struct {
	SellerID uuid.UUID
	Draft    Draft
}

// Create writes a draft.
//
// Drafts are free and unlimited: quota is checked at submit and spent at
// activation. A classifieds product that charges before the seller has seen
// their own listing loses the seller.
func (s *Service) Create(ctx context.Context, in CreateListingInput) (*Listing, error) {
	now := s.nowFn()
	if issues := in.Draft.Validate(now); len(issues) > 0 {
		return nil, validationError(issues)
	}

	makeID, modelID, err := s.repo.ResolveTaxonomy(ctx, in.Draft.MakeSlug, in.Draft.ModelSlug)
	if err != nil {
		return nil, err
	}

	sellerKind, dealerID, err := s.sellerIdentity(ctx, in.SellerID)
	if err != nil {
		return nil, err
	}

	var listingID uuid.UUID
	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		id, _, err := s.repo.Insert(ctx, tx, CreateInput{
			SellerID:   in.SellerID,
			SellerKind: sellerKind,
			DealerID:   dealerID,
			MakeID:     makeID,
			ModelID:    modelID,
			Draft:      in.Draft,
		})
		if err != nil {
			return err
		}
		listingID = id
		return s.screen(ctx, tx, id, in.Draft, now)
	})
	if err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, listingID)
}

// screen raises the evidence a listing carries on its face.
//
// These are the checks that need no other row: an odometer that disagrees with
// the model year, a VIN whose check digit fails on a spec that mandates it, a
// missing VIN on an expensive car. Cross-listing detection (rollback across
// relists, duplicate VINs, price outliers) needs the rest of the table and runs
// in the worker.
func (s *Service) screen(ctx context.Context, tx pgx.Tx, listingID uuid.UUID, d Draft, now time.Time) error {
	if s.signals == nil {
		return nil
	}

	switch ClassifyMileage(d.Year, d.MileageKM, now) {
	case BandSuspiciouslyLow:
		if err := s.signals.Raise(ctx, tx, "listing", listingID, "mileage_suspiciously_low", 55,
			map[string]any{"year": d.Year, "mileage_km": d.MileageKM}); err != nil {
			return err
		}
	case BandImplausible:
		if err := s.signals.Raise(ctx, tx, "listing", listingID, "mileage_implausible", 40,
			map[string]any{"year": d.Year, "mileage_km": d.MileageKM}); err != nil {
			return err
		}
	}

	vin := NormaliseVIN(d.VIN)
	// Only where the check digit is mandatory. A GCC or Japanese VIN failing it
	// is routine and flagging those would bury the reviewer in noise —
	// see the comment on CheckDigitValid.
	if vin != "" && d.RegionalSpec == "us" && !CheckDigitValid(vin) {
		if err := s.signals.Raise(ctx, tx, "listing", listingID, "vin_check_digit_failed", 70,
			map[string]any{"vin": vin, "regional_spec": d.RegionalSpec}); err != nil {
			return err
		}
	}

	return nil
}

// Submit sends a draft to the review queue.
//
// Quota is *checked* here and spent at activation. The gap is a real race — a
// seller with one credit can submit three listings and the third approval will
// fail on the database CHECK — and it is accepted rather than closed with a
// reservation table. The cost is that the failure lands on a moderator instead
// of the seller, which is the wrong person; it is logged as such in §8 of the
// phase doc rather than pretended away.
func (s *Service) Submit(ctx context.Context, userID, listingID uuid.UUID) error {
	l, err := s.repo.Get(ctx, listingID)
	if err != nil {
		return err
	}
	if l.SellerID != userID {
		return httpx.ErrForbidden
	}
	if s.quota != nil {
		if err := s.quota.CheckListingQuota(ctx, userID); err != nil {
			return err
		}
	}
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		return s.repo.Transition(ctx, tx, listingID, l.Status, StatusPendingReview,
			"seller", &userID, "submitted for review")
	})
}

// Approve is the moderator's decision. It is the only path to `active`.
func (s *Service) Approve(ctx context.Context, moderatorID, listingID uuid.UUID) error {
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		l, err := s.repo.GetForUpdate(ctx, tx, listingID)
		if err != nil {
			return err
		}
		if s.quota != nil {
			if err := s.quota.ConsumeListing(ctx, tx, l.SellerID); err != nil {
				return err
			}
		}
		return s.repo.Transition(ctx, tx, listingID, l.Status, StatusActive,
			"moderator", &moderatorID, "approved")
	})
}

func (s *Service) Reject(ctx context.Context, moderatorID, listingID uuid.UUID, reason string) error {
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		l, err := s.repo.GetForUpdate(ctx, tx, listingID)
		if err != nil {
			return err
		}
		return s.repo.Transition(ctx, tx, listingID, l.Status, StatusRejected,
			"moderator", &moderatorID, reason)
	})
}

// ChangePrice is separate from a general update because it is the one edit that
// does not re-enter review.
//
// A price drop is the healthiest thing a stale listing can do and putting it
// behind a review queue means sellers stop doing it. A price *rise* on an
// active listing is the bait-and-switch shape, so that one does re-enter.
func (s *Service) ChangePrice(ctx context.Context, userID, listingID uuid.UUID, priceMinor int64) error {
	if priceMinor <= 0 {
		return validationError([]FieldIssue{{Field: "price_minor", Code: "required"}})
	}
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		l, err := s.repo.GetForUpdate(ctx, tx, listingID)
		if err != nil {
			return err
		}
		if l.SellerID != userID {
			return httpx.ErrForbidden
		}
		if err := s.repo.UpdatePrice(ctx, tx, listingID, priceMinor, userID); err != nil {
			return err
		}

		if l.Status == StatusActive && priceMinor > l.PriceMinor {
			if s.signals != nil {
				if err := s.signals.Raise(ctx, tx, "listing", listingID, "price_raised_while_live", 45,
					map[string]any{"from": l.PriceMinor, "to": priceMinor}); err != nil {
					return err
				}
			}
			return s.repo.Transition(ctx, tx, listingID, StatusActive, StatusPendingReview,
				"system", nil, "price raised on a live listing")
		}
		return nil
	})
}

// MarkSold takes the listing down and records the seller's claim about price.
//
// The price is stored and never trusted. The platform does not observe the
// transaction — it happens at an RTA counter — so this number is a survey
// response, not a market observation. Everything downstream that reads it must
// say so; see §5 of the phase doc.
func (s *Service) MarkSold(ctx context.Context, userID, listingID uuid.UUID, soldPriceMinor int64) error {
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		l, err := s.repo.GetForUpdate(ctx, tx, listingID)
		if err != nil {
			return err
		}
		if l.SellerID != userID {
			return httpx.ErrForbidden
		}
		if err := s.repo.Transition(ctx, tx, listingID, l.Status, StatusSold,
			"seller", &userID, "marked sold by seller"); err != nil {
			return err
		}
		if soldPriceMinor > 0 {
			if _, err := tx.Exec(ctx,
				`UPDATE vehicle_listings SET sold_price_minor = $2 WHERE id = $1`,
				listingID, soldPriceMinor); err != nil {
				return fmt.Errorf("record sold price: %w", err)
			}
		}
		return nil
	})
}

func (s *Service) SetStatus(ctx context.Context, userID, listingID uuid.UUID, to Status, reason string) error {
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		l, err := s.repo.GetForUpdate(ctx, tx, listingID)
		if err != nil {
			return err
		}
		if l.SellerID != userID {
			return httpx.ErrForbidden
		}
		return s.repo.Transition(ctx, tx, listingID, l.Status, to, "seller", &userID, reason)
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Demand
// ─────────────────────────────────────────────────────────────────────────────

type InquireInput struct {
	BuyerID        uuid.UUID
	ListingID      uuid.UUID
	Source         string
	SourceReelID   *uuid.UUID
	SourceActionID *uuid.UUID
}

type Inquiry struct {
	LeadID         uuid.UUID
	ConversationID uuid.UUID
	Created        bool
}

// Inquire is this vertical's conversion event.
//
// It is the analogue of ordering.PlaceOrder and it is deliberately much
// smaller: no capacity, no pricing, no payment, no fulfillment. What it shares
// with PlaceOrder is the part that matters for the plan — the Phase 3
// attribution chain rides along, so "did a reel produce a lead" is answerable
// with the same query shape as "did a reel produce an order".
func (s *Service) Inquire(ctx context.Context, in InquireInput) (*Inquiry, error) {
	l, err := s.repo.Get(ctx, in.ListingID)
	if err != nil {
		return nil, err
	}
	if l.SellerID == in.BuyerID {
		return nil, fmt.Errorf("%w: a seller cannot inquire about their own listing", httpx.ErrConflict)
	}
	if !l.Status.Live() {
		return nil, fmt.Errorf("%w: this listing is %s", httpx.ErrConflict, l.Status)
	}

	source := in.Source
	if source == "" {
		source = "listing_page"
	}

	var out Inquiry
	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		convID, err := s.threads.Open(ctx, tx, l.ID, in.BuyerID, l.SellerID)
		if err != nil {
			return err
		}
		leadID, created, err := s.repo.UpsertLead(ctx, tx, LeadInput{
			ListingID:      l.ID,
			BuyerID:        in.BuyerID,
			SellerID:       l.SellerID,
			Source:         source,
			SourceReelID:   in.SourceReelID,
			SourceActionID: in.SourceActionID,
			ConversationID: uuid.NullUUID{UUID: convID, Valid: true},
		})
		if err != nil {
			return err
		}
		out = Inquiry{LeadID: leadID, ConversationID: convID, Created: created}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

type OfferInput struct {
	BuyerID     uuid.UUID
	ListingID   uuid.UUID
	AmountMinor int64
}

// MakeOffer records a non-binding number.
//
// No payment intent, no hold, no ledger entry, and that is not an omission.
// Escrow would solve most of this category's fraud and implies a
// payment-institution licence (SCOPE-EXTENSIONS §3). Until that decision is
// taken, the honest product is a stated intention — and the API says so rather
// than letting the word "accepted" imply a contract.
func (s *Service) MakeOffer(ctx context.Context, in OfferInput) (uuid.UUID, error) {
	l, err := s.repo.Get(ctx, in.ListingID)
	if err != nil {
		return uuid.Nil, err
	}
	if !l.Status.Live() {
		return uuid.Nil, fmt.Errorf("%w: this listing is %s", httpx.ErrConflict, l.Status)
	}
	if l.SellerID == in.BuyerID {
		return uuid.Nil, fmt.Errorf("%w: a seller cannot bid on their own listing", httpx.ErrConflict)
	}
	if in.AmountMinor <= 0 {
		return uuid.Nil, validationError([]FieldIssue{{Field: "amount_minor", Code: "required"}})
	}

	var offerID uuid.UUID
	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		convID, err := s.threads.Open(ctx, tx, l.ID, in.BuyerID, l.SellerID)
		if err != nil {
			return err
		}
		if _, _, err := s.repo.UpsertLead(ctx, tx, LeadInput{
			ListingID: l.ID, BuyerID: in.BuyerID, SellerID: l.SellerID,
			Source:         "listing_page",
			ConversationID: uuid.NullUUID{UUID: convID, Valid: true},
		}); err != nil {
			return err
		}
		offerID, err = s.repo.InsertOffer(ctx, tx, Offer{
			ListingID:      l.ID,
			ConversationID: uuid.NullUUID{UUID: convID, Valid: true},
			BuyerID:        in.BuyerID,
			AmountMinor:    in.AmountMinor,
			Currency:       l.Currency,
			ExpiresAt:      s.nowFn().Add(OfferTTL),
		})
		return err
	})
	return offerID, err
}

func (s *Service) RespondToOffer(ctx context.Context, sellerID, offerID uuid.UUID, accept bool) error {
	state := "rejected"
	if accept {
		state = "accepted"
	}
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		return s.repo.RespondToOffer(ctx, tx, offerID, sellerID, state)
	})
}

func (s *Service) Search(ctx context.Context, f Filter) ([]Result, int64, error) {
	return s.repo.Search(ctx, f)
}

// ─────────────────────────────────────────────────────────────────────────────

// sellerIdentity resolves whether this user posts as a private seller or under
// a dealer profile.
//
// Derived, never client-supplied. A private seller who can set seller_kind to
// 'dealer' in a request body gets the dealer badge for free, and the badge is
// the only thing a buyer has to go on.
func (s *Service) sellerIdentity(ctx context.Context, userID uuid.UUID) (string, uuid.NullUUID, error) {
	var dealerID uuid.NullUUID
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM dealer_profiles
		 WHERE owner_user_id = $1 AND status = 'active'`, userID).Scan(&dealerID)
	if err != nil {
		// No dealer profile is the common case, not an error.
		return "private", uuid.NullUUID{}, nil
	}
	if !dealerID.Valid {
		return "private", uuid.NullUUID{}, nil
	}
	return "dealer", dealerID, nil
}

func validationError(issues []FieldIssue) error {
	fields := make([]httpx.FieldError, 0, len(issues))
	for _, i := range issues {
		fields = append(fields, httpx.FieldError{Field: i.Field, Code: i.Code, Message: i.Message})
	}
	return &httpx.ValidationError{Fields: fields}
}

// PendingListing is one listing awaiting review.
type PendingListing struct {
	ID           uuid.UUID
	Title        string
	SellerType   string
	SellerID     uuid.UUID
	SellerName   string
	Make         string
	Model        string
	Year         int
	PriceMinor   int64
	Currency     string
	Mileage      int
	VIN          string
	RegionalSpec string
	City         string
	PhotoKey     string
	SubmittedAt  time.Time
	// TrustScore is the seller's standing at the moment of review. Carried with
	// the listing because a reviewer deciding on a car from a seller with two
	// confirmed fraud signals is making a different decision from one deciding
	// on the same car from a clean seller — and looking it up separately is a
	// step that gets skipped under queue pressure.
	TrustScore float64
	TrustBand  string
}

// ReviewQueue lists listings awaiting a decision.
//
// Written when the admin panel was built. Phase 6 shipped `Approve`/`Reject`
// and a fraud queue, and no way to find a listing waiting for either — so the
// only listings that could be reviewed were the ones somebody was already
// looking at.
//
// Oldest first, and the VIN and price come with the row. PHASE-6 §2 said a
// listing decision "needs the VIN and the price history in front of it"; a
// queue that returns titles and ids makes that a second request per row, and
// the second request is the one nobody makes.
func (s *Service) ReviewQueue(ctx context.Context, limit int) ([]PendingListing, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	rows, err := s.pool.Query(ctx, `
		SELECT l.id, coalesce(l.title, ''), l.seller_kind, l.seller_id,
		       coalesce(d.business_name, u.display_name, u.phone, ''),
		       coalesce(mk.name, ''), coalesce(md.name, ''), coalesce(l.year, 0),
		       l.price_minor, l.currency, coalesce(l.mileage_km, 0),
		       coalesce(l.vin, ''), l.regional_spec, coalesce(l.city, ''),
		       coalesce((SELECT p.s3_key FROM listing_photos p
		                  WHERE p.listing_id = l.id ORDER BY p.position LIMIT 1), ''),
		       l.updated_at,
		       coalesce(ts.score, 100), coalesce(ts.band, 'clean')
		  FROM vehicle_listings l
		  LEFT JOIN users u ON u.id = l.seller_id
		  LEFT JOIN dealer_profiles d ON d.id = l.dealer_id
		  LEFT JOIN vehicle_models md ON md.id = l.model_id
		  LEFT JOIN vehicle_makes mk  ON mk.id = md.make_id
		  -- Trust is keyed on the *user*, not the listing's seller kind: a
		  -- dealer's signals attach to the person who owns the account, which is
		  -- what makes a fraudster's second dealership visible.
		  LEFT JOIN trust_scores ts ON ts.subject_type = 'user' AND ts.subject_id = l.seller_id
		 WHERE l.status = 'pending_review'
		 ORDER BY l.updated_at
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("vehicles: review queue: %w", err)
	}
	defer rows.Close()

	var out []PendingListing
	for rows.Next() {
		var p PendingListing
		if err := rows.Scan(&p.ID, &p.Title, &p.SellerType, &p.SellerID, &p.SellerName,
			&p.Make, &p.Model, &p.Year, &p.PriceMinor, &p.Currency, &p.Mileage,
			&p.VIN, &p.RegionalSpec, &p.City, &p.PhotoKey, &p.SubmittedAt,
			&p.TrustScore, &p.TrustBand); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
