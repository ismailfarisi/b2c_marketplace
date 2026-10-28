package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/platform/postgres"
)

// UpcomingHorizon is how far ahead a customer sees their deliveries and can
// pick a dish. Matches the scheduler's materialisation window.
const UpcomingHorizon = 14 * 24 * time.Hour

// MySubscription is one row of the customer's "My subscriptions" list.
type MySubscription struct {
	ID             uuid.UUID `json:"id"`
	SubscriptionNo string    `json:"subscription_no"`
	Status         string    `json:"status"`
	BranchID       uuid.UUID `json:"branch_id"`
	KitchenName    string    `json:"kitchen_name"`
	KitchenNameAr  *string   `json:"kitchen_name_ar"`
	PlanName       string    `json:"plan_name"`
	PlanNameAr     *string   `json:"plan_name_ar"`
	StartDate      string    `json:"start_date"`
	EndDate        *string   `json:"end_date"`
	CycleNo        int       `json:"cycle_no"`
	CycleEndsOn    *string   `json:"cycle_ends_on"`
	AmountMinor    int64     `json:"amount_minor"`
	Currency       string    `json:"currency"`
	// The billing order still waiting to be paid, if any. The client pays it
	// with the ordinary payment-intent call.
	PendingOrderID *uuid.UUID `json:"pending_order_id"`
	// Set when this subscription is one week of a four-kitchen rotation.
	RotationID   *uuid.UUID `json:"rotation_id"`
	RotationWeek *int       `json:"rotation_week"`
}

// Dish is one option a customer can be served on a given day.
type Dish struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	NameAr    *string   `json:"name_ar"`
	Allergens []string  `json:"allergens"`
	Calories  *int      `json:"calories"`
	IsDefault bool      `json:"is_default"`
}

// UpcomingMeal is one scheduled meal and what will be cooked for it.
type UpcomingMeal struct {
	Date        string     `json:"date"`
	Weekday     int        `json:"weekday"`
	Slot        string     `json:"meal_slot"`
	WindowLabel string     `json:"window_label"`
	Options     []Dish     `json:"options"`
	ChosenID    *uuid.UUID `json:"chosen_dish_id"`
	ChosenBy    string     `json:"chosen_by"`
	CutoffAt    time.Time  `json:"cutoff_at"`
	Locked      bool       `json:"locked"`
}

