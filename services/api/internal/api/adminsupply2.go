package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/admin"
	"github.com/marketplace/api/internal/modules/feed"
)

// Listing detail, driver detail and the feed inspector — Phase C's remaining
// three.

// FeedInspector is the subset of feed.Service the console needs.
//
// Narrow on purpose: the console can ask what the feed *would* return and
// nothing else. There is no method here that serves a page, records an
// impression or spends a boost budget, so an inspector call cannot be mistaken
// by the analytics for a real view — which would make the tool that diagnoses
// the feed also a source of noise in it.
type FeedInspector interface {
	Inspect(ctx context.Context, q feed.Query) (*feed.Inspection, error)
}

func (h *AdminHandlers) Listing(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermContentReview)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "listingId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	lens := lensFor(grant)
	l, err := h.View.LoadListing(r.Context(), id, lens)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	signals := make([]map[string]any, 0, len(l.Signals))
	for _, f := range l.Signals {
		signals = append(signals, map[string]any{
			"kind": f.Kind, "severity": f.Severity,
			"detail": nilIfBlank(f.Detail), "state": f.State, "at": f.At,
		})
	}

	var entitlement any
	if e := l.Entitlement; e != nil {
		entitlement = map[string]any{
			"code": e.PackageCode, "name": e.PackageName,
			"purchased_at": e.PurchasedAt, "starts_at": e.StartsAt, "ends_at": e.EndsAt,
			"listing_quota": e.ListingQuota, "listings_used": e.ListingsUsed,
			"bump_quota": e.BumpQuota, "bumps_used": e.BumpsUsed,
			"status": e.Status,
		}
	}

	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"id": l.ID, "reference_no": l.ReferenceNo, "status": l.Status,
		"seller": map[string]any{
			"id": l.SellerID, "kind": l.SellerKind,
			"name": nilIfBlank(l.SellerName), "phone": nilIfBlank(l.SellerPhone),
			"dealer_id": l.DealerID, "dealer": nilIfBlank(l.Dealer),
			// The number that matters most on this page and is invisible from
			// the listing alone: a private seller with eleven cars is a dealer
			// who has not said so.
			"other_listings": l.SellerListings,
			"sold":           l.SellerSold,
		},
		"vehicle": map[string]any{
			"make": l.Make, "model": l.Model, "year": l.Year,
			"mileage_km": l.Mileage, "vin": nilIfBlank(l.VIN),
			"spec": nilIfBlank(l.Spec), "body": nilIfBlank(l.BodyType),
			"fuel": nilIfBlank(l.Fuel), "colour": nilIfBlank(l.Colour),
			"registration_expiry": dateOrNil(l.RegistrationExpiry),
			"warranty_until":      dateOrNil(l.WarrantyUntil),
		},
		"price":   amount(l.PriceMinor, l.Currency),
		"emirate": nilIfBlank(l.Emirate), "city": nilIfBlank(l.City),
		"photos": l.Photos,
		"lifecycle": map[string]any{
			"created_at": l.CreatedAt, "published_at": l.PublishedAt,
			"expires_at": l.ExpiresAt, "featured_until": l.FeaturedUntil,
			"bumped_at": l.BumpedAt, "sold_at": l.SoldAt,
			"sold_price": soldPrice(l.SoldPrice, l.Currency),
			"removed_at": l.RemovedAt, "rejection": nilIfBlank(l.RejectionNote),
		},
		"signals":     signals,
		"entitlement": entitlement,
	})

	h.recordIfSensitive(r, grant, lens, "listing", id)
}

func (h *AdminHandlers) ListingTimeline(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermContentReview); !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "listingId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	entries, err := h.View.ListingTimeline(r.Context(), id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": timelineJSON(entries)})
}

func (h *AdminHandlers) Driver(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermCommerceRead)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "driverId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	lens := lensFor(grant)
	d, err := h.View.LoadDriver(r.Context(), id, lens)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	docs := make([]map[string]any, 0, len(d.Documents))
	for _, doc := range d.Documents {
		docs = append(docs, map[string]any{
			"kind": doc.Kind, "state": doc.State,
			"expires_on": dateOrNil(doc.ExpiresOn), "expired": doc.Expired,
		})
	}

	var position any
	if d.LastLat != nil && d.LastLng != nil {
		position = map[string]any{"lat": *d.LastLat, "lng": *d.LastLng}
	}

	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"id": d.ID, "user_id": d.UserID, "name": d.DisplayName,
		"phone": nilIfBlank(d.Phone), "status": d.Status,
		"vehicle_type": d.VehicleType, "plate_no": nilIfBlank(d.PlateNo),
		"vendor_id": d.VendorID, "vendor": nilIfBlank(d.Vendor),
		"max_stops": d.MaxStops, "created_at": d.CreatedAt,
		// Position and its age, separately. A stale position with a recent ping
		// and no position at all are different faults needing different calls.
		"position":     position,
		"last_ping_at": d.LastPingAt,
		"shift": map[string]any{
			"on": d.OnShift, "from": d.ShiftFrom, "until": d.ShiftUntil,
		},
		"workload": map[string]any{
			"open_stops": d.OpenStops, "completed_today": d.TodayDone,
			"failed_today": d.TodayFailed,
		},
		"cash": map[string]any{
			"held":  amount(d.CashHeldMinor, d.CashCurrency),
			"stops": d.CashHeldStops, "oldest": d.OldestCashHeld,
		},
		"documents": docs,
	})

	h.recordIfSensitive(r, grant, lens, "driver", id)
}

