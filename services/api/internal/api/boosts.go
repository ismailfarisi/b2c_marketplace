package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/admin"
	"github.com/marketplace/api/internal/modules/boosts"
)

// BoostHandlers is the Phase 9 surface.
//
// Small on purpose. An ad platform's console is where scope goes to expand
// forever — audience builders, creative variants, lookalike targeting,
// dayparting UIs — and none of that is worth building before anyone has bought
// a campaign. What is here is the minimum an advertiser needs to spend money
// and see what it bought.
type BoostHandlers struct {
	Boosts  *boosts.Service
	Admin   *admin.Service
	Console *ConsoleHandlers
}

type createCampaignBody struct {
	ReelID        uuid.UUID  `json:"reel_id"`
	VendorID      *uuid.UUID `json:"vendor_id"`
	Objective     string     `json:"objective"`
	BidCPMinor    int        `json:"bid_cpm_minor"`
	FundedMinor   int64      `json:"funded_minor"`
	DailyCapMinor int64      `json:"daily_cap_minor"`
	StartsOn      string     `json:"starts_on"`
	EndsOn        string     `json:"ends_on"`
	Targeting     *struct {
		Lat         float64     `json:"lat"`
		Lng         float64     `json:"lng"`
		RadiusM     int         `json:"radius_m"`
		Emirates    []string    `json:"emirates"`
		CategoryIDs []uuid.UUID `json:"category_ids"`
		Locales     []string    `json:"locales"`
		HoursOfDay  []int       `json:"hours_of_day"`
		DaysOfWeek  []int       `json:"days_of_week"`
	} `json:"targeting"`
}

// Create opens a draft campaign.
//
// Draft, not active. Three things have to happen before a single impression is
// served — the advertiser pays, a human looks at the creative, and the schedule
// starts — and collapsing any of them into creation is how an unreviewed video
// reaches a feed.
func (h *BoostHandlers) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}

	var body createCampaignBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	// Who is buying. A vendor campaign authorises through vendor_users; a
	// private seller's authorises through being themselves. Resolved
	// server-side from the session either way — a client that could name its
	// own advertiser id could spend someone else's budget.
	advertiserType, advertiserID := "user", userID
	if body.VendorID != nil {
		if err := h.Console.Console.AuthorizeVendor(r.Context(), userID, *body.VendorID); err != nil {
			httpx.WriteProblem(w, r, err)
			return
		}
		advertiserType, advertiserID = "vendor", *body.VendorID
	}

	starts, err := time.Parse("2006-01-02", body.StartsOn)
	if err != nil {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "starts_on", Code: "invalid_date", Message: "expected YYYY-MM-DD",
		}}})
		return
	}
	var ends time.Time
	if body.EndsOn != "" {
		ends, err = time.Parse("2006-01-02", body.EndsOn)
		if err != nil {
			httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "ends_on", Code: "invalid_date", Message: "expected YYYY-MM-DD",
			}}})
			return
		}
	}

	in := boosts.CreateInput{
		AdvertiserType: advertiserType,
		AdvertiserID:   advertiserID,
		ReelID:         body.ReelID,
		Country:        "AE",
		Objective:      orDefault(body.Objective, "reach"),
		BidCPMinor:     body.BidCPMinor,
		FundedMinor:    body.FundedMinor,
		DailyCapMinor:  body.DailyCapMinor,
		StartsOn:       starts,
		EndsOn:         ends,
	}
	if t := body.Targeting; t != nil {
		in.Targeting = &boosts.Targeting{
			Lat: t.Lat, Lng: t.Lng, RadiusM: t.RadiusM,
			Emirates: t.Emirates, CategoryIDs: t.CategoryIDs, Locales: t.Locales,
			HoursOfDay: t.HoursOfDay, DaysOfWeek: t.DaysOfWeek,
		}
	}

	id, err := h.Boosts.Create(r.Context(), in)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, "", map[string]any{
		"id":     id,
		"status": "draft",
		"next":   "fund the campaign, then it goes for review",
		// Stated in the response because it is the single most common
		// misunderstanding about how this works: targeting narrows, it never
		// widens. A vendor who sets a 50 km radius does not thereby deliver to
		// 50 km.
		"note": "targeting narrows where your reel appears; it never extends past your service area",
	})
}

func (h *BoostHandlers) Report(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}

	advertiserType, advertiserID := "user", userID
	if raw := r.URL.Query().Get("vendor_id"); raw != "" {
		vendorID, err := uuid.Parse(raw)
		if err != nil {
			httpx.WriteProblem(w, r, httpx.ErrNotFound)
			return
		}
		if err := h.Console.Console.AuthorizeVendor(r.Context(), userID, vendorID); err != nil {
			httpx.WriteProblem(w, r, err)
			return
		}
		advertiserType, advertiserID = "vendor", vendorID
	}

	reports, err := h.Boosts.Report(r.Context(), advertiserType, advertiserID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	out := make([]map[string]any, 0, len(reports))
	for _, rep := range reports {
		out = append(out, map[string]any{
			"id":          rep.CampaignID,
			"reference":   rep.Reference,
			"status":      rep.Status,
			"impressions": rep.Impressions,
			"spent":       map[string]any{"amount": rep.SpentMinor, "currency": rep.Currency},
			"funded":      map[string]any{"amount": rep.FundedMinor, "currency": rep.Currency},
			"remaining": map[string]any{
				"amount": rep.FundedMinor - rep.SpentMinor, "currency": rep.Currency,
			},
		})
	}
	// No conversion number here, deliberately. Clicks and orders already have a
	// home in the Phase 3 funnel, and a second conversion figure computed a
	// different way in the ad console is exactly what PHASE-3 §4 warns about —
	// two numbers for one question, and whoever is presenting picks.
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"items":         out,
		"conversion_at": "/v1/reels/{reelId}/funnel",
	})
}

func (h *BoostHandlers) SetStatus(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	campaignID, err := uuid.Parse(chi.URLParam(r, "campaignId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.Boosts.AuthorizeAdvertiser(r.Context(), userID, campaignID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if err := h.Boosts.SetStatus(r.Context(), campaignID, body.Status); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Review is the staff gate on creative.
//
// A campaign's reel was already moderated before it could be published, so this
// is not a second content review — it is a review of the *campaign*: is a
// vendor paying to amplify a reel about something they are licensed to sell, is
// the targeting sane, is this a competitor's video. Different question, which is
// why it is a different queue.
func (h *BoostHandlers) Review(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	if err := h.Admin.RequireOps(r.Context(), userID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	campaignID, err := uuid.Parse(chi.URLParam(r, "campaignId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	var body struct {
		Approve bool   `json:"approve"`
		Note    string `json:"note"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.Boosts.Review(r.Context(), campaignID, userID, body.Approve, body.Note); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
