package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/auth"
	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/admin"
	"github.com/marketplace/api/internal/modules/adminview"
	"github.com/marketplace/api/internal/modules/kitchen"
	"github.com/marketplace/api/internal/modules/merch"
	"github.com/marketplace/api/internal/modules/ordering"
	"github.com/marketplace/api/internal/modules/payments"
	"github.com/marketplace/api/internal/modules/payouts"
	"github.com/marketplace/api/internal/modules/subscriptions"
)

// AdminHandlers is the admin console's read surface.
//
// Named apart from ConsoleHandlers next door, which is the *vendor* console — a
// different audience, a different authorisation model, and the two would be
// confused for each other at exactly the wrong moment.
//
// PLAN-ADMIN-CONSOLE §3.6: one endpoint per page. Every handler below returns a
// whole screen, and the redaction happens in `adminview` rather than here —
// which is what stops a serializer that forgets a check from leaking a phone
// number.
type AdminHandlers struct {
	View  *adminview.Service
	Admin *admin.Service
	Act   Actions
	// Media signs short-lived playback URLs for content that is not public.
	// Nil disables the media endpoint rather than failing it at request time —
	// an environment without an object store should refuse to offer playback,
	// not offer it and then 500.
	Media MediaSigner
	// Feed answers what the ranker would return for a point. Nil where there is
	// no media stack, which disables the inspector rather than offering a page
	// that always errors.
	Feed FeedInspector
	// Issuer mints view-as tokens. Nil disables impersonation entirely, which is
	// a perfectly good state for a deployment to be in.
	Issuer *auth.Issuer
	// Meals. Nil disables the Meals section.
	Kitchen       *kitchen.Service
	Subscriptions *subscriptions.Service
	// Merch is storefront merchandising. Nil hides it.
	Merch *merch.Service
}

// caller resolves the staff member and their permission, and logs the read when
// the permission is a sensitive one.
//
// The audit write happens here rather than inside `adminview` because this is
// the layer that knows the request — the IP, the user agent, the request id.
// §3.4: the question after a breach is not "was it encrypted", it is who looked
// at it.
func (h *AdminHandlers) caller(w http.ResponseWriter, r *http.Request, perm string) (admin.Grant, bool) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return admin.Grant{}, false
	}
	grant, err := h.Admin.Require(r.Context(), userID, perm)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return admin.Grant{}, false
	}
	return grant, true
}

// lensFor turns a grant into the redaction the read model applies.
func lensFor(g admin.Grant) adminview.Lens {
	return adminview.Lens{
		Contact:  g.Can(admin.PermCommerceContact),
		Money:    g.Can(admin.PermMoneyRead),
		Identity: g.Can(admin.PermIdentityReview),
	}
}

// recordIfSensitive writes the PII access row when the lens actually exposed
// something.
//
// Conditioned on the *lens*, not the route: a moderator opening an order sees a
// redacted document and has read nothing personal, so logging it would add noise
// to the log whose whole value is being quiet.
func (h *AdminHandlers) recordIfSensitive(r *http.Request, g admin.Grant, lens adminview.Lens, targetType string, targetID uuid.UUID) {
	if !lens.Contact && !lens.Identity {
		return
	}
	perm := admin.PermCommerceContact
	if lens.Identity && !lens.Contact {
		perm = admin.PermIdentityReview
	}
	h.Admin.RecordAccess(r.Context(), admin.Access{
		ActorID:    g.UserID,
		Permission: perm,
		TargetType: targetType,
		TargetID:   targetID,
		IP:         clientIP(r),
		UserAgent:  r.UserAgent(),
		RequestID:  httpx.RequestIDFromContext(r.Context()),
	})
}

// --- identity ---------------------------------------------------------------

// Whoami tells the console what this staff member may do.
//
// The UI uses it to hide what a person cannot use. That is a convenience, never
// a control — every endpoint checks server-side, and a hidden button is a
// nicety, not a permission.
func (h *AdminHandlers) Whoami(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	grant, err := h.Admin.Grants(r.Context(), userID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	perms := make([]string, 0, len(grant.Permissions))
	for p := range grant.Permissions {
		perms = append(perms, p)
	}
	roles := grant.Roles
	if roles == nil {
		roles = []string{}
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"user_id":     userID,
		"roles":       roles,
		"permissions": perms,
		"is_staff":    len(perms) > 0,
	})
}

