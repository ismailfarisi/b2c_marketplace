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
	for _, p := range plans {
		if !p.IsActive {
			continue
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
	return d, nil
}
