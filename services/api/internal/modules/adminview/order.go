package adminview

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/httpx"
)

// The order detail page — question #1 in the plan.
//
//	"A customer is on the phone about order AE-G3CRYK."
//
// Twelve tables, one document. What a support agent needs in the first ten
// seconds of that call is: did they pay, is it coming, where is it, and has
// anything already been done about it. Everything below is in that order.

type Order struct {
	ID          uuid.UUID
	OrderNo     string
	Status      string
	Kind        string
	ServiceType string
	Currency    string
	PlacedAt    time.Time
	Notes       string

	Customer OrderParty
	Branch   OrderBranch

	Lines    []OrderLine
	Quote    OrderQuote
	Payments []OrderPayment
	Stops    []OrderStop

	// Attribution answers "did this come from a reel", which is the number the
	// whole video bet is judged on and the one thing about an order that nobody
	// on a support call asks and everyone in a planning meeting does.
	AttributionModel string
	AttributedReelID *uuid.UUID

	// Notifications is what the customer was actually told. The first question
	// after "where is my order" is usually "nobody told me", and this is the
	// only place that is answerable.
	Notifications []OrderNotification

	Invoice *OrderInvoice
}

type OrderParty struct {
	ID    uuid.UUID
	Name  string
	Phone string
}

type OrderBranch struct {
	ID       uuid.UUID
	Name     string
	VendorID uuid.UUID
	Vendor   string
}

type OrderLine struct {
	Name     string
	Quantity int
	Options  []byte
}

type OrderQuote struct {
	State     string
	Subtotal  Money
	Discount  Money
	Delivery  Money
	Surcharge Money
	Tax       Money
	Total     Money
	// TaxInclusive is carried because it changes what every other number on the
	// page means. PLAN.md §7 requires inclusive pricing; a quote that says
	// otherwise is a bug worth seeing rather than a detail to hide.
	TaxInclusive bool
	Lines        []OrderQuoteLine
}

type OrderQuoteLine struct {
	Kind   string
	Label  string
	Amount Money
	TaxBP  int
	Tax    Money
}

type OrderPayment struct {
	ID          uuid.UUID
	Kind        string
	Status      string
	Provider    string
	Amount      Money
	ThreeDS     string
	FailureCode string
	CreatedAt   time.Time
	SettledAt   *time.Time
}

// OrderStop is a fulfillment as the console shows it.
//
// Note what is here that the *driver's* view of the same row does not have: the
// order it belongs to, the item list, the vendor. The Fulfillment primitive
// keeps those out of dispatch on purpose (PHASE-7 §2); the console is the place
// that is allowed to join them back, because "what is in the bag" is exactly
// what the customer is asking about.
type OrderStop struct {
	ID            uuid.UUID
	Kind          string
	Status        string
	Sequence      int
	Date          time.Time
	Window        string
	Address       string
	Recipient     string
	Phone         string
	DriverID      *uuid.UUID
	DriverName    string
	AttemptCount  int
	CODMinor      int64
	OTPIssued     bool
	OTPSent       bool
	ProofKind     string
	CompletedAt   *time.Time
	FailedAt      *time.Time
	FailureReason string
}

type OrderNotification struct {
	At          time.Time
	TemplateKey string
	Channel     string
	Status      string
	Suppressed  string
	Destination string
}

type OrderInvoice struct {
	ID     uuid.UUID
	Number string
	Total  Money
	Status string
}