// --- search -----------------------------------------------------------------

// Search resolves whatever a support agent typed.
//
// One endpoint over eight identifier shapes, because the person typing has a
// customer on the phone and does not know whether `AE-G3CRYK` is an order
// number or a reference — they know it is what was read out to them.
func (h *AdminHandlers) Search(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermCommerceRead)
	if !ok {
		return
	}

	hits, err := h.View.Search(r.Context(), r.URL.Query().Get("q"), lensFor(grant))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	out := make([]map[string]any, 0, len(hits))
	for _, hit := range hits {
		out = append(out, map[string]any{
			"kind": hit.Kind, "id": hit.ID, "label": hit.Label, "sub": hit.Sub,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": out})
}

// --- order ------------------------------------------------------------------

func (h *AdminHandlers) Order(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermCommerceRead)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "orderId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	lens := lensFor(grant)
	o, err := h.View.LoadOrder(r.Context(), id, lens)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	h.recordIfSensitive(r, grant, lens, "order", id)

	lines := make([]map[string]any, 0, len(o.Lines))
	for _, l := range o.Lines {
		lines = append(lines, map[string]any{
			"name": l.Name, "quantity": l.Quantity, "options": rawJSON(l.Options),
		})
	}

	quoteLines := make([]map[string]any, 0, len(o.Quote.Lines))
	for _, l := range o.Quote.Lines {
		quoteLines = append(quoteLines, map[string]any{
			"kind": l.Kind, "label": l.Label,
			"amount": money(l.Amount), "tax_rate_bp": l.TaxBP, "tax": money(l.Tax),
		})
	}

	payments := make([]map[string]any, 0, len(o.Payments))
	for _, p := range o.Payments {
		payments = append(payments, map[string]any{
			"id": p.ID, "kind": p.Kind, "status": p.Status, "provider": p.Provider,
			"amount": money(p.Amount), "three_ds": nullable(p.ThreeDS),
			"failure_code": nullable(p.FailureCode),
			"created_at":   p.CreatedAt.Format(time.RFC3339),
			"settled_at":   timeOrNil(p.SettledAt),
		})
	}

	stops := make([]map[string]any, 0, len(o.Stops))
	for _, st := range o.Stops {
		stops = append(stops, map[string]any{
			"id": st.ID, "kind": st.Kind, "status": st.Status, "sequence": st.Sequence,
			"date": st.Date.Format("2006-01-02"), "window": st.Window,
			"address": nullable(st.Address), "recipient": nullable(st.Recipient),
			"phone":       nullable(st.Phone),
			"driver_id":   st.DriverID,
			"driver_name": nullable(st.DriverName),
			"attempts":    st.AttemptCount,
			"cod_minor":   st.CODMinor,
			// The two together answer "is the customer holding a code" without
			// anyone reading the code — which nothing in this system can do.
			"otp_issued":     st.OTPIssued,
			"otp_sent":       st.OTPSent,
			"proof":          nullable(st.ProofKind),
			"completed_at":   timeOrNil(st.CompletedAt),
			"failed_at":      timeOrNil(st.FailedAt),
			"failure_reason": nullable(st.FailureReason),
		})
	}

	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"id": o.ID, "order_no": o.OrderNo, "status": o.Status, "kind": o.Kind,
		"service_type": o.ServiceType, "currency": o.Currency,
		"placed_at": o.PlacedAt.Format(time.RFC3339), "notes": nullable(o.Notes),
		"customer": map[string]any{
			"id": o.Customer.ID, "name": nullable(o.Customer.Name),
			"phone": nullable(o.Customer.Phone),
		},
		"branch": map[string]any{
			"id": o.Branch.ID, "name": o.Branch.Name,
			"vendor_id": o.Branch.VendorID, "vendor": o.Branch.Vendor,
		},
		"lines": lines,
		"quote": map[string]any{
			"state": o.Quote.State, "tax_inclusive": o.Quote.TaxInclusive,
			"subtotal": money(o.Quote.Subtotal), "discount": money(o.Quote.Discount),
			"delivery": money(o.Quote.Delivery), "surcharge": money(o.Quote.Surcharge),
			"tax": money(o.Quote.Tax), "total": money(o.Quote.Total),
			"lines": quoteLines,
		},
		"payments": payments,
		"stops":    stops,
		"attribution": map[string]any{
			"model": nullable(o.AttributionModel), "reel_id": o.AttributedReelID,
		},
		"notifications": notificationsJSON(o.Notifications),
		"invoice":       invoiceJSON(o.Invoice),
	})
}

