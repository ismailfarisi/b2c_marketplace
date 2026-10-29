package kitchen

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/httpx"
)

// PublicKitchen is one card on the customer storefront.
//
// Only kitchens a customer can actually subscribe to are listed: the vendor is
// approved, the branch is open, and at least one plan is on sale. A kitchen
// with no live plan has nothing to sell, and showing it trains customers that
// cards are not clickable.
type PublicKitchen struct {
	BranchID       uuid.UUID `json:"branch_id"`
	VendorID       uuid.UUID `json:"vendor_id"`
	Name           string    `json:"name"`
	NameAr         *string   `json:"name_ar"`
	VendorName     string    `json:"vendor_name"`
	VendorNameAr   *string   `json:"vendor_name_ar"`
	Description    *string   `json:"description"`
	DescriptionAr  *string   `json:"description_ar"`
	Cuisines       []string  `json:"cuisines"`
	DietaryTags    []string  `json:"dietary_tags"`
	CoverImageURL  *string   `json:"cover_image_url"`
	City           *string   `json:"city"`
	Rating         *float64  `json:"rating"`
	RatingCount    int       `json:"rating_count"`
	Currency       string    `json:"currency"`
	MealSlots      []string  `json:"meal_slots"`
	Plans          int       `json:"plans"`
	FromPriceMinor int64     `json:"from_price_minor"`
	// The cheapest price of one meal across the kitchen's plans: a cycle's
	// price over the meals it delivers.
	FromMealPriceMinor int64 `json:"from_meal_price_minor"`
	// The cheapest weekly price this kitchen charges inside a 4-kitchen
	// rotation, or nil when none of its plans has joined the rotation.
	RotationWeekPriceMinor *int64 `json:"rotation_week_price_minor"`
	// Staff checked a current food-safety licence for this kitchen.
	FoodSafetyVerified bool `json:"food_safety_verified"`
	// Up to four dishes from this week's menu, photographed ones first, so a
	// card shows food rather than only a kitchen name.
	WeekDishes []DishThumb `json:"week_dishes"`
}

// DishThumb is a dish shown as a thumbnail on a kitchen card.
type DishThumb struct {
	Name     string  `json:"name"`
	NameAr   *string `json:"name_ar"`
	ImageURL *string `json:"image_url"`
}

// DiscoverFilter narrows the storefront. A location, when given, is a hard
// filter: a kitchen that cannot deliver to the customer is not a result, it is
// a disappointment at checkout.
type DiscoverFilter struct {
	Lat, Lng *float64
	Query    string
	Slot     string
	Cuisine  string
	Dietary  string
	Rotation bool
}

const publicKitchenColumns = `
	b.id, v.id, b.name, b.name_ar, v.trade_name, v.trade_name_ar,
	b.kitchen_description, b.kitchen_description_ar, b.cuisines, b.dietary_tags,
	b.cover_image_url, b.city, b.rating::float8, b.rating_count, v.default_currency,
	p.slots, p.plans, p.from_price, p.rotation_price, p.meal_price,
	(b.food_safety_verified_at IS NOT NULL AND b.food_safety_expires_on >= CURRENT_DATE)`

// livePlans aggregates the plans on sale per branch. Joined LATERAL so a
// branch with none drops out of the result rather than showing zeroes.
const livePlans = `
	JOIN LATERAL (
		SELECT count(*)::int AS plans,
		       min(sp.price_minor) AS from_price,
		       min(sp.rotation_week_price_minor) FILTER (WHERE sp.rotation_enabled) AS rotation_price,
		       min(round(sp.price_minor::numeric / greatest(1,
		           round(sp.cycle_days * sp.days_per_week / 7.0) * sp.meals_per_day)))::bigint AS meal_price,
		       COALESCE(array(SELECT DISTINCT s FROM subscription_plans x, unnest(x.meal_slots) s
		                       WHERE x.branch_id = b.id AND x.service_type = $1 AND x.is_active
		                       ORDER BY s), '{}') AS slots
		  FROM subscription_plans sp
		 WHERE sp.branch_id = b.id AND sp.service_type = $1 AND sp.is_active
		HAVING count(*) > 0
	) p ON true`