// LoadOrder composes the page.
//
// Six queries rather than one join. A single query across twelve tables with
// four one-to-many relationships returns the cartesian product of lines ×
// payments × stops × notifications, and reassembling that in Go is both slower
// and the kind of code nobody can change later. Six round trips against a local
// pool is a millisecond.
func (s *Service) LoadOrder(ctx context.Context, id uuid.UUID, lens Lens) (*Order, error) {
	o := &Order{ID: id}

	var (
		reelID    *uuid.UUID
		attrModel *string
		notes     *string
	)
	err := s.pool.QueryRow(ctx, `
		SELECT o.order_no, o.status::text, o.kind, o.service_type, o.currency,
		       o.placed_at, o.notes,
		       o.user_id, coalesce(u.display_name, ''), coalesce(u.phone, ''),
		       o.branch_id, coalesce(b.name, ''), b.vendor_id, coalesce(v.trade_name, ''),
		       a.model, a.reel_id
		  FROM orders o
		  LEFT JOIN users u ON u.id = o.user_id
		  LEFT JOIN vendor_branches b ON b.id = o.branch_id
		  LEFT JOIN vendors v ON v.id = b.vendor_id
		  LEFT JOIN order_attributions a ON a.order_id = o.id
		 WHERE o.id = $1`, id).
		Scan(&o.OrderNo, &o.Status, &o.Kind, &o.ServiceType, &o.Currency,
			&o.PlacedAt, &notes,
			&o.Customer.ID, &o.Customer.Name, &o.Customer.Phone,
			&o.Branch.ID, &o.Branch.Name, &o.Branch.VendorID, &o.Branch.Vendor,
			&attrModel, &reelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, wrap("load order", err)
	}

	if notes != nil {
		o.Notes = *notes
	}
	if attrModel != nil {
		o.AttributionModel = *attrModel
	}
	o.AttributedReelID = reelID

	// Redaction at the boundary, not at the serializer. A caller without
	// `commerce.contact` gets a document with these already empty, so a handler
	// that forgets to check leaks nothing.
	o.Customer.Name = lens.contact(o.Customer.Name)
	o.Customer.Phone = lens.contact(o.Customer.Phone)

	if err := s.orderLines(ctx, o); err != nil {
		return nil, err
	}
	if lens.Money {
		if err := s.orderQuote(ctx, o); err != nil {
			return nil, err
		}
		if err := s.orderPayments(ctx, o); err != nil {
			return nil, err
		}
		if err := s.orderInvoice(ctx, o); err != nil {
			return nil, err
		}
	}
	if err := s.orderStops(ctx, o, lens); err != nil {
		return nil, err
	}
	if err := s.orderNotifications(ctx, o, lens); err != nil {
		return nil, err
	}
	return o, nil
}

func (s *Service) orderLines(ctx context.Context, o *Order) error {
	rows, err := s.pool.Query(ctx, `
		SELECT coalesce(ol.name_snapshot, so.name, ''), ol.quantity, ol.options
		  FROM order_lines ol
		  LEFT JOIN service_offerings so ON so.id = ol.offering_id
		 WHERE ol.order_id = $1
		 ORDER BY ol.created_at`, o.ID)
	if err != nil {
		return wrap("order lines", err)
	}
	defer rows.Close()

	for rows.Next() {
		var l OrderLine
		if err := rows.Scan(&l.Name, &l.Quantity, &l.Options); err != nil {
			return err
		}
		o.Lines = append(o.Lines, l)
	}
	return rows.Err()
}