// MyCycle is one billing period as the customer sees it.
type MyCycle struct {
	No          int    `json:"cycle_no"`
	StartsOn    string `json:"starts_on"`
	EndsOn      string `json:"ends_on"`
	Status      string `json:"status"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}

// MyPause is a date range the customer is away.
type MyPause struct {
	From string `json:"from_date"`
	To   string `json:"to_date"`
}

// MyDetail is the customer's view of one of their subscriptions.
type MyDetail struct {
	MySubscription
	CutoffHours    int            `json:"cutoff_hours"`
	PauseAllowance int            `json:"pause_allowance_days"`
	PauseDaysUsed  int            `json:"pause_days_used"`
	Upcoming       []UpcomingMeal `json:"upcoming"`
	Cycles         []MyCycle      `json:"cycles"`
	Pauses         []MyPause      `json:"pauses"`
}

const mySelect = `
	SELECT s.id, s.subscription_no, s.status, s.branch_id, b.name, b.name_ar,
	       p.name, p.name_ar, s.start_date, s.end_date,
	       COALESCE(c.cycle_no, 0), c.ends_on, COALESCE(c.amount_minor, 0),
	       COALESCE(c.currency, p.currency),
	       CASE WHEN c.status IN ('pending', 'past_due') THEN c.order_id END,
	       s.rotation_id, s.rotation_week
	  FROM subscriptions s
	  JOIN vendor_branches b ON b.id = s.branch_id
	  JOIN subscription_plans p ON p.id = s.plan_id
	  LEFT JOIN LATERAL (
	       SELECT cycle_no, ends_on, amount_minor, currency, status, order_id
	         FROM subscription_cycles WHERE subscription_id = s.id
	        ORDER BY cycle_no DESC LIMIT 1) c ON true`

func scanMine(row pgx.Row) (MySubscription, error) {
	var m MySubscription
	var start time.Time
	var end, cycleEnd *time.Time
	if err := row.Scan(&m.ID, &m.SubscriptionNo, &m.Status, &m.BranchID, &m.KitchenName,
		&m.KitchenNameAr, &m.PlanName, &m.PlanNameAr, &start, &end,
		&m.CycleNo, &cycleEnd, &m.AmountMinor, &m.Currency, &m.PendingOrderID,
		&m.RotationID, &m.RotationWeek); err != nil {
		return m, err
	}
	m.StartDate = start.Format("2006-01-02")
	m.EndDate = datePtr(end)
	m.CycleEndsOn = datePtr(cycleEnd)
	return m, nil
}

func datePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format("2006-01-02")
	return &s
}

// Mine lists the customer's subscriptions, newest first.
func (s *Service) Mine(ctx context.Context, userID uuid.UUID) ([]MySubscription, error) {
	rows, err := s.pool.Query(ctx, mySelect+` WHERE s.user_id = $1 ORDER BY s.created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("subscriptions: list mine: %w", err)
	}
	defer rows.Close()
	out := []MySubscription{}
	for rows.Next() {
		m, err := scanMine(rows)
		if err != nil {
			return nil, fmt.Errorf("subscriptions: scan mine: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MyDetail is one subscription the customer owns, with the next two weeks of
// meals and the dish options for each. Someone else's subscription is 404.
func (s *Service) MyDetail(ctx context.Context, userID, subID uuid.UUID, now time.Time) (MyDetail, error) {
	var d MyDetail
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		live, err := s.loadOwned(ctx, tx, userID, subID)
		if err != nil {
			return err
		}
		d.MySubscription, err = scanMine(tx.QueryRow(ctx, mySelect+` WHERE s.id = $1`, subID))
		if err != nil {
			return fmt.Errorf("subscriptions: load mine: %w", err)
		}
		d.CutoffHours = live.CutoffHours
		if d.PauseAllowance, d.PauseDaysUsed, err = s.pauseAllowance(ctx, tx, subID); err != nil {
			return err
		}
		if d.Cycles, err = myCycles(ctx, tx, subID); err != nil {
			return err
		}
		d.Pauses = []MyPause{}
		for _, p := range live.Pauses {
			if !p.Inactive && !p.To.Before(dayIn(now, live.Location)) {
				d.Pauses = append(d.Pauses, MyPause{p.From.Format("2006-01-02"), p.To.Format("2006-01-02")})
			}
		}
		d.Upcoming = []UpcomingMeal{}
		if live.Status != "active" && live.Status != "pending_payment" {
			return nil
		}
		d.Upcoming, err = s.upcoming(ctx, tx, live, dayIn(now, live.Location), dayIn(now, live.Location).Add(UpcomingHorizon), now)
		return err
	})
	return d, err
}

func myCycles(ctx context.Context, tx pgx.Tx, subID uuid.UUID) ([]MyCycle, error) {
	rows, err := tx.Query(ctx, `
		SELECT cycle_no, starts_on, ends_on, status, amount_minor, currency
		  FROM subscription_cycles WHERE subscription_id = $1 ORDER BY cycle_no DESC`, subID)
	if err != nil {
		return nil, fmt.Errorf("subscriptions: load cycles: %w", err)
	}
	defer rows.Close()
	out := []MyCycle{}
	for rows.Next() {
		var c MyCycle
		var from, to time.Time
		if err := rows.Scan(&c.No, &from, &to, &c.Status, &c.AmountMinor, &c.Currency); err != nil {
			return nil, fmt.Errorf("subscriptions: scan cycle: %w", err)
		}
		c.StartsOn, c.EndsOn = from.Format("2006-01-02"), to.Format("2006-01-02")
		out = append(out, c)
	}
	return out, rows.Err()
}

type menuKey struct {
	weekday int
	slot    string
}

// upcoming plans the meals between from and to and attaches each day's dish
// options and the customer's pick.
func (s *Service) upcoming(ctx context.Context, tx pgx.Tx, live *Live, from, to, now time.Time) ([]UpcomingMeal, error) {
	sub := live.Subscription
	// An unpaid subscription is suspended for the scheduler, but the customer
	// still previews, and may pick, the meals it will bring.
	if live.Status == "pending_payment" {
		sub.Suspended = false
	}
	occ := Plan(sub, live.Rules, live.Pauses, live.Skips, from, to, now)
	out := []UpcomingMeal{}
	if len(occ) == 0 {
		return out, nil
	}
	menu, err := branchMenu(ctx, tx, live.BranchID)
	if err != nil {
		return nil, err
	}
	windows, err := windowsByID(ctx, tx, live.BranchID)
	if err != nil {
		return nil, err
	}

	type pick struct {
		id *uuid.UUID
		by string
	}
	picks := map[string]pick{}
	rows, err := tx.Query(ctx, `
		SELECT service_date, meal_slot, menu_item_id, chosen_by
		  FROM subscription_meal_selections
		 WHERE subscription_id = $1 AND service_date BETWEEN $2 AND $3`,
		live.ID, from, to)
	if err != nil {
		return nil, fmt.Errorf("subscriptions: load selections: %w", err)
	}
	for rows.Next() {
		var day time.Time
		var slot, by string
		var id *uuid.UUID
		if err := rows.Scan(&day, &slot, &id, &by); err != nil {
			rows.Close()
			return nil, fmt.Errorf("subscriptions: scan selection: %w", err)
		}
		picks[day.Format("2006-01-02")+"/"+slot] = pick{id, by}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, o := range occ {
		date := o.Date.Format("2006-01-02")
		w := windows[o.WindowID]
		cutoff := CutoffAt(o.Date, live.CutoffHours, live.Location)
		m := UpcomingMeal{
			Date: date, Weekday: int(o.Date.Weekday()), Slot: o.Slot,
			WindowLabel: w.label, Options: menu[menuKey{int(o.Date.Weekday()), o.Slot}],
			ChosenBy: "default", CutoffAt: cutoff, Locked: !now.Before(cutoff),
		}
		if m.Options == nil {
			m.Options = []Dish{}
		}
		if p, ok := picks[date+"/"+o.Slot]; ok && p.id != nil {
			m.ChosenID, m.ChosenBy = p.id, p.by
		}
		out = append(out, m)
	}
	return out, nil
}

// CutoffAt is the last moment a meal can still be changed: the plan's cutoff
// hours before the start of the delivery day, in the kitchen's timezone. It is
// the same deadline CanModify enforces for skips and pauses.
func CutoffAt(day time.Time, cutoffHours int, loc *time.Location) time.Time {
	return dayIn(day, loc).Add(-time.Duration(cutoffHours) * time.Hour)
}

// branchMenu is the kitchen's active weekly menu, active dishes only, default
// first. Mirrors kitchen.WeeklyMenu without importing it.
func branchMenu(ctx context.Context, tx pgx.Tx, branchID uuid.UUID) (map[menuKey][]Dish, error) {
	rows, err := tx.Query(ctx, `
		SELECT cd.weekday, di.meal_slot, di.is_default,
		       m.id, m.name, m.name_ar, m.allergens, m.calories
		  FROM menu_cycles c
		  JOIN menu_cycle_days cd ON cd.cycle_id = c.id AND cd.week_no = 1
		  JOIN menu_day_items di ON di.cycle_day_id = cd.id
		  JOIN menu_items m ON m.id = di.menu_item_id AND m.is_active
		 WHERE c.id = (SELECT id FROM menu_cycles
		                WHERE branch_id = $1 AND is_active ORDER BY created_at LIMIT 1)
		 ORDER BY cd.weekday, di.meal_slot, di.is_default DESC, di.sort_order`, branchID)
	if err != nil {
		return nil, fmt.Errorf("subscriptions: load menu: %w", err)
	}
	defer rows.Close()
	out := map[menuKey][]Dish{}
	for rows.Next() {
		var k menuKey
		var d Dish
		if err := rows.Scan(&k.weekday, &k.slot, &d.IsDefault,
			&d.ID, &d.Name, &d.NameAr, &d.Allergens, &d.Calories); err != nil {
			return nil, fmt.Errorf("subscriptions: scan menu: %w", err)
		}
		if d.Allergens == nil {
			d.Allergens = []string{}
		}
		out[k] = append(out[k], d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// An inactive default leaves the alternative standing in for it.
	for k, ds := range out {
		if len(ds) > 0 && !ds[0].IsDefault {
			ds[0].IsDefault = true
			out[k] = ds
		}
	}
	return out, nil
}

type window struct {
	label string
}

func windowsByID(ctx context.Context, tx pgx.Tx, branchID uuid.UUID) (map[uuid.UUID]window, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, label FROM time_windows WHERE branch_id = $1`, branchID)
	if err != nil {
		return nil, fmt.Errorf("subscriptions: load windows: %w", err)
	}
	defer rows.Close()
	out := map[uuid.UUID]window{}
	for rows.Next() {
		var id uuid.UUID
		var w window
		if err := rows.Scan(&id, &w.label); err != nil {
			return nil, fmt.Errorf("subscriptions: scan window: %w", err)
		}
		out[id] = w
	}
	return out, rows.Err()
}

// ChooseMeal records the customer's pick between a day's default dish and its
// alternative. Only for a meal that is actually scheduled, only a dish on that
// day's menu, and only before the cutoff.
func (s *Service) ChooseMeal(ctx context.Context, userID, subID uuid.UUID, date time.Time, slot string, dishID uuid.UUID, now time.Time) (UpcomingMeal, error) {
	var out UpcomingMeal
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		live, err := s.loadOwned(ctx, tx, userID, subID)
		if err != nil {
			return err
		}
		if live.Status != "active" && live.Status != "pending_payment" {
			return httpx.Coded(httpx.ErrConflict, "not_active", "This subscription is not active.")
		}
		day := dayIn(date, live.Location)
		meals, err := s.upcoming(ctx, tx, live, day, day.AddDate(0, 0, 1).Add(-time.Second), now)
		if err != nil {
			return err
		}
		var meal *UpcomingMeal
		for i := range meals {
			if meals[i].Date == day.Format("2006-01-02") && meals[i].Slot == slot {
				meal = &meals[i]
			}
		}
		if meal == nil {
			return fmt.Errorf("%w: no %s delivery on that day", httpx.ErrNotFound, slot)
		}
		if meal.Locked {
			return httpx.Coded(httpx.ErrConflict, "cutoff_passed",
				"It is too late to change this meal.")
		}
		var dish *Dish
		for i := range meal.Options {
			if meal.Options[i].ID == dishID {
				dish = &meal.Options[i]
			}
		}
		if dish == nil {
			return &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "dish_id", Code: "not_on_menu",
				Message: "that dish is not on the menu for this meal",
			}}}
		}

		if _, err := tx.Exec(ctx, `SELECT create_meal_selection_partition($1)`, day); err != nil {
			return fmt.Errorf("subscriptions: selection partition: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO subscription_meal_selections (
				subscription_id, service_date, meal_slot, menu_item_id,
				item_name_snapshot, item_name_ar_snapshot, fulfillment_id, chosen_by
			) VALUES ($1, $2, $3, $4, $5, $6,
			          (SELECT id FROM fulfillments
			            WHERE subscription_id = $1 AND scheduled_date = $2
			              AND split_part(COALESCE(payload_ref, ''), ':', 1) = $3
			            LIMIT 1),
			          'customer')
			ON CONFLICT (subscription_id, service_date, meal_slot) DO UPDATE
			   SET menu_item_id = EXCLUDED.menu_item_id,
			       item_name_snapshot = EXCLUDED.item_name_snapshot,
			       item_name_ar_snapshot = EXCLUDED.item_name_ar_snapshot,
			       fulfillment_id = COALESCE(subscription_meal_selections.fulfillment_id, EXCLUDED.fulfillment_id),
			       chosen_by = 'customer', updated_at = now()`,
			subID, day, slot, dish.ID, dish.Name, dish.NameAr); err != nil {
			return fmt.Errorf("subscriptions: save selection: %w", err)
		}
		if err := s.repo.RecordEvent(ctx, tx, subID, "meal_chosen", "", "", "customer",
			uuid.NullUUID{UUID: userID, Valid: true},
			mustJSON(map[string]any{"date": meal.Date, "meal_slot": slot, "dish_id": dish.ID})); err != nil {
			return err
		}
		meal.ChosenID, meal.ChosenBy = &dish.ID, "customer"
		out = *meal
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return out, httpx.ErrNotFound
	}
	return out, err
}
