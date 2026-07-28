package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/admin"
	"github.com/marketplace/api/internal/modules/invoicing"
	"github.com/marketplace/api/internal/modules/kyc"
	"github.com/marketplace/api/internal/modules/moderation"
	"github.com/marketplace/api/internal/modules/notify"
)

// ComplianceHandlers is the Phase 8 surface: notifications, invoices, KYC and
// the appeals queue.
//
// Grouped because they share a property nothing else in the API has — every one
// of them exists to satisfy an obligation rather than to serve a feature
// request. A customer never asked for an opt-out switch, a vendor never asked
// to upload a trade licence, and an author never asked for an appeals form.
// They are all here because launching without them is not permitted.
type ComplianceHandlers struct {
	Notify   *notify.Service
	Invoices *invoicing.Service
	KYC      *kyc.Service
	Strikes  *moderation.StrikeService
	Admin    *admin.Service
	Vendors  *ConsoleHandlers
}

// --- notifications ----------------------------------------------------------

func (h *ComplianceHandlers) Inbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}

	items, err := h.Notify.Inbox(r.Context(), userID, 50)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		out = append(out, map[string]any{
			"id":         it.ID,
			"created_at": it.CreatedAt.Format(time.RFC3339),
			"key":        it.TemplateKey,
			"category":   it.Category,
			"body":       it.Body,
			"read":       it.Read,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": out})
}

func (h *ComplianceHandlers) MarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "notificationId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	if err := h.Notify.MarkRead(r.Context(), userID, id); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SetPreference is the marketing opt-out.
//
// A 422 rather than a 403 when someone tries to switch off a delivery code,
// with the reason in the body. The client shows it next to the switch, which is
// the only place a user can act on it — and the alternative, hiding the
// non-optional categories entirely, leaves people convinced there is a setting
// they cannot find.
func (h *ComplianceHandlers) SetPreference(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}

	var body struct {
		Category string `json:"category"`
		Channel  string `json:"channel"`
		Allowed  bool   `json:"allowed"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.Notify.SetPreference(r.Context(), userID,
		notify.Category(body.Category), notify.Channel(body.Channel), body.Allowed); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- invoices ---------------------------------------------------------------

func (h *ComplianceHandlers) MyInvoices(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}

	list, err := h.Invoices.List(r.Context(), userID, 50)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	out := make([]map[string]any, 0, len(list))
	for _, inv := range list {
		out = append(out, map[string]any{
			"id":     inv.ID,
			"number": inv.Number,
			"kind":   inv.Kind,
			"total":  map[string]any{"amount": inv.GrossMinor, "currency": inv.Currency},
			"tax":    map[string]any{"amount": inv.TaxMinor, "currency": inv.Currency},
			// The tax point, not the issue date. An accountant reconciling a
			// period needs the date of supply and nothing else on this row
			// tells them what it was.
			"supply_date": inv.SupplyDate.Format("2006-01-02"),
			"issued_at":   inv.IssuedAt.Format(time.RFC3339),
			"status":      inv.Status,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": out})
}

// --- KYC --------------------------------------------------------------------

// VendorCompliance is what a vendor sees about their own standing.
//
// Every reason is phrased as something they can do. "Not eligible" opens a
// support ticket; "your trade licence expired on 12 June" gets it renewed, and
// the difference is the entire value of this endpoint.
func (h *ComplianceHandlers) VendorCompliance(w http.ResponseWriter, r *http.Request) {
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
	if err := h.Vendors.Console.AuthorizeVendor(r.Context(), userID, vendorID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	status, err := h.KYC.Eligible(r.Context(), "vendor", vendorID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	reasons := status.Reasons
	if reasons == nil {
		reasons = []string{}
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"eligible_for_payout": status.Eligible,
		"blocking":            reasons,
	})
}

func (h *ComplianceHandlers) AddPayoutAccount(w http.ResponseWriter, r *http.Request) {
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
	if err := h.Vendors.Console.AuthorizeVendor(r.Context(), userID, vendorID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	var body struct {
		IBAN       string `json:"iban"`
		BankName   string `json:"bank_name"`
		HolderName string `json:"holder_name"`
		Currency   string `json:"currency"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if body.Currency == "" {
		body.Currency = "AED"
	}

	id, err := h.KYC.AddPayoutAccount(r.Context(), kyc.PayoutAccount{
		OwnerType: "vendor", OwnerID: vendorID,
		IBAN: body.IBAN, BankName: body.BankName,
		HolderName: body.HolderName, Currency: body.Currency,
	})
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, "", map[string]any{
		"id": id,
		// Never echoed in full, not even to the person who just typed it. The
		// response is logged, cached and screenshotted in ways the request body
		// is not.
		"iban":   kyc.MaskIBAN(body.IBAN),
		"status": "pending",
		"note":   "a new or changed account re-enters verification before the next payout",
	})
}

