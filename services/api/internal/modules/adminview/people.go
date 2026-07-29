package adminview

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/httpx"
)

// The customer and vendor detail pages — questions #2 and #5 in the plan.
//
//	"This vendor says they haven't been paid."
//	"Did this customer ever get the OTP?"

// ─────────────────────────────────────────────────────────────────────────────
// Customer
// ─────────────────────────────────────────────────────────────────────────────

type Customer struct {
	ID           uuid.UUID
	Phone        string
	Email        string
	DisplayName  string
	Locale       string
	NotifyLocale string
	Status       string
	Country      string
	CreatedAt    time.Time

	Addresses     []CustomerAddress
	Orders        []CustomerOrder
	Subscriptions []CustomerSubscription
	Notifications []OrderNotification

	// Standing is the author-side record. A customer who is also a seller has
	// strikes, and a support agent looking at "why can this person not post"
	// should not have to go to a different screen to find out.
	Standing CustomerStanding
	// Blocks are people this viewer has blocked. Small, and it explains an
	// otherwise baffling class of report — "their listing doesn't appear for me".
	BlockedCount int
	WalletMinor  int64
}

type CustomerAddress struct {
	ID        uuid.UUID
	Label     string
	Recipient string
	Line      string
	Area      string
	City      string
	IsDefault bool
}

type CustomerOrder struct {
	ID       uuid.UUID
	OrderNo  string
	Status   string
	Total    Money
	PlacedAt time.Time
	Branch   string
}

type CustomerSubscription struct {
	ID     uuid.UUID
	Status string
	Plan   string
	NextAt *time.Time
}

type CustomerStanding struct {
	ActiveStrikes int
	Points        int
	Enforcement   string
	OpenAppeals   int
}

func (s *Service) LoadCustomer(ctx context.Context, id uuid.UUID, lens Lens) (*Customer, error) {
	c := &Customer{ID: id}

	var email, name, notifyLocale *string
	err := s.pool.QueryRow(ctx, `
		SELECT u.phone, u.email::text, u.display_name, u.locale::text,
		       u.notification_locale::text, u.status::text, u.country, u.created_at,
		       (SELECT count(*) FROM user_blocks b WHERE b.user_id = u.id)
		  FROM users u WHERE u.id = $1 AND u.deleted_at IS NULL`, id).
		Scan(&c.Phone, &email, &name, &c.Locale, &notifyLocale, &c.Status,
			&c.Country, &c.CreatedAt, &c.BlockedCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, wrap("load customer", err)
	}

	if email != nil {
		c.Email = *email
	}
	if name != nil {
		c.DisplayName = *name
	}
	if notifyLocale != nil {
		c.NotifyLocale = *notifyLocale
	}

	// The phone *is* the identity here, so redacting it would leave a page
	// nobody can use. What the lens gates instead is everything that reveals
	// where the person lives — which is the disclosure that matters.
	c.Phone = lens.contact(c.Phone)
	c.Email = lens.contact(c.Email)

	if lens.Contact {
		if err := s.customerAddresses(ctx, c); err != nil {
			return nil, err
		}
	}
	if err := s.customerOrders(ctx, c, lens); err != nil {
		return nil, err
	}
	if err := s.customerSubscriptions(ctx, c); err != nil {
		return nil, err
	}
	if err := s.customerNotifications(ctx, c, lens); err != nil {
		return nil, err
	}
	return c, s.customerStanding(ctx, c)
}

func (s *Service) customerAddresses(ctx context.Context, c *Customer) error {
	rows, err := s.pool.Query(ctx, `
		SELECT id, label, coalesce(recipient_name, ''),
		       concat_ws(', ', nullif(flat, ''), nullif(building, ''), nullif(street, '')),
		       coalesce(area, ''), coalesce(city, ''), is_default
		  FROM addresses WHERE user_id = $1 AND deleted_at IS NULL
		 ORDER BY is_default DESC, created_at`, c.ID)
	if err != nil {
		return wrap("customer addresses", err)
	}
	defer rows.Close()

	for rows.Next() {
		var a CustomerAddress
		if err := rows.Scan(&a.ID, &a.Label, &a.Recipient, &a.Line, &a.Area,
			&a.City, &a.IsDefault); err != nil {
			return err
		}
		c.Addresses = append(c.Addresses, a)
	}
	return rows.Err()
}

