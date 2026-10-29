package company

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/promo"
	"github.com/marketplace/api/internal/modules/subscriptions"
	"github.com/marketplace/api/internal/platform/postgres"
)

func isUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

type LunchInput struct {
	CompanyID uuid.UUID
	OfficeID  uuid.UUID
	Sub       subscriptions.SubscribeInput
}

type LunchResult struct {
	Subscription *subscriptions.Result `json:"-"`
	ChargedMinor int64                 `json:"charged_minor"`
}

// OfficeAddress returns the caller's own address row for a company office,
// creating it on first use. Delivery-area checks and fulfilment snapshots
// then work exactly as for a home address.
func (s *Service) OfficeAddress(ctx context.Context, userID, companyID, officeID uuid.UUID) (uuid.UUID, error) {
	if _, err := s.role(ctx, s.pool, userID, companyID); err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			SELECT a.id FROM addresses a
			  JOIN company_offices o ON o.id = a.company_office_id
			 WHERE a.user_id = $1 AND a.company_office_id = $2 AND o.company_id = $3
			   AND a.deleted_at IS NULL AND o.deleted_at IS NULL`, userID, officeID, companyID).Scan(&id)
		if err == nil {
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("company: office address: %w", err)
		}
		err = tx.QueryRow(ctx, `
			INSERT INTO addresses (user_id, label, building, area, city, point, company_office_id)
			SELECT $1, c.name || ' · ' || o.label, o.building, o.area, o.city, o.point, o.id
			  FROM company_offices o JOIN companies c ON c.id = o.company_id
			 WHERE o.id = $2 AND o.company_id = $3 AND o.deleted_at IS NULL
			RETURNING id`, userID, officeID, companyID).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return invalid("office_id", "not_found", "that office is not on your company's list")
		}
		if err != nil {
			return fmt.Errorf("company: create office address: %w", err)
		}
		return nil
	})
	return id, err
}

// checkLunch applies the company's policy to a requested subscription.
func (s *Service) checkLunch(ctx context.Context, in *LunchInput) (Company, error) {
	c, err := scanCompany(s.pool.QueryRow(ctx, `SELECT `+companyCols+` FROM companies c WHERE c.id = $1`, in.CompanyID))
	if err != nil {
		return c, err
	}
	if _, err := s.role(ctx, s.pool, in.Sub.UserID, in.CompanyID); err != nil {
		return c, err
	}
	if c.Status != "active" {
		return c, httpx.Coded(httpx.ErrConflict, "company_not_active",
			"Your company's lunch account is not active yet.")
	}
	if len(in.Sub.Rules) == 0 {
		return c, invalid("schedule", "required", "a subscription needs at least one delivery day")
	}
	for _, r := range in.Sub.Rules {
		if !c.Policy.covers(int(r.Weekday), r.Slot) {
			return c, invalid("schedule", "not_covered",
				"your company covers only its chosen days and meals")
		}
		if r.AddressID.Valid {
			return c, invalid("schedule", "office_only", "office lunches are delivered to the office")
		}
	}
	addr, err := s.OfficeAddress(ctx, in.Sub.UserID, in.CompanyID, in.OfficeID)
	if err != nil {
		return c, err
	}
	in.Sub.AddressID = addr
	in.Sub.Promo = promo.Ref{}
	in.Sub.RenewalMode = "auto"
	in.Sub.CompanyID = uuid.NullUUID{UUID: in.CompanyID, Valid: true}
	return c, nil
}

// mealsInFirstCycle counts meals, not days: two meals on one day are two.
func perMeal(p subscriptions.SubscriptionPreview, rules []subscriptions.Rule) int64 {
	days := map[subscriptions.Weekday]bool{}
	for _, r := range rules {
		days[r.Weekday] = true
	}
	if len(days) == 0 || p.DeliverableDays == 0 {
		return p.TotalMinor
	}
	meals := int64(p.DeliverableDays) * int64(len(rules)) / int64(len(days))
	if meals <= 0 {
		return p.TotalMinor
	}
	return (p.TotalMinor + meals - 1) / meals
}

// Preview prices an office lunch and says whether the company covers it.
func (s *Service) Preview(ctx context.Context, in LunchInput) (subscriptions.SubscriptionPreview, int64, error) {
	c, err := s.checkLunch(ctx, &in)
	if err != nil {
		return subscriptions.SubscriptionPreview{}, 0, err
	}
	p, err := s.subs.PreviewSubscription(ctx, in.Sub)
	if err != nil {
		return p, 0, err
	}
	pm := perMeal(p, in.Sub.Rules)
	if pm > c.Policy.MealCapMinor {
		return p, pm, overCap(c)
	}
	return p, pm, nil
}

func overCap(c Company) error {
	return httpx.Coded(httpx.ErrConflict, "over_meal_cap",
		fmt.Sprintf("This plan costs more per meal than your company covers (%s %.2f).",
			c.Currency, float64(c.Policy.MealCapMinor)/100))
}

// Subscribe starts an office lunch: an ordinary subscription to the office
// address, with its billing order authorized on the company's account.
func (s *Service) Subscribe(ctx context.Context, in LunchInput) (*subscriptions.Result, error) {
	if _, _, err := s.Preview(ctx, in); err != nil {
		return nil, err
	}
	if _, err := s.checkLunch(ctx, &in); err != nil {
		return nil, err
	}
	res, err := s.subs.Subscribe(ctx, in.Sub)
	if err != nil {
		return nil, err
	}
	if err := s.settle(ctx, in.CompanyID, in.Sub.UserID, res.ID, res.OrderID); err != nil {
		// The subscription stays pending and unpaid; cancel it so the employee
		// is not left holding a bill meant for their company.
		if cerr := s.subs.Cancel(ctx, in.Sub.UserID, res.ID, "company could not pay"); cerr != nil {
			slog.ErrorContext(ctx, "company: cancel unpaid lunch failed", "subscription_id", res.ID, "error", cerr)
		}
		return nil, err
	}
	res.Status = "active"
	return res, nil
}

// settle authorizes one billing order on the company account, inside the
// credit limit. The company row is locked so two employees cannot both use
// the last of the limit.
func (s *Service) settle(ctx context.Context, companyID, userID, subID, orderID uuid.UUID) error {
	_, err := s.pay.AuthorizeOnAccount(ctx, userID, orderID, "company:"+companyID.String(),
		func(ctx context.Context, tx pgx.Tx, amount int64, currency string) error {
			var status, cur string
			var limit int64
			if err := tx.QueryRow(ctx, `
				SELECT status, currency, credit_limit_minor FROM companies WHERE id = $1 FOR UPDATE`,
				companyID).Scan(&status, &cur, &limit); err != nil {
				return fmt.Errorf("company: lock: %w", err)
			}
			if status != "active" {
				return httpx.Coded(httpx.ErrConflict, "company_not_active",
					"Your company's lunch account is not active.")
			}
			var member bool
			if err := tx.QueryRow(ctx, `
				SELECT EXISTS (SELECT 1 FROM company_members
				 WHERE company_id = $1 AND user_id = $2 AND status = 'active')`, companyID, userID).Scan(&member); err != nil {
				return err
			}
			if !member {
				return httpx.Coded(httpx.ErrConflict, "not_member", "You are no longer on your company's lunch list.")
			}
			if cur != currency {
				return httpx.Coded(httpx.ErrConflict, "currency_mismatch", "This kitchen bills in a different currency.")
			}
			cr, err := credit(ctx, tx, companyID, limit)
			if err != nil {
				return err
			}
			if amount > cr.AvailableMinor {
				return httpx.Coded(httpx.ErrConflict, "over_credit_limit",
					"Your company has reached its lunch credit limit. Ask your lunch admin.")
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO company_charges (company_id, kind, subscription_id, order_id, user_id, amount_minor, currency)
				VALUES ($1, 'charge', $2, $3, $4, $5, $6)`, companyID, subID, orderID, userID, amount, currency)
			if err != nil {
				return fmt.Errorf("company: record charge: %w", err)
			}
			return nil
		})
	return err
}

