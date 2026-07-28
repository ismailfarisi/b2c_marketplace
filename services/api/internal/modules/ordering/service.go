// Package ordering orchestrates placing an order.
//
// It is the only place that knows the sequence: validate → quote → reserve
// capacity → materialise fulfillments → persist. It knows nothing about what a
// laundry or a meal is; every vertical-specific decision is delegated to a
// strategy resolved from the registry.
package ordering

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/platform/postgres"
	"github.com/marketplace/api/internal/seam/fulfillment"
	"github.com/marketplace/api/internal/seam/servicetype"
)

type Service struct {
	pool      *pgxpool.Pool
	verticals *servicetype.Registry
	repo      *Repository

	// reservationTTL bounds how long an unpaid order holds capacity. Long
	// enough for 3DS on a bad connection, short enough that an abandoned cart
	// does not cost the vendor a slot for the rest of the day.
	reservationTTL time.Duration
}

func NewService(pool *pgxpool.Pool, verticals *servicetype.Registry) *Service {
	return &Service{
		pool:           pool,
		verticals:      verticals,
		repo:           &Repository{},
		reservationTTL: 15 * time.Minute,
	}
}

type PlaceOrderInput struct {
	UserID    uuid.UUID
	BranchID  uuid.UUID
	AddressID uuid.UUID
	Lines     []LineInput
	Slots     []fulfillment.Slot
	Notes     string

	SourceReelID   *uuid.UUID
	SourceActionID *uuid.UUID
}

type LineInput struct {
	OfferingID uuid.UUID
	Quantity   int
	Options    map[string]any
}