func (h *ComplianceHandlers) DecideKYC(w http.ResponseWriter, r *http.Request) {
	reviewerID, ok := h.requireOps(w, r)
	if !ok {
		return
	}
	caseID, err := uuid.Parse(chi.URLParam(r, "caseId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
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

	if err := h.KYC.Decide(r.Context(), caseID, reviewerID, body.Approve, body.Reason); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ComplianceHandlers) VerifyPayoutAccount(w http.ResponseWriter, r *http.Request) {
	reviewerID, ok := h.requireOps(w, r)
	if !ok {
		return
	}
	accountID, err := uuid.Parse(chi.URLParam(r, "accountId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	var body struct {
		Approve bool `json:"approve"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.KYC.VerifyPayoutAccount(r.Context(), accountID, reviewerID, body.Approve); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- strikes and appeals ----------------------------------------------------

// Standing is what an author is told about their own record.
//
// Including the date the next strike expires. "You are restricted" with no end
// date is indistinguishable from a ban, and an author who believes they have
// been banned does not come back when the restriction lifts.
func (h *ComplianceHandlers) Standing(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}

	state, err := h.Strikes.Standing(r.Context(), "user", userID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	out := map[string]any{
		"standing":       string(state.Enforcement),
		"active_strikes": state.Active,
	}
	if !state.NextExpiry.IsZero() {
		out["next_expires_at"] = state.NextExpiry.Format(time.RFC3339)
	}
	httpx.WriteJSON(w, http.StatusOK, "", out)
}

func (h *ComplianceHandlers) OpenAppeal(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}

	var body struct {
		SubjectType string `json:"subject_type"`
		SubjectID   string `json:"subject_id"`
		Statement   string `json:"statement"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	subjectID, err := uuid.Parse(body.SubjectID)
	if err != nil {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "subject_id", Code: "invalid",
		}}})
		return
	}

	id, err := h.Strikes.OpenAppeal(r.Context(), moderation.Appeal{
		SubjectType: body.SubjectType,
		SubjectID:   subjectID,
		AppellantID: userID,
		Statement:   body.Statement,
	})
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, "", map[string]any{
		"id":          id,
		"status":      "open",
		"due_in_days": int(moderation.AppealSLA.Hours() / 24),
	})
}

