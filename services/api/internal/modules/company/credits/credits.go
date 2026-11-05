// Package credits applies office-lunch refund credits to a company's open
// statements. It sits apart from package company so the subscriptions module,
// which writes the credit when a lunch is cancelled, can apply it in the same
// transaction without importing company (which imports subscriptions).
package credits

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Open is an issued, unpaid statement and what it still asks for.
type Open struct {
	ID       uuid.UUID
	DueMinor int64
}

// Credit is an unbilled refund credit, as a positive amount.
type Credit struct {
	ID          uuid.UUID
	AmountMinor int64
}

// Step attaches AmountMinor of credit Credit (an index into the credits
// passed to Allocate) to statement Statement (an index into the open
// statements). Split means the credit is larger than what is left on that
// statement, so the rest is split off into a new row and carried on.
type Step struct {
	Credit, Statement int
	AmountMinor       int64
	Split             bool
}

// Allocate spends credits, oldest first, against open statements, oldest
// first, never taking a statement below zero. Whatever is left over is
// carried forward to the next statement.
func Allocate(open []Open, credits []Credit) []Step {
	due := make([]int64, len(open))
	for i, o := range open {
		due[i] = o.DueMinor
	}
	var steps []Step
	st := 0
	for ci, c := range credits {
		left := c.AmountMinor
		for left > 0 {
			for st < len(due) && due[st] <= 0 {
				st++
			}
			if st == len(due) {
				return steps
			}
			take := min(left, due[st])
			steps = append(steps, Step{Credit: ci, Statement: st, AmountMinor: take, Split: take < left})
			due[st] -= take
			left -= take
		}
	}
	return steps
}

// Apply attaches the company's refund credits for lunches that were already
// billed to its open statements, as adjustment lines that reduce what those
// statements ask for. A statement brought to zero is settled. Credits for
// lunches not yet billed are left alone: they already net against the
// matching charge on the next statement.
func Apply(ctx context.Context, tx pgx.Tx, companyID uuid.UUID) error {
	// Same lock IssueStatement takes, so a statement being issued and a credit
	// being applied never see each other half-done.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM companies WHERE id = $1 FOR UPDATE`, companyID); err != nil {
		return fmt.Errorf("credits: lock company: %w", err)
	}
	open, err := collect(ctx, tx, `
		SELECT s.id, s.total_minor + coalesce((SELECT sum(a.amount_minor) FROM company_charges a
		                                        WHERE a.statement_id = s.id AND a.applied_at IS NOT NULL), 0)
		  FROM company_statements s
		 WHERE s.company_id = $1 AND s.status = 'issued'
		 ORDER BY s.issued_at, s.id
		   FOR UPDATE`, companyID, func(r pgx.Rows) (Open, error) {
		var o Open
		return o, r.Scan(&o.ID, &o.DueMinor)
	})
	if err != nil || len(open) == 0 {
		return err
	}
	credits, err := collect(ctx, tx, `
		SELECT ch.id, -ch.amount_minor
		  FROM company_charges ch
		 WHERE ch.company_id = $1 AND ch.kind = 'credit' AND ch.statement_id IS NULL AND ch.amount_minor < 0
		   AND EXISTS (SELECT 1 FROM subscription_cycles c
		                 JOIN company_charges b ON b.order_id = c.order_id AND b.kind = 'charge'
		                WHERE c.id = ch.cycle_id AND b.statement_id IS NOT NULL)
		 ORDER BY ch.created_at, ch.id
		   FOR UPDATE OF ch`, companyID, func(r pgx.Rows) (Credit, error) {
		var c Credit
		return c, r.Scan(&c.ID, &c.AmountMinor)
	})
	if err != nil {
		return err
	}
	steps := Allocate(open, credits)
	current := make([]uuid.UUID, len(credits))
	left := make([]int64, len(credits))
	for i, c := range credits {
		current[i], left[i] = c.ID, c.AmountMinor
	}
	touched := map[uuid.UUID]bool{}
	for _, s := range steps {
		row, statementID := current[s.Credit], open[s.Statement].ID
		touched[statementID] = true
		if s.Split {
			var rest uuid.UUID
			if err := tx.QueryRow(ctx, `
				INSERT INTO company_charges
				    (company_id, kind, subscription_id, cycle_id, user_id, amount_minor, currency, carried_from, created_at)
				SELECT company_id, 'credit', subscription_id, cycle_id, user_id, $2, currency, coalesce(carried_from, id), created_at
				  FROM company_charges WHERE id = $1
				RETURNING id`, row, -(left[s.Credit] - s.AmountMinor)).Scan(&rest); err != nil {
				return fmt.Errorf("credits: carry forward: %w", err)
			}
			current[s.Credit] = rest
		}
		if _, err := tx.Exec(ctx, `
			UPDATE company_charges SET amount_minor = $2, statement_id = $3, applied_at = now()
			 WHERE id = $1`, row, -s.AmountMinor, statementID); err != nil {
			return fmt.Errorf("credits: apply: %w", err)
		}
		left[s.Credit] -= s.AmountMinor
	}
	for id := range touched {
		if _, err := tx.Exec(ctx, `
			UPDATE company_statements s SET status = 'paid', paid_at = now(), paid_reference = ''
			 WHERE s.id = $1 AND s.status = 'issued'
			   AND s.total_minor + (SELECT coalesce(sum(a.amount_minor), 0) FROM company_charges a
			                         WHERE a.statement_id = s.id AND a.applied_at IS NOT NULL) <= 0`, id); err != nil {
			return fmt.Errorf("credits: settle statement: %w", err)
		}
	}
	return nil
}

func collect[T any](ctx context.Context, tx pgx.Tx, sql string, companyID uuid.UUID, scan func(pgx.Rows) (T, error)) ([]T, error) {
	rows, err := tx.Query(ctx, sql, companyID)
	if err != nil {
		return nil, fmt.Errorf("credits: load: %w", err)
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
