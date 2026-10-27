// Package kitchen is the vendor's side of the meal marketplace: the dishes a
// kitchen cooks, the weekly menu those dishes fill, and the plans it sells.
//
// It owns writes to menu_items, menu_cycles, menu_cycle_days, menu_day_items,
// subscription_plans and plan_tiers. The subscription engine reads the same
// tables and never writes them, so the shape rules below are the only place a
// plan can come into existence.
//
// Most kitchens in the GCC run a fixed weekly menu — biryani every Monday — and
// some offer a choice of two on a given day. That is exactly what one
// one-week menu cycle models: per weekday and meal, a default dish and an
// optional alternative. Customers who do not choose get the default.
package kitchen

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
)

// Slots in the order a day is eaten. The order is used for display and for
// stable output, never for meaning.
var Slots = []string{"breakfast", "lunch", "dinner", "snack", "suhoor", "iftar"}

// Allergens is the closed list a dish may declare: the fourteen that EU and
// GSO labelling rules name. Free text here would make "contains nuts" and
// "tree nuts" two filters that each miss half the menu.
var Allergens = []string{
	"gluten", "crustaceans", "eggs", "fish", "peanuts", "soy", "milk",
	"tree_nuts", "celery", "mustard", "sesame", "sulphites", "lupin", "molluscs",
}

var tagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// Roles that may change what a kitchen sells. Staff accounts run the day's
// queue; they do not reprice the plans.
func CanEdit(role string) bool { return role == "owner" || role == "manager" }

type Dish struct {
	ID            uuid.UUID `json:"id"`
	BranchID      uuid.UUID `json:"branch_id"`
	Name          string    `json:"name"`
	NameAr        *string   `json:"name_ar"`
	Description   *string   `json:"description"`
	DescriptionAr *string   `json:"description_ar"`
	ImageURL      *string   `json:"image_url"`
	Calories      *int      `json:"calories"`
	ProteinG      *float64  `json:"protein_g"`
	CarbsG        *float64  `json:"carbs_g"`
	FatG          *float64  `json:"fat_g"`
	Allergens     []string  `json:"allergens"`
	Tags          []string  `json:"tags"`
	IsActive      bool      `json:"is_active"`
	// How many menu slots currently serve this dish. Shown before archiving,
	// because archiving a dish that is Monday's lunch empties Monday's lunch.
	MenuSlots int `json:"menu_slots"`
}

type DishInput struct {
	Name          string   `json:"name"`
	NameAr        *string  `json:"name_ar"`
	Description   *string  `json:"description"`
	DescriptionAr *string  `json:"description_ar"`
	ImageURL      *string  `json:"image_url"`
	Calories      *int     `json:"calories"`
	ProteinG      *float64 `json:"protein_g"`
	CarbsG        *float64 `json:"carbs_g"`
	FatG          *float64 `json:"fat_g"`
	Allergens     []string `json:"allergens"`
	Tags          []string `json:"tags"`
	IsActive      *bool    `json:"is_active"`
}

type MenuChoice struct {
	DishID    uuid.UUID `json:"dish_id"`
	Name      string    `json:"name"`
	NameAr    *string   `json:"name_ar"`
	Allergens []string  `json:"allergens"`
	Calories  *int      `json:"calories"`
	ImageURL  *string   `json:"image_url"`
	IsActive  bool      `json:"is_active"`
}

// MenuSlot is one cell of the weekly grid. Weekday 0 is Sunday, matching the
// scheduler and the rest of the schema.
type MenuSlot struct {
	Weekday     int         `json:"weekday"`
	Slot        string      `json:"slot"`
	Default     *MenuChoice `json:"default"`
	Alternative *MenuChoice `json:"alternative"`
}

type WeeklyMenu struct {
	CycleID uuid.NullUUID `json:"cycle_id"`
	Slots   []MenuSlot    `json:"slots"`
}

