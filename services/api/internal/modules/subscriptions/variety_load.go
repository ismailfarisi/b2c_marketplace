package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/modules/kitchen"
)

// querier is the read side shared by pgx.Tx and *pgxpool.Pool.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type cycleDish struct {
	week, weekday int
	slot          string
	name          string
	nameAr        *string
}

type kitchenMenu struct {
	cuisines []string
	weeks    int
	anchor   time.Time
	dishes   []cycleDish
}

// loadKitchenMenu reads a kitchen's cuisines and the dishes of its menu
// cycle: the plan's own cycle when it has one, otherwise the newest active one.
func loadKitchenMenu(ctx context.Context, q querier, branchID uuid.UUID, cycleID uuid.NullUUID) (kitchenMenu, error) {
	var m kitchenMenu
	if err := q.QueryRow(ctx,
		`SELECT COALESCE(cuisines, '{}') FROM vendor_branches WHERE id = $1`, branchID,
	).Scan(&m.cuisines); err != nil {
		return m, fmt.Errorf("subscriptions: variety cuisines: %w", err)
	}
	var cycle uuid.UUID
	err := q.QueryRow(ctx, `
		SELECT id, weeks, anchor_date FROM menu_cycles
		 WHERE branch_id = $1 AND is_active
		 ORDER BY (id = $2) DESC, created_at DESC LIMIT 1`, branchID, cycleID,
	).Scan(&cycle, &m.weeks, &m.anchor)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, nil
	}
	if err != nil {
		return m, fmt.Errorf("subscriptions: variety cycle: %w", err)
	}
	rows, err := q.Query(ctx, `
		SELECT cd.week_no, cd.weekday, di.meal_slot, mi.name, mi.name_ar
		  FROM menu_cycle_days cd
		  JOIN menu_day_items di ON di.cycle_day_id = cd.id
		  JOIN menu_items mi ON mi.id = di.menu_item_id AND mi.is_active
		 WHERE cd.cycle_id = $1
		 ORDER BY cd.week_no, cd.weekday, di.meal_slot, di.is_default DESC, di.sort_order, mi.name`, cycle)
	if err != nil {
		return m, fmt.Errorf("subscriptions: variety dishes: %w", err)
	}
	defer rows.Close()
	seen := map[[3]any]bool{}
	for rows.Next() {
		var d cycleDish
		if err := rows.Scan(&d.week, &d.weekday, &d.slot, &d.name, &d.nameAr); err != nil {
			return m, fmt.Errorf("subscriptions: scan variety dish: %w", err)
		}
		// The default dish of each slot is what a customer without a
		// preference gets, so it stands for that slot.
		key := [3]any{d.week, d.weekday, d.slot}
		if !seen[key] {
			seen[key] = true
			m.dishes = append(m.dishes, d)
		}
	}
	return m, rows.Err()
}

// weekDishes lists the dishes the kitchen cooks on the delivery days of the
// week starting at from.
func (m kitchenMenu) weekDishes(from time.Time, weekdays []int, slots []string) []VarietyDish {
	out := []VarietyDish{}
	if len(m.dishes) == 0 {
		return out
	}
	for i := 0; i < 7; i++ {
		day := from.AddDate(0, 0, i)
		wd := int(day.Weekday())
		if !containsInt(weekdays, wd) {
			continue
		}
		cw := kitchen.CycleWeek(m.anchor, m.weeks, day)
		for _, d := range m.dishes {
			if d.week == cw && d.weekday == wd && containsString(slots, d.slot) {
				out = append(out, VarietyDish{Weekday: wd, Name: d.name, NameAr: d.nameAr})
			}
		}
	}
	return out
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// rotationVariety scores the quote's weeks with each kitchen's dishes for the
// week it cooks. It is advisory: a failure is logged and the quote goes out
// without it.
func rotationVariety(ctx context.Context, q querier, plans []rotationPlan, quote RotationQuote,
	weekdays []int, slots []string) *Variety {
	byBranch := map[uuid.UUID]rotationPlan{}
	for _, p := range plans {
		byBranch[p.BranchID] = p
	}
	kitchens := make([]VarietyKitchen, 0, len(quote.Weeks))
	for _, w := range quote.Weeks {
		p := byBranch[w.BranchID]
		m, err := loadKitchenMenu(ctx, q, w.BranchID, p.MenuCycleID)
		if err != nil {
			slog.WarnContext(ctx, "rotation variety check failed", "error", err)
			return nil
		}
		from, err := time.Parse("2006-01-02", w.StartsOn)
		if err != nil {
			return nil
		}
		kitchens = append(kitchens, VarietyKitchen{
			Key: w.BranchID.String(), Cuisines: m.cuisines, Dishes: m.weekDishes(from, weekdays, slots),
		})
	}
	v := ScoreVariety(kitchens)
	return &v
}