func (h *AdminHandlers) OrderTimeline(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermCommerceRead); !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "orderId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	entries, err := h.View.OrderTimeline(r.Context(), id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": timelineJSON(entries)})
}

// --- customer ---------------------------------------------------------------

func (h *AdminHandlers) Customer(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermCommerceRead)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	lens := lensFor(grant)
	c, err := h.View.LoadCustomer(r.Context(), id, lens)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	h.recordIfSensitive(r, grant, lens, "user", id)

	addresses := make([]map[string]any, 0, len(c.Addresses))
	for _, a := range c.Addresses {
		addresses = append(addresses, map[string]any{
			"id": a.ID, "label": a.Label, "recipient": a.Recipient,
			"line": a.Line, "area": a.Area, "city": a.City, "is_default": a.IsDefault,
		})
	}

	orders := make([]map[string]any, 0, len(c.Orders))
	for _, o := range c.Orders {
		orders = append(orders, map[string]any{
			"id": o.ID, "order_no": o.OrderNo, "status": o.Status,
			"total": money(o.Total), "placed_at": o.PlacedAt.Format(time.RFC3339),
			"branch": o.Branch,
		})
	}

	subs := make([]map[string]any, 0, len(c.Subscriptions))
	for _, s := range c.Subscriptions {
		subs = append(subs, map[string]any{
			"id": s.ID, "status": s.Status, "plan": s.Plan,
			"materialised_through": timeOrNil(s.NextAt),
		})
	}

	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"id": c.ID, "phone": nullable(c.Phone), "email": nullable(c.Email),
		"display_name": nullable(c.DisplayName), "locale": c.Locale,
		"notification_locale": nullable(c.NotifyLocale),
		"status":              c.Status, "country": c.Country,
		"created_at":    c.CreatedAt.Format(time.RFC3339),
		"addresses":     addresses,
		"orders":        orders,
		"subscriptions": subs,
		"notifications": notificationsJSON(c.Notifications),
		"standing": map[string]any{
			"active_strikes": c.Standing.ActiveStrikes,
			"open_appeals":   c.Standing.OpenAppeals,
		},
		"blocked_count": c.BlockedCount,
	})
}

func (h *AdminHandlers) CustomerTimeline(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermCommerceRead); !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	entries, err := h.View.CustomerTimeline(r.Context(), id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": timelineJSON(entries)})
}

// --- vendor -----------------------------------------------------------------

func (h *AdminHandlers) Vendor(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermCommerceRead)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "vendorId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	lens := lensFor(grant)
	v, err := h.View.LoadVendor(r.Context(), id, lens)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	h.recordIfSensitive(r, grant, lens, "vendor", id)

	branches := make([]map[string]any, 0, len(v.Branches))
	for _, b := range v.Branches {
		branches = append(branches, map[string]any{
			"id": b.ID, "name": b.Name, "city": nullable(b.City),
			"is_active": b.IsActive, "drivers": b.Drivers,
		})
	}
	staff := make([]map[string]any, 0, len(v.Staff))
	for _, st := range v.Staff {
		staff = append(staff, map[string]any{
			"user_id": st.UserID, "name": nullable(st.Name),
			"phone": nullable(st.Phone), "role": st.Role,
		})
	}
	statements := make([]map[string]any, 0, len(v.Statements))
	for _, st := range v.Statements {
		statements = append(statements, map[string]any{
			"id": st.ID, "reference": st.Reference,
			"period_start": st.Start.Format("2006-01-02"),
			"period_end":   st.End.Format("2006-01-02"),
			"net":          money(st.Net), "state": st.State,
			"paid_at": timeOrNil(st.PaidAt),
		})
	}

	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"id": v.ID, "slug": v.Slug, "legal_name": v.LegalName,
		"trade_name": v.TradeName, "status": v.Status, "country": v.Country,
		"currency": v.Currency, "rating": v.Rating,
		"created_at": v.CreatedAt.Format(time.RFC3339),
		"trade_licence": map[string]any{
			"number":  nullable(v.TradeLicence),
			"expires": dateOrNil(v.LicenceExpires),
			// Computed here rather than left to the client, because "expired" is
			// the fact that blocks a payout and a client that computes it in a
			// different timezone gets it wrong for one day a year.
			"expired": v.LicenceExpires != nil && v.LicenceExpires.Before(time.Now()),
		},
		"tax_registration": nullable(v.TaxRegistration),
		"branches":         branches,
		"staff":            staff,
		"compliance": map[string]any{
			"kyc_state":      v.Compliance.KYCState,
			"payout_account": v.Compliance.Account,
		},
		"payable":       money(v.Payable),
		"statements":    statements,
		"open_disputes": v.Disputes,
		"reels": map[string]any{
			"published":      v.Reels.Published,
			"pending_review": v.Reels.Pending,
			"last_published": timeOrNil(v.Reels.LastPublished),
		},
	})
}

