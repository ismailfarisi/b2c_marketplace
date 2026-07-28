package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/admin"
	"github.com/marketplace/api/internal/modules/console"
	"github.com/marketplace/api/internal/modules/driver"
	"github.com/marketplace/api/internal/modules/payouts"
)

// ConsoleHandlers is the vendor dashboard and the ops surface.
//
// Every branch-scoped route authorises through console.AuthorizeBranch, which
// returns 404 rather than 403 for a branch the caller does not work for. A 403
// confirms the branch exists, and vendor ids are the one identifier competitors
// would most like to enumerate.
type ConsoleHandlers struct {
	Console *console.Service
	Payouts *payouts.Service
	Driver  *driver.Service
	Admin   *admin.Service
}

func (h *ConsoleHandlers) Memberships(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	memberships, err := h.Console.Memberships(r.Context(), userID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(memberships))
	for _, m := range memberships {
		items = append(items, map[string]any{
			"vendor_id": m.VendorID, "role": m.Role, "branch_id": m.BranchID,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func (h *ConsoleHandlers) Summary(w http.ResponseWriter, r *http.Request) {
	branchID, ok := h.branch(w, r)
	if !ok {
		return
	}
	sum, err := h.Console.Today(r.Context(), branchID, dateParam(r, "date"))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"date":            sum.Date.Format("2006-01-02"),
		"scheduled":       sum.Scheduled,
		"in_progress":     sum.InProgress,
		"completed":       sum.Completed,
		"failed":          sum.Failed,
		"unassigned":      sum.Unassigned,
		"cash_held_minor": sum.CashHeldMinor,
		// Live from the ledger, not from the last statement. A vendor asking
		// "what am I owed" on a Wednesday wants the current number, and
		// statements close weekly.
		"payable_minor": sum.PayableMinor,
		"currency":      sum.Currency,
	})
}

func (h *ConsoleHandlers) Queue(w http.ResponseWriter, r *http.Request) {
	branchID, ok := h.branch(w, r)
	if !ok {
		return
	}
	items, err := h.Console.Queue(r.Context(), branchID, dateParam(r, "date"))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	out := make([]map[string]any, 0, len(items))
	for _, q := range items {
		out = append(out, map[string]any{
			"fulfillment_id": q.FulfillmentID,
			"order_no":       q.OrderNo,
			"service_type":   q.ServiceType,
			"kind":           q.Kind,
			"status":         q.Status,
			"window":         q.WindowLabel,
			"recipient_name": q.RecipientName,
			"area":           q.AddressArea,
			"items":          q.ItemSummary,
			"payload_ref":    q.PayloadRef,
			"driver_name":    q.DriverName,
			"eta":            q.ETA,
			"attempt_count":  q.AttemptCount,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": out})
}

func (h *ConsoleHandlers) Calendar(w http.ResponseWriter, r *http.Request) {
	branchID, ok := h.branch(w, r)
	if !ok {
		return
	}
	from := dateParam(r, "from")
	to := from.AddDate(0, 0, 13)
	if raw := r.URL.Query().Get("to"); raw != "" {
		if d, err := time.Parse("2006-01-02", raw); err == nil {
			to = d
		}
	}

	cells, err := h.Console.Calendar(r.Context(), branchID, from, to)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	out := make([]map[string]any, 0, len(cells))
	for _, c := range cells {
		out = append(out, map[string]any{
			"date": c.Date.Format("2006-01-02"), "window_id": c.WindowID,
			"window": c.WindowLabel, "resource_key": c.ResourceKey,
			"capacity": c.Capacity, "reserved": c.Reserved, "consumed": c.Consumed,
			"remaining": c.Remaining(),
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"cells": out})
}

func (h *ConsoleHandlers) Roster(w http.ResponseWriter, r *http.Request) {
	branchID, ok := h.branch(w, r)
	if !ok {
		return
	}
	entries, err := h.Console.Roster(r.Context(), branchID, dateParam(r, "date"))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]any{
			"subscription_no": e.SubscriptionNo,
			"customer_name":   e.CustomerName,
			"meal_slot":       e.Slot,
			"window":          e.WindowLabel,
			"area":            e.AddressArea,
			"item":            e.SelectionName,
			"status":          e.Status,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": out})
}

// --- payouts ----------------------------------------------------------------

func (h *ConsoleHandlers) Statements(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	vendorID, err := uuid.Parse(chi.URLParam(r, "vendorId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	if err := h.Console.AuthorizeVendor(r.Context(), userID, vendorID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	statements, err := h.Payouts.List(r.Context(), vendorID, atoiOr(r.URL.Query().Get("limit"), 24))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	out := make([]map[string]any, 0, len(statements))
	for _, st := range statements {
		out = append(out, map[string]any{
			"id": st.ID, "reference_no": st.ReferenceNo,
			"period_start": st.PeriodStart.Format("2006-01-02"),
			"period_end":   st.PeriodEnd.Format("2006-01-02"),
			"currency":     st.Currency,
			"gross_minor":  st.GrossMinor, "commission_minor": st.CommissionMinor,
			"vat_minor": st.VATMinor, "adjustments_minor": st.AdjustmentsMinor,
			"net_minor": st.NetMinor, "state": st.State,
			"issued_at": st.IssuedAt, "paid_at": st.PaidAt,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": out})
}

func (h *ConsoleHandlers) StatementDetail(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	vendorID, verr := uuid.Parse(chi.URLParam(r, "vendorId"))
	statementID, serr := uuid.Parse(chi.URLParam(r, "statementId"))
	if verr != nil || serr != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	if err := h.Console.AuthorizeVendor(r.Context(), userID, vendorID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	lines, err := h.Payouts.Detail(r.Context(), vendorID, statementID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	out := make([]map[string]any, 0, len(lines))
	for _, l := range lines {
		out = append(out, map[string]any{
			"occurred_on": l.OccurredOn.Format("2006-01-02"),
			"kind":        l.Kind,
			"description": l.Description,
			// The pointer back into the ledger. This is what makes "why is this
			// AED 43" answerable rather than re-derivable.
			"journal_id":       l.JournalID,
			"reference_type":   l.ReferenceType,
			"reference_id":     l.ReferenceID,
			"gross_minor":      l.GrossMinor,
			"commission_minor": l.CommissionMinor,
			"vat_minor":        l.VATMinor,
			"net_minor":        l.NetMinor,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"lines": out})
}

// SettleRemittance is the branch counting a driver's cash.
//
// Vendor-side rather than driver-side on purpose: the person who counts must not
// be the person who declared.
func (h *ConsoleHandlers) SettleRemittance(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	remittanceID, err := uuid.Parse(chi.URLParam(r, "remittanceId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	var body struct {
		CountedMinor int64 `json:"counted_minor"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.Driver.SettleRemittance(r.Context(), remittanceID, body.CountedMinor, userID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"status": "settled",
		// The honest description of what a shortfall is. No posting moves it:
		// it stays in the driver's float, which is exactly what that account
		// means — cash they are holding that the platform has not received.
		"note": "any difference remains on the driver's float until it is recovered or written off",
	})
}

// --- disputes ---------------------------------------------------------------

func (h *ConsoleHandlers) OpenDispute(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	var body struct {
		SubjectType string    `json:"subject_type"`
		SubjectID   uuid.UUID `json:"subject_id"`
		RaisedAs    string    `json:"raised_as"`
		Kind        string    `json:"kind"`
		Body        string    `json:"body"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if body.RaisedAs == "" {
		body.RaisedAs = "customer"
	}

	d, err := h.Admin.Open(r.Context(), admin.OpenInput{
		SubjectType:  body.SubjectType,
		SubjectID:    body.SubjectID,
		RaisedByType: body.RaisedAs,
		RaisedByID:   userID,
		Kind:         body.Kind,
		Body:         body.Body,
	})
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, "", map[string]any{
		"id": d.ID, "reference_no": d.ReferenceNo, "state": d.State,
		// The SLA, returned rather than kept internal. A customer told when to
		// expect an answer stops asking for one every hour.
		"due_at": d.DueAt,
	})
}

func (h *ConsoleHandlers) DisputeQueue(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	if err := h.Admin.RequireOps(r.Context(), userID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	items, err := h.Admin.Queue(r.Context(), atoiOr(r.URL.Query().Get("limit"), 50))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	now := time.Now()
	out := make([]map[string]any, 0, len(items))
	for _, d := range items {
		overdue := d.DueAt != nil && d.DueAt.Before(now)
		out = append(out, map[string]any{
			"id": d.ID, "reference_no": d.ReferenceNo,
			"subject_type": d.SubjectType, "subject_id": d.SubjectID,
			"kind": d.Kind, "state": d.State, "due_at": d.DueAt,
			"assigned_to": d.AssignedTo, "created_at": d.CreatedAt,
			"overdue": overdue,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": out})
}

func (h *ConsoleHandlers) ResolveDispute(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	if err := h.Admin.RequireOps(r.Context(), userID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	disputeID, err := uuid.Parse(chi.URLParam(r, "disputeId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	var body struct {
		Resolution  string `json:"resolution"`
		AmountMinor int64  `json:"amount_minor"`
		Note        string `json:"note"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.Admin.Resolve(r.Context(), disputeID, userID,
		body.Resolution, body.AmountMinor, body.Note,
		httpx.RequestIDFromContext(r.Context())); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"state": "resolved"})
}

func (h *ConsoleHandlers) branch(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return uuid.Nil, false
	}
	branchID, ok := branchParam(w, r)
	if !ok {
		return uuid.Nil, false
	}
	if _, err := h.Console.AuthorizeBranch(r.Context(), userID, branchID); err != nil {
		httpx.WriteProblem(w, r, err)
		return uuid.Nil, false
	}
	return branchID, true
}
