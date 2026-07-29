package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/admin"
	"github.com/marketplace/api/internal/modules/adminview"
	"github.com/marketplace/api/internal/modules/payouts"
)

// The money endpoints — PLAN-ADMIN-CONSOLE §4.3.
//
// All of them require `money.read`, which is not in the moderator or support
// roles. That is the §3.5 argument made concrete: a content moderator has no
// business knowing what a vendor is owed, and the fact that they could find out
// today by asking someone with a psql prompt is not an argument for building
// them a page.
//
// Read-only, every one. The payout *actions* — issue, approve, pay — are not
// here, and their absence is deliberate rather than unfinished: see the note on
// PayoutRuns below.

func (h *AdminHandlers) TrialBalance(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermMoneyRead); !ok {
		return
	}

	currency := r.URL.Query().Get("currency")
	if currency == "" {
		currency = "AED"
	}
	from, to := dayRange(r)

	tb, err := h.View.LoadTrialBalance(r.Context(), currency, from, to)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	accounts := make([]map[string]any, 0, len(tb.Accounts))
	for _, a := range tb.Accounts {
		accounts = append(accounts, map[string]any{
			"account_id": a.AccountID,
			"type":       a.Type,
			"name":       a.Name,
			"owner_type": nilIfBlank(a.OwnerType),
			"owner_id":   a.OwnerID,
			"debit":      amount(a.DebitMinor, a.Currency),
			"credit":     amount(a.CreditMinor, a.Currency),
			"net":        amount(a.NetMinor, a.Currency),
			"entries":    a.Entries,
		})
	}

	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"as_of":        tb.AsOf,
		"currency":     tb.Currency,
		"accounts":     accounts,
		"total_debit":  amount(tb.TotalDebitMinor, tb.Currency),
		"total_credit": amount(tb.TotalCreditMinor, tb.Currency),
		// The tick everyone looks for. Carried as a boolean rather than left for
		// the client to compare two numbers, because a client that compares
		// them wrongly reports green on a broken ledger.
		"balanced": tb.Balanced,
	})
}

func (h *AdminHandlers) Ledger(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermMoneyRead); !ok {
		return
	}

	q := r.URL.Query()
	from, to := dayRange(r)
	f := adminview.LedgerFilter{
		EventType: q.Get("event_type"),
		From:      from,
		To:        to,
		Limit:     intParam(q.Get("limit"), 50),
	}
	if id, err := uuid.Parse(q.Get("account_id")); err == nil {
		f.AccountID = &id
	}
	if id, err := uuid.Parse(q.Get("reference_id")); err == nil {
		f.Reference = &id
	}

	journals, err := h.View.LoadJournals(r.Context(), f)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	items := make([]map[string]any, 0, len(journals))
	for _, j := range journals {
		lines := make([]map[string]any, 0, len(j.Lines))
		var debit, credit int64
		for _, l := range j.Lines {
			if l.Direction == "debit" {
				debit += l.AmountMinor
			} else {
				credit += l.AmountMinor
			}
			lines = append(lines, map[string]any{
				"account_id":   l.AccountID,
				"account_type": l.AccountType,
				"account":      l.AccountName,
				"direction":    l.Direction,
				"amount":       amount(l.AmountMinor, j.Currency),
			})
		}
		items = append(items, map[string]any{
			"id":             j.ID,
			"event_type":     j.EventType,
			"reference_type": nilIfBlank(j.ReferenceType),
			"reference_id":   j.ReferenceID,
			"description":    nilIfBlank(j.Description),
			"occurred_at":    j.OccurredAt,
			"reverses_id":    j.ReversesID,
			"lines":          lines,
			// Per-journal balance, so an imbalanced posting is visible at the
			// row that caused it rather than only in a total at the bottom of
			// a page nobody scrolls to.
			"balanced": debit == credit,
		})
	}

	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func (h *AdminHandlers) LedgerEventTypes(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermMoneyRead); !ok {
		return
	}
	types, err := h.View.EventTypes(r.Context())
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": types})
}

