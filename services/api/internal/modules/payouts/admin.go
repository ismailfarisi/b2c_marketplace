package payouts

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/admin"
	"github.com/marketplace/api/internal/modules/ledger"
	"github.com/marketplace/api/internal/platform/postgres"
)

// The console-facing payout actions.
//
// Written because PLAN-ADMIN-CONSOLE §4.3 asked for issue and pay buttons and
// the first pass refused to build them: `Issue` and `Pay` above take no actor,
// write no `admin_actions` row, and `Pay` does not re-check eligibility. A
// button wired to those would have given the console the ability to pay a
// blocked vendor in one click with nobody's name on it.
//
// This file adds the three things that were missing, rather than a UI on top of
// what was already there.
//
// **An actor and a reason**, both required, both recorded. Moving money to a
// third party's bank account is the single most consequential thing this system
// does, and "who authorised this and why" is the first question after any of it
// goes wrong.
//
// **The audit row in the same transaction as the state change**, which is the
// §3.3 rule honoured properly. There is no third-party gateway inside these
// transactions — the bank transfer happens out of band and its reference is
// recorded afterwards — so the compromise made for refunds does not apply.
//
// **Eligibility re-checked at pay, not only at issue.** A statement is issued
// days before it is paid, and a trade licence that lapses in between makes the
// vendor ineligible without anything writing to their record. `Issue` gates
// correctly; `Pay` trusted that gate and inherited its staleness.
//
// This package now imports `admin`. That is a real dependency rather than an
// accident of convenience: "every movement of money names the person who caused
// it" is a rule this module has to know, and threading an auditor interface
// through would obscure it to save an import.

// Auditor is the subset of admin.Service this package needs.
//
// An interface rather than the concrete type so that a test can assert the audit
// row is written without standing up a permission catalogue — and so the
// dependency is visibly one method wide.
type Auditor interface {
	Record(ctx context.Context, tx pgx.Tx, a admin.Action) error
}

// WithAuditor returns a copy of the service that records what it does.
//
// Separate from NewService because the worker constructs this service to run
// `Generate` on a timer, and a scheduled generation has no actor. Requiring an
// auditor there would mean inventing a fake one, which is worse than not having
// it: a system account in the audit log reads exactly like a person until
// someone checks.
func (s *Service) WithAuditor(a Auditor) *Service {
	clone := *s
	clone.audit = a
	return &clone
}

// IssueInput is a staff issue.
type IssueInput struct {
	StatementID uuid.UUID
	ActorID     uuid.UUID
	Reason      string
}