// SettleDue pays renewal cycles of office lunches from the company account.
// A cycle the company cannot pay (removed employee, suspended account, over
// the limit) is left for the normal dunning path, which suspends it.
func (s *Service) SettleDue(ctx context.Context, batch int) (int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.company_id, s.user_id, s.id, c.order_id
		  FROM subscription_cycles c
		  JOIN subscriptions s ON s.id = c.subscription_id
		 WHERE s.company_id IS NOT NULL
		   AND c.status IN ('pending', 'past_due') AND c.order_id IS NOT NULL
		   AND s.status IN ('active', 'paused', 'suspended')
		   AND NOT EXISTS (SELECT 1 FROM company_charges ch WHERE ch.order_id = c.order_id)
		 ORDER BY c.starts_on LIMIT $1`, batch)
	if err != nil {
		return 0, fmt.Errorf("company: due cycles: %w", err)
	}
	type due struct{ company, user, sub, order uuid.UUID }
	var list []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.company, &d.user, &d.sub, &d.order); err != nil {
			rows.Close()
			return 0, err
		}
		list = append(list, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	paid := 0
	for _, d := range list {
		if err := s.settle(ctx, d.company, d.user, d.sub, d.order); err != nil {
			slog.WarnContext(ctx, "company: renewal not settled", "subscription_id", d.sub, "error", err)
			continue
		}
		paid++
	}
	return paid, nil
}
