package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/kitchen"
	"github.com/marketplace/api/internal/modules/merch"
	"github.com/marketplace/api/internal/modules/promo"
	"github.com/marketplace/api/internal/modules/reviews"
)

// MealsHandlers is the customer storefront for meal subscriptions.
//
// Public and anonymous, for the same reason the feed is: a link to a kitchen
// shared in a WhatsApp group has to open the kitchen, not a login wall.
type MealsHandlers struct {
	Kitchen *kitchen.Service
	// Merch serves the merchandised home and collection pages. Nil disables them.
	Merch *merch.Service
	// Promos serves a kitchen's public offers. Nil disables them.
	Promos *promo.Service
	// Reviews serves a kitchen's published reviews. Nil disables them.
	Reviews *reviews.Service
}

// point reads an optional lat/lng pair. Half a pair, or a value off the
// globe, is a client bug worth telling the client about rather than silently
// showing kitchens that do not deliver.
func point(w http.ResponseWriter, r *http.Request) (lat, lng *float64, ok bool) {
	q := r.URL.Query()
	ls, gs := q.Get("lat"), q.Get("lng")
	if ls == "" && gs == "" {
		return nil, nil, true
	}
	la, err1 := strconv.ParseFloat(ls, 64)
	lo, err2 := strconv.ParseFloat(gs, 64)
	if err1 != nil || err2 != nil || la < -90 || la > 90 || lo < -180 || lo > 180 {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "lat", Code: "invalid_point", Message: "lat and lng must be given together as decimal degrees",
		}}})
		return nil, nil, false
	}
	return &la, &lo, true
}

func (h *MealsHandlers) KitchenList(w http.ResponseWriter, r *http.Request) {
	lat, lng, ok := point(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	items, err := h.Kitchen.Discover(r.Context(), kitchen.DiscoverFilter{
		Lat: lat, Lng: lng,
		Query:    q.Get("q"),
		Slot:     q.Get("slot"),
		Cuisine:  q.Get("cuisine"),
		Dietary:  q.Get("dietary"),
		Rotation: q.Get("rotation") == "true",
	})
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

var dubai = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Dubai")
	if err != nil {
		return time.FixedZone("Asia/Dubai", 4*3600)
	}
	return loc
}()

// TomorrowDishes answers GET /meals/dishes/tomorrow: the default dishes of
// the next delivery day (Asia/Dubai), from kitchens that deliver to the
// customer. Usually tomorrow; over a weekend with no deliveries, the first day
// after it that has dishes.
func (h *MealsHandlers) TomorrowDishes(w http.ResponseWriter, r *http.Request) {
	lat, lng, ok := point(w, r)
	if !ok {
		return
	}
	f := kitchen.DiscoverFilter{Lat: lat, Lng: lng, Slot: r.URL.Query().Get("slot")}
	today := time.Now().In(dubai)
	day := today.AddDate(0, 0, 1)
	items := []kitchen.DayDish{}
	for i := 1; i <= 7; i++ {
		d := today.AddDate(0, 0, i)
		got, err := h.Kitchen.DishesOn(r.Context(), f, int(d.Weekday()))
		if err != nil {
			httpx.WriteProblem(w, r, err)
			return
		}
		if len(got) > 0 {
			day, items = d, got
			break
		}
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"date": day.Format(time.DateOnly), "weekday": int(day.Weekday()), "items": items,
	})
}

func (h *MealsHandlers) KitchenPage(w http.ResponseWriter, r *http.Request) {
	branchID, ok := uuidURLParam(w, r, "branchId")
	if !ok {
		return
	}
	lat, lng, ok := point(w, r)
	if !ok {
		return
	}
	d, err := h.Kitchen.PublicKitchen(r.Context(), branchID, lat, lng)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", d)
}
