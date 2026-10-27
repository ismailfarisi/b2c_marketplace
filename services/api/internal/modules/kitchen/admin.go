package kitchen

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

// KitchenSummary is one row of the staff kitchen directory.
type KitchenSummary struct {
	BranchID          uuid.UUID `json:"branch_id"`
	BranchName        string    `json:"branch_name"`
	City              *string   `json:"city"`
	BranchActive      bool      `json:"branch_active"`
	VendorID          uuid.UUID `json:"vendor_id"`
	VendorName        string    `json:"vendor_name"`
	VendorStatus      string    `json:"vendor_status"`
	Dishes            int       `json:"dishes"`
	LivePlans         int       `json:"live_plans"`
	RotationPlans     int       `json:"rotation_plans"`
	ActiveSubscribers int       `json:"active_subscribers"`
	CreatedAt         time.Time `json:"created_at"`
}

// AdminKitchens lists every branch that has started setting up meals: a dish
// or a plan. A branch with neither is a vendor of some other kind.
func (s *Service) AdminKitchens(ctx context.Context, q string, rotationOnly bool) ([]KitchenSummary, error) {
	q = strings.TrimSpace(q)
	rows, err := s.pool.Query(ctx, `
		SELECT b.id, b.name, b.city, b.is_active, v.id, v.trade_name, v.status,
		       (SELECT count(*) FROM menu_items m WHERE m.branch_id = b.id AND m.is_active),
		       (SELECT count(*) FROM subscription_plans p
		         WHERE p.branch_id = b.id AND p.service_type = $1 AND p.is_active),
		       (SELECT count(*) FROM subscription_plans p
		         WHERE p.branch_id = b.id AND p.service_type = $1 AND p.is_active AND p.rotation_enabled),
		       (SELECT count(*) FROM subscriptions s
		         WHERE s.branch_id = b.id AND s.status = 'active'),
		       b.created_at
		  FROM vendor_branches b
		  JOIN vendors v ON v.id = b.vendor_id
		 WHERE b.deleted_at IS NULL
		   AND (EXISTS (SELECT 1 FROM menu_items m WHERE m.branch_id = b.id)
		        OR EXISTS (SELECT 1 FROM subscription_plans p
		                    WHERE p.branch_id = b.id AND p.service_type = $1))
		   AND ($2 = '' OR b.name ILIKE '%' || $2 || '%' OR v.trade_name ILIKE '%' || $2 || '%')
		   AND (NOT $3 OR EXISTS (SELECT 1 FROM subscription_plans p
		                           WHERE p.branch_id = b.id AND p.service_type = $1
		                             AND p.is_active AND p.rotation_enabled))
		 ORDER BY v.trade_name, b.name
		 LIMIT 500`, planServiceType, q, rotationOnly)
	if err != nil {
		return nil, fmt.Errorf("kitchen: admin list: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (KitchenSummary, error) {
		var k KitchenSummary
		err := row.Scan(&k.BranchID, &k.BranchName, &k.City, &k.BranchActive, &k.VendorID,
			&k.VendorName, &k.VendorStatus, &k.Dishes, &k.LivePlans, &k.RotationPlans,
			&k.ActiveSubscribers, &k.CreatedAt)
		return k, err
	})
}

// SetPlanActive is the staff lever on a plan: taking it off sale stops new
// sign-ups without touching anyone already subscribed.
func (s *Service) SetPlanActive(ctx context.Context, planID uuid.UUID, active bool) (Plan, error) {
	var branchID uuid.UUID
	err := s.pool.QueryRow(ctx, `
		UPDATE subscription_plans SET is_active = $2, updated_at = now()
		 WHERE id = $1 AND service_type = $3
		RETURNING branch_id`, planID, active, planServiceType).Scan(&branchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Plan{}, httpx.ErrNotFound
	}
	if err != nil {
		return Plan{}, mapConstraint(fmt.Errorf("kitchen: set plan active: %w", err))
	}
	return s.GetPlan(ctx, branchID, planID)
}