// IssueByStaff freezes a statement, with a name against it.
func (s *Service) IssueByStaff(ctx context.Context, in IssueInput) error {
	if err := requireReason(in.Reason,
		"issuing a statement tells a vendor what they are owed — the reason is what "+
			"explains a correction later"); err != nil {
		return err
	}
	if s.kyc == nil {
		return fmt.Errorf("%w: no compliance gate is configured, so no statement can be "+
			"issued — see docs/PHASE-8 §4", httpx.ErrConflict)
	}

	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var vendorID uuid.UUID
		var net int64
		var currency string
		if err := tx.QueryRow(ctx, `
			SELECT vendor_id, net_minor, currency
			  FROM payout_statements WHERE id = $1 AND state = 'draft' FOR UPDATE`,
			in.StatementID).Scan(&vendorID, &net, &currency); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: this statement is not a draft — only drafts can be issued",
					httpx.ErrConflict)
			}
			return fmt.Errorf("load statement: %w", err)
		}

		if err := s.gate(ctx, tx, vendorID); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			UPDATE payout_statements SET state = 'issued', issued_at = now()
			 WHERE id = $1 AND state = 'draft'`, in.StatementID); err != nil {
			return fmt.Errorf("issue statement: %w", err)
		}

		return s.record(ctx, tx, in.ActorID, "payout.issue", in.StatementID, in.Reason,
			map[string]any{"state": "draft"},
			map[string]any{"state": "issued", "net_minor": net, "currency": currency,
				"vendor_id": vendorID})
	})
}

// PayInput is a staff settlement.
type PayInput struct {
	StatementID uuid.UUID
	ActorID     uuid.UUID
	// PaymentRef is the bank's reference for the transfer. Required, because a
	// settlement with no reference cannot be traced to a movement of money and
	// is therefore indistinguishable from a state change somebody made up.
	PaymentRef string
	Reason     string
}

// PayByStaff settles a statement, re-checking eligibility first.
//
// The re-check is the substantive difference from `Pay`. A statement is issued
// days before it is paid; a trade licence that lapses in between makes the
// vendor ineligible and nothing writes to their record when the date passes. The
// gate at issue was correct when it ran and is stale by the time the money
// moves, which is the only moment that matters.
func (s *Service) PayByStaff(ctx context.Context, in PayInput) error {
	if err := requireReason(in.Reason,
		"a payment with no stated reason cannot be told apart from an unauthorised one"); err != nil {
		return err
	}
	if strings.TrimSpace(in.PaymentRef) == "" {
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "payment_ref", Code: "required",
			Message: "the bank reference is required — a settlement that cannot be traced to a " +
				"transfer is indistinguishable from a state change somebody made up",
		}}}
	}

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
			in.StatementID).Scan(&vendorID, &net, &currency, &state); err != nil {
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
			return fmt.Errorf("%w: this statement nets %d and is not payable",
				httpx.ErrConflict, net)
		}

		// Again, at the moment the money moves.
		if err := s.gate(ctx, tx, vendorID); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			UPDATE payout_statements
			   SET state = 'paid', paid_at = now(), payment_ref = $2
			 WHERE id = $1`, in.StatementID, strings.TrimSpace(in.PaymentRef)); err != nil {
			return fmt.Errorf("mark statement paid: %w", err)
		}

		if _, err := ledger.PostVendorPaid(ctx, tx, ledger.VendorPayout{
			StatementID: in.StatementID,
			VendorID:    vendorID,
			Currency:    currency,
			NetMinor:    net,
		}); err != nil {
			return err
		}

		return s.record(ctx, tx, in.ActorID, "payout.pay", in.StatementID, in.Reason,
			map[string]any{"state": "issued"},
			map[string]any{"state": "paid", "net_minor": net, "currency": currency,
				"payment_ref": strings.TrimSpace(in.PaymentRef), "vendor_id": vendorID})
	})
}

// ApproveRunInput approves a whole run for issue.
type ApproveRunInput struct {
	RunID   uuid.UUID
	ActorID uuid.UUID
	Reason  string
}

// ApproveRun records that a human looked at the run before anything was issued.
//
// Approval is deliberately *separate* from issuing and does not issue anything.
// The run is the unit a finance person reviews — the total, the vendor count,
// the outliers — and issuing is per statement because eligibility is per vendor.
// Collapsing them into one button would mean either approving statements
// individually (which nobody would do for two hundred vendors) or issuing
// blocked ones silently.
//
// It also refuses to approve a run with nothing in it, which is the shape a
// generation failure takes: a run row exists, the job died, and a zero-statement
// run approved and issued looks exactly like a quiet week.
func (s *Service) ApproveRun(ctx context.Context, in ApproveRunInput) error {
	if err := requireReason(in.Reason,
		"approval is the record that a person reviewed these numbers"); err != nil {
		return err
	}

	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var state string
		var count int
		var total int64
		if err := tx.QueryRow(ctx, `
			SELECT state, statement_count, total_minor
			  FROM payout_runs WHERE id = $1 FOR UPDATE`, in.RunID).
			Scan(&state, &count, &total); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.ErrNotFound
			}
			return fmt.Errorf("load run: %w", err)
		}
		if state != "draft" {
			return fmt.Errorf("%w: this run is already %s", httpx.ErrConflict, state)
		}
		if count == 0 {
			return fmt.Errorf("%w: this run has no statements — that is what a failed "+
				"generation looks like, and approving it would make the failure permanent",
				httpx.ErrConflict)
		}

		if _, err := tx.Exec(ctx, `
			UPDATE payout_runs
			   SET state = 'approved', approved_by = $2, approved_at = now()
			 WHERE id = $1`, in.RunID, in.ActorID); err != nil {
			return fmt.Errorf("approve run: %w", err)
		}

		return s.record(ctx, tx, in.ActorID, "payout.approve_run", in.RunID, in.Reason,
			map[string]any{"state": "draft"},
			map[string]any{"state": "approved", "statements": count, "total_minor": total})
	})
}

// IssueRunResult reports what happened to each statement in a bulk issue.
type IssueRunResult struct {
	Issued  int
	Skipped []SkippedStatement
}

// SkippedStatement is one vendor who could not be issued, and why.
type SkippedStatement struct {
	StatementID uuid.UUID
	VendorID    uuid.UUID
	Vendor      string
	Reason      string
}

