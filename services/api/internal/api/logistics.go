package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/dispatch"
	"github.com/marketplace/api/internal/modules/driver"
	"github.com/marketplace/api/internal/seam/fulfillment"
)

// DriverHandlers is the driver's whole API.
//
// PLAN.md §5 P4 calls the native driver app "the single most expensive item in
// this plan" and recommends launching with a web view instead. That advice is
// taken — there is no native app, `apps/web/driver` is the client — and this
// handler set is what either of them would talk to. Nothing here is
// browser-specific, so building the native app later costs a client and not a
// backend.
type DriverHandlers struct {
	Driver   *driver.Service
	Dispatch *dispatch.Service
}

// driverID resolves the signed-in user to a driver on every call.
//
// The client never sends a driver id. An app that can name which driver it is
// acting as is an app that can complete someone else's deliveries.
func (h *DriverHandlers) driverID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return uuid.Nil, false
	}
	id, err := h.Driver.Identify(r.Context(), userID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return uuid.Nil, false
	}
	return id, true
}

func (h *DriverHandlers) StartShift(w http.ResponseWriter, r *http.Request) {
	id, ok := h.driverID(w, r)
	if !ok {
		return
	}
	var body struct {
		BranchID *uuid.UUID `json:"branch_id"`
	}
	_ = httpx.DecodeJSON(r, &body)

	var branch uuid.NullUUID
	if body.BranchID != nil {
		branch = uuid.NullUUID{UUID: *body.BranchID, Valid: true}
	}

	shiftID, err := h.Driver.StartShift(r.Context(), id, branch)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, "", map[string]any{"shift_id": shiftID})
}

func (h *DriverHandlers) EndShift(w http.ResponseWriter, r *http.Request) {
	id, ok := h.driverID(w, r)
	if !ok {
		return
	}
	if err := h.Driver.EndShift(r.Context(), id); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"status": "ended",
		// Said out loud, because it is the surprising part: ending a shift
		// hands undelivered work back rather than keeping it reserved for a
		// driver who has gone home.
		"note": "any undelivered stops have been returned to dispatch",
	})
}

func (h *DriverHandlers) Manifest(w http.ResponseWriter, r *http.Request) {
	id, ok := h.driverID(w, r)
	if !ok {
		return
	}
	date := dateParam(r, "date")

	stops, err := h.Driver.Manifest(r.Context(), id, date)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	items := make([]map[string]any, 0, len(stops))
	for _, s := range stops {
		items = append(items, map[string]any{
			"fulfillment_id": s.FulfillmentID,
			"route_id":       s.RouteID,
			"sequence":       s.Sequence,
			"kind":           s.Kind,
			"status":         s.Status,
			"address": map[string]any{
				"line": s.AddressLine, "landmark": s.AddressLandmark,
				"area": s.AddressArea, "city": s.AddressCity, "notes": s.AddressNotes,
				"lat": s.Lat, "lng": s.Lng,
			},
			"recipient_name":  s.RecipientName,
			"recipient_phone": s.RecipientPhone,
			"window":          s.WindowLabel,
			"eta":             s.ETA,
			"cod_minor":       s.CODMinor,
			"currency":        s.Currency,
			"attempt_count":   s.AttemptCount,
			// Sent with the manifest, not discovered at completion. A driver who
			// learns at the door that they needed an OTP has already walked away.
			"requires_otp": s.RequiresOTP,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"date": date.Format("2006-01-02"), "stops": items})
}

func (h *DriverHandlers) Dispatch_(w http.ResponseWriter, r *http.Request) {
	id, fulfillmentID, ok := h.driverAndStop(w, r)
	if !ok {
		return
	}
	// The code never comes back here. Phase 7 returned it to this handler with a
	// note that sending it was Phase 8's job; Phase 8 moved the enqueue inside
	// the dispatch transaction, so the only thing this layer learns is whether
	// the stop is now dispatched.
	if err := h.Driver.Dispatch(r.Context(), id, fulfillmentID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"status": "dispatched",
		"note":   "a delivery code has been sent to the recipient",
	})
}

func (h *DriverHandlers) Advance(w http.ResponseWriter, r *http.Request) {
	id, fulfillmentID, ok := h.driverAndStop(w, r)
	if !ok {
		return
	}
	var body struct {
		Status string   `json:"status"`
		Lat    *float64 `json:"lat"`
		Lng    *float64 `json:"lng"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.Driver.Advance(r.Context(), id, fulfillmentID,
		fulfillment.Status(body.Status), pointOf(body.Lat, body.Lng)); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"status": body.Status})
}

func (h *DriverHandlers) Complete(w http.ResponseWriter, r *http.Request) {
	id, fulfillmentID, ok := h.driverAndStop(w, r)
	if !ok {
		return
	}
	var body struct {
		Proof          string   `json:"proof"`
		OTP            string   `json:"otp"`
		SignatureKey   string   `json:"signature_key"`
		PhotoKey       string   `json:"photo_key"`
		RecipientName  string   `json:"recipient_name"`
		Note           string   `json:"note"`
		CollectedMinor int64    `json:"collected_minor"`
		Lat            *float64 `json:"lat"`
		Lng            *float64 `json:"lng"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.Driver.Complete(r.Context(), id, driver.CompleteInput{
		FulfillmentID:  fulfillmentID,
		Proof:          driver.ProofKind(body.Proof),
		OTP:            body.OTP,
		SignatureKey:   body.SignatureKey,
		PhotoKey:       body.PhotoKey,
		RecipientName:  body.RecipientName,
		Note:           body.Note,
		CollectedMinor: body.CollectedMinor,
		At:             pointOf(body.Lat, body.Lng),
	}); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"status": "completed"})
}