func (h *AdminHandlers) Refunds(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermMoneyRead)
	if !ok {
		return
	}
	from, to := dayRange(r)

	refunds, err := h.View.LoadRefunds(r.Context(), from, to,
		intParam(r.URL.Query().Get("limit"), 100), lensFor(grant))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	items := make([]map[string]any, 0, len(refunds))
	for _, rf := range refunds {
		items = append(items, map[string]any{
			"id":       rf.ID,
			"order_id": rf.OrderID,
			"order_no": rf.OrderNo,
			"customer": nilIfBlank(rf.Customer),
			"vendor":   nilIfBlank(rf.Vendor),
			"amount":   amount(rf.AmountMinor, rf.Currency),
			"provider": rf.Provider,
			"at":       rf.At,
			"reason":   nilIfBlank(rf.Reason),
			// A null actor means no admin action matched — the refund was issued
			// by something other than a person. Shown rather than hidden,
			// because that is the row worth asking about.
			"actor": nilIfBlank(rf.Actor),
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

// PayoutRuns lists settlement cycles.
//
// Read-only, and the missing actions are a decision. Issuing and paying a run
// moves real money to real bank accounts, and per §3.2 those go through
// `payouts.Service` — which today has no method that takes an actor, writes an
// `admin_actions` row, and refuses to pay a vendor whose licence expired
// yesterday. Adding a button that calls the existing method would produce a
// console that can pay a blocked vendor in one click, which is worse than a
// console that cannot pay anyone.
//
// What this does give the operator is the reason each statement is blocked, in
// words, which is the part that currently takes a four-table join nobody does
// at 5pm on a Thursday.
func (h *AdminHandlers) PayoutRuns(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermMoneyRead); !ok {
		return
	}
	runs, err := h.View.LoadPayoutRuns(r.Context(), intParam(r.URL.Query().Get("limit"), 50))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	items := make([]map[string]any, 0, len(runs))
	for _, run := range runs {
		items = append(items, map[string]any{
			"id":           run.ID,
			"period_start": run.PeriodStart,
			"period_end":   run.PeriodEnd,
			"state":        run.State,
			"statements":   run.StatementCount,
			"total":        amount(run.TotalMinor, run.Currency),
			"approved_by":  nilIfBlank(run.ApprovedBy),
			"approved_at":  run.ApprovedAt,
			"paid_at":      run.PaidAt,
			"created_at":   run.CreatedAt,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func (h *AdminHandlers) PayoutStatements(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermMoneyRead); !ok {
		return
	}

	var runID, vendorID *uuid.UUID
	if id, err := uuid.Parse(chi.URLParam(r, "runId")); err == nil {
		runID = &id
	}
	if id, err := uuid.Parse(r.URL.Query().Get("vendor_id")); err == nil {
		vendorID = &id
	}

	statements, err := h.View.LoadStatements(r.Context(), runID, vendorID,
		intParam(r.URL.Query().Get("limit"), 100))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	items := make([]map[string]any, 0, len(statements))
	for _, st := range statements {
		items = append(items, map[string]any{
			"id":           st.ID,
			"run_id":       st.RunID,
			"vendor_id":    st.VendorID,
			"vendor":       st.Vendor,
			"reference_no": st.ReferenceNo,
			"period_start": st.PeriodStart,
			"period_end":   st.PeriodEnd,
			"gross":        amount(st.GrossMinor, st.Currency),
			"commission":   amount(st.Commission, st.Currency),
			"vat":          amount(st.VATMinor, st.Currency),
			"adjustments":  amount(st.Adjustments, st.Currency),
			"net":          amount(st.NetMinor, st.Currency),
			"state":        st.State,
			"issued_at":    st.IssuedAt,
			"paid_at":      st.PaidAt,
			"payment_ref":  nilIfBlank(st.PaymentRef),
			"blocked":      nilIfBlank(st.BlockedBecause),
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func (h *AdminHandlers) DriverCash(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermMoneyRead)
	if !ok {
		return
	}
	cash, err := h.View.LoadDriverCash(r.Context(), lensFor(grant))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	items := make([]map[string]any, 0, len(cash))
	var totalHeld int64
	currency := "AED"
	for _, c := range cash {
		totalHeld += c.HeldMinor
		currency = c.Currency
		items = append(items, map[string]any{
			"driver_id":   c.DriverID,
			"driver":      c.Driver,
			"phone":       nilIfBlank(c.Phone),
			"held":        amount(c.HeldMinor, c.Currency),
			"held_stops":  c.HeldStops,
			"remitted":    amount(c.RemittedMinor, c.Currency),
			"settled":     amount(c.SettledMinor, c.Currency),
			"shortfall":   amount(c.ShortfallMinor, c.Currency),
			"oldest_held": c.OldestHeld,
		})
	}

	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"items": items,
		// The one number this page exists for: how much of the platform's cash
		// is currently in somebody else's pocket.
		"total_held": amount(totalHeld, currency),
	})
}

func (h *AdminHandlers) Remittances(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermMoneyRead); !ok {
		return
	}

	var driverID *uuid.UUID
	if id, err := uuid.Parse(r.URL.Query().Get("driver_id")); err == nil {
		driverID = &id
	}

	rems, err := h.View.LoadRemittances(r.Context(), driverID,
		intParam(r.URL.Query().Get("limit"), 100))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	items := make([]map[string]any, 0, len(rems))
	for _, rm := range rems {
		var counted any
		if rm.CountedMinor != nil {
			counted = amount(*rm.CountedMinor, rm.Currency)
		}
		items = append(items, map[string]any{
			"id":           rm.ID,
			"driver_id":    rm.DriverID,
			"driver":       rm.Driver,
			"reference_no": rm.ReferenceNo,
			"declared":     amount(rm.DeclaredMinor, rm.Currency),
			"counted":      counted,
			"expected":     amount(rm.ExpectedMinor, rm.Currency),
			"variance":     amount(rm.VarianceMinor, rm.Currency),
			"state":        rm.State,
			"method":       rm.Method,
			"declared_at":  rm.DeclaredAt,
			"settled_at":   rm.SettledAt,
			"collections":  rm.CollectionsQty,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func (h *AdminHandlers) Invoices(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermMoneyRead)
	if !ok {
		return
	}
	from, to := dayRange(r)

	invoices, err := h.View.LoadInvoices(r.Context(), r.URL.Query().Get("q"), from, to,
		intParam(r.URL.Query().Get("limit"), 100), lensFor(grant))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	items := make([]map[string]any, 0, len(invoices))
	for _, i := range invoices {
		items = append(items, map[string]any{
			"id":              i.ID,
			"number":          i.Number,
			"kind":            i.Kind,
			"status":          i.Status,
			"supplier":        i.SupplierName,
			"supplier_trn":    nilIfBlank(i.SupplierTRN),
			"buyer":           nilIfBlank(i.BuyerName),
			"buyer_trn":       nilIfBlank(i.BuyerTRN),
			"country":         i.Country,
			"net":             amount(i.NetMinor, i.Currency),
			"tax":             amount(i.TaxMinor, i.Currency),
			"gross":           amount(i.GrossMinor, i.Currency),
			"supply_date":     i.SupplyDate,
			"issued_at":       i.IssuedAt,
			"order_id":        i.OrderID,
			"order_no":        nilIfBlank(i.OrderNo),
			"corrects_id":     i.CorrectsID,
			"corrects_number": nilIfBlank(i.CorrectsNumber),
			"has_pdf":         i.HasPDF,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

// ── shared helpers ──────────────────────────────────────────────────────────

// dateRange reads `from` and `to`, both optional, both dates.
//
// `to` is pushed to the *end* of the named day before being used as an
// exclusive bound, so "to = 2026-07-29" includes everything that happened on
// the 29th. Without it, a user asking for a single day gets nothing, decides
// the filter is broken, and stops using it.
func dayRange(r *http.Request) (*time.Time, *time.Time) {
	q := r.URL.Query()
	var from, to *time.Time
	if t, err := time.Parse("2006-01-02", q.Get("from")); err == nil {
		from = &t
	}
	if t, err := time.Parse("2006-01-02", q.Get("to")); err == nil {
		end := t.AddDate(0, 0, 1)
		to = &end
	}
	return from, to
}

func intParam(s string, fallback int) int {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n
	}
	return fallback
}

func nilIfBlank(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// amount is the shape every money value takes on the wire.
//
// Minor units and a currency code, never a formatted string and never a float.
// A float would be wrong by fractions of a fils at scale; a formatted string
// moves the rounding decision to whoever wrote the formatter, which in practice
// means each screen rounds differently and a total disagrees with its own rows.
func amount(minor int64, currency string) map[string]any {
	return map[string]any{"amount": minor, "currency": currency}
}

// ── payout actions ──────────────────────────────────────────────────────────
//
// The buttons the first pass refused to build. They exist now because the
// service methods behind them do: `payouts.IssueByStaff` and `PayByStaff` take
// an actor, re-check eligibility at the moment money moves rather than trusting
// a gate that ran days earlier, and write their audit row in the same
// transaction as the state change.
//
// All three need `money.write`, which is deliberately never in the same default
// role as `identity.review`. Approving a bank account and then paying it is
// payout fraud in two clicks, and the permission model is where that is
// prevented rather than in a policy document.

func (h *AdminHandlers) ApproveRun(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermMoneyWrite)
	if !ok {
		return
	}
	runID, err := uuid.Parse(chi.URLParam(r, "runId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.Act.Payouts.ApproveRun(r.Context(), payouts.ApproveRunInput{
		RunID: runID, ActorID: grant.UserID, Reason: body.Reason,
	}); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"status": "approved",
		// Said explicitly because the next click is the one that matters and the
		// word "approved" reads like it already happened.
		"note": "nothing has been issued yet. Approval records that a person reviewed the totals.",
	})
}

func (h *AdminHandlers) IssueRun(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermMoneyWrite)
	if !ok {
		return
	}
	runID, err := uuid.Parse(chi.URLParam(r, "runId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	result, err := h.Act.Payouts.IssueRun(r.Context(), runID, grant.UserID, body.Reason)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	skipped := make([]map[string]any, 0, len(result.Skipped))
	for _, s := range result.Skipped {
		skipped = append(skipped, map[string]any{
			"statement_id": s.StatementID,
			"vendor_id":    s.VendorID,
			"vendor":       s.Vendor,
			"reason":       s.Reason,
		})
	}

	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"issued": result.Issued,
		// Returned rather than swallowed. Eighteen skips that all read the same
		// is a process problem, and an operator can only see that if they are
		// given the list rather than a count.
		"skipped": skipped,
	})
}

func (h *AdminHandlers) IssueStatement(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermMoneyWrite)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "statementId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.Act.Payouts.IssueByStaff(r.Context(), payouts.IssueInput{
		StatementID: id, ActorID: grant.UserID, Reason: body.Reason,
	}); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"status": "issued"})
}

func (h *AdminHandlers) PayStatement(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermMoneyWrite)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "statementId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	var body struct {
		PaymentRef string `json:"payment_ref"`
		Reason     string `json:"reason"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.Act.Payouts.PayByStaff(r.Context(), payouts.PayInput{
		StatementID: id, ActorID: grant.UserID,
		PaymentRef: body.PaymentRef, Reason: body.Reason,
	}); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"status": "paid",
		"note":   "the ledger has been posted. The transfer itself happens in the bank.",
	})
}