func (s *Service) customerOrders(ctx context.Context, c *Customer, lens Lens) error {
	rows, err := s.pool.Query(ctx, `
		SELECT o.id, o.order_no, o.status::text,
		       coalesce(q.total_minor, 0), o.currency, o.placed_at,
		       coalesce(b.name, '')
		  FROM orders o
		  LEFT JOIN price_quotes q ON q.order_id = o.id AND q.superseded_by IS NULL
		  LEFT JOIN vendor_branches b ON b.id = o.branch_id
		 WHERE o.user_id = $1
		 ORDER BY o.placed_at DESC
		 LIMIT 50`, c.ID)
	if err != nil {
		return wrap("customer orders", err)
	}
	defer rows.Close()

	for rows.Next() {
		var o CustomerOrder
		if err := rows.Scan(&o.ID, &o.OrderNo, &o.Status, &o.Total.AmountMinor,
			&o.Total.Currency, &o.PlacedAt, &o.Branch); err != nil {
			return err
		}
		if !lens.Money {
			o.Total = Money{Currency: o.Total.Currency}
		}
		c.Orders = append(c.Orders, o)
	}
	return rows.Err()
}

func (s *Service) customerSubscriptions(ctx context.Context, c *Customer) error {
	rows, err := s.pool.Query(ctx, `
		SELECT s.id, s.status, coalesce(p.name, ''), s.materialised_through
		  FROM subscriptions s
		  LEFT JOIN subscription_plans p ON p.id = s.plan_id
		 WHERE s.user_id = $1
		 ORDER BY s.created_at DESC LIMIT 20`, c.ID)
	if err != nil {
		return wrap("customer subscriptions", err)
	}
	defer rows.Close()

	for rows.Next() {
		var sub CustomerSubscription
		if err := rows.Scan(&sub.ID, &sub.Status, &sub.Plan, &sub.NextAt); err != nil {
			return err
		}
		c.Subscriptions = append(c.Subscriptions, sub)
	}
	return rows.Err()
}

// customerNotifications answers question #5 directly.
//
// Suppressed rows are included, and that is the whole point. "Did they get the
// OTP" has three answers — sent, failed, and *we decided not to send it* — and
// only the third explains a customer who is certain they received nothing while
// the system is certain it did everything right.
func (s *Service) customerNotifications(ctx context.Context, c *Customer, lens Lens) error {
	rows, err := s.pool.Query(ctx, `
		SELECT created_at, template_key, channel, status,
		       coalesce(suppressed_reason, ''), destination
		  FROM notifications WHERE user_id = $1
		 ORDER BY created_at DESC LIMIT 100`, c.ID)
	if err != nil {
		return wrap("customer notifications", err)
	}
	defer rows.Close()

	for rows.Next() {
		var n OrderNotification
		if err := rows.Scan(&n.At, &n.TemplateKey, &n.Channel, &n.Status,
			&n.Suppressed, &n.Destination); err != nil {
			return err
		}
		n.Destination = lens.contact(n.Destination)
		c.Notifications = append(c.Notifications, n)
	}
	return rows.Err()
}