func (h *DriverHandlers) Fail(w http.ResponseWriter, r *http.Request) {
	id, fulfillmentID, ok := h.driverAndStop(w, r)
	if !ok {
		return
	}
	var body struct {
		Outcome string   `json:"outcome"`
		Note    string   `json:"note"`
		Lat     *float64 `json:"lat"`
		Lng     *float64 `json:"lng"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.Driver.Fail(r.Context(), id, fulfillmentID,
		body.Outcome, body.Note, pointOf(body.Lat, body.Lng)); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"status": "failed", "outcome": body.Outcome})
}

func (h *DriverHandlers) Ping(w http.ResponseWriter, r *http.Request) {
	id, ok := h.driverID(w, r)
	if !ok {
		return
	}
	var body struct {
		Lat       float64    `json:"lat"`
		Lng       float64    `json:"lng"`
		AccuracyM float32    `json:"accuracy_m"`
		SpeedKMH  float32    `json:"speed_kmh"`
		RouteID   *uuid.UUID `json:"route_id"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	var route uuid.NullUUID
	if body.RouteID != nil {
		route = uuid.NullUUID{UUID: *body.RouteID, Valid: true}
	}

	if err := h.Driver.Ping(r.Context(), id,
		driver.Point{Lat: body.Lat, Lng: body.Lng},
		body.AccuracyM, body.SpeedKMH, route); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	// 204, and no body. This is the highest-frequency write in the system —
	// every driver, every fifteen seconds — and a JSON response the client
	// discards is bytes on a mobile connection somebody is paying for.
	w.WriteHeader(http.StatusNoContent)
}

func (h *DriverHandlers) Cash(w http.ResponseWriter, r *http.Request) {
	id, ok := h.driverID(w, r)
	if !ok {
		return
	}
	held, err := h.Driver.HeldCash(r.Context(), id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"collections": held.Collections,
		"total_minor": held.TotalMinor,
		"currency":    held.Currency,
	})
}

func (h *DriverHandlers) DeclareRemittance(w http.ResponseWriter, r *http.Request) {
	id, ok := h.driverID(w, r)
	if !ok {
		return
	}
	remittanceID, total, err := h.Driver.DeclareRemittance(r.Context(), id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, "", map[string]any{
		"remittance_id":  remittanceID,
		"declared_minor": total,
		// The distinction the whole cash model turns on: the driver declares,
		// someone at the branch counts, and the difference is a fact the system
		// can hold rather than one it forces someone to hide.
		"next_step": "hand the cash in — the branch confirms the counted amount",
	})
}

// --- dispatch board (vendor and ops) ----------------------------------------

func (h *DriverHandlers) Board(w http.ResponseWriter, r *http.Request) {
	branchID, ok := branchParam(w, r)
	if !ok {
		return
	}
	board, err := h.Dispatch.Board(r.Context(), branchID, dateParam(r, "date"))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	routes := make([]map[string]any, 0, len(board.Routes))
	for _, rt := range board.Routes {
		routes = append(routes, map[string]any{
			"id": rt.ID, "driver_id": rt.DriverID, "driver_name": rt.DriverName,
			"status": rt.Status, "planned_stops": rt.PlannedStops,
			"completed_stops": rt.CompletedStops, "failed_stops": rt.FailedStops,
			"planned_distance_m": rt.PlannedDistanceM,
			"first_eta":          rt.FirstETA, "last_eta": rt.LastETA,
			"started_at": rt.StartedAt,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"date":       board.Date.Format("2006-01-02"),
		"routes":     routes,
		"unassigned": board.Unassigned,
	})
}

func (h *DriverHandlers) BuildRoutes(w http.ResponseWriter, r *http.Request) {
	branchID, ok := branchParam(w, r)
	if !ok {
		return
	}
	res, err := h.Dispatch.BuildRoutes(r.Context(), branchID, dateParam(r, "date"))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"routes_created": res.RoutesCreated,
		"stops_routed":   res.StopsRouted,
		"unrouted":       res.Unrouted,
		// Surfaced rather than buried. A plan with late stops in it is a
		// reassignment decision now, not a customer discovery in three hours.
		"stops_already_late": res.LateStops,
	})
}

func (h *DriverHandlers) AssignRoute(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	routeID, err := uuid.Parse(chi.URLParam(r, "routeId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	var body struct {
		DriverID uuid.UUID `json:"driver_id"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.Dispatch.Assign(r.Context(), routeID, body.DriverID, "dispatcher", &userID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"status": "assigned"})
}

// --- helpers ----------------------------------------------------------------

func (h *DriverHandlers) driverAndStop(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	id, ok := h.driverID(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	fulfillmentID, err := uuid.Parse(chi.URLParam(r, "fulfillmentId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return uuid.Nil, uuid.Nil, false
	}
	return id, fulfillmentID, true
}

func pointOf(lat, lng *float64) *driver.Point {
	if lat == nil || lng == nil {
		return nil
	}
	return &driver.Point{Lat: *lat, Lng: *lng}
}

// dateParam defaults to today rather than erroring.
//
// A driver opening the app has not chosen a date and never will. Requiring one
// would put a 422 between them and their first stop of the morning.
func dateParam(r *http.Request, key string) time.Time {
	if raw := r.URL.Query().Get(key); raw != "" {
		if d, err := time.Parse("2006-01-02", raw); err == nil {
			return d
		}
	}
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

func branchParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	branchID, err := uuid.Parse(chi.URLParam(r, "branchId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return uuid.Nil, false
	}
	return branchID, true
}
