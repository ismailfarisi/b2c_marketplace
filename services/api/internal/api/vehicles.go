package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/classifieds"
	"github.com/marketplace/api/internal/modules/moderation"
	"github.com/marketplace/api/internal/modules/trust"
	"github.com/marketplace/api/internal/modules/vehicles"
)

// VehicleHandlers is the classifieds HTTP surface.
//
// Separate from ReelHandlers and from the order handlers for the same reason
// SubscriptionHandlers is: this is a different business behind the same feed.
// Nothing here creates an order, draws capacity or moves money to a vendor.
type VehicleHandlers struct {
	Listings   *vehicles.Service
	Packages   *classifieds.Service
	Trust      *trust.Service
	Moderation *moderation.Service
}

// --- search and detail ------------------------------------------------------

func (h *VehicleHandlers) Search(w http.ResponseWriter, r *http.Request) {
	f, err := parseFilter(r)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	results, total, err := h.Listings.Search(r.Context(), f)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	items := make([]map[string]any, 0, len(results))
	for _, v := range results {
		item := map[string]any{
			"id":            v.ID,
			"reference_no":  v.ReferenceNo,
			"year":          v.Year,
			"mileage_km":    v.MileageKM,
			"price_minor":   v.PriceMinor,
			"currency":      v.Currency,
			"regional_spec": v.RegionalSpec,
			"seller_kind":   v.SellerKind,
			"is_negotiable": v.IsNegotiable,
			"title":         v.Title,
			"emirate":       v.Emirate,
			"city":          v.City,
			"published_at":  v.PublishedAt,
			// Labelled, always. A promoted result that looks organic is the
			// thing that makes buyers stop trusting the order, at which point
			// the placement the seller paid for is worth nothing.
			"promoted": v.Promoted,
		}
		if v.DistanceM != nil {
			item["distance_m"] = int64(*v.DistanceM)
		}
		items = append(items, item)
	}

	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"items": items,
		"total": total,
		"limit": f.Normalise().Limit,
	})
}

func parseFilter(r *http.Request) (vehicles.Filter, error) {
	q := r.URL.Query()
	f := vehicles.Filter{
		YearMin:       atoiOr(q.Get("year_min"), 0),
		YearMax:       atoiOr(q.Get("year_max"), 0),
		PriceMinMinor: atoi64Or(q.Get("price_min_minor"), 0),
		PriceMaxMinor: atoi64Or(q.Get("price_max_minor"), 0),
		MileageMax:    atoiOr(q.Get("mileage_max"), 0),
		RegionalSpecs: csv(q.Get("regional_spec")),
		Transmissions: csv(q.Get("transmission")),
		FuelTypes:     csv(q.Get("fuel_type")),
		BodyTypes:     csv(q.Get("body_type")),
		SellerKind:    q.Get("seller_kind"),
		Text:          q.Get("q"),
		Sort:          vehicles.Sort(q.Get("sort")),
		Limit:         atoiOr(q.Get("limit"), 0),
		Offset:        atoiOr(q.Get("offset"), 0),
		RadiusM:       atoiOr(q.Get("radius_m"), 0),
	}

	for _, raw := range csv(q.Get("make_id")) {
		id, err := uuid.Parse(raw)
		if err != nil {
			return f, &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "make_id", Code: "invalid",
			}}}
		}
		f.MakeIDs = append(f.MakeIDs, id)
	}
	for _, raw := range csv(q.Get("model_id")) {
		id, err := uuid.Parse(raw)
		if err != nil {
			return f, &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "model_id", Code: "invalid",
			}}}
		}
		f.ModelIDs = append(f.ModelIDs, id)
	}

	lat, latErr := strconv.ParseFloat(q.Get("lat"), 64)
	lng, lngErr := strconv.ParseFloat(q.Get("lng"), 64)
	if latErr == nil && lngErr == nil {
		f.Lat, f.Lng, f.HasGeo = lat, lng, true
	}
	return f, nil
}

// --- authoring --------------------------------------------------------------

type listingBody struct {
	Make         string  `json:"make"`
	Model        string  `json:"model"`
	Year         int     `json:"year"`
	MileageKM    int     `json:"mileage_km"`
	RegionalSpec string  `json:"regional_spec"`
	PriceMinor   int64   `json:"price_minor"`
	Currency     string  `json:"currency"`
	VIN          string  `json:"vin"`
	Transmission string  `json:"transmission"`
	FuelType     string  `json:"fuel_type"`
	BodyType     string  `json:"body_type"`
	Title        string  `json:"title"`
	Description  string  `json:"description"`
	Lat          float64 `json:"lat"`
	Lng          float64 `json:"lng"`
	RadiusM      int     `json:"radius_m"`
}

