package payouts

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
	"github.com/marketplace/api/internal/modules/ledger"
	"github.com/marketplace/api/internal/platform/postgres"
)

// Eligibility is the compliance gate on money leaving the platform.
//
// Declared here rather than imported, per the module rule. What this package
// knows is one question — may this vendor be paid — and deliberately nothing
// about trade licences, Emirates IDs or IBAN check digits.
//
// PLAN.md §7 requires identity and bank verification "before first payout", and
// putting the check anywhere other than in front of the transfer means it can
// be bypassed by a code path that forgot. There is exactly one path to a bank
// transfer in this system and this interface sits across it.
type Eligibility interface {
	EligibleTx(ctx context.Context, tx pgx.Tx, subjectType string, subjectID uuid.UUID) (Decision, error)
}

// Decision is the gate's answer. Reasons are shown to the vendor, so every one
// of them has to be something they can act on.
type Decision struct {
	Eligible bool
	Reasons  []string
}

type Service struct {
	pool *pgxpool.Pool
	loc  *time.Location
	kyc  Eligibility
}

// NewService takes the eligibility gate. Nil is permitted and is not a way to
// skip the check — `Issue` refuses outright when it has no gate, because a
// deployment that cannot verify a vendor must not pay one.
func NewService(pool *pgxpool.Pool, loc *time.Location, gate Eligibility) *Service {
	if loc == nil {
		loc = time.FixedZone("Asia/Dubai", 4*3600)
	}
	return &Service{pool: pool, loc: loc, kyc: gate}
}

func (s *Service) Location() *time.Location { return s.loc }

