package adminview

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The money read models — PLAN-ADMIN-CONSOLE §4.3.
//
// Every one of these answers a question that is currently answered with `psql`,
// and the first payout run is the first time anyone has to explain a number to
// someone who did not compute it.
//
// The same three rules as the rest of this package apply, and the third does
// most of the work here: nothing below computes a balance. Balances come from
// `ledger_entries` by summation at request time, never from a stored column.
// A cached balance that drifts from the journal is the one number in this
// system nobody can afford to have wrong, and the way it drifts is somebody
// optimising exactly these queries.

// ── trial balance ───────────────────────────────────────────────────────────

// AccountBalance is one account's position.
type AccountBalance struct {
	AccountID uuid.UUID
	Type      string
	Name      string
	Currency  string
	OwnerType string
	OwnerID   *uuid.UUID
	// Debits and credits are carried separately rather than netted, because the
	// net alone cannot distinguish a quiet account from a busy one that happens
	// to balance — and "this account had no activity" is a different alarm to
	// "this account moved 400k in both directions".
	DebitMinor  int64
	CreditMinor int64
	NetMinor    int64
	Entries     int
}

// TrialBalance is every account, plus whether the books balance.
type TrialBalance struct {
	AsOf     time.Time
	Currency string
	Accounts []AccountBalance
	// TotalDebitMinor and TotalCreditMinor must be equal. The double-entry
	// constraint is enforced per journal at write time, so an imbalance here
	// means something wrote entries outside the journal path — which is worth
	// finding out about loudly rather than eventually.
	TotalDebitMinor  int64
	TotalCreditMinor int64
	Balanced         bool
}