func (h *VehicleHandlers) CreateListing(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}

	var body listingBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	// seller_kind is deliberately absent from the request body. It is derived
	// from whether this user has an active dealer profile — a private seller who
	// could set it themselves would get the dealer badge for free, and that
	// badge is the only thing a buyer has to go on.
	listing, err := h.Listings.Create(r.Context(), vehicles.CreateListingInput{
		SellerID: userID,
		Draft: vehicles.Draft{
			MakeSlug: body.Make, ModelSlug: body.Model,
			Year: body.Year, MileageKM: body.MileageKM,
			RegionalSpec: body.RegionalSpec,
			PriceMinor:   body.PriceMinor, Currency: body.Currency,
			VIN: body.VIN, Transmission: body.Transmission,
			FuelType: body.FuelType, BodyType: body.BodyType,
			Title: body.Title, Description: body.Description,
			Lat: body.Lat, Lng: body.Lng, RadiusM: body.RadiusM,
		},
	})
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, "", map[string]any{
		"id":           listing.ID,
		"reference_no": listing.ReferenceNo,
		"status":       listing.Status,
		"next_step":    "add photos and a video, then POST /v1/listings/{id}/submit for review",
	})
}

func (h *VehicleHandlers) SubmitListing(w http.ResponseWriter, r *http.Request) {
	userID, listingID, ok := userAndListing(w, r)
	if !ok {
		return
	}
	if err := h.Listings.Submit(r.Context(), userID, listingID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"status": "pending_review"})
}

func (h *VehicleHandlers) ChangePrice(w http.ResponseWriter, r *http.Request) {
	userID, listingID, ok := userAndListing(w, r)
	if !ok {
		return
	}
	var body struct {
		PriceMinor int64 `json:"price_minor"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if err := h.Listings.ChangePrice(r.Context(), userID, listingID, body.PriceMinor); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"price_minor": body.PriceMinor})
}

func (h *VehicleHandlers) MarkSold(w http.ResponseWriter, r *http.Request) {
	userID, listingID, ok := userAndListing(w, r)
	if !ok {
		return
	}
	var body struct {
		SoldPriceMinor int64 `json:"sold_price_minor"`
	}
	_ = httpx.DecodeJSON(r, &body)

	if err := h.Listings.MarkSold(r.Context(), userID, listingID, body.SoldPriceMinor); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"status": "sold",
		// Said out loud in the response because it governs everything
		// downstream: the platform never observes the transaction, so this
		// number is a survey answer and must never be quoted as a market price.
		"note": "the sale price is recorded as declared by the seller and is not verified",
	})
}

func (h *VehicleHandlers) SetStatus(w http.ResponseWriter, r *http.Request) {
	userID, listingID, ok := userAndListing(w, r)
	if !ok {
		return
	}
	var body struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if err := h.Listings.SetStatus(r.Context(), userID, listingID,
		vehicles.Status(body.Status), body.Reason); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"status": body.Status})
}

// --- demand -----------------------------------------------------------------

func (h *VehicleHandlers) Inquire(w http.ResponseWriter, r *http.Request) {
	userID, listingID, ok := userAndListing(w, r)
	if !ok {
		return
	}
	var body struct {
		Source         string     `json:"source"`
		SourceReelID   *uuid.UUID `json:"source_reel_id"`
		SourceActionID *uuid.UUID `json:"source_action_id"`
	}
	_ = httpx.DecodeJSON(r, &body)

	out, err := h.Listings.Inquire(r.Context(), vehicles.InquireInput{
		BuyerID:        userID,
		ListingID:      listingID,
		Source:         body.Source,
		SourceReelID:   body.SourceReelID,
		SourceActionID: body.SourceActionID,
	})
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	status := http.StatusOK
	if out.Created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, "", map[string]any{
		"lead_id":         out.LeadID,
		"conversation_id": out.ConversationID,
	})
}

func (h *VehicleHandlers) MakeOffer(w http.ResponseWriter, r *http.Request) {
	userID, listingID, ok := userAndListing(w, r)
	if !ok {
		return
	}
	var body struct {
		AmountMinor int64 `json:"amount_minor"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	offerID, err := h.Listings.MakeOffer(r.Context(), vehicles.OfferInput{
		BuyerID: userID, ListingID: listingID, AmountMinor: body.AmountMinor,
	})
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, "", map[string]any{
		"id":         offerID,
		"expires_in": vehicles.OfferTTL.String(),
		// The most important line in this file. An "accepted" offer creates no
		// obligation and moves no money; escrow implies a payment-institution
		// licence and is not built. A client that renders this as a contract has
		// misled the buyer, so the API says it rather than leaving it to
		// interface copy.
		"binding": false,
		"note":    "an offer is a stated intention — no payment is held and acceptance is not a contract",
	})
}