// LoadLines reads a vendor's period out of the ledger.
//
// This is the whole of statement generation, and it reads `ledger_journals`
// rather than `orders`. That is the point: an order that was refunded, a
// subscription cycle recognised across three weeks and a cash collection at the
// door are three different tables and one ledger, and a statement built from the
// order table would have to re-derive every one of those rules and would get one
// of them wrong.
//
// A journal belongs to this vendor if any of its entries touches their payable
// account. Everything else on the journal — commission, VAT — is read from the
// sibling entries, so the statement's breakdown is the same arithmetic the
// posting used rather than a second implementation of it.
func (s *Service) LoadLines(ctx context.Context, vendorID uuid.UUID, p Period) ([]Line, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT j.id,
		       (j.occurred_at AT TIME ZONE 'UTC')::date,
		       j.event_type,
		       COALESCE(j.reference_type, ''), j.reference_id,
		       COALESCE(j.description, ''),
		       COALESCE(sum(e.amount_minor) FILTER (
		           WHERE a.type = 'vendor_payable' AND a.owner_id = $1 AND e.direction = 'credit'), 0)
		     - COALESCE(sum(e.amount_minor) FILTER (
		           WHERE a.type = 'vendor_payable' AND a.owner_id = $1 AND e.direction = 'debit'), 0),
		       COALESCE(sum(e.amount_minor) FILTER (
		           WHERE a.type = 'platform_revenue' AND e.direction = 'credit'), 0)
		     - COALESCE(sum(e.amount_minor) FILTER (
		           WHERE a.type = 'platform_revenue' AND e.direction = 'debit'), 0),
		       COALESCE(sum(e.amount_minor) FILTER (
		           WHERE a.type = 'vat_payable' AND e.direction = 'credit'), 0)
		     - COALESCE(sum(e.amount_minor) FILTER (
		           WHERE a.type = 'vat_payable' AND e.direction = 'debit'), 0)
		  FROM ledger_journals j
		  JOIN ledger_entries e ON e.journal_id = j.id
		  JOIN ledger_accounts a ON a.id = e.account_id
		 WHERE j.occurred_at >= $2 AND j.occurred_at < $3
		   AND j.currency = $4
		   -- Payouts are settlements, not sales. Including them would net a
		   -- vendor's previous payment out of this period and pay them the
		   -- difference, which is how a vendor gets paid once and billed twice.
		   AND j.event_type <> 'vendor_paid'
		   AND EXISTS (
		       SELECT 1 FROM ledger_entries e2
		         JOIN ledger_accounts a2 ON a2.id = e2.account_id
		        WHERE e2.journal_id = j.id
		          AND a2.type = 'vendor_payable' AND a2.owner_id = $1)
		 GROUP BY j.id
		 HAVING COALESCE(sum(e.amount_minor) FILTER (
		            WHERE a.type = 'vendor_payable' AND a.owner_id = $1), 0) <> 0
		 ORDER BY j.occurred_at`,
		vendorID, p.Start, p.End.AddDate(0, 0, 1), "AED")
	if err != nil {
		return nil, fmt.Errorf("load statement lines: %w", err)
	}
	defer rows.Close()

	var out []Line
	for rows.Next() {
		var (
			l         Line
			eventType string
			refID     uuid.NullUUID
			net, comm int64
			vat       int64
		)
		if err := rows.Scan(&l.JournalID, &l.OccurredOn, &eventType,
			&l.ReferenceType, &refID, &l.Description, &net, &comm, &vat); err != nil {
			return nil, fmt.Errorf("scan statement line: %w", err)
		}
		l.ReferenceID = refID
		l.NetMinor = net
		l.CommissionMinor = comm + vat
		l.VATMinor = vat
		l.GrossMinor = net + comm + vat
		l.Kind = kindFor(eventType)
		out = append(out, l)

		// Cash the driver handed over never reached the platform, so the
		// vendor's payable is not something the platform can pay out. The offset
		// is emitted as its own line rather than netted into the first, because
		// a vendor looking at a statement needs to see that the order *was*
		// earned and *was* already settled in cash.
		if eventType == "cod_collected" {
			out = append(out, Line{
				JournalID: l.JournalID, OccurredOn: l.OccurredOn, Kind: KindCODOffset,
				ReferenceType: l.ReferenceType, ReferenceID: l.ReferenceID,
				Description: "collected in cash at the door", NetMinor: -net,
			})
		}
	}
	return out, rows.Err()
}

func kindFor(eventType string) Kind {
	switch eventType {
	case "order_refunded", "subscription_credited", "subscription_cancelled":
		return KindRefund
	case "subscription_recognised", "subscription_charged":
		return KindSubscription
	case "cod_collected":
		return KindOrder
	default:
		return KindOrder
	}
}

// Generate builds statements for every vendor with activity in a period.
//
// One transaction per vendor. A run that generated four hundred statements in
// one transaction and failed on the last has produced nothing, and the retry
// re-reads the same four hundred — whereas per-vendor transactions make the run
// resumable, which is what you want at 03:00 on a Sunday.
func (s *Service) Generate(ctx context.Context, p Period) (int, error) {
	var runID uuid.UUID
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO payout_runs (period_start, period_end, currency, state)
		VALUES ($1, $2, 'AED', 'draft')
		ON CONFLICT (period_start, period_end, currency) DO UPDATE
		   SET state = payout_runs.state
		RETURNING id`, p.Start, p.End,
	).Scan(&runID); err != nil {
		return 0, fmt.Errorf("open payout run: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT a.owner_id
		  FROM ledger_accounts a
		  JOIN ledger_entries e ON e.account_id = a.id
		  JOIN ledger_journals j ON j.id = e.journal_id
		 WHERE a.type = 'vendor_payable' AND a.owner_id IS NOT NULL
		   AND j.occurred_at >= $1 AND j.occurred_at < $2`,
		p.Start, p.End.AddDate(0, 0, 1))
	if err != nil {
		return 0, fmt.Errorf("find vendors with activity: %w", err)
	}
	var vendors []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		vendors = append(vendors, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	created := 0
	for _, vendorID := range vendors {
		made, err := s.generateOne(ctx, runID, vendorID, p)
		if err != nil {
			return created, fmt.Errorf("vendor %s: %w", vendorID, err)
		}
		if made {
			created++
		}
	}

	if _, err := s.pool.Exec(ctx, `
		UPDATE payout_runs r
		   SET statement_count = sub.n, total_minor = sub.total
		  FROM (SELECT count(*) AS n, COALESCE(sum(net_minor), 0) AS total
		          FROM payout_statements WHERE run_id = $1) sub
		 WHERE r.id = $1`, runID); err != nil {
		return created, fmt.Errorf("summarise payout run: %w", err)
	}
	return created, nil
}

func (s *Service) generateOne(ctx context.Context, runID, vendorID uuid.UUID, p Period) (bool, error) {
	lines, err := s.LoadLines(ctx, vendorID, p)
	if err != nil {
		return false, err
	}
	if len(lines) == 0 {
		return false, nil
	}

	totals := Sum(lines)
	// Checked here as well as by the database CHECK. The constraint makes a bad
	// statement impossible to store; this makes the failure legible, which
	// matters when it fires inside a batch at three in the morning.
	if err := totals.Validate(); err != nil {
		return false, err
	}

	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var statementID uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO payout_statements
				(run_id, vendor_id, reference_no, period_start, period_end, currency,
				 gross_minor, commission_minor, vat_minor, adjustments_minor, net_minor, state)
			VALUES ($1, $2, generate_reference('PAY'), $3, $4, 'AED', $5, $6, $7, $8, $9, 'draft')
			ON CONFLICT (vendor_id, period_start, period_end, currency) DO UPDATE
			   -- Regenerating a draft is normal: a late-arriving refund changes
			   -- the period. Regenerating an *issued* one is not, and the WHERE
			   -- clause refuses it rather than silently restating a document the
			   -- vendor has already reconciled against.
			   SET gross_minor = EXCLUDED.gross_minor,
			       commission_minor = EXCLUDED.commission_minor,
			       vat_minor = EXCLUDED.vat_minor,
			       adjustments_minor = EXCLUDED.adjustments_minor,
			       net_minor = EXCLUDED.net_minor,
			       run_id = EXCLUDED.run_id
			 WHERE payout_statements.state = 'draft'
			RETURNING id`,
			runID, vendorID, p.Start, p.End,
			totals.GrossMinor, totals.CommissionMinor, totals.VATMinor,
			totals.AdjustmentsMinor, totals.NetMinor,
		).Scan(&statementID)
		if errors.Is(err, pgx.ErrNoRows) {
			// The ON CONFLICT WHERE filtered it out — already issued. Not an
			// error; the run simply leaves it alone.
			return nil
		}
		if err != nil {
			return fmt.Errorf("upsert statement: %w", err)
		}

		if _, err := tx.Exec(ctx,
			`DELETE FROM payout_statement_lines WHERE statement_id = $1`, statementID); err != nil {
			return fmt.Errorf("clear statement lines: %w", err)
		}
		for _, l := range lines {
			if _, err := tx.Exec(ctx, `
				INSERT INTO payout_statement_lines
					(statement_id, journal_id, occurred_on, kind, reference_type,
					 reference_id, description, gross_minor, commission_minor, vat_minor, net_minor)
				VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, NULLIF($7, ''), $8, $9, $10, $11)`,
				statementID, l.JournalID, l.OccurredOn, string(l.Kind),
				l.ReferenceType, l.ReferenceID, l.Description,
				l.GrossMinor, l.CommissionMinor, l.VATMinor, l.NetMinor,
			); err != nil {
				return fmt.Errorf("insert statement line: %w", err)
			}
		}
		return nil
	})
	return err == nil, err
}