func (s *Service) customerStanding(ctx context.Context, c *Customer) error {
	return s.pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM author_strikes
		    WHERE author_type = 'user' AND author_id = $1
		      AND voided_at IS NULL AND expires_at > now()),
		  (SELECT count(*) FROM content_appeals
		    WHERE appellant_id = $1 AND status IN ('open', 'in_review'))`, c.ID).
		Scan(&c.Standing.ActiveStrikes, &c.Standing.OpenAppeals)
}

// ─────────────────────────────────────────────────────────────────────────────
// Vendor
// ─────────────────────────────────────────────────────────────────────────────

type Vendor struct {
	ID        uuid.UUID
	Slug      string
	LegalName string
	TradeName string
	Status    string
	Country   string
	Currency  string
	Rating    *float64
	CreatedAt time.Time

	TradeLicence    string
	LicenceExpires  *time.Time
	TaxRegistration string

	Branches []VendorBranch
	Staff    []VendorStaff

	// Compliance is the answer to "why can't this vendor be paid", which is the
	// question that brought whoever is looking here.
	Compliance VendorCompliance
	Statements []VendorStatement
	// Payable is the vendor's ledger position: what the platform owes them right
	// now, derived from `ledger_entries` rather than from a cached column,
	// because the ledger is the truth and a cached number drifts.
	Payable  Money
	Disputes int
	Reels    VendorReels
}

type VendorBranch struct {
	ID       uuid.UUID
	Name     string
	City     string
	IsActive bool
	Drivers  int
}

type VendorStaff struct {
	UserID uuid.UUID
	Name   string
	Phone  string
	Role   string
}

type VendorCompliance struct {
	Eligible bool
	Blocking []string
	KYCState string
	Account  string
}

type VendorStatement struct {
	ID        uuid.UUID
	Reference string
	Start     time.Time
	End       time.Time
	Net       Money
	State     string
	PaidAt    *time.Time
}

type VendorReels struct {
	Published int
	Pending   int
	// LastPublished is the supply-health number. §4 of the video-first plan
	// calls content supply the existential risk; a vendor who has published
	// nothing in three weeks is that risk arriving one vendor at a time.
	LastPublished *time.Time
}

func (s *Service) LoadVendor(ctx context.Context, id uuid.UUID, lens Lens) (*Vendor, error) {
	v := &Vendor{ID: id}

	var licence, taxReg *string
	err := s.pool.QueryRow(ctx, `
		SELECT slug, legal_name, trade_name, status, country, default_currency,
		       rating, created_at, trade_licence_no, trade_licence_expires,
		       tax_registration_no
		  FROM vendors WHERE id = $1 AND deleted_at IS NULL`, id).
		Scan(&v.Slug, &v.LegalName, &v.TradeName, &v.Status, &v.Country,
			&v.Currency, &v.Rating, &v.CreatedAt, &licence, &v.LicenceExpires, &taxReg)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, wrap("load vendor", err)
	}
	if licence != nil {
		v.TradeLicence = *licence
	}
	if taxReg != nil {
		v.TaxRegistration = *taxReg
	}

	if err := s.vendorBranches(ctx, v); err != nil {
		return nil, err
	}
	if err := s.vendorStaff(ctx, v, lens); err != nil {
		return nil, err
	}
	if err := s.vendorReels(ctx, v); err != nil {
		return nil, err
	}
	if lens.Money {
		if err := s.vendorMoney(ctx, v); err != nil {
			return nil, err
		}
	}
	return v, s.vendorCompliance(ctx, v, lens)
}

func (s *Service) vendorBranches(ctx context.Context, v *Vendor) error {
	rows, err := s.pool.Query(ctx, `
		SELECT b.id, b.name, coalesce(b.city, ''), b.is_active,
		       (SELECT count(*) FROM drivers d WHERE d.vendor_id = v.id AND d.status = 'active')
		  FROM vendor_branches b JOIN vendors v ON v.id = b.vendor_id
		 WHERE b.vendor_id = $1 AND b.deleted_at IS NULL
		 ORDER BY b.name`, v.ID)
	if err != nil {
		return wrap("vendor branches", err)
	}
	defer rows.Close()

	for rows.Next() {
		var b VendorBranch
		if err := rows.Scan(&b.ID, &b.Name, &b.City, &b.IsActive, &b.Drivers); err != nil {
			return err
		}
		v.Branches = append(v.Branches, b)
	}
	return rows.Err()
}

func (s *Service) vendorStaff(ctx context.Context, v *Vendor, lens Lens) error {
	rows, err := s.pool.Query(ctx, `
		SELECT vu.user_id, coalesce(u.display_name, ''), u.phone, vu.role
		  FROM vendor_users vu JOIN users u ON u.id = vu.user_id
		 WHERE vu.vendor_id = $1 ORDER BY vu.role`, v.ID)
	if err != nil {
		return wrap("vendor staff", err)
	}
	defer rows.Close()

	for rows.Next() {
		var st VendorStaff
		if err := rows.Scan(&st.UserID, &st.Name, &st.Phone, &st.Role); err != nil {
			return err
		}
		st.Phone = lens.contact(st.Phone)
		v.Staff = append(v.Staff, st)
	}
	return rows.Err()
}

func (s *Service) vendorReels(ctx context.Context, v *Vendor) error {
	return s.pool.QueryRow(ctx, `
		SELECT
		  count(*) FILTER (WHERE r.state = 'published'),
		  count(*) FILTER (WHERE r.state = 'review_pending'),
		  max(r.published_at)
		  FROM reels r
		 WHERE r.author_type = 'vendor_branch'
		   AND r.author_id IN (SELECT id FROM vendor_branches WHERE vendor_id = $1)`,
		v.ID).Scan(&v.Reels.Published, &v.Reels.Pending, &v.Reels.LastPublished)
}

// vendorMoney reads the payable straight out of the ledger.
//
// Not from a cached balance column, and PLAN.md §2 explains why in one line: a
// `vendors.balance` column will drift and then silently be wrong. The console is
// where someone goes to settle an argument about money, so it reads the thing
// that is authoritative even though that costs a sum over entries.
func (s *Service) vendorMoney(ctx context.Context, v *Vendor) error {
	if err := s.pool.QueryRow(ctx, `
		SELECT coalesce(sum(CASE WHEN e.direction = 'credit' THEN e.amount_minor
		                         ELSE -e.amount_minor END), 0)
		  FROM ledger_entries e JOIN ledger_accounts a ON a.id = e.account_id
		 WHERE a.type = 'vendor_payable' AND a.owner_id = $1`, v.ID).
		Scan(&v.Payable.AmountMinor); err != nil {
		return wrap("vendor payable", err)
	}
	v.Payable.Currency = v.Currency

	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM disputes
		 WHERE state IN ('open', 'investigating', 'awaiting_response')
		   AND subject_type = 'payout_statement'
		   AND subject_id IN (SELECT id FROM payout_statements WHERE vendor_id = $1)`,
		v.ID).Scan(&v.Disputes); err != nil {
		return wrap("vendor disputes", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, reference_no, period_start, period_end, net_minor, currency, state, paid_at
		  FROM payout_statements WHERE vendor_id = $1
		 ORDER BY period_start DESC LIMIT 24`, v.ID)
	if err != nil {
		return wrap("vendor statements", err)
	}
	defer rows.Close()

	for rows.Next() {
		var st VendorStatement
		if err := rows.Scan(&st.ID, &st.Reference, &st.Start, &st.End,
			&st.Net.AmountMinor, &st.Net.Currency, &st.State, &st.PaidAt); err != nil {
			return err
		}
		v.Statements = append(v.Statements, st)
	}
	return rows.Err()
}

// vendorCompliance summarises why a payout is or is not possible.
//
// The *reasons* come from `kyc.Eligible`, which is the module that owns the
// rule. This function reads the raw state so the page can show it; it
// deliberately does not re-derive eligibility, because two implementations of
// "may this vendor be paid" is one more than is safe.
func (s *Service) vendorCompliance(ctx context.Context, v *Vendor, lens Lens) error {
	if !lens.Identity {
		v.Compliance.KYCState = "hidden"
		v.Compliance.Account = "hidden"
		return nil
	}

	var kycState, acctState *string
	if err := s.pool.QueryRow(ctx, `
		SELECT (SELECT status FROM kyc_cases
		         WHERE subject_type = 'vendor' AND subject_id = $1
		         ORDER BY created_at DESC LIMIT 1),
		       (SELECT status FROM payout_accounts
		         WHERE owner_type = 'vendor' AND owner_id = $1
		           AND status IN ('pending', 'verified') LIMIT 1)`, v.ID).
		Scan(&kycState, &acctState); err != nil {
		return wrap("vendor compliance", err)
	}
	if kycState != nil {
		v.Compliance.KYCState = *kycState
	} else {
		v.Compliance.KYCState = "none"
	}
	if acctState != nil {
		v.Compliance.Account = *acctState
	} else {
		v.Compliance.Account = "none"
	}
	return nil
}

// VendorTimeline merges the vendor's history.
func (s *Service) VendorTimeline(ctx context.Context, id uuid.UUID) ([]TimelineEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT j.occurred_at, 'ledger', j.event_type, coalesce(j.description, ''), ''
		  FROM ledger_journals j
		  JOIN ledger_entries e ON e.journal_id = j.id
		  JOIN ledger_accounts a ON a.id = e.account_id
		 WHERE a.type = 'vendor_payable' AND a.owner_id = $1

		UNION ALL
		SELECT p.issued_at, 'payout', p.state, p.reference_no, ''
		  FROM payout_statements p WHERE p.vendor_id = $1 AND p.issued_at IS NOT NULL

		UNION ALL
		SELECT c.reviewed_at, 'kyc', c.status, coalesce(c.rejection_reason, ''),
		       coalesce(u.display_name, u.phone, '')
		  FROM kyc_cases c LEFT JOIN users u ON u.id = c.reviewed_by
		 WHERE c.subject_type = 'vendor' AND c.subject_id = $1 AND c.reviewed_at IS NOT NULL

		UNION ALL
		SELECT a.created_at, 'staff', a.action, coalesce(a.reason, ''),
		       coalesce(u.display_name, u.phone, '')
		  FROM admin_actions a LEFT JOIN users u ON u.id = a.actor_id
		 WHERE a.subject_type = 'vendor' AND a.subject_id = $1

		ORDER BY 1 DESC LIMIT 200`, id)
	if err != nil {
		return nil, wrap("vendor timeline", err)
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

// CustomerTimeline merges what happened to a person.
func (s *Service) CustomerTimeline(ctx context.Context, id uuid.UUID) ([]TimelineEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT o.placed_at, 'order', o.status::text, o.order_no, ''
		  FROM orders o WHERE o.user_id = $1

		UNION ALL
		SELECT n.created_at, 'notification', n.template_key || ' · ' || n.status,
		       coalesce(n.suppressed_reason, n.channel), ''
		  FROM notifications n WHERE n.user_id = $1

		UNION ALL
		SELECT st.issued_at, 'strike', st.severity, st.reason,
		       coalesce(u.display_name, u.phone, '')
		  FROM author_strikes st LEFT JOIN users u ON u.id = st.issued_by
		 WHERE st.author_type = 'user' AND st.author_id = $1

		UNION ALL
		SELECT a.created_at, 'staff', a.action, coalesce(a.reason, ''),
		       coalesce(u.display_name, u.phone, '')
		  FROM admin_actions a LEFT JOIN users u ON u.id = a.actor_id
		 WHERE a.subject_type = 'user' AND a.subject_id = $1

		ORDER BY 1 DESC LIMIT 200`, id)
	if err != nil {
		return nil, wrap("customer timeline", err)
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

// UserLocale is the language this customer reads the app in.
//
// Used by view-as so an agent sees the screen the customer sees. Debugging "the
// text is wrong" in English while the customer reads Arabic is debugging a
// different screen — and in an RTL layout, a different one in more ways than
// the words.
func (s *Service) UserLocale(ctx context.Context, userID uuid.UUID) (string, error) {
	var locale string
	err := s.pool.QueryRow(ctx,
		`SELECT locale::text FROM users WHERE id = $1`, userID).Scan(&locale)
	if err != nil {
		return "", notFoundOr("user locale", err)
	}
	return locale, nil
}