func (h *AdminHandlers) DriverTimeline(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermCommerceRead); !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "driverId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	entries, err := h.View.DriverTimeline(r.Context(), id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": timelineJSON(entries)})
}

// FeedInspect answers "the feed looked wrong at 14:20".
//
// Runs the real retrieval for a point and shows the score with its terms kept.
// Without it, the only available answers are "the ranker is working as designed"
// and a re-implementation of the ranker in a spreadsheet — and the second is
// always subtly different from the first.
//
// The ranked order is returned, not a reconstructed page. Exploration slots come
// from a seeded shuffle of the viewer key and page number, so a reel that
// appeared at position 4 for one viewer will not appear there for an operator.
// Naming exploration separately is honest; implying a reconstruction is the page
// somebody saw is not.
func (h *AdminHandlers) FeedInspect(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermHealthRead); !ok {
		return
	}
	if h.Feed == nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	q := r.URL.Query()
	lat, latErr := strconv.ParseFloat(q.Get("lat"), 64)
	lng, lngErr := strconv.ParseFloat(q.Get("lng"), 64)
	if latErr != nil || lngErr != nil {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "lat", Code: "required",
			Message: "a point is required — the feed is local discovery and has no meaning " +
				"without one",
		}}})
		return
	}

	query := feed.Query{
		Lat: lat, Lng: lng,
		Limit: intParam(q.Get("limit"), 25),
		// Defaults to the operator's own viewer key rather than an empty one,
		// so an inspection with no viewer named still exercises the seen-set
		// and interest-profile joins instead of a simpler path that would
		// quietly diverge from what a real viewer gets.
		ViewerKey: defaultStr(q.Get("viewer_key"), "admin-inspector"),
	}
	if id, err := uuid.Parse(q.Get("viewer_id")); err == nil {
		query.ViewerID = uuid.NullUUID{UUID: id, Valid: true}
	}
	if id, err := uuid.Parse(q.Get("category_id")); err == nil {
		query.Category = &id
	}

	result, err := h.Feed.Inspect(r.Context(), query)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	items := make([]map[string]any, 0, len(result.Candidates))
	for _, c := range result.Candidates {
		items = append(items, map[string]any{
			"rank": c.Rank, "reel_id": c.ReelID,
			"caption": nilIfBlank(c.Caption), "author": nilIfBlank(c.Author),
			"score": c.Score,
			// Every term, always. A breakdown that hides zeroes cannot show
			// that proximity contributed nothing, which is the usual answer to
			// "why is a reel from another emirate here".
			"terms": map[string]any{
				"conversion": c.Terms.Conversion,
				"intent":     c.Terms.Intent,
				"engagement": c.Terms.Engagement,
				"author":     c.Terms.Author,
				"recency":    c.Terms.Recency,
				"affinity":   c.Terms.Affinity,
				"followed":   c.Terms.Followed,
				"proximity":  c.Terms.Proximity,
			},
			"features": map[string]any{
				"impressions": c.Features.Impressions,
				"views":       c.Features.Views,
				"orders":      c.Features.Orders,
				"lifetime":    c.Features.LifetimeImpressions,
				"distance_km": c.Features.DistanceKM,
				"age_hours":   c.Features.Age.Hours(),
				"affinity":    c.Features.Affinity,
				"followed":    c.Features.Followed,
			},
			"explore_eligible": c.ExploreEligible,
		})
	}

	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"profile": result.Profile,
		// The retrieved count before the limit. A page of ten drawn from eleven
		// candidates and one drawn from four hundred look identical in the list
		// and are completely different situations.
		"retrieved": result.Retrieved,
		"shown":     len(items),
		"items":     items,
	})
}

func soldPrice(minor *int64, currency string) any {
	if minor == nil {
		return nil
	}
	return amount(*minor, currency)
}

func defaultStr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
