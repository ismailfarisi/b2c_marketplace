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

		if h.Reels != nil {
			// The feed itself. Anonymous viewers get it keyed by device;
			// signed-in viewers additionally get block filtering.
			r.Get("/feed", h.Reels.GetFeed)
			// Event ingest is here rather than behind auth for the same reason:
			// anonymous viewing is a supported mode, and its engagement data is
			// exactly what Phase 4's ranking needs. Rate limiting, not
			// authentication, is what protects this endpoint.
			r.Post("/events", h.Reels.SubmitEvents)
			// CTA presses. Public for the same reason: an anonymous viewer who
			// clicks, signs up, and orders is the feed doing the whole job, and
			// it is the conversion path most worth being able to see.
			r.Post("/actions/{actionId}/click", h.Reels.ClickAction)
		}
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

		if h.Reels == nil {
			return
		}

		// Authoring. The upload endpoint creates a draft and hands back a
		// presigned PUT; bytes never pass through this process.
		r.Post("/reels/uploads", h.Reels.CreateUpload)
		r.Post("/reels/{reelId}", h.Reels.UpdateReel)
		r.Get("/reels/{reelId}", h.Reels.GetReel)
		r.Delete("/reels/{reelId}", h.Reels.DeleteReel)

		// In-feed abuse reporting. Ships in Phase 2, not later: a feed that can
		// be reported into is a different product from one that cannot, and
		// retrofitting it after launch means launching without it.
		r.Post("/reels/{reelId}/report", h.Reels.ReportReel)

		// Phase 3 — the action layer.
		r.Put("/reels/{reelId}/actions", h.Reels.SetActions)
		r.Post("/me/follows", h.Reels.Follow)
		r.Delete("/me/follows/{followeeId}", h.Reels.Unfollow)
		r.Put("/reels/{reelId}/save", h.Reels.SaveReel)
		r.Delete("/reels/{reelId}/save", h.Reels.UnsaveReel)

		// The vendor's own conversion numbers, scoped by authorship.
		r.Get("/reels/{reelId}/funnel", h.Reels.ReelFunnel)

		// Admin. Authorisation is checked in the service against
		// platform_admins on every call, not carried in a token claim.
		r.Get("/moderation/queue", h.Reels.ModerationQueue)
		r.Post("/moderation/{reelId}/decide", h.Reels.DecideModeration)

		// The Phase 3 decision itself. Admin-only: this is the number that
		// determines whether the video bet continues, and it must not circulate
		// stripped of the definitions that make it meaningful.
		r.Get("/analytics/feed-funnel", h.Reels.PlatformFunnel)
	})
}

// MountFeed wires the read-only surface served by cmd/feed.
//
// The split PLAN-VIDEO-FIRST §3 calls for, and the reason it is worth it: feed
// reads scale with viewers and order writes scale with buyers, and those two
// numbers differ by two orders of magnitude in a video-first product. Sharing a
// process means autoscaling the transactional core on feed traffic — paying for
// order capacity to serve people who are only watching.
func MountFeed(r chi.Router, h *ReelHandlers, issuer *auth.Issuer) {
	r.Group(func(r chi.Router) {
		r.Use(auth.Optional(issuer))

		r.Get("/feed", h.GetFeed)
		r.Post("/events", h.SubmitEvents)
		// The click endpoint belongs here too. It is on the same hot path as
		// the feed — every CTA press on a served page hits it — and separating
		// them would put the highest-intent event in the product behind the
		// transactional core's scaling profile.
		r.Post("/actions/{actionId}/click", h.ClickAction)
	})
}
