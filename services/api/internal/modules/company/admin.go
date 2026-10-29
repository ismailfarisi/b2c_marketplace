package company

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/platform/postgres"
)

type AdminSummary struct {
	Company
	Members int    `json:"members"`
	Lunches int    `json:"lunches"`
	Credit  Credit `json:"credit"`
}

func (s *Service) AdminList(ctx context.Context, status, q string) ([]AdminSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+companyCols+`,
		       (SELECT count(*) FROM company_members m WHERE m.company_id = c.id AND m.status = 'active'),
		       (SELECT count(*) FROM subscriptions s WHERE s.company_id = c.id AND s.status IN ('active','paused'))
		  FROM companies c
		 WHERE ($1 = '' OR c.status = $1)
		   AND ($2 = '' OR c.name ILIKE '%' || $2 || '%' OR c.legal_name ILIKE '%' || $2 || '%' OR c.trn = $2)
		 ORDER BY (c.status = 'pending') DESC, c.created_at DESC LIMIT 200`, status, strings.TrimSpace(q))
	if err != nil {
		return nil, fmt.Errorf("company: admin list: %w", err)
	}
	var out []AdminSummary
	for rows.Next() {
		var a AdminSummary
		var days []int16
		if err := rows.Scan(&a.ID, &a.Name, &a.LegalName, &a.TRN, &a.BillingEmail, &a.BillingPhone,
			&a.Status, &a.Currency, &a.CreditLimitMinor, &a.PaymentTermsDays,
			&days, &a.Policy.Slots, &a.Policy.MealCapMinor, &a.ReviewNote, &a.ReviewedAt, &a.CreatedAt,
			&a.Members, &a.Lunches); err != nil {
			rows.Close()
			return nil, err
		}
		for _, d := range days {
			a.Policy.Weekdays = append(a.Policy.Weekdays, int(d))
		}
		out = append(out, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Credit, err = credit(ctx, s.pool, out[i].ID, out[i].CreditLimitMinor); err != nil {
			return nil, err
		}
	}
	if out == nil {
		out = []AdminSummary{}
	}
	return out, nil
}

func (s *Service) AdminDetail(ctx context.Context, companyID uuid.UUID) (Dashboard, error) {
	d, err := s.detail(ctx, companyID)
	d.Role = "staff"
	return d, err
}

func (s *Service) AdminStatement(ctx context.Context, companyID, statementID uuid.UUID) (Statement, error) {
	return s.statement(ctx, companyID, statementID)
}

// Review approves, rejects, suspends or reactivates a company and sets its
// credit terms. Suspending stops new lunches and renewals; running lunches
// keep the days already paid for.
func (s *Service) Review(ctx context.Context, actorID, companyID uuid.UUID, in ReviewInput) error {
	switch in.Status {
	case "", "active", "rejected", "suspended":
	default:
		return invalid("status", "invalid", "status is active, rejected or suspended")
	}
	if in.CreditLimitMinor != nil && *in.CreditLimitMinor < 0 {
		return invalid("credit_limit_minor", "invalid", "the credit limit cannot be negative")
	}
	if in.PaymentTermsDays != nil && (*in.PaymentTermsDays < 0 || *in.PaymentTermsDays > 90) {
		return invalid("payment_terms_days", "invalid", "payment terms are 0 to 90 days")
	}
	if in.Status == "active" && in.CreditLimitMinor == nil {
		var limit int64
		if err := s.pool.QueryRow(ctx, `SELECT credit_limit_minor FROM companies WHERE id = $1`, companyID).
			Scan(&limit); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.ErrNotFound
			}
			return err
		}
		if limit == 0 {
			return invalid("credit_limit_minor", "required", "set a credit limit before approving")
		}
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE companies
		   SET status = coalesce(NULLIF($2,''), status),
		       credit_limit_minor = coalesce($3, credit_limit_minor),
		       payment_terms_days = coalesce($4, payment_terms_days),
		       review_note = coalesce(NULLIF($5,''), review_note),
		       reviewed_by = $6, reviewed_at = now(), updated_at = now()
		 WHERE id = $1`, companyID, in.Status, in.CreditLimitMinor, in.PaymentTermsDays,
		strings.TrimSpace(in.Note), actorID)
	if err != nil {
		return fmt.Errorf("company: review: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	if in.Status == "rejected" {
		return s.cancelLunches(ctx, companyID, nil, actorID, "company account rejected")
	}
	return nil
}

// IssueStatement bills everything unbilled up to the end of periodEnd.
func (s *Service) IssueStatement(ctx context.Context, actorID, companyID uuid.UUID, periodEnd time.Time) (uuid.UUID, error) {
	var id uuid.UUID
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var terms int
		var currency string
		if err := tx.QueryRow(ctx, `
			SELECT payment_terms_days, currency FROM companies WHERE id = $1 FOR UPDATE`, companyID).
			Scan(&terms, &currency); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.ErrNotFound
			}
			return err
		}
		cutoff := time.Date(periodEnd.Year(), periodEnd.Month(), periodEnd.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
		var total int64
		var n int
		if err := tx.QueryRow(ctx, `
			SELECT coalesce(sum(amount_minor), 0), count(*) FROM company_charges
			 WHERE company_id = $1 AND statement_id IS NULL AND created_at < $2`, companyID, cutoff).
			Scan(&total, &n); err != nil {
			return err
		}
		if n == 0 {
			return httpx.Coded(httpx.ErrConflict, "nothing_to_bill", "There is nothing unbilled up to that date.")
		}
		if total < 0 {
			total = 0
		}
		for attempt := 0; ; attempt++ {
			err := tx.QueryRow(ctx, `
				INSERT INTO company_statements
				    (statement_no, company_id, period_end, total_minor, currency, issued_by, due_on)
				VALUES ('CS-' || to_char(now(), 'YYYYMM') || '-' ||
				        lpad((SELECT count(*) + 1 + $7 FROM company_statements
				               WHERE issued_at >= date_trunc('month', now()))::text, 5, '0'),
				        $1, $2, $3, $4, $5, CURRENT_DATE + $6::int)
				RETURNING id`, companyID, periodEnd, total, currency, actorID, terms, attempt).Scan(&id)
			if err == nil {
				break
			}
			if !isUniqueViolation(err) || attempt >= 3 {
				return fmt.Errorf("company: insert statement: %w", err)
			}
		}
		_, err := tx.Exec(ctx, `
			UPDATE company_charges SET statement_id = $3
			 WHERE company_id = $1 AND statement_id IS NULL AND created_at < $2`, companyID, cutoff, id)
		return err
	})
	return id, err
}

func (s *Service) MarkStatementPaid(ctx context.Context, actorID, companyID, statementID uuid.UUID, reference string) error {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return invalid("reference", "required", "enter the bank transfer reference")
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE company_statements
		   SET status = 'paid', paid_at = now(), paid_reference = $3, paid_marked_by = $4
		 WHERE id = $1 AND company_id = $2 AND status = 'issued'`, statementID, companyID, reference, actorID)
	if err != nil {
		return fmt.Errorf("company: mark paid: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return httpx.Coded(httpx.ErrConflict, "not_payable", "Only an issued statement can be marked paid.")
	}
	return nil
}
