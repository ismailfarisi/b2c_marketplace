package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/admin"
	"github.com/marketplace/api/internal/modules/company"
	"github.com/marketplace/api/internal/modules/subscriptions"
)

// CompanyHandlers are office lunch accounts: company sign-up, the company's
// lunch page for its admins, and an employee starting a company-paid lunch.
type CompanyHandlers struct {
	Company *company.Service
}

func callerAndCompany(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return uuid.Nil, uuid.Nil, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "companyId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return uuid.Nil, uuid.Nil, false
	}
	return userID, id, true
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func (h *CompanyHandlers) Register(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	var body company.RegisterInput
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	c, err := h.Company.Register(r.Context(), userID, body)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, "", c)
}

func (h *CompanyHandlers) Mine(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	items, err := h.Company.Benefits(r.Context(), userID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func (h *CompanyHandlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := callerAndCompany(w, r)
	if !ok {
		return
	}
	d, err := h.Company.Dashboard(r.Context(), userID, id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", d)
}

func (h *CompanyHandlers) Statement(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := callerAndCompany(w, r)
	if !ok {
		return
	}
	sid, ok := pathID(w, r, "statementId")
	if !ok {
		return
	}
	st, err := h.Company.Statement(r.Context(), userID, id, sid)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", st)
}

func (h *CompanyHandlers) AddMember(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := callerAndCompany(w, r)
	if !ok {
		return
	}
	var body struct {
		Phone string `json:"phone"`
		Name  string `json:"name"`
		Role  string `json:"role"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if err := h.Company.AddMember(r.Context(), userID, id, body.Phone, body.Name, body.Role); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *CompanyHandlers) RemoveMember(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := callerAndCompany(w, r)
	if !ok {
		return
	}
	mid, ok := pathID(w, r, "memberId")
	if !ok {
		return
	}
	if err := h.Company.RemoveMember(r.Context(), userID, id, mid); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *CompanyHandlers) SetPolicy(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := callerAndCompany(w, r)
	if !ok {
		return
	}
	var body company.Policy
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if err := h.Company.SetPolicy(r.Context(), userID, id, body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *CompanyHandlers) AddOffice(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := callerAndCompany(w, r)
	if !ok {
		return
	}
	var body company.OfficeInput
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if err := h.Company.AddOffice(r.Context(), userID, id, body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *CompanyHandlers) RemoveOffice(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := callerAndCompany(w, r)
	if !ok {
		return
	}
	oid, ok := pathID(w, r, "officeId")
	if !ok {
		return
	}
	if err := h.Company.RemoveOffice(r.Context(), userID, id, oid); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type lunchBody struct {
	OfficeID  uuid.UUID  `json:"office_id"`
	PlanID    uuid.UUID  `json:"plan_id"`
	TierID    *uuid.UUID `json:"tier_id"`
	StartDate string     `json:"start_date"`
	Schedule  []struct {
		Weekday  int       `json:"weekday"`
		Slot     string    `json:"meal_slot"`
		WindowID uuid.UUID `json:"window_id"`
	} `json:"schedule"`
}

func (h *CompanyHandlers) lunch(w http.ResponseWriter, r *http.Request) (company.LunchInput, bool) {
	userID, id, ok := callerAndCompany(w, r)
	if !ok {
		return company.LunchInput{}, false
	}
	var body lunchBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return company.LunchInput{}, false
	}
	start, err := time.Parse("2006-01-02", body.StartDate)
	if err != nil {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "start_date", Code: "invalid_date", Message: "expected YYYY-MM-DD",
		}}})
		return company.LunchInput{}, false
	}
	rules := make([]subscriptions.Rule, 0, len(body.Schedule))
	for _, s := range body.Schedule {
		rules = append(rules, subscriptions.Rule{
			Weekday: subscriptions.Weekday(s.Weekday), Slot: s.Slot, WindowID: s.WindowID,
		})
	}
	var tierID uuid.NullUUID
	if body.TierID != nil {
		tierID = uuid.NullUUID{UUID: *body.TierID, Valid: true}
	}
	return company.LunchInput{
		CompanyID: id,
		OfficeID:  body.OfficeID,
		Sub: subscriptions.SubscribeInput{
			UserID: userID, PlanID: body.PlanID, TierID: tierID, StartDate: start, Rules: rules,
		},
	}, true
}

func (h *CompanyHandlers) PreviewLunch(w http.ResponseWriter, r *http.Request) {
	in, ok := h.lunch(w, r)
	if !ok {
		return
	}
	p, perMeal, err := h.Company.Preview(r.Context(), in)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"preview": p, "per_meal_minor": perMeal})
}

func (h *CompanyHandlers) StartLunch(w http.ResponseWriter, r *http.Request) {
	in, ok := h.lunch(w, r)
	if !ok {
		return
	}
	res, err := h.Company.Subscribe(r.Context(), in)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, "", map[string]any{
		"id":              res.ID,
		"subscription_no": res.SubscriptionNo,
		"status":          res.Status,
		"start_date":      res.StartDate.Format("2006-01-02"),
		"amount_minor":    res.AmountMinor,
		"currency":        res.Currency,
		"paid_by":         "company",
	})
}

// Staff side, in the admin console's Meals section.

func (h *AdminHandlers) Companies(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermCommerceRead); !ok {
		return
	}
	items, err := h.Companies_.AdminList(r.Context(), r.URL.Query().Get("status"), r.URL.Query().Get("q"))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func (h *AdminHandlers) CompanyDetail(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermCommerceRead); !ok {
		return
	}
	id, ok := pathID(w, r, "companyId")
	if !ok {
		return
	}
	d, err := h.Companies_.AdminDetail(r.Context(), id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", d)
}

func (h *AdminHandlers) CompanyStatement(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermCommerceRead); !ok {
		return
	}
	id, ok := pathID(w, r, "companyId")
	if !ok {
		return
	}
	sid, ok := pathID(w, r, "statementId")
	if !ok {
		return
	}
	st, err := h.Companies_.AdminStatement(r.Context(), id, sid)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", st)
}

func (h *AdminHandlers) ReviewCompany(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermCommerceWrite)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "companyId")
	if !ok {
		return
	}
	var body company.ReviewInput
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if err := h.Companies_.Review(r.Context(), grant.UserID, id, body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	reason := body.Note
	if reason == "" {
		reason = "company review: " + body.Status
	}
	h.record(r, grant, "company.review", "company", id, reason, map[string]any{
		"status": body.Status, "credit_limit_minor": body.CreditLimitMinor, "payment_terms_days": body.PaymentTermsDays,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *AdminHandlers) IssueCompanyStatement(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermCommerceWrite)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "companyId")
	if !ok {
		return
	}
	var body struct {
		PeriodEnd string `json:"period_end"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	end, err := time.Parse("2006-01-02", body.PeriodEnd)
	if err != nil {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "period_end", Code: "invalid_date", Message: "expected YYYY-MM-DD",
		}}})
		return
	}
	sid, err := h.Companies_.IssueStatement(r.Context(), grant.UserID, id, end)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	h.record(r, grant, "company.statement_issue", "company", id, "statement to "+body.PeriodEnd, map[string]any{
		"statement_id": sid, "period_end": body.PeriodEnd,
	})
	httpx.WriteJSON(w, http.StatusCreated, "", map[string]any{"id": sid})
}

func (h *AdminHandlers) MarkCompanyStatementPaid(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermCommerceWrite)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "companyId")
	if !ok {
		return
	}
	sid, ok := pathID(w, r, "statementId")
	if !ok {
		return
	}
	var body struct {
		Reference string `json:"reference"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if err := h.Companies_.MarkStatementPaid(r.Context(), grant.UserID, id, sid, body.Reference); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	h.record(r, grant, "company.statement_paid", "company_statement", sid, body.Reference, nil)
	w.WriteHeader(http.StatusNoContent)
}