type Tier struct {
	ID          uuid.UUID `json:"id"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	NameAr      *string   `json:"name_ar"`
	DaysPerWeek *int      `json:"days_per_week"`
	MealsPerDay *int      `json:"meals_per_day"`
	PortionSize *string   `json:"portion_size"`
	PriceMinor  int64     `json:"price_minor"`
	SortOrder   int       `json:"sort_order"`
	IsActive    bool      `json:"is_active"`
}

type TierInput struct {
	Key         string  `json:"key"`
	Name        string  `json:"name"`
	NameAr      *string `json:"name_ar"`
	DaysPerWeek *int    `json:"days_per_week"`
	MealsPerDay *int    `json:"meals_per_day"`
	PortionSize *string `json:"portion_size"`
	PriceMinor  int64   `json:"price_minor"`
}

type Plan struct {
	ID                     uuid.UUID `json:"id"`
	BranchID               uuid.UUID `json:"branch_id"`
	Name                   string    `json:"name"`
	NameAr                 *string   `json:"name_ar"`
	Description            *string   `json:"description"`
	DescriptionAr          *string   `json:"description_ar"`
	ImageURL               *string   `json:"image_url"`
	CycleDays              int       `json:"cycle_days"`
	DaysPerWeek            int       `json:"days_per_week"`
	MealsPerDay            int       `json:"meals_per_day"`
	MealSlots              []string  `json:"meal_slots"`
	DeliveryWeekdays       []int     `json:"delivery_weekdays"`
	AllowedPauseDays       int       `json:"allowed_pause_days"`
	CutoffHours            int       `json:"cutoff_hours"`
	PriceMinor             int64     `json:"price_minor"`
	Currency               string    `json:"currency"`
	TaxRateBP              int       `json:"tax_rate_bp"`
	RotationEnabled        bool      `json:"rotation_enabled"`
	RotationWeekPriceMinor *int64    `json:"rotation_week_price_minor"`
	IsActive               bool      `json:"is_active"`
	ActiveSubscribers      int       `json:"active_subscribers"`
	Tiers                  []Tier    `json:"tiers"`
}

type PlanInput struct {
	Name                   string      `json:"name"`
	NameAr                 *string     `json:"name_ar"`
	Description            *string     `json:"description"`
	DescriptionAr          *string     `json:"description_ar"`
	ImageURL               *string     `json:"image_url"`
	CycleDays              int         `json:"cycle_days"`
	DaysPerWeek            int         `json:"days_per_week"`
	MealSlots              []string    `json:"meal_slots"`
	DeliveryWeekdays       []int       `json:"delivery_weekdays"`
	AllowedPauseDays       int         `json:"allowed_pause_days"`
	CutoffHours            int         `json:"cutoff_hours"`
	PriceMinor             int64       `json:"price_minor"`
	RotationEnabled        bool        `json:"rotation_enabled"`
	RotationWeekPriceMinor *int64      `json:"rotation_week_price_minor"`
	IsActive               *bool       `json:"is_active"`
	Tiers                  []TierInput `json:"tiers"`
}

type Profile struct {
	BranchID        uuid.UUID `json:"branch_id"`
	VendorID        uuid.UUID `json:"vendor_id"`
	VendorName      string    `json:"vendor_name"`
	VendorStatus    string    `json:"vendor_status"`
	Name            string    `json:"name"`
	NameAr          *string   `json:"name_ar"`
	Description     *string   `json:"description"`
	DescriptionAr   *string   `json:"description_ar"`
	Cuisines        []string  `json:"cuisines"`
	DietaryTags     []string  `json:"dietary_tags"`
	CoverImageURL   *string   `json:"cover_image_url"`
	Phone           *string   `json:"phone"`
	AddressLine     *string   `json:"address_line"`
	City            *string   `json:"city"`
	Emirate         *string   `json:"emirate"`
	Timezone        string    `json:"timezone"`
	Currency        string    `json:"currency"`
	IsActive        bool      `json:"is_active"`
	HasServiceArea  bool      `json:"has_service_area"`
	DeliveryWindows []Window  `json:"delivery_windows"`
}

type Window struct {
	ID       uuid.UUID `json:"id"`
	Label    string    `json:"label"`
	StartsAt string    `json:"starts_at"`
	EndsAt   string    `json:"ends_at"`
}

type ProfileInput struct {
	Name          string   `json:"name"`
	NameAr        *string  `json:"name_ar"`
	Description   *string  `json:"description"`
	DescriptionAr *string  `json:"description_ar"`
	Cuisines      []string `json:"cuisines"`
	DietaryTags   []string `json:"dietary_tags"`
	CoverImageURL *string  `json:"cover_image_url"`
	Phone         *string  `json:"phone"`
}

// MenuGap is a day and meal some active plan delivers but the menu leaves
// empty. A customer on that plan would be billed for a meal with no dish.
type MenuGap struct {
	Weekday int    `json:"weekday"`
	Slot    string `json:"slot"`
}

type Overview struct {
	ActiveSubscribers int       `json:"active_subscribers"`
	PausedSubscribers int       `json:"paused_subscribers"`
	PendingPayment    int       `json:"pending_payment"`
	ActivePlans       int       `json:"active_plans"`
	RotationPlans     int       `json:"rotation_plans"`
	ActiveDishes      int       `json:"active_dishes"`
	MenuGaps          []MenuGap `json:"menu_gaps"`
	ProfileComplete   bool      `json:"profile_complete"`
}

// ── validation ─────────────────────────────────────────────────────────────

type fieldErrs []httpx.FieldError

func (f *fieldErrs) add(field, code, msg string) {
	*f = append(*f, httpx.FieldError{Field: field, Code: code, Message: msg})
}

func (f fieldErrs) err() error {
	if len(f) == 0 {
		return nil
	}
	return &httpx.ValidationError{Fields: f}
}

func trimPtr(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	if t == "" {
		return nil
	}
	return &t
}

func normaliseTags(field string, in []string, errs *fieldErrs) []string {
	out := []string{}
	for _, raw := range in {
		t := strings.ToLower(strings.TrimSpace(raw))
		if t == "" || slices.Contains(out, t) {
			continue
		}
		if !tagPattern.MatchString(t) {
			errs.add(field, "invalid_tag", fmt.Sprintf("%q: use lowercase letters, digits, - or _", raw))
			continue
		}
		out = append(out, t)
	}
	if len(out) > 20 {
		errs.add(field, "too_many", "at most 20")
	}
	return out
}

// NormaliseDish trims and validates a dish, returning the cleaned input.
func NormaliseDish(in DishInput) (DishInput, error) {
	var errs fieldErrs
	in.Name = strings.TrimSpace(in.Name)
	in.NameAr, in.Description, in.DescriptionAr, in.ImageURL =
		trimPtr(in.NameAr), trimPtr(in.Description), trimPtr(in.DescriptionAr), trimPtr(in.ImageURL)

	if in.Name == "" {
		errs.add("name", "required", "a dish needs a name")
	} else if len([]rune(in.Name)) > 120 {
		errs.add("name", "too_long", "at most 120 characters")
	}
	if in.ImageURL != nil && !strings.HasPrefix(*in.ImageURL, "https://") {
		errs.add("image_url", "invalid_url", "must be an https:// URL")
	}
	if in.Calories != nil && (*in.Calories < 0 || *in.Calories > 5000) {
		errs.add("calories", "out_of_range", "between 0 and 5000")
	}
	for field, v := range map[string]*float64{"protein_g": in.ProteinG, "carbs_g": in.CarbsG, "fat_g": in.FatG} {
		if v != nil && (*v < 0 || *v > 1000) {
			errs.add(field, "out_of_range", "between 0 and 1000 grams")
		}
	}

	allergens := []string{}
	for _, a := range in.Allergens {
		a = strings.ToLower(strings.TrimSpace(a))
		if !slices.Contains(Allergens, a) {
			errs.add("allergens", "unknown_allergen", fmt.Sprintf("%q is not one of the declared allergens", a))
			continue
		}
		if !slices.Contains(allergens, a) {
			allergens = append(allergens, a)
		}
	}
	in.Allergens = allergens
	in.Tags = normaliseTags("tags", in.Tags, &errs)
	return in, errs.err()
}

// ValidateMenuSlot checks one cell of the weekly grid before it is written.
// Clearing a cell is a nil default and a nil alternative.
func ValidateMenuSlot(weekday int, slot string, def, alt uuid.NullUUID) error {
	var errs fieldErrs
	if weekday < 0 || weekday > 6 {
		errs.add("weekday", "invalid_weekday", "0 (Sunday) to 6 (Saturday)")
	}
	if !slices.Contains(Slots, slot) {
		errs.add("slot", "invalid_slot", "unknown meal slot")
	}
	if alt.Valid && !def.Valid {
		errs.add("alternative_dish_id", "default_required",
			"an alternative needs a default — customers who do not choose get the default")
	}
	if def.Valid && alt.Valid && def.UUID == alt.UUID {
		errs.add("alternative_dish_id", "same_as_default", "the alternative must be a different dish")
	}
	return errs.err()
}

// NormalisePlan validates a plan and its tiers. It also sorts and dedupes the
// slot and weekday lists so two plans with the same shape compare equal —
// which is what the rotation's compatibility check relies on.
func NormalisePlan(in PlanInput) (PlanInput, error) {
	var errs fieldErrs
	in.Name = strings.TrimSpace(in.Name)
	in.NameAr, in.Description, in.DescriptionAr, in.ImageURL =
		trimPtr(in.NameAr), trimPtr(in.Description), trimPtr(in.DescriptionAr), trimPtr(in.ImageURL)

	if in.Name == "" {
		errs.add("name", "required", "a plan needs a name")
	} else if len([]rune(in.Name)) > 120 {
		errs.add("name", "too_long", "at most 120 characters")
	}
	if in.ImageURL != nil && !strings.HasPrefix(*in.ImageURL, "https://") {
		errs.add("image_url", "invalid_url", "must be an https:// URL")
	}
	if in.CycleDays < 7 || in.CycleDays > 365 {
		errs.add("cycle_days", "out_of_range", "a billing cycle is 7 to 365 days")
	}

	slots := []string{}
	for _, s := range Slots {
		if slices.Contains(in.MealSlots, s) {
			slots = append(slots, s)
		}
	}
	for _, s := range in.MealSlots {
		if !slices.Contains(Slots, s) {
			errs.add("meal_slots", "invalid_slot", fmt.Sprintf("%q is not a meal slot", s))
		}
	}
	if len(in.MealSlots) == 0 {
		errs.add("meal_slots", "required", "choose at least one meal")
	}
	in.MealSlots = slots

	days := []int{}
	for d := 0; d <= 6; d++ {
		if slices.Contains(in.DeliveryWeekdays, d) {
			days = append(days, d)
		}
	}
	for _, d := range in.DeliveryWeekdays {
		if d < 0 || d > 6 {
			errs.add("delivery_weekdays", "invalid_weekday", "0 (Sunday) to 6 (Saturday)")
		}
	}
	if len(in.DeliveryWeekdays) == 0 {
		errs.add("delivery_weekdays", "required", "choose at least one delivery day")
	}
	in.DeliveryWeekdays = days

	if in.DaysPerWeek < 1 || in.DaysPerWeek > 7 {
		errs.add("days_per_week", "out_of_range", "1 to 7")
	} else if len(days) > 0 && in.DaysPerWeek > len(days) {
		errs.add("days_per_week", "exceeds_delivery_days",
			fmt.Sprintf("the kitchen delivers %d days a week", len(days)))
	}
	if in.CutoffHours < 0 || in.CutoffHours > 168 {
		errs.add("cutoff_hours", "out_of_range", "0 to 168 hours")
	}
	if in.AllowedPauseDays < 0 || (in.CycleDays > 0 && in.AllowedPauseDays > in.CycleDays) {
		errs.add("allowed_pause_days", "out_of_range", "0 up to the cycle length")
	}
	if in.PriceMinor <= 0 {
		errs.add("price_minor", "required", "a plan needs a price")
	}

	if in.RotationEnabled {
		if in.RotationWeekPriceMinor == nil || *in.RotationWeekPriceMinor <= 0 {
			errs.add("rotation_week_price_minor", "required",
				"set the price of one week of this plan inside a rotation")
		}
	} else if in.RotationWeekPriceMinor != nil && *in.RotationWeekPriceMinor <= 0 {
		in.RotationWeekPriceMinor = nil
	}

	keys := map[string]bool{}
	for i := range in.Tiers {
		t := &in.Tiers[i]
		field := fmt.Sprintf("tiers[%d]", i)
		t.Key = strings.ToLower(strings.TrimSpace(t.Key))
		t.Name = strings.TrimSpace(t.Name)
		t.NameAr = trimPtr(t.NameAr)
		if !tagPattern.MatchString(t.Key) {
			errs.add(field+".key", "invalid_key", "lowercase letters, digits, - or _")
		} else if keys[t.Key] {
			errs.add(field+".key", "duplicate", "tier keys must be unique")
		}
		keys[t.Key] = true
		if t.Name == "" {
			errs.add(field+".name", "required", "a tier needs a name")
		}
		if t.PriceMinor <= 0 {
			errs.add(field+".price_minor", "required", "a tier needs a price")
		}
		if t.DaysPerWeek != nil && (*t.DaysPerWeek < 1 || *t.DaysPerWeek > len(days)) {
			errs.add(field+".days_per_week", "exceeds_delivery_days", "no more than the delivery days")
		}
		if t.MealsPerDay != nil && (*t.MealsPerDay < 1 || *t.MealsPerDay > len(slots)) {
			errs.add(field+".meals_per_day", "exceeds_meal_slots", "no more than the plan's meals")
		}
		if t.PortionSize != nil && !slices.Contains([]string{"small", "regular", "large"}, *t.PortionSize) {
			errs.add(field+".portion_size", "invalid", "small, regular or large")
		}
	}
	return in, errs.err()
}

// NormaliseProfile validates the kitchen's storefront card.
func NormaliseProfile(in ProfileInput) (ProfileInput, error) {
	var errs fieldErrs
	in.Name = strings.TrimSpace(in.Name)
	in.NameAr, in.Description, in.DescriptionAr, in.CoverImageURL, in.Phone =
		trimPtr(in.NameAr), trimPtr(in.Description), trimPtr(in.DescriptionAr), trimPtr(in.CoverImageURL), trimPtr(in.Phone)
	if in.Name == "" {
		errs.add("name", "required", "the kitchen needs a name")
	}
	if in.Description != nil && len([]rune(*in.Description)) > 1000 {
		errs.add("description", "too_long", "at most 1000 characters")
	}
	if in.CoverImageURL != nil && !strings.HasPrefix(*in.CoverImageURL, "https://") {
		errs.add("cover_image_url", "invalid_url", "must be an https:// URL")
	}
	in.Cuisines = normaliseTags("cuisines", in.Cuisines, &errs)
	in.DietaryTags = normaliseTags("dietary_tags", in.DietaryTags, &errs)
	return in, errs.err()
}

// MenuGaps lists the (weekday, slot) cells that a plan delivers but the menu
// has no default dish for, in weekday then slot order.
func MenuGaps(plans []Plan, menu WeeklyMenu) []MenuGap {
	filled := map[MenuGap]bool{}
	for _, s := range menu.Slots {
		if s.Default != nil && s.Default.IsActive {
			filled[MenuGap{s.Weekday, s.Slot}] = true
		}
	}
	need := map[MenuGap]bool{}
	for _, p := range plans {
		if !p.IsActive {
			continue
		}
		for _, d := range p.DeliveryWeekdays {
			for _, s := range p.MealSlots {
				need[MenuGap{d, s}] = true
			}
		}
	}
	gaps := []MenuGap{}
	for d := 0; d <= 6; d++ {
		for _, s := range Slots {
			g := MenuGap{d, s}
			if need[g] && !filled[g] {
				gaps = append(gaps, g)
			}
		}
	}
	return gaps
}