func (h *ComplianceHandlers) DecideAppeal(w http.ResponseWriter, r *http.Request) {
	reviewerID, ok := h.requireOps(w, r)
	if !ok {
		return
	}
	appealID, err := uuid.Parse(chi.URLParam(r, "appealId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	var body struct {
		Overturn bool   `json:"overturn"`
		Note     string `json:"note"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.Strikes.DecideAppeal(r.Context(), appealID, reviewerID, body.Overturn, body.Note); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AppealQueue lists appeals that have breached their SLA.
//
// Overdue first and only, rather than everything sorted by age. The failure
// mode of an appeals process is not that appeals are decided wrongly — it is
// that they are never decided at all, and a queue showing every open appeal
// makes the breached ones invisible among them.
func (h *ComplianceHandlers) AppealQueue(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireOps(w, r); !ok {
		return
	}

	items, err := h.Strikes.OverdueAppeals(r.Context(), 100)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	out := make([]map[string]any, 0, len(items))
	for _, a := range items {
		out = append(out, map[string]any{
			"id":               a.ID,
			"subject_type":     a.SubjectType,
			"subject_id":       a.SubjectID,
			"appellant_id":     a.AppellantID,
			"created_at":       a.CreatedAt.Format(time.RFC3339),
			"due_at":           a.DueAt.Format(time.RFC3339),
			"overdue_by_hours": int(time.Since(a.DueAt).Hours()),
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": out})
}

// requireOps resolves the caller and checks staff authority.
//
// Checked against `platform_admins` on every call rather than carried in a
// token claim — the same rule Phase 2 set when the table was created. Revoking
// someone must take effect immediately, and a claim in a token issued this
// morning does not.
func (h *ComplianceHandlers) requireOps(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return uuid.Nil, false
	}
	if err := h.Admin.RequireOps(r.Context(), userID); err != nil {
		httpx.WriteProblem(w, r, err)
		return uuid.Nil, false
	}
	return userID, true
}

// --- review queues ----------------------------------------------------------

// Queues is the overview an ops shift starts on.
//
// One call rather than nine. It returns every queue including the empty ones —
// a queue that disappears from a dashboard when it empties is one whose sudden
// reappearance nobody notices.
func (h *ComplianceHandlers) Queues(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOps(w, r)
	if !ok {
		return
	}

	depths, err := h.Admin.Queues(r.Context(), userID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	out := make([]map[string]any, 0, len(depths))
	for _, d := range depths {
		out = append(out, map[string]any{
			"queue":          d.Queue,
			"waiting":        d.Waiting,
			"oldest_seconds": d.OldestSeconds,
			// The SLA travels with the row so the client renders the deadline
			// this system actually promises rather than a number a designer
			// picked.
			"sla_seconds":  int(admin.SLAFor(d.Queue).Seconds()),
			"sla_breached": d.BreachedSLA > 0,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"queues": out})
}

// KYCQueue lists identity checks awaiting a decision, with their documents.
func (h *ComplianceHandlers) KYCQueue(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireOps(w, r); !ok {
		return
	}

	cases, err := h.KYC.Queue(r.Context(), 100)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	out := make([]map[string]any, 0, len(cases))
	for _, c := range cases {
		docs := make([]map[string]any, 0, len(c.Documents))
		for _, d := range c.Documents {
			docs = append(docs, map[string]any{
				"id":   d.ID,
				"kind": d.Kind,
				// A reference into object storage, never the document. The
				// client turns it into a short-lived signed link; it is not an
				// image src.
				"document_ref": nullable(d.DocumentRef),
				"number_last4": nullable(d.NumberLast4),
				"issued_on":    dateOrNil(d.IssuedOn),
				"expires_on":   dateOrNil(d.ExpiresOn),
				"status":       d.Status,
				// Computed server-side. A reviewer approving a case backed by a
				// licence that expired last week is the failure this flag
				// exists to make impossible to miss.
				"expired": d.ExpiresOn != nil && d.ExpiresOn.Before(time.Now()),
			})
		}
		out = append(out, map[string]any{
			"id":           c.ID,
			"subject_type": c.SubjectType,
			"subject_id":   c.SubjectID,
			"subject_name": c.SubjectName,
			"tier":         string(c.Tier),
			"status":       c.Status,
			"created_at":   c.CreatedAt.Format(time.RFC3339),
			"documents":    docs,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": out})
}

// PayoutAccountQueue lists bank accounts awaiting the name check.
func (h *ComplianceHandlers) PayoutAccountQueue(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireOps(w, r); !ok {
		return
	}

	accounts, err := h.KYC.PendingAccounts(r.Context(), 100)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	out := make([]map[string]any, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, map[string]any{
			"id":         a.ID,
			"owner_type": a.OwnerType,
			"owner_id":   a.OwnerID,
			// Both names, uncompared. The reviewer's entire job is deciding
			// whether they are the same entity, and no code here should
			// pre-empt that with a fuzzy match that is right 90% of the time.
			"owner_name":  a.OwnerName,
			"holder_name": a.HolderName,
			"iban":        a.MaskedIBAN,
			"bank_name":   nullable(a.BankName),
			"currency":    a.Currency,
			"created_at":  a.CreatedAt.Format(time.RFC3339),
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": out})
}

func dateOrNil(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format("2006-01-02")
}
