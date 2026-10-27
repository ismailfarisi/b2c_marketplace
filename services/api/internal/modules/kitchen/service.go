package kitchen

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
)

// The service type every plan created here sells. One-off meal boxes are a
// different product with their own catalogue path.
const planServiceType = "meals_subscription"

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// ── dishes ─────────────────────────────────────────────────────────────────

const dishColumns = `
	m.id, m.branch_id, m.name, m.name_ar, m.description, m.description_ar, m.image_url,
	m.calories, m.protein_g::float8, m.carbs_g::float8, m.fat_g::float8,
	m.allergens, m.tags, m.is_active,
	(SELECT count(*) FROM menu_day_items di
	   JOIN menu_cycle_days cd ON cd.id = di.cycle_day_id
	   JOIN menu_cycles c ON c.id = cd.cycle_id AND c.is_active
	  WHERE di.menu_item_id = m.id)::int`

func scanDish(row pgx.Row) (Dish, error) {
	var d Dish
	err := row.Scan(&d.ID, &d.BranchID, &d.Name, &d.NameAr, &d.Description, &d.DescriptionAr, &d.ImageURL,
		&d.Calories, &d.ProteinG, &d.CarbsG, &d.FatG,
		&d.Allergens, &d.Tags, &d.IsActive, &d.MenuSlots)
	return d, err
}