// Issue freezes a statement so a vendor can reconcile against it.
//
// The compliance gate is here rather than at Pay, and the placement is a
// judgement worth stating. Pay is where money moves, so it looks like the
// natural place — but a statement issued to a vendor is a document saying "we
// owe you this and will send it", and issuing one the platform is not permitted
// to settle creates an expectation it must then explain away. Blocking at issue
// means the vendor sees the reason a week before the money was due rather than
// on the day it did not arrive.
//
// The check runs inside the transaction that flips the state, so a case
// revoked concurrently cannot slip past between the read and the write.
func (s *Service) Issue(ctx context.Context, statementID uuid.UUID) error {
	if s.kyc == nil {
		return fmt.Errorf("%w: no compliance gate is configured, so no statement can be "+
			"issued — see docs/PHASE-8 §4", httpx.ErrConflict)
	}

	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var vendorID uuid.UUID
		if err := tx.QueryRow(ctx,
			`SELECT vendor_id FROM payout_statements WHERE id = $1 AND state = 'draft' FOR UPDATE`,
			statementID).Scan(&vendorID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: this statement is not a draft", httpx.ErrConflict)
			}
			return fmt.Errorf("load statement: %w", err)
		}

		decision, err := s.kyc.EligibleTx(ctx, tx, "vendor", vendorID)
		if err != nil {
			return err
		}
		if !decision.Eligible {
			return &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "vendor", Code: "kyc_incomplete",
				Message: strings.Join(decision.Reasons, "; "),
			}}}
		}

		if _, err := tx.Exec(ctx, `
			UPDATE payout_statements SET state = 'issued', issued_at = now()
			 WHERE id = $1 AND state = 'draft'`, statementID); err != nil {
			return fmt.Errorf("issue statement: %w", err)
		}
		return nil
	})
}