// IssueRun issues every eligible draft in an approved run.
//
// Returns what it skipped rather than failing on the first blocked vendor.
// Failing the whole batch because one vendor's licence expired would hold up
// nineteen vendors who are fine, and an operator who has to clear blockers
// before anyone is paid will start clearing them carelessly.
//
// Each statement gets its own transaction, so one failure cannot roll back
// issues that succeeded. That is the right trade here: issuing is idempotent per
// statement (the state check refuses a second attempt) and a partial batch is
// recoverable by running it again, whereas a batch that must be all-or-nothing
// across two hundred vendors is a batch that never completes.
func (s *Service) IssueRun(ctx context.Context, runID, actorID uuid.UUID, reason string) (*IssueRunResult, error) {
	if err := requireReason(reason, "issuing a run needs a reason, once, for all of it"); err != nil {
		return nil, err
	}

	var runState string
	if err := s.pool.QueryRow(ctx,
		`SELECT state FROM payout_runs WHERE id = $1`, runID).Scan(&runState); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, httpx.ErrNotFound
		}
		return nil, fmt.Errorf("load run: %w", err)
	}
	if runState != "approved" {
		return nil, fmt.Errorf("%w: a run must be approved before its statements are issued, "+
			"this one is %s", httpx.ErrConflict, runState)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT st.id, st.vendor_id, coalesce(v.trade_name, '')
		  FROM payout_statements st
		  JOIN vendors v ON v.id = st.vendor_id
		 WHERE st.run_id = $1 AND st.state = 'draft'
		 ORDER BY st.net_minor DESC`, runID)
	if err != nil {
		return nil, fmt.Errorf("load drafts: %w", err)
	}

	type draft struct {
		id       uuid.UUID
		vendorID uuid.UUID
		vendor   string
	}
	var drafts []draft
	for rows.Next() {
		var d draft
		if err := rows.Scan(&d.id, &d.vendorID, &d.vendor); err != nil {
			rows.Close()
			return nil, err
		}
		drafts = append(drafts, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := &IssueRunResult{}
	for _, d := range drafts {
		err := s.IssueByStaff(ctx, IssueInput{
			StatementID: d.id, ActorID: actorID, Reason: reason,
		})
		if err == nil {
			result.Issued++
			continue
		}
		result.Skipped = append(result.Skipped, SkippedStatement{
			StatementID: d.id, VendorID: d.vendorID, Vendor: d.vendor,
			Reason: humanReason(err),
		})
	}
	return result, nil
}

// gate runs the compliance check inside the caller's transaction.
func (s *Service) gate(ctx context.Context, tx pgx.Tx, vendorID uuid.UUID) error {
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
	return nil
}

// record writes the audit row, and refuses to proceed without an auditor.
//
// Hard failure rather than a skip. An unaudited payout is the exact thing this
// file exists to prevent, and a service constructed without an auditor that
// silently moved money would be a worse outcome than one that refuses to start
// the action at all.
func (s *Service) record(ctx context.Context, tx pgx.Tx, actorID uuid.UUID,
	action string, subjectID uuid.UUID, reason string, before, after map[string]any) error {

	if s.audit == nil {
		return fmt.Errorf("%w: this service was built without an auditor, so it cannot "+
			"perform staff actions — payouts move money and every movement names a person",
			httpx.ErrConflict)
	}
	return s.audit.Record(ctx, tx, admin.Action{
		ActorID:     actorID,
		Action:      action,
		SubjectType: "payout_statement",
		SubjectID:   uuid.NullUUID{UUID: subjectID, Valid: true},
		Reason:      reason,
		Before:      before,
		After:       after,
	})
}

func requireReason(reason, why string) error {
	if strings.TrimSpace(reason) == "" {
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "reason", Code: "required", Message: why,
		}}}
	}
	return nil
}

// humanReason extracts the sentence from a validation or conflict error.
//
// Used for the per-vendor skip list, where the whole value is that an operator
// can read down a column of reasons and see that eighteen of them say the same
// thing — which is a process problem, not nineteen individual ones.
func humanReason(err error) string {
	var v *httpx.ValidationError
	if errors.As(err, &v) && len(v.Fields) > 0 {
		parts := make([]string, 0, len(v.Fields))
		for _, f := range v.Fields {
			parts = append(parts, f.Message)
		}
		return strings.Join(parts, "; ")
	}
	return err.Error()
}