// PlaceOrder runs the whole flow in one transaction.
//
// One transaction, not several: capacity must not be held for an order that
// failed to persist, and an order must not exist without the capacity it
// assumes. Splitting these — reserve, then insert, then quote — creates three
// windows where a crash leaves the system lying to a vendor about their
// availability.
func (s *Service) PlaceOrder(ctx context.Context, in PlaceOrderInput) (*Order, error) {
	if len(in.Lines) == 0 {
		return nil, &httpx.ValidationError{Fields: []httpx.FieldError{
			{Field: "lines", Code: "required", Message: "an order needs at least one line"},
		}}
	}

	var order *Order

	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		branch, err := s.repo.LoadBranch(ctx, tx, in.BranchID)
		if err != nil {
			return err
		}

		address, err := s.repo.LoadAddress(ctx, tx, in.UserID, in.AddressID)
		if err != nil {
			return err
		}

		offerings, err := s.repo.LoadOfferings(ctx, tx, in.BranchID, offeringIDs(in.Lines))
		if err != nil {
			return err
		}

		// Every line must belong to the branch being ordered from. Without this
		// check a crafted request could price a cheap offering from one vendor
		// against another vendor's capacity.
		serviceType := ""
		for _, line := range in.Lines {
			o, ok := offerings[line.OfferingID]
			if !ok {
				return &httpx.ValidationError{Fields: []httpx.FieldError{{
					Field: "lines", Code: "unknown_offering",
					Message: fmt.Sprintf("%s is not offered at this branch", line.OfferingID),
				}}}
			}
			if serviceType == "" {
				serviceType = o.ServiceType
			} else if serviceType != o.ServiceType {
				// Mixing verticals in one order would mean two fulfillment
				// patterns and two capacity pools on one row. It is a real
				// product question (basket across vendors), not a technical
				// limit — and answering it silently here would be wrong.
				return &httpx.ValidationError{Fields: []httpx.FieldError{{
					Field: "lines", Code: "mixed_service_types",
					Message: "one order cannot mix service types",
				}}}
			}
		}

		vertical, err := s.verticals.Get(serviceType)
		if err != nil {
			return fmt.Errorf("%w: service type %q", httpx.ErrNotFound, serviceType)
		}

		// 1. Vertical validates its own options.
		for _, line := range in.Lines {
			if err := vertical.Selection.Validate(ctx, line.OfferingID.String(), line.Options); err != nil {
				return &httpx.ValidationError{Fields: []httpx.FieldError{{
					Field: "lines.options", Code: "invalid", Message: err.Error(),
				}}}
			}
		}

		// 2. Vertical prices it, from snapshots rather than live catalogue rows.
		pricingCtx := servicetype.PricingContext{
			BranchID:    in.BranchID,
			ServiceType: serviceType,
			Currency:    branch.Currency,
			Options:     mergedOptions(in.Lines),
			Lines:       make([]servicetype.PricingLine, 0, len(in.Lines)),
		}
		for _, line := range in.Lines {
			o := offerings[line.OfferingID]
			pricingCtx.Lines = append(pricingCtx.Lines, servicetype.PricingLine{
				OfferingID:      o.ID,
				Name:            o.Name,
				NameAr:          o.NameAr,
				UnitPriceMinor:  o.BasePriceMinor,
				TaxRateBP:       o.TaxRateBP,
				Quantity:        line.Quantity,
				PriceIsEstimate: o.PriceIsEstimate,
				Options:         line.Options,
			})
		}

		// 3. Vertical decides how many physical events this becomes.
		orderCtx := fulfillment.OrderContext{
			BranchID:    in.BranchID,
			ServiceType: serviceType,
			Slots:       in.Slots,
			Options:     mergedOptions(in.Lines),
		}
		requests, err := vertical.Fulfillment.Plan(ctx, orderCtx)
		if err != nil {
			return &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "schedule", Code: "invalid", Message: err.Error(),
			}}}
		}
		pricingCtx.FulfillmentCount = len(requests)

		quote, err := vertical.Pricing.Quote(ctx, pricingCtx)
		if err != nil {
			return fmt.Errorf("quote: %w", err)
		}

		// 4. Vertical decides how much capacity each event consumes.
		units, err := vertical.Capacity.Units(ctx, orderCtx)
		if err != nil {
			return fmt.Errorf("capacity units: %w", err)
		}

		// 5. Persist. Order first so fulfillments and reservations can
		//    reference it.
		orderID := uuid.New()
		if err := s.repo.InsertOrder(ctx, tx, insertOrderParams{
			ID:             orderID,
			UserID:         in.UserID,
			BranchID:       branch.ID,
			VendorID:       branch.VendorID,
			ServiceType:    serviceType,
			Currency:       branch.Currency,
			Country:        branch.Country,
			Notes:          in.Notes,
			SourceReelID:   in.SourceReelID,
			SourceActionID: in.SourceActionID,
		}); err != nil {
			return err
		}

		for i, line := range in.Lines {
			o := offerings[line.OfferingID]
			if err := s.repo.InsertOrderLine(ctx, tx, orderID, i, line, o); err != nil {
				return err
			}
		}

		for _, req := range requests {
			bucketID, err := s.repo.FindBucket(ctx, tx, branch.ID,
				vertical.Capacity.ResourceKey(), req.ScheduledDate, req.WindowID)
			if err != nil {
				return err
			}

			reservationID, err := s.repo.Reserve(ctx, tx, bucketID, orderID, units, s.reservationTTL)
			if err != nil {
				return err
			}
			if reservationID == uuid.Nil {
				// The function returned NULL: the window filled between the
				// customer seeing availability and submitting. A real race, and
				// the client's job is to offer another slot — not to retry.
				return httpx.ErrCapacityTaken
			}

			if err := s.repo.InsertFulfillment(ctx, tx, orderID, branch.ID, req, reservationID, address); err != nil {
				return err
			}
		}

		quoteID, err := s.repo.InsertQuote(ctx, tx, orderID, quote)
		if err != nil {
			return err
		}

		// 6. Outbox, in the same transaction. An event published outside the
		//    transaction that produced it either fires for an order that rolled
		//    back or never fires for one that committed — both silent.
		if err := s.repo.InsertOutbox(ctx, tx, "order", orderID, "order.placed", map[string]any{
			"order_id":     orderID,
			"user_id":      in.UserID,
			"branch_id":    branch.ID,
			"vendor_id":    branch.VendorID,
			"service_type": serviceType,
			"total_minor":  quote.TotalMinor,
			"currency":     branch.Currency,
		}); err != nil {
			return err
		}

		order, err = s.repo.LoadOrder(ctx, tx, orderID, in.UserID)
		if err != nil {
			return err
		}
		order.QuoteID = quoteID
		return nil
	})

	if err != nil {
		return nil, err
	}
	return order, nil
}

// GetOrder returns one order scoped to its owner.
//
// Scoping in the query rather than checking after the read means a mistake
// here is a 404, not a data leak.
func (s *Service) GetOrder(ctx context.Context, userID, orderID uuid.UUID) (*Order, error) {
	var order *Order
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		order, err = s.repo.LoadOrder(ctx, tx, orderID, userID)
		return err
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, httpx.ErrNotFound
		}
		return nil, err
	}
	return order, nil
}

func offeringIDs(lines []LineInput) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(lines))
	for _, l := range lines {
		ids = append(ids, l.OfferingID)
	}
	return ids
}

// mergedOptions flattens line options for strategies that reason about the
// order as a whole — laundry's capacity calculation needs the total garment
// count, not per-line counts.
func mergedOptions(lines []LineInput) map[string]any {
	out := map[string]any{}
	for _, l := range lines {
		for k, v := range l.Options {
			if existing, ok := out[k].(float64); ok {
				if add, ok := v.(float64); ok {
					out[k] = existing + add
					continue
				}
			}
			out[k] = v
		}
	}
	return out
}