// Pay settles a statement and posts it.
//
// The ledger entry is written here, not at issue. Issuing creates no obligation
// that did not already exist — the payable was credited when the order was
// captured — so posting on issue would double-count everything the statement
// summarises.
func (s *Service) Pay(ctx context.Context, statementID uuid.UUID, paymentRef string) error {
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var (
			vendorID uuid.UUID
			net      int64
			currency string
			state    string
		)
		if err := tx.QueryRow(ctx, `
			SELECT vendor_id, net_minor, currency, state
			  FROM payout_statements WHERE id = $1 FOR UPDATE`,
			statementID).Scan(&vendorID, &net, &currency, &state); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.ErrNotFound
			}
			return fmt.Errorf("load statement: %w", err)
		}
		if state == "paid" {
			return nil // idempotent
		}
		if state != "issued" {
			return fmt.Errorf("%w: a statement must be issued before it is paid, this one is %s",
				httpx.ErrConflict, state)
		}
		if net <= 0 {
			return fmt.Errorf("%w: this statement nets %d and is not payable", httpx.ErrConflict, net)
		}

		if _, err := tx.Exec(ctx, `
			UPDATE payout_statements
			   SET state = 'paid', paid_at = now(), payment_ref = NULLIF($2, '')
			 WHERE id = $1`, statementID, paymentRef); err != nil {
			return fmt.Errorf("mark statement paid: %w", err)
		}

		_, err := ledger.PostVendorPaid(ctx, tx, ledger.VendorPayout{
			StatementID: statementID,
			VendorID:    vendorID,
			Currency:    currency,
			NetMinor:    net,
		})
		return err
	})
}

// Statement is the vendor-facing view.
type Statement struct {
	ID               uuid.UUID
	ReferenceNo      string
	PeriodStart      time.Time
	PeriodEnd        time.Time
	Currency         string
	GrossMinor       int64
	CommissionMinor  int64
	VATMinor         int64
	AdjustmentsMinor int64
	NetMinor         int64
	State            string
	IssuedAt         *time.Time
	PaidAt           *time.Time
}

func (s *Service) List(ctx context.Context, vendorID uuid.UUID, limit int) ([]Statement, error) {
	if limit <= 0 || limit > 100 {
		limit = 24
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, reference_no, period_start, period_end, currency,
		       gross_minor, commission_minor, vat_minor, adjustments_minor, net_minor,
		       state, issued_at, paid_at
		  FROM payout_statements
		 WHERE vendor_id = $1 AND state <> 'cancelled'
		 ORDER BY period_end DESC
		 LIMIT $2`, vendorID, limit)
	if err != nil {
		return nil, fmt.Errorf("list statements: %w", err)
	}
	defer rows.Close()

	var out []Statement
	for rows.Next() {
		var st Statement
		if err := rows.Scan(&st.ID, &st.ReferenceNo, &st.PeriodStart, &st.PeriodEnd,
			&st.Currency, &st.GrossMinor, &st.CommissionMinor, &st.VATMinor,
			&st.AdjustmentsMinor, &st.NetMinor, &st.State, &st.IssuedAt, &st.PaidAt); err != nil {
			return nil, fmt.Errorf("scan statement: %w", err)
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// Detail returns a statement's lines, for the screen where a vendor disagrees
// with a number.
func (s *Service) Detail(ctx context.Context, vendorID, statementID uuid.UUID) ([]Line, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT l.journal_id, l.occurred_on, l.kind, COALESCE(l.reference_type, ''),
		       l.reference_id, COALESCE(l.description, ''),
		       l.gross_minor, l.commission_minor, l.vat_minor, l.net_minor
		  FROM payout_statement_lines l
		  JOIN payout_statements s ON s.id = l.statement_id
		 WHERE l.statement_id = $1 AND s.vendor_id = $2
		 ORDER BY l.occurred_on, l.id`, statementID, vendorID)
	if err != nil {
		return nil, fmt.Errorf("load statement detail: %w", err)
	}
	defer rows.Close()

	var out []Line
	for rows.Next() {
		var l Line
		var kind string
		if err := rows.Scan(&l.JournalID, &l.OccurredOn, &kind, &l.ReferenceType,
			&l.ReferenceID, &l.Description, &l.GrossMinor, &l.CommissionMinor,
			&l.VATMinor, &l.NetMinor); err != nil {
			return nil, fmt.Errorf("scan statement line: %w", err)
		}
		l.Kind = Kind(kind)
		out = append(out, l)
	}
	return out, rows.Err()
}