func (s *Service) orderQuote(ctx context.Context, o *Order) error {
	var quoteID uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT id, state::text, subtotal_minor, discount_minor, delivery_minor,
		       surcharge_minor, tax_minor, total_minor, tax_inclusive
		  FROM price_quotes
		 WHERE order_id = $1 AND superseded_by IS NULL`, o.ID).
		Scan(&quoteID, &o.Quote.State,
			&o.Quote.Subtotal.AmountMinor, &o.Quote.Discount.AmountMinor,
			&o.Quote.Delivery.AmountMinor, &o.Quote.Surcharge.AmountMinor,
			&o.Quote.Tax.AmountMinor, &o.Quote.Total.AmountMinor, &o.Quote.TaxInclusive)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return wrap("order quote", err)
	}

	for _, m := range []*Money{
		&o.Quote.Subtotal, &o.Quote.Discount, &o.Quote.Delivery,
		&o.Quote.Surcharge, &o.Quote.Tax, &o.Quote.Total,
	} {
		m.Currency = o.Currency
	}

	rows, err := s.pool.Query(ctx, `
		SELECT kind, label, amount_minor, coalesce(tax_rate_bp, 0), tax_minor
		  FROM quote_lines WHERE quote_id = $1 ORDER BY sort_order, label`, quoteID)
	if err != nil {
		return wrap("quote lines", err)
	}
	defer rows.Close()

	for rows.Next() {
		var l OrderQuoteLine
		if err := rows.Scan(&l.Kind, &l.Label, &l.Amount.AmountMinor, &l.TaxBP,
			&l.Tax.AmountMinor); err != nil {
			return err
		}
		l.Amount.Currency, l.Tax.Currency = o.Currency, o.Currency
		o.Quote.Lines = append(o.Quote.Lines, l)
	}
	return rows.Err()
}

func (s *Service) orderPayments(ctx context.Context, o *Order) error {
	rows, err := s.pool.Query(ctx, `
		SELECT id, kind::text, status::text, provider, amount_minor, currency,
		       coalesce(three_ds_status, ''), coalesce(failure_code, ''),
		       created_at, settled_at
		  FROM payments WHERE order_id = $1 ORDER BY created_at`, o.ID)
	if err != nil {
		return wrap("order payments", err)
	}
	defer rows.Close()

	for rows.Next() {
		var p OrderPayment
		if err := rows.Scan(&p.ID, &p.Kind, &p.Status, &p.Provider,
			&p.Amount.AmountMinor, &p.Amount.Currency, &p.ThreeDS, &p.FailureCode,
			&p.CreatedAt, &p.SettledAt); err != nil {
			return err
		}
		o.Payments = append(o.Payments, p)
	}
	return rows.Err()
}

func (s *Service) orderStops(ctx context.Context, o *Order, lens Lens) error {
	rows, err := s.pool.Query(ctx, `
		SELECT f.id, f.kind::text, f.status::text, f.sequence, f.scheduled_date,
		       coalesce(w.label, ''),
		       concat_ws(', ', nullif(f.address_line, ''), nullif(f.address_area, ''),
		                 nullif(f.address_city, '')),
		       coalesce(f.recipient_name, ''), coalesce(f.recipient_phone, ''),
		       f.driver_id, coalesce(d.display_name, ''),
		       f.attempt_count,
		       coalesce(c.expected_minor, 0),
		       f.otp_hash IS NOT NULL, f.otp_sent_at IS NOT NULL,
		       coalesce(p.kind, ''),
		       f.completed_at, f.failed_at, coalesce(f.failure_reason, '')
		  FROM fulfillments f
		  LEFT JOIN time_windows w ON w.id = f.window_id
		  LEFT JOIN drivers d ON d.id = f.driver_id
		  LEFT JOIN cod_collections c ON c.fulfillment_id = f.id
		  LEFT JOIN proof_of_delivery p ON p.fulfillment_id = f.id
		 WHERE f.order_id = $1
		 ORDER BY f.sequence`, o.ID)
	if err != nil {
		return wrap("order stops", err)
	}
	defer rows.Close()

	for rows.Next() {
		var st OrderStop
		if err := rows.Scan(&st.ID, &st.Kind, &st.Status, &st.Sequence, &st.Date,
			&st.Window, &st.Address, &st.Recipient, &st.Phone,
			&st.DriverID, &st.DriverName, &st.AttemptCount, &st.CODMinor,
			&st.OTPIssued, &st.OTPSent, &st.ProofKind,
			&st.CompletedAt, &st.FailedAt, &st.FailureReason); err != nil {
			return err
		}
		st.Address = lens.contact(st.Address)
		st.Recipient = lens.contact(st.Recipient)
		st.Phone = lens.contact(st.Phone)
		o.Stops = append(o.Stops, st)
	}
	return rows.Err()
}

func (s *Service) orderNotifications(ctx context.Context, o *Order, lens Lens) error {
	// Joined by the fulfillments this order owns rather than by user, because a
	// customer with four orders wants to know about *this* one — and the
	// delivery OTP is recorded against the fulfillment.
	rows, err := s.pool.Query(ctx, `
		SELECT n.created_at, n.template_key, n.channel, n.status,
		       coalesce(n.suppressed_reason, ''), n.destination
		  FROM notifications n
		 WHERE n.id IN (SELECT otp_notification_id FROM fulfillments
		                 WHERE order_id = $1 AND otp_notification_id IS NOT NULL)
		    -- $3 is cast explicitly. pgx sends a Go time.Time as an untyped
		    -- parameter, so subtracting an interval from it resolves as
		    -- interval-minus-interval rather than timestamptz-minus-interval —
		    -- the same class of parameter-inference failure that broke the
		    -- delivery OTP in the QA pass.
		    OR (n.user_id = $2 AND n.created_at >= $3::timestamptz - interval '1 hour')
		 ORDER BY n.created_at DESC
		 LIMIT 50`, o.ID, o.Customer.ID, o.PlacedAt)
	if err != nil {
		return wrap("order notifications", err)
	}
	defer rows.Close()

	for rows.Next() {
		var n OrderNotification
		if err := rows.Scan(&n.At, &n.TemplateKey, &n.Channel, &n.Status,
			&n.Suppressed, &n.Destination); err != nil {
			return err
		}
		n.Destination = lens.contact(n.Destination)
		o.Notifications = append(o.Notifications, n)
	}
	return rows.Err()
}

func (s *Service) orderInvoice(ctx context.Context, o *Order) error {
	var inv OrderInvoice
	err := s.pool.QueryRow(ctx, `
		SELECT id, number, gross_minor, currency, status
		  FROM invoices
		 WHERE order_id = $1 AND kind <> 'credit_note'
		 ORDER BY issued_at DESC LIMIT 1`, o.ID).
		Scan(&inv.ID, &inv.Number, &inv.Total.AmountMinor, &inv.Total.Currency, &inv.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return wrap("order invoice", err)
	}
	o.Invoice = &inv
	return nil
}

// OrderTimeline merges everything that happened to this order.
//
// Five sources, one ordering. `fulfillment_events` carries the delivery
// lifecycle, `payments` the money, `ledger_journals` the accounting,
// `notifications` what the customer was told, and `admin_actions` what staff
// did — and until this function existed, answering "what happened to order X"
// meant five queries and a mental merge.
func (s *Service) OrderTimeline(ctx context.Context, id uuid.UUID) ([]TimelineEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.occurred_at, 'fulfillment', e.to_status::text,
		       concat_ws(' · ', f.kind::text, nullif(e.metadata->>'reason', '')),
		       coalesce(d.display_name, '')
		  FROM fulfillment_events e
		  JOIN fulfillments f ON f.id = e.fulfillment_id
		  LEFT JOIN drivers d ON d.id = e.actor_id
		 WHERE f.order_id = $1

		UNION ALL
		SELECT p.created_at, 'payment', p.kind::text || ' ' || p.status::text,
		       p.provider || ' · ' || to_char(p.amount_minor / 100.0, 'FM999999990.00')
		         || ' ' || p.currency, ''
		  FROM payments p WHERE p.order_id = $1

		UNION ALL
		SELECT j.occurred_at, 'ledger', j.event_type,
		       coalesce(j.description, ''), ''
		  FROM ledger_journals j
		 WHERE j.reference_type = 'order' AND j.reference_id = $1

		UNION ALL
		SELECT n.created_at, 'notification',
		       n.template_key || ' · ' || n.status,
		       coalesce(n.suppressed_reason, n.channel), ''
		  FROM notifications n
		 WHERE n.id IN (SELECT otp_notification_id FROM fulfillments
		                 WHERE order_id = $1 AND otp_notification_id IS NOT NULL)

		UNION ALL
		SELECT a.created_at, 'staff', a.action, coalesce(a.reason, ''),
		       coalesce(u.display_name, u.phone, '')
		  FROM admin_actions a
		  LEFT JOIN users u ON u.id = a.actor_id
		 WHERE a.subject_type = 'order' AND a.subject_id = $1

		ORDER BY 1 DESC
		LIMIT 200`, id)
	if err != nil {
		return nil, wrap("order timeline", err)
	}
	defer rows.Close()

	var out []TimelineEntry
	for rows.Next() {
		var e TimelineEntry
		if err := rows.Scan(&e.At, &e.Source, &e.Kind, &e.Detail, &e.Actor); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func normalise(q string) string { return strings.TrimSpace(q) }

func wrap(what string, err error) error {
	return fmt.Errorf("adminview: %s: %w", what, err)
}