func (s *Service) ListDishes(ctx context.Context, branchID uuid.UUID) ([]Dish, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+dishColumns+`
		  FROM menu_items m WHERE m.branch_id = $1
		 ORDER BY m.is_active DESC, lower(m.name)`, branchID)
	if err != nil {
		return nil, fmt.Errorf("kitchen: list dishes: %w", err)
	}
	defer rows.Close()
	out := []Dish{}
	for rows.Next() {
		d, err := scanDish(rows)
		if err != nil {
			return nil, fmt.Errorf("kitchen: scan dish: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Service) dish(ctx context.Context, q pgx.Tx, branchID, dishID uuid.UUID) (Dish, error) {
	var row pgx.Row
	sql := `SELECT ` + dishColumns + ` FROM menu_items m WHERE m.id = $1 AND m.branch_id = $2`
	if q != nil {
		row = q.QueryRow(ctx, sql, dishID, branchID)
	} else {
		row = s.pool.QueryRow(ctx, sql, dishID, branchID)
	}
	d, err := scanDish(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Dish{}, httpx.ErrNotFound
	}
	if err != nil {
		return Dish{}, fmt.Errorf("kitchen: load dish: %w", err)
	}
	return d, nil
}

func (s *Service) CreateDish(ctx context.Context, branchID uuid.UUID, in DishInput) (Dish, error) {
	in, err := NormaliseDish(in)
	if err != nil {
		return Dish{}, err
	}
	active := in.IsActive == nil || *in.IsActive
	var id uuid.UUID
	err = s.pool.QueryRow(ctx, `
		INSERT INTO menu_items (branch_id, name, name_ar, description, description_ar, image_url,
		                        calories, protein_g, carbs_g, fat_g, allergens, tags, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id`,
		branchID, in.Name, in.NameAr, in.Description, in.DescriptionAr, in.ImageURL,
		in.Calories, in.ProteinG, in.CarbsG, in.FatG, in.Allergens, in.Tags, active).Scan(&id)
	if err != nil {
		return Dish{}, fmt.Errorf("kitchen: create dish: %w", err)
	}
	return s.dish(ctx, nil, branchID, id)
}

func (s *Service) UpdateDish(ctx context.Context, branchID, dishID uuid.UUID, in DishInput) (Dish, error) {
	in, err := NormaliseDish(in)
	if err != nil {
		return Dish{}, err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE menu_items SET
		    name = $3, name_ar = $4, description = $5, description_ar = $6, image_url = $7,
		    calories = $8, protein_g = $9, carbs_g = $10, fat_g = $11,
		    allergens = $12, tags = $13, is_active = COALESCE($14, is_active), updated_at = now()
		 WHERE id = $1 AND branch_id = $2`,
		dishID, branchID, in.Name, in.NameAr, in.Description, in.DescriptionAr, in.ImageURL,
		in.Calories, in.ProteinG, in.CarbsG, in.FatG, in.Allergens, in.Tags, in.IsActive)
	if err != nil {
		return Dish{}, fmt.Errorf("kitchen: update dish: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return Dish{}, httpx.ErrNotFound
	}
	return s.dish(ctx, nil, branchID, dishID)
}

// ── the weekly menu ────────────────────────────────────────────────────────

// weeklyCycle returns the branch's active one-week cycle, creating it the
// first time a kitchen edits its menu. Locked so two managers editing the grid
// at once cannot create two cycles.
func weeklyCycle(ctx context.Context, tx pgx.Tx, branchID uuid.UUID) (uuid.UUID, error) {
	// Serialise per branch: the cycle may not exist yet, so there is no row to
	// lock, and an advisory lock keyed on the branch is the row we do not have.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, branchID); err != nil {
		return uuid.Nil, fmt.Errorf("kitchen: lock menu: %w", err)
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT id FROM menu_cycles WHERE branch_id = $1 AND is_active
		 ORDER BY created_at LIMIT 1`, branchID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("kitchen: load cycle: %w", err)
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO menu_cycles (branch_id, name, name_ar, weeks, anchor_date)
		VALUES ($1, 'Weekly menu', 'القائمة الأسبوعية', 1,
		        current_date - extract(dow FROM current_date)::int)
		RETURNING id`, branchID).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("kitchen: create cycle: %w", err)
	}
	// Plans created before the menu existed point at nothing; point them here.
	if _, err := tx.Exec(ctx, `
		UPDATE subscription_plans SET menu_cycle_id = $2, updated_at = now()
		 WHERE branch_id = $1 AND menu_cycle_id IS NULL`, branchID, id); err != nil {
		return uuid.Nil, fmt.Errorf("kitchen: attach plans to cycle: %w", err)
	}
	return id, nil
}

func (s *Service) WeeklyMenu(ctx context.Context, branchID uuid.UUID) (WeeklyMenu, error) {
	menu := WeeklyMenu{Slots: []MenuSlot{}}
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM menu_cycles WHERE branch_id = $1 AND is_active
		 ORDER BY created_at LIMIT 1`, branchID).Scan(&menu.CycleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return menu, nil
	}
	if err != nil {
		return menu, fmt.Errorf("kitchen: load cycle: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT cd.weekday, di.meal_slot, di.is_default,
		       m.id, m.name, m.name_ar, m.allergens, m.calories, m.image_url, m.is_active
		  FROM menu_cycle_days cd
		  JOIN menu_day_items di ON di.cycle_day_id = cd.id
		  JOIN menu_items m ON m.id = di.menu_item_id
		 WHERE cd.cycle_id = $1 AND cd.week_no = 1
		 ORDER BY cd.weekday, di.meal_slot, di.is_default DESC, di.sort_order`, menu.CycleID.UUID)
	if err != nil {
		return menu, fmt.Errorf("kitchen: load menu: %w", err)
	}
	defer rows.Close()

	index := map[MenuGap]int{}
	for rows.Next() {
		var weekday int
		var slot string
		var isDefault bool
		var c MenuChoice
		if err := rows.Scan(&weekday, &slot, &isDefault,
			&c.DishID, &c.Name, &c.NameAr, &c.Allergens, &c.Calories, &c.ImageURL, &c.IsActive); err != nil {
			return menu, fmt.Errorf("kitchen: scan menu: %w", err)
		}
		key := MenuGap{weekday, slot}
		i, ok := index[key]
		if !ok {
			menu.Slots = append(menu.Slots, MenuSlot{Weekday: weekday, Slot: slot})
			i = len(menu.Slots) - 1
			index[key] = i
		}
		choice := c
		switch {
		case isDefault && menu.Slots[i].Default == nil:
			menu.Slots[i].Default = &choice
		case menu.Slots[i].Alternative == nil:
			menu.Slots[i].Alternative = &choice
		}
	}
	if err := rows.Err(); err != nil {
		return menu, err
	}
	slices.SortStableFunc(menu.Slots, func(a, b MenuSlot) int {
		if a.Weekday != b.Weekday {
			return a.Weekday - b.Weekday
		}
		return slices.Index(Slots, a.Slot) - slices.Index(Slots, b.Slot)
	})
	return menu, nil
}

// SetMenuSlot replaces one cell of the weekly grid. Both dishes must belong to
// the branch and be active: an archived dish is one the kitchen has stopped
// cooking.
func (s *Service) SetMenuSlot(ctx context.Context, branchID uuid.UUID, weekday int, slot string, def, alt uuid.NullUUID) (WeeklyMenu, error) {
	if err := ValidateMenuSlot(weekday, slot, def, alt); err != nil {
		return WeeklyMenu{}, err
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for field, id := range map[string]uuid.NullUUID{"default_dish_id": def, "alternative_dish_id": alt} {
			if !id.Valid {
				continue
			}
			var active bool
			err := tx.QueryRow(ctx, `SELECT is_active FROM menu_items WHERE id = $1 AND branch_id = $2`,
				id.UUID, branchID).Scan(&active)
			if errors.Is(err, pgx.ErrNoRows) {
				return &httpx.ValidationError{Fields: []httpx.FieldError{{Field: field, Code: "unknown_dish"}}}
			}
			if err != nil {
				return fmt.Errorf("kitchen: check dish: %w", err)
			}
			if !active {
				return &httpx.ValidationError{Fields: []httpx.FieldError{{
					Field: field, Code: "dish_archived", Message: "restore the dish before putting it on the menu"}}}
			}
		}

		cycleID, err := weeklyCycle(ctx, tx, branchID)
		if err != nil {
			return err
		}
		var dayID uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO menu_cycle_days (cycle_id, week_no, weekday) VALUES ($1, 1, $2)
			ON CONFLICT (cycle_id, week_no, weekday) DO UPDATE SET weekday = EXCLUDED.weekday
			RETURNING id`, cycleID, weekday).Scan(&dayID)
		if err != nil {
			return fmt.Errorf("kitchen: upsert day: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM menu_day_items WHERE cycle_day_id = $1 AND meal_slot = $2`,
			dayID, slot); err != nil {
			return fmt.Errorf("kitchen: clear slot: %w", err)
		}
		if def.Valid {
			if _, err := tx.Exec(ctx, `
				INSERT INTO menu_day_items (cycle_day_id, menu_item_id, meal_slot, is_default, sort_order)
				VALUES ($1, $2, $3, true, 0)`, dayID, def.UUID, slot); err != nil {
				return fmt.Errorf("kitchen: set default: %w", err)
			}
		}
		if alt.Valid {
			if _, err := tx.Exec(ctx, `
				INSERT INTO menu_day_items (cycle_day_id, menu_item_id, meal_slot, is_default, sort_order)
				VALUES ($1, $2, $3, false, 1)`, dayID, alt.UUID, slot); err != nil {
				return fmt.Errorf("kitchen: set alternative: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return WeeklyMenu{}, err
	}
	return s.WeeklyMenu(ctx, branchID)
}

// ── plans ──────────────────────────────────────────────────────────────────

const planColumns = `
	p.id, p.branch_id, p.name, p.name_ar, p.description, p.description_ar, p.image_url,
	p.cycle_days, p.days_per_week, p.meals_per_day, p.meal_slots, p.delivery_weekdays,
	p.allowed_pause_days, p.cutoff_hours, p.price_minor, p.currency, p.tax_rate_bp,
	p.rotation_enabled, p.rotation_week_price_minor, p.is_active,
	(SELECT count(*) FROM subscriptions s
	  WHERE s.plan_id = p.id AND s.status IN ('active', 'paused', 'suspended', 'pending_payment'))::int`

func scanPlan(row pgx.Row) (Plan, error) {
	var p Plan
	var weekdays []int16
	err := row.Scan(&p.ID, &p.BranchID, &p.Name, &p.NameAr, &p.Description, &p.DescriptionAr, &p.ImageURL,
		&p.CycleDays, &p.DaysPerWeek, &p.MealsPerDay, &p.MealSlots, &weekdays,
		&p.AllowedPauseDays, &p.CutoffHours, &p.PriceMinor, &p.Currency, &p.TaxRateBP,
		&p.RotationEnabled, &p.RotationWeekPriceMinor, &p.IsActive, &p.ActiveSubscribers)
	p.DeliveryWeekdays = make([]int, len(weekdays))
	for i, d := range weekdays {
		p.DeliveryWeekdays[i] = int(d)
	}
	p.Tiers = []Tier{}
	return p, err
}

func (s *Service) loadTiers(ctx context.Context, plans []Plan) error {
	if len(plans) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(plans))
	at := map[uuid.UUID]int{}
	for i, p := range plans {
		ids[i] = p.ID
		at[p.ID] = i
	}
	rows, err := s.pool.Query(ctx, `
		SELECT plan_id, id, key, name, name_ar, days_per_week, meals_per_day, portion_size,
		       price_minor, sort_order, is_active
		  FROM plan_tiers WHERE plan_id = ANY($1)
		 ORDER BY sort_order, key`, ids)
	if err != nil {
		return fmt.Errorf("kitchen: load tiers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var planID uuid.UUID
		var t Tier
		var days, meals *int16
		if err := rows.Scan(&planID, &t.ID, &t.Key, &t.Name, &t.NameAr, &days, &meals, &t.PortionSize,
			&t.PriceMinor, &t.SortOrder, &t.IsActive); err != nil {
			return fmt.Errorf("kitchen: scan tier: %w", err)
		}
		if days != nil {
			v := int(*days)
			t.DaysPerWeek = &v
		}
		if meals != nil {
			v := int(*meals)
			t.MealsPerDay = &v
		}
		plans[at[planID]].Tiers = append(plans[at[planID]].Tiers, t)
	}
	return rows.Err()
}

func (s *Service) ListPlans(ctx context.Context, branchID uuid.UUID) ([]Plan, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+planColumns+`
		  FROM subscription_plans p
		 WHERE p.branch_id = $1 AND p.service_type = $2
		 ORDER BY p.is_active DESC, p.price_minor`, branchID, planServiceType)
	if err != nil {
		return nil, fmt.Errorf("kitchen: list plans: %w", err)
	}
	defer rows.Close()
	out := []Plan{}
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, fmt.Errorf("kitchen: scan plan: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, s.loadTiers(ctx, out)
}

func (s *Service) GetPlan(ctx context.Context, branchID, planID uuid.UUID) (Plan, error) {
	p, err := scanPlan(s.pool.QueryRow(ctx, `SELECT `+planColumns+`
		  FROM subscription_plans p WHERE p.id = $1 AND p.branch_id = $2 AND p.service_type = $3`,
		planID, branchID, planServiceType))
	if errors.Is(err, pgx.ErrNoRows) {
		return Plan{}, httpx.ErrNotFound
	}
	if err != nil {
		return Plan{}, fmt.Errorf("kitchen: load plan: %w", err)
	}
	plans := []Plan{p}
	if err := s.loadTiers(ctx, plans); err != nil {
		return Plan{}, err
	}
	return plans[0], nil
}

func weekdaysParam(days []int) []int16 {
	out := make([]int16, len(days))
	for i, d := range days {
		out[i] = int16(d)
	}
	return out
}

func (s *Service) CreatePlan(ctx context.Context, branchID uuid.UUID, in PlanInput) (Plan, error) {
	in, err := NormalisePlan(in)
	if err != nil {
		return Plan{}, err
	}
	active := in.IsActive == nil || *in.IsActive
	var id uuid.UUID
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		cycleID, err := weeklyCycle(ctx, tx, branchID)
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `
			INSERT INTO subscription_plans (
			    branch_id, service_type, menu_cycle_id, name, name_ar, description, description_ar, image_url,
			    cycle_days, days_per_week, meals_per_day, meal_slots, delivery_weekdays,
			    allowed_pause_days, cutoff_hours, price_minor, currency,
			    rotation_enabled, rotation_week_price_minor, is_active)
			SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
			       v.default_currency, $17, $18, $19
			  FROM vendor_branches b JOIN vendors v ON v.id = b.vendor_id
			 WHERE b.id = $1
			RETURNING id`,
			branchID, planServiceType, cycleID, in.Name, in.NameAr, in.Description, in.DescriptionAr, in.ImageURL,
			in.CycleDays, in.DaysPerWeek, len(in.MealSlots), in.MealSlots, weekdaysParam(in.DeliveryWeekdays),
			in.AllowedPauseDays, in.CutoffHours, in.PriceMinor,
			in.RotationEnabled, in.RotationWeekPriceMinor, active).Scan(&id)
		if err != nil {
			return fmt.Errorf("kitchen: create plan: %w", err)
		}
		return writeTiers(ctx, tx, id, in.Tiers)
	})
	if err != nil {
		return Plan{}, err
	}
	return s.GetPlan(ctx, branchID, id)
}

// ErrPlanShapeLocked is returned when a change would alter what existing
// subscribers already chose their schedule against.
var ErrPlanShapeLocked = httpx.Coded(httpx.ErrConflict, "plan_has_subscribers",
	"This plan has subscribers, so its days, meals and cycle length cannot change. Create a new plan instead.")

func (s *Service) UpdatePlan(ctx context.Context, branchID, planID uuid.UUID, in PlanInput) (Plan, error) {
	in, err := NormalisePlan(in)
	if err != nil {
		return Plan{}, err
	}
	current, err := s.GetPlan(ctx, branchID, planID)
	if err != nil {
		return Plan{}, err
	}
	// A subscriber's schedule rules were validated against days_per_week and
	// meals_per_day. Changing either under them makes every renewal of theirs
	// fail validation, or worse, bill a shape they never picked.
	if current.ActiveSubscribers > 0 && (current.CycleDays != in.CycleDays ||
		current.DaysPerWeek != in.DaysPerWeek || !slices.Equal(current.MealSlots, in.MealSlots)) {
		return Plan{}, ErrPlanShapeLocked
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE subscription_plans SET
			    name = $3, name_ar = $4, description = $5, description_ar = $6, image_url = $7,
			    cycle_days = $8, days_per_week = $9, meals_per_day = $10, meal_slots = $11,
			    delivery_weekdays = $12, allowed_pause_days = $13, cutoff_hours = $14,
			    price_minor = $15, rotation_enabled = $16, rotation_week_price_minor = $17,
			    is_active = COALESCE($18, is_active), updated_at = now()
			 WHERE id = $1 AND branch_id = $2`,
			planID, branchID, in.Name, in.NameAr, in.Description, in.DescriptionAr, in.ImageURL,
			in.CycleDays, in.DaysPerWeek, len(in.MealSlots), in.MealSlots,
			weekdaysParam(in.DeliveryWeekdays), in.AllowedPauseDays, in.CutoffHours,
			in.PriceMinor, in.RotationEnabled, in.RotationWeekPriceMinor, in.IsActive)
		if err != nil {
			return fmt.Errorf("kitchen: update plan: %w", err)
		}
		return writeTiers(ctx, tx, planID, in.Tiers)
	})
	if err != nil {
		return Plan{}, mapConstraint(err)
	}
	return s.GetPlan(ctx, branchID, planID)
}

// writeTiers makes the plan's active tiers exactly `tiers`, keyed by `key`.
// Tiers that disappear are deactivated rather than deleted: a subscription
// row still references the tier its customer bought.
func writeTiers(ctx context.Context, tx pgx.Tx, planID uuid.UUID, tiers []TierInput) error {
	keys := make([]string, 0, len(tiers))
	for i, t := range tiers {
		keys = append(keys, t.Key)
		var days, meals *int16
		if t.DaysPerWeek != nil {
			v := int16(*t.DaysPerWeek)
			days = &v
		}
		if t.MealsPerDay != nil {
			v := int16(*t.MealsPerDay)
			meals = &v
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO plan_tiers (plan_id, key, name, name_ar, days_per_week, meals_per_day,
			                        portion_size, price_minor, sort_order, is_active)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, true)
			ON CONFLICT (plan_id, key) DO UPDATE SET
			    name = EXCLUDED.name, name_ar = EXCLUDED.name_ar,
			    days_per_week = EXCLUDED.days_per_week, meals_per_day = EXCLUDED.meals_per_day,
			    portion_size = EXCLUDED.portion_size, price_minor = EXCLUDED.price_minor,
			    sort_order = EXCLUDED.sort_order, is_active = true`,
			planID, t.Key, t.Name, t.NameAr, days, meals, t.PortionSize, t.PriceMinor, i); err != nil {
			return fmt.Errorf("kitchen: write tier: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE plan_tiers SET is_active = false
		 WHERE plan_id = $1 AND NOT (key = ANY($2))`, planID, keys); err != nil {
		return fmt.Errorf("kitchen: retire tiers: %w", err)
	}
	return nil
}

// mapConstraint turns a CHECK violation the validator should have caught into
// a 422 rather than a 500, so a gap between the two is a bad request and not
// an outage.
func mapConstraint(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23514" {
		return &httpx.ValidationError{Fields: []httpx.FieldError{{Field: pg.ConstraintName, Code: "invalid"}}}
	}
	return err
}

// ── profile ────────────────────────────────────────────────────────────────

func (s *Service) Profile(ctx context.Context, branchID uuid.UUID) (Profile, error) {
	var p Profile
	err := s.pool.QueryRow(ctx, `
		SELECT b.id, v.id, v.trade_name, v.status, b.name, b.name_ar,
		       b.kitchen_description, b.kitchen_description_ar, b.cuisines, b.dietary_tags,
		       b.cover_image_url, b.phone, b.address_line, b.city, b.emirate, b.timezone,
		       v.default_currency, b.is_active, b.service_area IS NOT NULL
		  FROM vendor_branches b JOIN vendors v ON v.id = b.vendor_id
		 WHERE b.id = $1 AND b.deleted_at IS NULL`, branchID).Scan(
		&p.BranchID, &p.VendorID, &p.VendorName, &p.VendorStatus, &p.Name, &p.NameAr,
		&p.Description, &p.DescriptionAr, &p.Cuisines, &p.DietaryTags,
		&p.CoverImageURL, &p.Phone, &p.AddressLine, &p.City, &p.Emirate, &p.Timezone,
		&p.Currency, &p.IsActive, &p.HasServiceArea)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, httpx.ErrNotFound
	}
	if err != nil {
		return Profile{}, fmt.Errorf("kitchen: load profile: %w", err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, label, to_char(starts_at, 'HH24:MI'), to_char(ends_at, 'HH24:MI')
		  FROM time_windows WHERE branch_id = $1 AND is_active ORDER BY sort_order, starts_at`, branchID)
	if err != nil {
		return Profile{}, fmt.Errorf("kitchen: load windows: %w", err)
	}
	defer rows.Close()
	p.DeliveryWindows = []Window{}
	for rows.Next() {
		var w Window
		if err := rows.Scan(&w.ID, &w.Label, &w.StartsAt, &w.EndsAt); err != nil {
			return Profile{}, fmt.Errorf("kitchen: scan window: %w", err)
		}
		p.DeliveryWindows = append(p.DeliveryWindows, w)
	}
	return p, rows.Err()
}

func (s *Service) UpdateProfile(ctx context.Context, branchID uuid.UUID, in ProfileInput) (Profile, error) {
	in, err := NormaliseProfile(in)
	if err != nil {
		return Profile{}, err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE vendor_branches SET
		    name = $2, name_ar = $3, kitchen_description = $4, kitchen_description_ar = $5,
		    cuisines = $6, dietary_tags = $7, cover_image_url = $8, phone = $9, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL`,
		branchID, in.Name, in.NameAr, in.Description, in.DescriptionAr,
		in.Cuisines, in.DietaryTags, in.CoverImageURL, in.Phone)
	if err != nil {
		return Profile{}, fmt.Errorf("kitchen: update profile: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return Profile{}, httpx.ErrNotFound
	}
	return s.Profile(ctx, branchID)
}

// ── overview ───────────────────────────────────────────────────────────────

func (s *Service) Overview(ctx context.Context, branchID uuid.UUID) (Overview, error) {
	var o Overview
	err := s.pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM subscriptions WHERE branch_id = $1 AND status = 'active')::int,
		  (SELECT count(*) FROM subscriptions WHERE branch_id = $1 AND status = 'paused')::int,
		  (SELECT count(*) FROM subscriptions WHERE branch_id = $1 AND status = 'pending_payment')::int,
		  (SELECT count(*) FROM subscription_plans WHERE branch_id = $1 AND is_active
		      AND service_type = $2)::int,
		  (SELECT count(*) FROM subscription_plans WHERE branch_id = $1 AND is_active
		      AND service_type = $2 AND rotation_enabled)::int,
		  (SELECT count(*) FROM menu_items WHERE branch_id = $1 AND is_active)::int,
		  (SELECT kitchen_description IS NOT NULL AND cardinality(cuisines) > 0
		     FROM vendor_branches WHERE id = $1)`,
		branchID, planServiceType).Scan(
		&o.ActiveSubscribers, &o.PausedSubscribers, &o.PendingPayment,
		&o.ActivePlans, &o.RotationPlans, &o.ActiveDishes, &o.ProfileComplete)
	if err != nil {
		return o, fmt.Errorf("kitchen: overview: %w", err)
	}
	plans, err := s.ListPlans(ctx, branchID)
	if err != nil {
		return o, err
	}
	menu, err := s.WeeklyMenu(ctx, branchID)
	if err != nil {
		return o, err
	}
	o.MenuGaps = MenuGaps(plans, menu)
	return o, nil
}