func scanPublicKitchen(row pgx.Row) (PublicKitchen, error) {
	var k PublicKitchen
	err := row.Scan(&k.BranchID, &k.VendorID, &k.Name, &k.NameAr, &k.VendorName, &k.VendorNameAr,
		&k.Description, &k.DescriptionAr, &k.Cuisines, &k.DietaryTags,
		&k.CoverImageURL, &k.City, &k.Rating, &k.RatingCount, &k.Currency,
		&k.MealSlots, &k.Plans, &k.FromPriceMinor, &k.RotationWeekPriceMinor, &k.FromMealPriceMinor, &k.FoodSafetyVerified)
	return k, err
}

// Discover lists the kitchens a customer can subscribe to.
func (s *Service) Discover(ctx context.Context, f DiscoverFilter) ([]PublicKitchen, error) {
	if f.Slot != "" && !slices.Contains(Slots, f.Slot) {
		return nil, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "slot", Code: "invalid_slot", Message: "unknown meal slot",
		}}}
	}
	var lat, lng any
	if f.Lat != nil && f.Lng != nil {
		lat, lng = *f.Lat, *f.Lng
	}
	q := strings.TrimSpace(f.Query)
	rows, err := s.pool.Query(ctx, `SELECT `+publicKitchenColumns+`
		  FROM vendor_branches b
		  JOIN vendors v ON v.id = b.vendor_id
		  `+livePlans+`
		 WHERE b.deleted_at IS NULL AND b.is_active
		   AND v.deleted_at IS NULL AND v.status = 'active'
		   AND ($2::float8 IS NULL OR (b.service_area IS NOT NULL AND
		        ST_Intersects(b.service_area, ST_SetSRID(ST_MakePoint($3, $2), 4326)::geography)))
		   AND ($4 = '' OR b.name ILIKE '%' || $4 || '%' OR v.trade_name ILIKE '%' || $4 || '%'
		        OR b.name_ar ILIKE '%' || $4 || '%' OR v.trade_name_ar ILIKE '%' || $4 || '%')
		   AND ($5 = '' OR $5 = ANY (p.slots))
		   AND ($6 = '' OR $6 = ANY (b.cuisines))
		   AND ($7 = '' OR $7 = ANY (b.dietary_tags))
		   AND (NOT $8 OR p.rotation_price IS NOT NULL)
		 ORDER BY b.rating DESC NULLS LAST, b.rating_count DESC, b.name
		 LIMIT 200`,
		planServiceType, lat, lng, q, f.Slot, strings.ToLower(f.Cuisine), strings.ToLower(f.Dietary), f.Rotation)
	if err != nil {
		return nil, fmt.Errorf("kitchen: discover: %w", err)
	}
	defer rows.Close()
	out := []PublicKitchen{}
	for rows.Next() {
		k, err := scanPublicKitchen(rows)
		if err != nil {
			return nil, fmt.Errorf("kitchen: scan public kitchen: %w", err)
		}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachWeekDishes(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

func branchIDs(ks []PublicKitchen) []uuid.UUID {
	ids := make([]uuid.UUID, len(ks))
	for i, k := range ks {
		ids[i] = k.BranchID
	}
	return ids
}

// attachWeekDishes fills WeekDishes from each kitchen's default dishes in
// week 1 of its active menu, photographed dishes first, then by weekday.
func (s *Service) attachWeekDishes(ctx context.Context, ks []PublicKitchen) error {
	for i := range ks {
		ks[i].WeekDishes = []DishThumb{}
	}
	if len(ks) == 0 {
		return nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT branch_id, name, name_ar, image_url FROM (
		  SELECT c.branch_id, mi.name, mi.name_ar, mi.image_url,
		         row_number() OVER (PARTITION BY c.branch_id
		             ORDER BY (mi.image_url IS NULL), min(cd.weekday), mi.name) AS rn
		    FROM menu_cycles c
		    JOIN menu_cycle_days cd ON cd.cycle_id = c.id AND cd.week_no = 1
		    JOIN menu_day_items di ON di.cycle_day_id = cd.id AND di.is_default
		    JOIN menu_items mi ON mi.id = di.menu_item_id AND mi.is_active
		   WHERE c.branch_id = ANY($1) AND c.is_active
		   GROUP BY c.branch_id, mi.id
		) d
		 WHERE rn <= 4
		 ORDER BY branch_id, rn`, branchIDs(ks))
	if err != nil {
		return fmt.Errorf("kitchen: week dishes: %w", err)
	}
	defer rows.Close()
	idx := make(map[uuid.UUID]int, len(ks))
	for i, k := range ks {
		idx[k.BranchID] = i
	}
	for rows.Next() {
		var id uuid.UUID
		var d DishThumb
		if err := rows.Scan(&id, &d.Name, &d.NameAr, &d.ImageURL); err != nil {
			return fmt.Errorf("kitchen: scan week dish: %w", err)
		}
		if i, ok := idx[id]; ok {
			ks[i].WeekDishes = append(ks[i].WeekDishes, d)
		}
	}
	return rows.Err()
}

// DayDish is a dish a kitchen that delivers to the customer cooks on a given
// weekday: the storefront's "on the menu tomorrow" row.
type DayDish struct {
	BranchID           uuid.UUID `json:"branch_id"`
	KitchenName        string    `json:"kitchen_name"`
	KitchenNameAr      *string   `json:"kitchen_name_ar"`
	Name               string    `json:"name"`
	NameAr             *string   `json:"name_ar"`
	ImageURL           *string   `json:"image_url"`
	Slot               string    `json:"slot"`
	HasAlternative     bool      `json:"has_alternative"`
	FromMealPriceMinor int64     `json:"from_meal_price_minor"`
	Currency           string    `json:"currency"`
	Rating             *float64  `json:"rating"`
	RatingCount        int       `json:"rating_count"`
}

const maxDayDishes = 24

// DishesOn lists the default dishes delivered on a weekday (0 = Sunday) by
// the kitchens Discover returns for f: only slots a live plan delivers that
// day, photographed dishes first, then in the kitchens' discovery order.
func (s *Service) DishesOn(ctx context.Context, f DiscoverFilter, weekday int) ([]DayDish, error) {
	ks, err := s.Discover(ctx, f)
	if err != nil {
		return nil, err
	}
	out := []DayDish{}
	if len(ks) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT c.branch_id, mi.name, mi.name_ar, mi.image_url, di.meal_slot,
		       EXISTS (SELECT 1 FROM menu_day_items a
		                 JOIN menu_items ai ON ai.id = a.menu_item_id AND ai.is_active
		                WHERE a.cycle_day_id = di.cycle_day_id AND a.meal_slot = di.meal_slot
		                  AND NOT a.is_default)
		  FROM menu_cycles c
		  JOIN menu_cycle_days cd ON cd.cycle_id = c.id AND cd.week_no = 1 AND cd.weekday = $2
		  JOIN menu_day_items di ON di.cycle_day_id = cd.id AND di.is_default
		  JOIN menu_items mi ON mi.id = di.menu_item_id AND mi.is_active
		 WHERE c.branch_id = ANY($1) AND c.is_active
		   AND EXISTS (SELECT 1 FROM subscription_plans sp
		                WHERE sp.branch_id = c.branch_id AND sp.service_type = $3 AND sp.is_active
		                  AND $2 = ANY (sp.delivery_weekdays) AND di.meal_slot = ANY (sp.meal_slots))
		 ORDER BY (mi.image_url IS NULL), array_position($1, c.branch_id), di.sort_order`,
		branchIDs(ks), weekday, planServiceType)
	if err != nil {
		return nil, fmt.Errorf("kitchen: dishes on day: %w", err)
	}
	defer rows.Close()
	byID := make(map[uuid.UUID]PublicKitchen, len(ks))
	for _, k := range ks {
		byID[k.BranchID] = k
	}
	for rows.Next() {
		var d DayDish
		if err := rows.Scan(&d.BranchID, &d.Name, &d.NameAr, &d.ImageURL, &d.Slot, &d.HasAlternative); err != nil {
			return nil, fmt.Errorf("kitchen: scan day dish: %w", err)
		}
		k := byID[d.BranchID]
		d.KitchenName, d.KitchenNameAr = k.Name, k.NameAr
		d.FromMealPriceMinor, d.Currency = k.FromMealPriceMinor, k.Currency
		d.Rating, d.RatingCount = k.Rating, k.RatingCount
		out = append(out, d)
		if len(out) == maxDayDishes {
			break
		}
	}
	return out, rows.Err()
}

// PublicKitchenDetail is the kitchen page: who they are, what they cook on
// which day, and what it costs.
type PublicKitchenDetail struct {
	PublicKitchen
	// Nil when the customer did not share a location, or the kitchen has not
	// drawn its delivery area yet.
	DeliversHere    *bool      `json:"delivers_here"`
	DeliveryWindows []Window   `json:"delivery_windows"`
	Menu            WeeklyMenu `json:"menu"`
	PlanList        []Plan     `json:"plan_list"`
	// The plan with the most active subscribers, when the kitchen sells more
	// than one and somebody subscribes. Counts themselves stay private.
	PopularPlanID *uuid.UUID `json:"popular_plan_id"`
}

// PublicKitchen loads one kitchen for the storefront. A kitchen that is not
// sellable (vendor suspended, branch closed, nothing on sale) is a 404 rather
// than a page with a disabled button: a shared link to it should not look
// like a kitchen that still trades.
func (s *Service) PublicKitchen(ctx context.Context, branchID uuid.UUID, lat, lng *float64) (PublicKitchenDetail, error) {
	var la, ln any
	if lat != nil && lng != nil {
		la, ln = *lat, *lng
	}
	var d PublicKitchenDetail
	row := s.pool.QueryRow(ctx, `SELECT `+publicKitchenColumns+`,
		       CASE WHEN $3::float8 IS NULL OR b.service_area IS NULL THEN NULL
		            ELSE ST_Intersects(b.service_area, ST_SetSRID(ST_MakePoint($4, $3), 4326)::geography) END
		  FROM vendor_branches b
		  JOIN vendors v ON v.id = b.vendor_id
		  `+livePlans+`
		 WHERE b.id = $2 AND b.deleted_at IS NULL AND b.is_active
		   AND v.deleted_at IS NULL AND v.status = 'active'`,
		planServiceType, branchID, la, ln)
	err := row.Scan(&d.BranchID, &d.VendorID, &d.Name, &d.NameAr, &d.VendorName, &d.VendorNameAr,
		&d.Description, &d.DescriptionAr, &d.Cuisines, &d.DietaryTags,
		&d.CoverImageURL, &d.City, &d.Rating, &d.RatingCount, &d.Currency,
		&d.MealSlots, &d.Plans, &d.FromPriceMinor, &d.RotationWeekPriceMinor, &d.FromMealPriceMinor, &d.FoodSafetyVerified,
		&d.DeliversHere)
	if errors.Is(err, pgx.ErrNoRows) {
		return PublicKitchenDetail{}, httpx.ErrNotFound
	}
	if err != nil {
		return PublicKitchenDetail{}, fmt.Errorf("kitchen: load public kitchen: %w", err)
	}

	prof, err := s.Profile(ctx, branchID)
	if err != nil {
		return PublicKitchenDetail{}, err
	}
	d.DeliveryWindows = prof.DeliveryWindows

	menu, err := s.WeeklyMenu(ctx, branchID)
	if err != nil {
		return PublicKitchenDetail{}, err
	}
	// Archived dishes stay in the vendor's grid so the gap is visible to them;
	// a customer just sees no dish that day.
	slots := menu.Slots[:0]
	for _, sl := range menu.Slots {
		if sl.Alternative != nil && !sl.Alternative.IsActive {
			sl.Alternative = nil
		}
		if sl.Default != nil && !sl.Default.IsActive {
			sl.Default, sl.Alternative = sl.Alternative, nil
		}
		if sl.Default != nil {
			slots = append(slots, sl)
		}
	}
	menu.Slots = slots
	d.Menu = menu

	plans, err := s.ListPlans(ctx, branchID)
	if err != nil {
		return PublicKitchenDetail{}, err
	}
	d.PlanList = []Plan{}
	top := 0
	for _, p := range plans {
		if !p.IsActive {
			continue
		}
		if p.ActiveSubscribers > top {
			top, d.PopularPlanID = p.ActiveSubscribers, &p.ID
		}
		tiers := p.Tiers[:0]
		for _, t := range p.Tiers {
			if t.IsActive {
				tiers = append(tiers, t)
			}
		}
		p.Tiers = tiers
		// Subscriber counts are the kitchen's business, not the customer's.
		p.ActiveSubscribers = 0
		d.PlanList = append(d.PlanList, p)
	}
	if len(d.PlanList) < 2 {
		d.PopularPlanID = nil
	}
	weekDishes := []PublicKitchen{d.PublicKitchen}
	if err := s.attachWeekDishes(ctx, weekDishes); err != nil {
		return PublicKitchenDetail{}, err
	}
	d.WeekDishes = weekDishes[0].WeekDishes
	return d, nil
}
