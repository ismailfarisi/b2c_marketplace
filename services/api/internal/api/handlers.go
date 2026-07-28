// Package api is the HTTP boundary.
//
// Handlers decode, call one service method, and encode. Any logic that lands
// here cannot be exercised from the worker — and the worker is where the
// retries live.
//
// These are hand-wired to chi against the paths in api/openapi.yaml. Once
// `make gen-go` has run they become the generated StrictServerInterface
// implementation; the service calls do not change.
package api

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/identity"
	"github.com/marketplace/api/internal/modules/ordering"
	"github.com/marketplace/api/internal/modules/payments"
	"github.com/marketplace/api/internal/seam/fulfillment"
)

type Handlers struct {
	Identity *identity.Service
	Ordering *ordering.Service
	Payments *payments.Service
	Catalog  *CatalogQueries
}

// --- auth -------------------------------------------------------------------

type otpRequestBody struct {
	Phone   string `json:"phone"`
	Purpose string `json:"purpose"`
	Locale  string `json:"locale"`
}

func (h *Handlers) RequestOTP(w http.ResponseWriter, r *http.Request) {
	var body otpRequestBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	challenge, err := h.Identity.RequestOTP(r.Context(), body.Phone, body.Purpose, clientIP(r))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusAccepted, "", map[string]any{
		"challenge_id":         challenge.ID,
		"expires_at":           challenge.ExpiresAt.Format(time.RFC3339),
		"resend_after_seconds": challenge.ResendAfterSeconds,
	})
}

type otpVerifyBody struct {
	ChallengeID uuid.UUID `json:"challenge_id"`
	Code        string    `json:"code"`
	Device      struct {
		Platform  string `json:"platform"`
		Name      string `json:"name"`
		PushToken string `json:"push_token"`
	} `json:"device"`
}

func (h *Handlers) VerifyOTP(w http.ResponseWriter, r *http.Request) {
	var body otpVerifyBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	session, err := h.Identity.VerifyOTP(r.Context(), body.ChallengeID, body.Code, identity.DeviceInfo{
		Platform:  body.Device.Platform,
		Name:      body.Device.Name,
		PushToken: body.Device.PushToken,
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	})
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, "", tokenPairResponse(session))
}

func (h *Handlers) Refresh(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	session, err := h.Identity.Refresh(r.Context(), body.RefreshToken, identity.DeviceInfo{
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	})
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, "", tokenPairResponse(session))
}

// --- orders -----------------------------------------------------------------

type createOrderBody struct {
	BranchID  uuid.UUID `json:"branch_id"`
	AddressID uuid.UUID `json:"address_id"`
	Lines     []struct {
		OfferingID uuid.UUID      `json:"offering_id"`
		Quantity   int            `json:"quantity"`
		Options    map[string]any `json:"options"`
	} `json:"lines"`
	Schedule struct {
		Slots []struct {
			Kind     string `json:"kind"`
			Date     string `json:"date"`
			WindowID string `json:"window_id"`
		} `json:"slots"`
	} `json:"schedule"`
	Notes  string `json:"notes"`
	Source struct {
		ReelID   *uuid.UUID `json:"reel_id"`
		ActionID *uuid.UUID `json:"action_id"`
	} `json:"source"`
}

func (h *Handlers) CreateOrder(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}

	var body createOrderBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	in := ordering.PlaceOrderInput{
		UserID:         userID,
		BranchID:       body.BranchID,
		AddressID:      body.AddressID,
		Notes:          body.Notes,
		SourceReelID:   body.Source.ReelID,
		SourceActionID: body.Source.ActionID,
	}

	for _, l := range body.Lines {
		in.Lines = append(in.Lines, ordering.LineInput{
			OfferingID: l.OfferingID,
			Quantity:   l.Quantity,
			Options:    l.Options,
		})
	}

	var fieldErrs []httpx.FieldError
	for i, slot := range body.Schedule.Slots {
		// Dates are parsed as bare calendar days, not timestamps. A slot is
		// "2026-08-03 in the branch's timezone", and coercing it to UTC
		// midnight shifts it a day for any branch east of Greenwich — which is
		// every branch we have.
		date, err := time.Parse("2006-01-02", slot.Date)
		if err != nil {
			fieldErrs = append(fieldErrs, httpx.FieldError{
				Field: "schedule.slots", Code: "invalid_date",
				Message: "expected YYYY-MM-DD",
			})
			continue
		}
		windowID, err := uuid.Parse(slot.WindowID)
		if err != nil {
			fieldErrs = append(fieldErrs, httpx.FieldError{
				Field: "schedule.slots", Code: "invalid_window_id",
			})
			continue
		}
		_ = i
		in.Slots = append(in.Slots, fulfillment.Slot{
			Kind:     fulfillment.Kind(slot.Kind),
			Date:     date,
			WindowID: windowID,
		})
	}
	if len(fieldErrs) > 0 {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: fieldErrs})
		return
	}

	order, err := h.Ordering.PlaceOrder(r.Context(), in)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, "", orderResponse(order))
}

func (h *Handlers) GetOrder(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}

	orderID, err := uuid.Parse(chi.URLParam(r, "orderId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	order, err := h.Ordering.GetOrder(r.Context(), userID, orderID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, "", orderResponse(order))
}

func (h *Handlers) CreatePaymentIntent(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}

	orderID, err := uuid.Parse(chi.URLParam(r, "orderId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	intent, err := h.Payments.Authorize(r.Context(), userID, orderID, r.Header.Get("Idempotency-Key"))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, "", map[string]any{
		"id":       intent.ID,
		"status":   intent.Status,
		"provider": intent.Provider,
		"amount": map[string]any{
			"amount":   intent.AmountMinor,
			"currency": intent.Currency,
		},
		"client_secret": nullable(intent.ClientSecret),
		"redirect_url":  nullable(intent.RedirectURL),
	})
}

// --- helpers ----------------------------------------------------------------

func tokenPairResponse(s *identity.Session) map[string]any {
	return map[string]any{
		"access_token":  s.AccessToken,
		"refresh_token": s.RefreshToken,
		"token_type":    "Bearer",
		"expires_in":    s.ExpiresIn,
		"user": map[string]any{
			"id":           s.User.ID,
			"phone":        s.User.Phone,
			"email":        s.User.Email,
			"display_name": s.User.DisplayName,
			"locale":       s.User.Locale,
			"status":       s.User.Status,
			"created_at":   s.User.CreatedAt.Format(time.RFC3339),
		},
	}
}

func orderResponse(o *ordering.Order) map[string]any {
	return map[string]any{
		"id":           o.ID,
		"order_no":     o.OrderNo,
		"status":       o.Status,
		"branch_id":    o.BranchID,
		"service_type": o.ServiceType,
		"currency":     o.Currency,
		"created_at":   o.PlacedAt.Format(time.RFC3339),
	}
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// clientIP prefers X-Forwarded-For's first entry.
//
// Only trustworthy behind a proxy that overwrites the header — which is the
// deployment assumption. A directly-exposed server must not trust it, because
// a client can then forge any address it likes and defeat the OTP IP limit.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		return strings.TrimSpace(first)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ""
	}
	return host
}
