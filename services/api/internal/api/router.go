package api

import (
	"github.com/go-chi/chi/v5"

	"github.com/marketplace/api/internal/auth"
	"github.com/marketplace/api/internal/httpx"
)

// Mount wires the v1 surface.
//
// Route paths mirror api/openapi.yaml exactly. When the generated boundary
// lands, this function is replaced by openapi.HandlerFromMux and the handler
// methods keep their signatures.
func Mount(r chi.Router, h *Handlers, issuer *auth.Issuer, idem httpx.IdempotencyStore) {
	// Public.
	r.Group(func(r chi.Router) {
		r.Use(httpx.Idempotency(idem))

		r.Post("/auth/otp/request", h.RequestOTP)
		r.Post("/auth/otp/verify", h.VerifyOTP)
		r.Post("/auth/refresh", h.Refresh)
	})

	// Public reads. Optional auth so the same endpoint serves an anonymous
	// browser and a signed-in user — the feed depends on this, and putting a
	// login wall in front of the product's front door would be fatal to it.
	r.Group(func(r chi.Router) {
		r.Use(auth.Optional(issuer))

		r.Get("/branches/{branchId}/offerings", h.Catalog.ListOfferings)
		r.Get("/branches/{branchId}/availability", h.Catalog.GetAvailability)
	})

	// Authenticated.
	//
	// Idempotency runs *after* auth so the stored response is namespaced by
	// user. Reversing the order would let an unauthenticated caller claim a key
	// and then read back another user's response.
	r.Group(func(r chi.Router) {
		r.Use(auth.Authenticated(issuer))
		r.Use(httpx.Idempotency(idem))

		r.Post("/orders", h.CreateOrder)
		r.Get("/orders/{orderId}", h.GetOrder)
		r.Post("/orders/{orderId}/payment-intent", h.CreatePaymentIntent)
	})
}
