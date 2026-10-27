package kitchen

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// PrepLine is how many portions of one dish a kitchen cooks for one meal.
type PrepLine struct {
	Slot             string     `json:"meal_slot"`
	DishID           *uuid.UUID `json:"dish_id"`
	Name             string     `json:"name"`
	NameAr           *string    `json:"name_ar"`
	Portions         int        `json:"portions"`
	ChosenByCustomer int        `json:"chosen_by_customer"`
}

// PrepSheet counts the day's subscription meals by dish: the customer's pick
// where they made one, otherwise that weekday's default dish.
func (s *Service) PrepSheet(ctx context.Context, branchID uuid.UUID, date time.Time) ([]PrepLine, error) {
	rows, err := s.pool.Query(ctx, `
		WITH f AS (
		    SELECT f.subscription_id, f.scheduled_date,
		           split_part(COALESCE(f.payload_ref, ''), ':', 1) AS slot
		      FROM fulfillments f
		     WHERE f.branch_id = $1 AND f.scheduled_date = $2
		       AND f.subscription_id IS NOT NULL AND f.status <> 'cancelled'
		), def AS (
		    SELECT DISTINCT ON (di.meal_slot) di.meal_slot, di.menu_item_id
		      FROM menu_cycle_days cd
		      JOIN menu_day_items di ON di.cycle_day_id = cd.id
		      JOIN menu_items m ON m.id = di.menu_item_id AND m.is_active
		     WHERE cd.cycle_id = (SELECT id FROM menu_cycles
		                           WHERE branch_id = $1 AND is_active ORDER BY created_at LIMIT 1)
		       AND cd.week_no = 1
		       AND cd.weekday = extract(dow FROM $2::date)::int
		     ORDER BY di.meal_slot, di.is_default DESC, di.sort_order
		)
		SELECT f.slot, m.id, COALESCE(sms.item_name_snapshot, m.name, ''),
		       COALESCE(sms.item_name_ar_snapshot, m.name_ar),
		       count(*)::int, count(*) FILTER (WHERE sms.chosen_by = 'customer')::int
		  FROM f
		  LEFT JOIN subscription_meal_selections sms
		         ON sms.subscription_id = f.subscription_id
		        AND sms.service_date = f.scheduled_date AND sms.meal_slot = f.slot
		  LEFT JOIN def ON def.meal_slot = f.slot
		  LEFT JOIN menu_items m ON m.id = COALESCE(sms.menu_item_id, def.menu_item_id)
		 GROUP BY 1, 2, 3, 4
		 ORDER BY 1, 5 DESC, 3`, branchID, date)
	if err != nil {
		return nil, fmt.Errorf("kitchen: prep sheet: %w", err)
	}
	defer rows.Close()
	out := []PrepLine{}
	for rows.Next() {
		var l PrepLine
		if err := rows.Scan(&l.Slot, &l.DishID, &l.Name, &l.NameAr, &l.Portions, &l.ChosenByCustomer); err != nil {
			return nil, fmt.Errorf("kitchen: scan prep line: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