func (h *VehicleHandlers) RespondToOffer(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	offerID, err := uuid.Parse(chi.URLParam(r, "offerId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	var body struct {
		Accept bool `json:"accept"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if err := h.Listings.RespondToOffer(r.Context(), userID, offerID, body.Accept); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	state := "rejected"
	if body.Accept {
		state = "accepted"
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"state": state, "binding": false})
}

func (h *VehicleHandlers) Favourite(w http.ResponseWriter, r *http.Request) {
	userID, listingID, ok := userAndListing(w, r)
	if !ok {
		return
	}
	on := r.Method != http.MethodDelete
	if err := h.Listings.Repo().Favourite(r.Context(), userID, listingID, on); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *VehicleHandlers) ReportListing(w http.ResponseWriter, r *http.Request) {
	userID, listingID, ok := userAndListing(w, r)
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if body.Reason == "" {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "reason", Code: "required",
		}}})
		return
	}
	if err := h.Trust.RaiseReport(r.Context(), "listing", listingID, userID, body.Reason); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	// 202: the report is recorded, the outcome is a human's. Returning 200 with
	// no qualifier reads as "handled", and the reporter checks back tomorrow to
	// find the listing still up.
	httpx.WriteJSON(w, http.StatusAccepted, "", map[string]any{"status": "received"})
}

// --- packages ---------------------------------------------------------------

func (h *VehicleHandlers) ListPackages(w http.ResponseWriter, r *http.Request) {
	packages, err := h.Packages.Packages(r.Context(), r.URL.Query().Get("audience"))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(packages))
	for _, p := range packages {
		items = append(items, map[string]any{
			"id": p.ID, "code": p.Code, "name": p.Name, "name_ar": p.NameAr,
			"audience": p.Audience, "listing_quota": p.ListingQuota,
			"duration_days": p.DurationDays, "featured_days": p.FeaturedDays,
			"bump_quota": p.BumpQuota, "price_minor": p.PriceMinor,
			"currency": p.Currency, "is_recurring": p.IsRecurring,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func (h *VehicleHandlers) BuyPackage(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	packageID, err := uuid.Parse(chi.URLParam(r, "packageId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	purchase, err := h.Packages.Buy(r.Context(), userID, packageID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, "", map[string]any{
		"id":           purchase.ID,
		"status":       purchase.Status,
		"amount_minor": purchase.AmountMinor,
		"tax_minor":    purchase.TaxMinor,
		"currency":     purchase.Currency,
		"starts_at":    purchase.StartsAt,
		"ends_at":      purchase.EndsAt,
	})
}

func (h *VehicleHandlers) Bump(w http.ResponseWriter, r *http.Request) {
	userID, listingID, ok := userAndListing(w, r)
	if !ok {
		return
	}
	if err := h.Packages.ConsumeBump(r.Context(), userID, listingID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"bumped": true})
}

func (h *VehicleHandlers) Feature(w http.ResponseWriter, r *http.Request) {
	userID, listingID, ok := userAndListing(w, r)
	if !ok {
		return
	}
	if err := h.Packages.Feature(r.Context(), userID, listingID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"featured": true})
}

// --- moderation -------------------------------------------------------------

func (h *VehicleHandlers) DecideListing(w http.ResponseWriter, r *http.Request) {
	userID, listingID, ok := userAndListing(w, r)
	if !ok {
		return
	}
	if err := h.Moderation.RequireStaff(r.Context(), userID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	var body struct {
		Approve bool   `json:"approve"`
		Reason  string `json:"reason"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	var err error
	if body.Approve {
		err = h.Listings.Approve(r.Context(), userID, listingID)
	} else {
		err = h.Listings.Reject(r.Context(), userID, listingID, body.Reason)
	}
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"decided": true})
}

func (h *VehicleHandlers) FraudQueue(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	if err := h.Moderation.RequireStaff(r.Context(), userID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	items, err := h.Trust.Queue(r.Context(), atoiOr(r.URL.Query().Get("limit"), 50))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	out := make([]map[string]any, 0, len(items))
	for _, q := range items {
		out = append(out, map[string]any{
			"subject_type": q.SubjectType,
			"subject_id":   q.SubjectID,
			"kind":         q.Kind,
			"severity":     q.Severity,
			"score":        q.Score,
			"band":         q.Band,
			// What this kind is worth before anyone confirms it. A reviewer
			// deciding without knowing the cost of "confirm" is guessing.
			"weight_if_confirmed": trust.Weight(q.Kind) * 2,
			"created_at":          q.CreatedAt,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": out})
}

func (h *VehicleHandlers) DecideFraudSignal(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	if err := h.Moderation.RequireStaff(r.Context(), userID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	var body struct {
		SubjectType string    `json:"subject_type"`
		SubjectID   uuid.UUID `json:"subject_id"`
		Kind        string    `json:"kind"`
		Confirm     bool      `json:"confirm"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.Trust.Decide(r.Context(), body.SubjectType, body.SubjectID,
		body.Kind, userID, body.Confirm); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	score, band, err := h.Trust.Rescore(r.Context(), body.SubjectType, body.SubjectID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"score": score, "band": band})
}

// --- helpers ----------------------------------------------------------------

func userAndListing(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return uuid.Nil, uuid.Nil, false
	}
	listingID, err := uuid.Parse(chi.URLParam(r, "listingId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return uuid.Nil, uuid.Nil, false
	}
	return userID, listingID, true
}

func csv(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func atoiOr(s string, fallback int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return fallback
}

func atoi64Or(s string, fallback int64) int64 {
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	return fallback
}