func (h *AdminHandlers) VendorTimeline(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermCommerceRead); !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "vendorId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	entries, err := h.View.VendorTimeline(r.Context(), id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": timelineJSON(entries)})
}

// --- staff ------------------------------------------------------------------

func (h *AdminHandlers) Staff(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermStaffAdmin)
	if !ok {
		return
	}
	members, err := h.Admin.Staff(r.Context(), grant.UserID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	roles, err := h.Admin.Roles(r.Context())
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	out := make([]map[string]any, 0, len(members))
	for _, m := range members {
		out = append(out, map[string]any{
			"user_id": m.UserID, "phone": m.Phone, "name": nullable(m.DisplayName),
			"roles": m.Roles, "permissions": m.Permissions, "locales": m.Locales,
			"granted_at": m.GrantedAt.Format(time.RFC3339),
			"expires_at": timeOrNil(m.ExpiresAt),
		})
	}
	roleOut := make([]map[string]any, 0, len(roles))
	for _, role := range roles {
		roleOut = append(roleOut, map[string]any{
			"key": role.Key, "name": role.Name, "description": role.Description,
			"is_builtin": role.IsBuiltin, "permissions": role.Permissions,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": out, "roles": roleOut})
}

func (h *AdminHandlers) GrantRole(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermStaffAdmin)
	if !ok {
		return
	}
	var body struct {
		UserID    uuid.UUID `json:"user_id"`
		Role      string    `json:"role"`
		Reason    string    `json:"reason"`
		ExpiresAt string    `json:"expires_at"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	var expires *time.Time
	if body.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, body.ExpiresAt)
		if err != nil {
			httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "expires_at", Code: "invalid", Message: "expected RFC3339",
			}}})
			return
		}
		expires = &t
	}

	if err := h.Admin.GrantRole(r.Context(), grant.UserID, body.UserID,
		body.Role, body.Reason, expires); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *AdminHandlers) RevokeRole(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermStaffAdmin)
	if !ok {
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	if err := h.Admin.RevokeRole(r.Context(), grant.UserID, userID,
		chi.URLParam(r, "role"), ""); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AccessHistory answers "who looked at this record".
func (h *AdminHandlers) AccessHistory(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermStaffAdmin)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	entries, err := h.Admin.AccessHistory(r.Context(), grant.UserID,
		chi.URLParam(r, "type"), id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]any{
			"at": e.At.Format(time.RFC3339), "action": e.Action, "actor": e.Actor,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": out})
}

// --- shared serialisation ---------------------------------------------------

func money(m adminview.Money) map[string]any {
	return map[string]any{"amount": m.AmountMinor, "currency": m.Currency}
}

func timelineJSON(entries []adminview.TimelineEntry) []map[string]any {
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]any{
			"at": e.At.Format(time.RFC3339), "source": e.Source,
			"kind": e.Kind, "detail": nullable(e.Detail), "actor": nullable(e.Actor),
		})
	}
	return out
}

func notificationsJSON(ns []adminview.OrderNotification) []map[string]any {
	out := make([]map[string]any, 0, len(ns))
	for _, n := range ns {
		out = append(out, map[string]any{
			"at": n.At.Format(time.RFC3339), "key": n.TemplateKey,
			"channel": n.Channel, "status": n.Status,
			// Suppression is a first-class outcome, not an absence. "Did they get
			// the OTP" has three answers and this is the one that explains a
			// customer who received nothing while the system did everything right.
			"suppressed":  nullable(n.Suppressed),
			"destination": nullable(n.Destination),
		})
	}
	return out
}

func invoiceJSON(i *adminview.OrderInvoice) any {
	if i == nil {
		return nil
	}
	return map[string]any{
		"id": i.ID, "number": i.Number, "total": money(i.Total), "status": i.Status,
	}
}

func timeOrNil(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(time.RFC3339)
}

func rawJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

// --- actions ----------------------------------------------------------------
//
// Every one goes through the service that owns the data (§3.2) and records an
// `admin_actions` row with a required reason (§3.3). The table has been written
// correctly since Phase 7 and read by nothing; the timeline above is the first
// thing that reads it back.

// Actions carries the services the console is allowed to call.
//
// Named individually rather than handed a registry, because the list *is* the
// contract: what the console can do is exactly what is in this struct, and
// widening it is a visible change rather than an emergent one.
type Actions struct {
	Payments interface {
		Refund(ctx context.Context, in payments.RefundInput) (uuid.UUID, error)
	}
	Ordering interface {
		Cancel(ctx context.Context, in ordering.CancelInput) error
	}
	// Accounts carries the identity mutations. Separate from the rest because
	// they are gated on identity.review rather than commerce or money — the
	// person who can change a phone number and the person who can move money
	// out of the account should be two people.
	Accounts Accounts
	Payouts  interface {
		ApproveRun(ctx context.Context, in payouts.ApproveRunInput) error
		IssueRun(ctx context.Context, runID, actorID uuid.UUID, reason string) (*payouts.IssueRunResult, error)
		IssueByStaff(ctx context.Context, in payouts.IssueInput) error
		PayByStaff(ctx context.Context, in payouts.PayInput) error
	}
}

func (h *AdminHandlers) RefundOrder(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermMoneyWrite)
	if !ok {
		return
	}
	orderID, err := uuid.Parse(chi.URLParam(r, "orderId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	var body struct {
		AmountMinor int64  `json:"amount_minor"`
		Reason      string `json:"reason"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	refundID, err := h.Act.Payments.Refund(r.Context(), payments.RefundInput{
		OrderID: orderID, AmountMinor: body.AmountMinor, Reason: body.Reason,
	})
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	h.record(r, grant, "order.refund", "order", orderID, body.Reason, map[string]any{
		"refund_id":    refundID,
		"amount_minor": body.AmountMinor,
	})
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"refund_id": refundID, "status": "refunded",
	})
}

func (h *AdminHandlers) CancelOrder(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermCommerceWrite)
	if !ok {
		return
	}
	orderID, err := uuid.Parse(chi.URLParam(r, "orderId"))
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

	actor := grant.UserID
	if err := h.Act.Ordering.Cancel(r.Context(), ordering.CancelInput{
		OrderID: orderID, Reason: body.Reason, ActorID: &actor,
	}); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	h.record(r, grant, "order.cancel", "order", orderID, body.Reason, nil)
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"status": "cancelled",
		// Said out loud because it is the surprising part, and because the next
		// question is always "so are they getting their money back".
		"note": "capacity released and undelivered stops cancelled. This did not refund anything — refund separately if it is owed.",
	})
}

// record writes the audit row.
//
// Not inside the service transaction, and that is a real compromise worth
// naming: §3.3 asks for the action and its record to commit together, which
// would mean every service method taking an actor and an audit writer. That is
// the right end state and a large refactor across nine modules.
//
// What this does instead is write immediately after — see
// admin.RecordOutside, where the exposure is set out in full.
func (h *AdminHandlers) record(r *http.Request, g admin.Grant, action, subjectType string,
	subjectID uuid.UUID, reason string, after map[string]any) {
	if err := h.Admin.RecordOutside(r.Context(), admin.Action{
		ActorID:     g.UserID,
		Action:      action,
		SubjectType: subjectType,
		SubjectID:   uuid.NullUUID{UUID: subjectID, Valid: true},
		Reason:      reason,
		After:       after,
		RequestID:   httpx.RequestIDFromContext(r.Context()),
	}); err != nil {
		slog.ErrorContext(r.Context(), "admin action not recorded",
			"action", action, "subject", subjectID, "error", err)
	}
}