// LoadTrialBalance sums every account.
//
// The date filters are inclusive of `from` and exclusive of `to`, which is the
// only convention that makes consecutive periods tile without double-counting
// the boundary day. Stated because getting it wrong is invisible until a
// month-end total is one day of revenue too high.
func (s *Service) LoadTrialBalance(ctx context.Context, currency string, from, to *time.Time) (*TrialBalance, error) {
	tb := &TrialBalance{AsOf: time.Now(), Currency: currency}

	// Grouped by *identity*, not by row.
	//
	// An account is (type, owner, currency) — the row is an implementation
	// detail, and until migration 22 the implementation created several rows per
	// identity for platform-level accounts. Those rows are real and hold real
	// entries; the ledger is append-only so they cannot be merged away, and
	// merging them would rewrite history to make a report tidier.
	//
	// Aggregating here is the honest fix: the read model presents the account
	// the accounting always meant, and the rows behind it stay untouched. It is
	// also correct independent of the bug — this is what an account is.
	rows, err := s.pool.Query(ctx, `
		SELECT min(a.id::text)::uuid, a.type::text, min(a.name), a.currency,
		       coalesce(a.owner_type, ''), a.owner_id,
		       coalesce(sum(e.amount_minor) FILTER (WHERE e.direction = 'debit'), 0),
		       coalesce(sum(e.amount_minor) FILTER (WHERE e.direction = 'credit'), 0),
		       count(e.id)
		  FROM ledger_accounts a
		  LEFT JOIN ledger_entries e ON e.account_id = a.id
		       AND ($2::timestamptz IS NULL OR e.created_at >= $2)
		       AND ($3::timestamptz IS NULL OR e.created_at < $3)
		 WHERE a.currency = $1
		 GROUP BY a.type, a.currency, a.owner_type, a.owner_id
		 -- Identities with no movement still appear. An account that should have
		 -- activity and has none is a finding, and dropping empty rows hides it.
		 ORDER BY a.type, min(a.name)`,
		currency, from, to)
	if err != nil {
		return nil, wrap("trial balance", err)
	}
	defer rows.Close()

	for rows.Next() {
		var a AccountBalance
		if err := rows.Scan(&a.AccountID, &a.Type, &a.Name, &a.Currency,
			&a.OwnerType, &a.OwnerID, &a.DebitMinor, &a.CreditMinor, &a.Entries); err != nil {
			return nil, err
		}
		a.NetMinor = a.CreditMinor - a.DebitMinor
		tb.TotalDebitMinor += a.DebitMinor
		tb.TotalCreditMinor += a.CreditMinor
		tb.Accounts = append(tb.Accounts, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	tb.Balanced = tb.TotalDebitMinor == tb.TotalCreditMinor
	return tb, nil
}

// ── ledger explorer ─────────────────────────────────────────────────────────

// Journal is one posting with its lines.
type Journal struct {
	ID            uuid.UUID
	EventType     string
	ReferenceType string
	ReferenceID   *uuid.UUID
	Description   string
	Currency      string
	OccurredAt    time.Time
	ReversesID    *uuid.UUID
	Lines         []JournalLine
}

// JournalLine is one entry within a journal.
type JournalLine struct {
	AccountID   uuid.UUID
	AccountType string
	AccountName string
	Direction   string
	AmountMinor int64
}

// LedgerFilter narrows the explorer.
type LedgerFilter struct {
	EventType string
	AccountID *uuid.UUID
	Reference *uuid.UUID
	From      *time.Time
	To        *time.Time
	Limit     int
}

// LoadJournals answers "where did this money go".
//
// Two queries rather than a join, deliberately. Joining lines onto journals
// returns the journal row once per line, and paginating that is wrong in a way
// that is easy to miss: a LIMIT of 50 on the joined shape returns somewhere
// between 12 and 50 journals depending on how many lines each has, so "the last
// 50 postings" silently becomes "some postings". Selecting the journals first
// makes the page size mean what it says.
func (s *Service) LoadJournals(ctx context.Context, f LedgerFilter) ([]Journal, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}

	rows, err := s.pool.Query(ctx, `
		SELECT j.id, j.event_type, coalesce(j.reference_type, ''), j.reference_id,
		       coalesce(j.description, ''), j.currency, j.occurred_at, j.reverses_id
		  FROM ledger_journals j
		 WHERE ($1::text IS NULL OR j.event_type = $1)
		   AND ($2::uuid IS NULL OR j.reference_id = $2)
		   AND ($3::timestamptz IS NULL OR j.occurred_at >= $3)
		   AND ($4::timestamptz IS NULL OR j.occurred_at < $4)
		   AND ($5::uuid IS NULL OR EXISTS (
		         SELECT 1 FROM ledger_entries e
		          WHERE e.journal_id = j.id AND e.account_id = $5))
		 ORDER BY j.occurred_at DESC, j.id DESC
		 LIMIT $6`,
		nilIfEmpty(f.EventType), f.Reference, f.From, f.To, f.AccountID, f.Limit)
	if err != nil {
		return nil, wrap("journals", err)
	}

	var out []Journal
	byID := map[uuid.UUID]int{}
	for rows.Next() {
		var j Journal
		if err := rows.Scan(&j.ID, &j.EventType, &j.ReferenceType, &j.ReferenceID,
			&j.Description, &j.Currency, &j.OccurredAt, &j.ReversesID); err != nil {
			rows.Close()
			return nil, err
		}
		byID[j.ID] = len(out)
		out = append(out, j)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}

	ids := make([]uuid.UUID, 0, len(out))
	for _, j := range out {
		ids = append(ids, j.ID)
	}

	lines, err := s.pool.Query(ctx, `
		SELECT e.journal_id, e.account_id, a.type::text, a.name,
		       e.direction::text, e.amount_minor
		  FROM ledger_entries e
		  JOIN ledger_accounts a ON a.id = e.account_id
		 WHERE e.journal_id = ANY($1)
		 ORDER BY e.journal_id, e.id`, ids)
	if err != nil {
		return nil, wrap("journal lines", err)
	}
	defer lines.Close()

	for lines.Next() {
		var journalID uuid.UUID
		var l JournalLine
		if err := lines.Scan(&journalID, &l.AccountID, &l.AccountType,
			&l.AccountName, &l.Direction, &l.AmountMinor); err != nil {
			return nil, err
		}
		if i, ok := byID[journalID]; ok {
			out[i].Lines = append(out[i].Lines, l)
		}
	}
	return out, lines.Err()
}

// EventTypes lists what has actually been posted.
//
// Read from the data rather than from a constant, because the constant would
// drift: every module that posts a journal names its own event type, and a
// filter dropdown built from a hard-coded list silently omits whichever one was
// added last.
func (s *Service) EventTypes(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT event_type FROM ledger_journals ORDER BY event_type`)
	if err != nil {
		return nil, wrap("event types", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ── refunds ─────────────────────────────────────────────────────────────────

// Refund is one refund with who authorised it.
type Refund struct {
	ID          uuid.UUID
	OrderID     uuid.UUID
	OrderNo     string
	Customer    string
	Vendor      string
	AmountMinor int64
	Currency    string
	Provider    string
	At          time.Time
	// Reason and Actor come from `admin_actions`, not from the payment. A
	// refund with no matching admin action was issued by something other than a
	// person — a webhook, a script, a migration — and showing that as an empty
	// actor rather than hiding the row is the point of the list.
	Reason string
	Actor  string
}

// LoadRefunds is the list nobody wants to look at and everybody eventually does.
func (s *Service) LoadRefunds(ctx context.Context, from, to *time.Time, limit int, lens Lens) ([]Refund, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.order_id, o.order_no,
		       coalesce(u.display_name, u.phone, ''), coalesce(v.trade_name, ''),
		       p.amount_minor, p.currency, p.provider, p.created_at,
		       coalesce(aa.reason, ''), coalesce(su.display_name, su.phone, '')
		  FROM payments p
		  JOIN orders o ON o.id = p.order_id
		  LEFT JOIN users u ON u.id = p.user_id
		  LEFT JOIN vendor_branches b ON b.id = o.branch_id
		  LEFT JOIN vendors v ON v.id = b.vendor_id
		  -- LATERAL rather than a join: an order can be refunded more than once,
		  -- and joining admin_actions on the order would pair every refund with
		  -- every reason, inventing refunds that never happened.
		  LEFT JOIN LATERAL (
		      SELECT a.reason, a.actor_id
		        FROM admin_actions a
		       WHERE a.subject_id = p.order_id
		         AND a.action = 'order.refund'
		         AND a.created_at BETWEEN p.created_at - interval '1 minute'
		                             AND p.created_at + interval '1 minute'
		       ORDER BY abs(extract(epoch FROM a.created_at - p.created_at))
		       LIMIT 1
		  ) aa ON true
		  LEFT JOIN users su ON su.id = aa.actor_id
		 WHERE p.kind = 'refund' AND p.status = 'succeeded'
		   AND ($1::timestamptz IS NULL OR p.created_at >= $1)
		   AND ($2::timestamptz IS NULL OR p.created_at < $2)
		 ORDER BY p.created_at DESC
		 LIMIT $3`, from, to, limit)
	if err != nil {
		return nil, wrap("refunds", err)
	}
	defer rows.Close()

	var out []Refund
	for rows.Next() {
		var r Refund
		if err := rows.Scan(&r.ID, &r.OrderID, &r.OrderNo, &r.Customer, &r.Vendor,
			&r.AmountMinor, &r.Currency, &r.Provider, &r.At, &r.Reason, &r.Actor); err != nil {
			return nil, err
		}
		r.Customer = lens.contact(r.Customer)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ── payout runs ─────────────────────────────────────────────────────────────

// PayoutRun is one settlement cycle.
type PayoutRun struct {
	ID             uuid.UUID
	PeriodStart    time.Time
	PeriodEnd      time.Time
	Currency       string
	State          string
	StatementCount int
	TotalMinor     int64
	ApprovedBy     string
	ApprovedAt     *time.Time
	PaidAt         *time.Time
	CreatedAt      time.Time
}

// PayoutStatement is one vendor's share of a run.
type PayoutStatement struct {
	ID          uuid.UUID
	RunID       *uuid.UUID
	VendorID    uuid.UUID
	Vendor      string
	ReferenceNo string
	PeriodStart time.Time
	PeriodEnd   time.Time
	Currency    string
	GrossMinor  int64
	Commission  int64
	VATMinor    int64
	Adjustments int64
	NetMinor    int64
	State       string
	IssuedAt    *time.Time
	PaidAt      *time.Time
	PaymentRef  string
	// BlockedBecause is why this statement cannot be paid, in words. Empty means
	// it can. This is the column the console exists for: "why has this vendor
	// not been paid" is otherwise a join across kyc_cases, payout_accounts and
	// vendor_documents that nobody does at 5pm on a Thursday.
	BlockedBecause string
}

func (s *Service) LoadPayoutRuns(ctx context.Context, limit int) ([]PayoutRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.period_start, r.period_end, r.currency, r.state,
		       r.statement_count, r.total_minor,
		       coalesce(u.display_name, u.phone, ''), r.approved_at, r.paid_at, r.created_at
		  FROM payout_runs r
		  LEFT JOIN users u ON u.id = r.approved_by
		 ORDER BY r.period_end DESC, r.created_at DESC
		 LIMIT $1`, limit)
	if err != nil {
		return nil, wrap("payout runs", err)
	}
	defer rows.Close()

	var out []PayoutRun
	for rows.Next() {
		var r PayoutRun
		if err := rows.Scan(&r.ID, &r.PeriodStart, &r.PeriodEnd, &r.Currency, &r.State,
			&r.StatementCount, &r.TotalMinor, &r.ApprovedBy, &r.ApprovedAt,
			&r.PaidAt, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// LoadStatements lists statements, optionally for one run, with the block reason.
//
// The block reason is computed in SQL rather than in Go so that the *list* can
// be filtered on it. "Show me everyone who is blocked" is the question this
// page is opened with, and computing the answer after the rows arrive means
// paginating over vendors who are fine.
func (s *Service) LoadStatements(ctx context.Context, runID *uuid.UUID, vendorID *uuid.UUID, limit int) ([]PayoutStatement, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT st.id, st.run_id, st.vendor_id, coalesce(v.trade_name, ''),
		       st.reference_no, st.period_start, st.period_end, st.currency,
		       st.gross_minor, st.commission_minor, st.vat_minor,
		       st.adjustments_minor, st.net_minor, st.state,
		       st.issued_at, st.paid_at, coalesce(st.payment_ref, ''),
		       CASE
		         WHEN v.status <> 'active' THEN 'vendor is ' || v.status
		         WHEN pa.id IS NULL THEN 'no payout account on file'
		         WHEN pa.status <> 'approved' THEN 'payout account is ' || pa.status
		         WHEN kc.status IS DISTINCT FROM 'approved' THEN
		              'identity check is ' || coalesce(kc.status, 'not started')
		         WHEN v.trade_licence_expires IS NOT NULL
		              AND v.trade_licence_expires < current_date THEN
		              'trade licence expired ' || v.trade_licence_expires::text
		         ELSE ''
		       END
		  FROM payout_statements st
		  JOIN vendors v ON v.id = st.vendor_id
		  LEFT JOIN payout_accounts pa ON pa.owner_type = 'vendor' AND pa.owner_id = st.vendor_id
		  LEFT JOIN kyc_cases kc ON kc.subject_type = 'vendor' AND kc.subject_id = st.vendor_id
		 WHERE ($1::uuid IS NULL OR st.run_id = $1)
		   AND ($2::uuid IS NULL OR st.vendor_id = $2)
		 ORDER BY st.period_end DESC, st.net_minor DESC
		 LIMIT $3`, runID, vendorID, limit)
	if err != nil {
		return nil, wrap("payout statements", err)
	}
	defer rows.Close()

	var out []PayoutStatement
	for rows.Next() {
		var st PayoutStatement
		if err := rows.Scan(&st.ID, &st.RunID, &st.VendorID, &st.Vendor,
			&st.ReferenceNo, &st.PeriodStart, &st.PeriodEnd, &st.Currency,
			&st.GrossMinor, &st.Commission, &st.VATMinor, &st.Adjustments,
			&st.NetMinor, &st.State, &st.IssuedAt, &st.PaidAt, &st.PaymentRef,
			&st.BlockedBecause); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// ── COD reconciliation ──────────────────────────────────────────────────────

// DriverCash is what one driver is holding and what they owe.
type DriverCash struct {
	DriverID uuid.UUID
	Driver   string
	Phone    string
	Currency string
	// HeldMinor is cash collected and not yet remitted — the platform's money in
	// somebody else's pocket, which is the number this page exists for.
	HeldMinor      int64
	HeldStops      int
	RemittedMinor  int64
	SettledMinor   int64
	ShortfallMinor int64
	OldestHeld     *time.Time
}

// LoadDriverCash is the cash position per driver.
//
// Shortfall is `collected − expected` summed over settled collections, and it is
// carried even when zero. A reconciliation page that only lists discrepancies
// cannot distinguish "everyone settled correctly" from "the query is broken",
// and those look identical on a quiet day.
func (s *Service) LoadDriverCash(ctx context.Context, lens Lens) ([]DriverCash, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d.id, d.display_name, coalesce(u.phone, ''),
		       coalesce(max(c.currency), 'AED'),
		       coalesce(sum(c.collected_minor) FILTER (WHERE c.state = 'held'), 0),
		       count(c.id) FILTER (WHERE c.state = 'held'),
		       coalesce(sum(c.collected_minor) FILTER (WHERE c.state = 'remitted'), 0),
		       coalesce(sum(c.collected_minor) FILTER (WHERE c.state = 'settled'), 0),
		       coalesce(sum(c.collected_minor - c.expected_minor)
		                FILTER (WHERE c.state IN ('remitted', 'settled')), 0),
		       min(c.collected_at) FILTER (WHERE c.state = 'held')
		  FROM drivers d
		  LEFT JOIN users u ON u.id = d.user_id
		  LEFT JOIN cod_collections c ON c.driver_id = d.id
		 GROUP BY d.id, d.display_name, u.phone
		 -- Ordered by what is outstanding, because the question is always "who is
		 -- carrying the most of our cash", never "who is alphabetically first".
		 ORDER BY 5 DESC, d.display_name`)
	if err != nil {
		return nil, wrap("driver cash", err)
	}
	defer rows.Close()

	var out []DriverCash
	for rows.Next() {
		var c DriverCash
		if err := rows.Scan(&c.DriverID, &c.Driver, &c.Phone, &c.Currency,
			&c.HeldMinor, &c.HeldStops, &c.RemittedMinor, &c.SettledMinor,
			&c.ShortfallMinor, &c.OldestHeld); err != nil {
			return nil, err
		}
		c.Phone = lens.contact(c.Phone)
		out = append(out, c)
	}
	return out, rows.Err()
}

// Remittance is one declared hand-in of cash.
type Remittance struct {
	ID          uuid.UUID
	DriverID    uuid.UUID
	Driver      string
	ReferenceNo string
	// DeclaredMinor is what the driver said they were handing in; CountedMinor
	// is what was counted on receipt. Both are kept because they are different
	// claims by different people, and collapsing them into one number is how a
	// dispute becomes unarguable in the wrong direction.
	DeclaredMinor  int64
	CountedMinor   *int64
	ExpectedMinor  int64
	VarianceMinor  int64
	Currency       string
	State          string
	Method         string
	DeclaredAt     time.Time
	SettledAt      *time.Time
	CollectionsQty int
}

func (s *Service) LoadRemittances(ctx context.Context, driverID *uuid.UUID, limit int) ([]Remittance, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	// `expected` is summed from the collections attached to the remittance, and
	// `variance` is the column the settlement process wrote. They are shown
	// side by side rather than one being derived from the other: if they
	// disagree, that disagreement *is* the finding, and computing one from the
	// other would guarantee they never can.
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.driver_id, coalesce(d.display_name, ''), r.reference_no,
		       r.declared_minor, r.counted_minor,
		       coalesce(sum(c.collected_minor), 0), r.variance_minor,
		       r.currency, r.state, r.method, r.declared_at, r.settled_at, count(c.id)
		  FROM cod_remittances r
		  LEFT JOIN drivers d ON d.id = r.driver_id
		  LEFT JOIN cod_collections c ON c.remittance_id = r.id
		 WHERE ($1::uuid IS NULL OR r.driver_id = $1)
		 GROUP BY r.id, r.driver_id, d.display_name, r.reference_no, r.declared_minor,
		          r.counted_minor, r.variance_minor, r.currency, r.state, r.method,
		          r.declared_at, r.settled_at
		 ORDER BY r.declared_at DESC
		 LIMIT $2`, driverID, limit)
	if err != nil {
		return nil, wrap("remittances", err)
	}
	defer rows.Close()

	var out []Remittance
	for rows.Next() {
		var r Remittance
		if err := rows.Scan(&r.ID, &r.DriverID, &r.Driver, &r.ReferenceNo,
			&r.DeclaredMinor, &r.CountedMinor, &r.ExpectedMinor, &r.VarianceMinor,
			&r.Currency, &r.State, &r.Method, &r.DeclaredAt, &r.SettledAt,
			&r.CollectionsQty); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ── invoices ────────────────────────────────────────────────────────────────

// Invoice is one tax document.
type Invoice struct {
	ID           uuid.UUID
	Number       string
	Kind         string
	Status       string
	SupplierName string
	SupplierTRN  string
	BuyerName    string
	BuyerTRN     string
	Country      string
	Currency     string
	NetMinor     int64
	TaxMinor     int64
	GrossMinor   int64
	SupplyDate   time.Time
	IssuedAt     time.Time
	OrderID      *uuid.UUID
	OrderNo      string
	// Corrects points at the invoice this one credits. A credit note that does
	// not name what it corrects is not a credit note, it is a second invoice
	// with a negative total.
	CorrectsID     *uuid.UUID
	CorrectsNumber string
	HasPDF         bool
}

func (s *Service) LoadInvoices(ctx context.Context, q string, from, to *time.Time, limit int, lens Lens) ([]Invoice, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT i.id, i.number, i.kind, i.status, i.supplier_name,
		       coalesce(i.supplier_trn, ''), coalesce(i.buyer_name, ''),
		       coalesce(i.buyer_trn, ''), i.country, i.currency,
		       i.net_minor, i.tax_minor, i.gross_minor, i.supply_date, i.issued_at,
		       i.order_id, coalesce(o.order_no, ''),
		       i.corrects_invoice_id, coalesce(ci.number, ''),
		       i.pdf_ref IS NOT NULL
		  FROM invoices i
		  LEFT JOIN orders o ON o.id = i.order_id
		  LEFT JOIN invoices ci ON ci.id = i.corrects_invoice_id
		 WHERE ($1::text IS NULL
		        OR upper(i.number) = upper($1)
		        OR upper(coalesce(o.order_no, '')) = upper($1)
		        OR i.buyer_name ILIKE '%' || $1 || '%')
		   AND ($2::timestamptz IS NULL OR i.issued_at >= $2)
		   AND ($3::timestamptz IS NULL OR i.issued_at < $3)
		 ORDER BY i.issued_at DESC
		 LIMIT $4`, nilIfEmpty(q), from, to, limit)
	if err != nil {
		return nil, wrap("invoices", err)
	}
	defer rows.Close()

	var out []Invoice
	for rows.Next() {
		var i Invoice
		if err := rows.Scan(&i.ID, &i.Number, &i.Kind, &i.Status, &i.SupplierName,
			&i.SupplierTRN, &i.BuyerName, &i.BuyerTRN, &i.Country, &i.Currency,
			&i.NetMinor, &i.TaxMinor, &i.GrossMinor, &i.SupplyDate, &i.IssuedAt,
			&i.OrderID, &i.OrderNo, &i.CorrectsID, &i.CorrectsNumber, &i.HasPDF); err != nil {
			return nil, err
		}
		// A buyer name on a tax invoice is contact information. Redacted at this
		// boundary rather than in the handler, so a serializer that forgets the
		// check cannot leak it.
		i.BuyerName = lens.contact(i.BuyerName)
		out = append(out, i)
	}
	return out, rows.Err()
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
