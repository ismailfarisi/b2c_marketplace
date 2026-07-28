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

			// Reading a thread does not require an account. Writing one does —
			// an anonymous comment cannot be struck, rate-limited across
			// sessions, or held against a reputation.
			r.Get("/reels/{reelId}/comments", h.Reels.ListComments)
		}

		// Phase 6 — classifieds browse.
		//
		// Public for the same reason the feed is. Car search is the highest-
		// intent surface in the product and the one most likely to arrive from
		// a shared link or a search engine; a login wall there converts a
		// motivated buyer into a bounce.
		if h.Vehicles != nil {
			r.Get("/listings", h.Vehicles.Search)
			r.Get("/listing-packages", h.Vehicles.ListPackages)
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

		// Phase 5 — recurring. A subscription's money moves through the order
		// endpoints above: signup returns a billing order, and the client pays
		// it with the same payment-intent call a one-off order uses.
		if h.Subscriptions != nil {
			r.Post("/subscriptions", h.Subscriptions.Subscribe)
			r.Post("/subscriptions/{subscriptionId}/pause", h.Subscriptions.Pause)
			r.Post("/subscriptions/{subscriptionId}/skip", h.Subscriptions.Skip)
			r.Post("/subscriptions/{subscriptionId}/cancel", h.Subscriptions.Cancel)
		}

		// Phase 6 — classifieds.
		//
		// Mounted before the media nil-check below, because the vehicles
		// vertical does not depend on the media stack. A deployment with no
		// object store still sells cars; it just sells them without video.
		if h.Vehicles != nil {
			r.Post("/listings", h.Vehicles.CreateListing)
			r.Post("/listings/{listingId}/submit", h.Vehicles.SubmitListing)
			r.Patch("/listings/{listingId}/price", h.Vehicles.ChangePrice)
			r.Post("/listings/{listingId}/sold", h.Vehicles.MarkSold)
			r.Post("/listings/{listingId}/status", h.Vehicles.SetStatus)

			// Demand. Inquire is this vertical's conversion event — the
			// analogue of POST /orders, and the thing the whole video bet is
			// measured against for cars.
			r.Post("/listings/{listingId}/inquire", h.Vehicles.Inquire)
			r.Post("/listings/{listingId}/offers", h.Vehicles.MakeOffer)
			r.Post("/offers/{offerId}/respond", h.Vehicles.RespondToOffer)
			r.Put("/listings/{listingId}/favourite", h.Vehicles.Favourite)
			r.Delete("/listings/{listingId}/favourite", h.Vehicles.Favourite)
			r.Post("/listings/{listingId}/report", h.Vehicles.ReportListing)

			// Monetisation. Paid placement is bought here and labelled in the
			// search response — see VehicleHandlers.Search.
			r.Post("/listing-packages/{packageId}/purchase", h.Vehicles.BuyPackage)
			r.Post("/listings/{listingId}/bump", h.Vehicles.Bump)
			r.Post("/listings/{listingId}/feature", h.Vehicles.Feature)

			// Listings get their own moderation queue rather than sharing the
			// reel one. Different reviewer skill, different SLA, and a listing
			// decision needs the VIN and the price history in front of it.
			r.Post("/moderation/listings/{listingId}/decide", h.Vehicles.DecideListing)
			r.Get("/moderation/fraud", h.Vehicles.FraudQueue)
			r.Post("/moderation/fraud/decide", h.Vehicles.DecideFraudSignal)
		}

		if h.Chat != nil {
			r.Get("/me/conversations", h.Chat.Inbox)
			r.Get("/conversations/{conversationId}/messages", h.Chat.History)
			r.Post("/conversations/{conversationId}/messages", h.Chat.Send)
			r.Post("/conversations/{conversationId}/read", h.Chat.MarkRead)

			// A third queue. Held messages arrive at chat volume and are judged
			// in seconds; interleaving them with reels would bury the queue
			// that has a publish SLA on it — the same argument that gave
			// comments their own queue in Phase 4.
			r.Get("/moderation/messages", h.Chat.Queue)
			r.Post("/moderation/messages/{messageId}/decide", h.Chat.Decide)
		}

		// Phase 7 — logistics.
		//
		// The driver routes carry no branch or vendor in the path. Every one of
		// them resolves the signed-in user to a driver server-side, because a
		// client that can name which driver it is acting as is a client that can
		// complete someone else's deliveries.
		if h.DriverOps != nil {
			r.Post("/driver/shifts", h.DriverOps.StartShift)
			r.Post("/driver/shifts/end", h.DriverOps.EndShift)
			r.Get("/driver/manifest", h.DriverOps.Manifest)
			r.Post("/driver/stops/{fulfillmentId}/dispatch", h.DriverOps.Dispatch_)
			r.Post("/driver/stops/{fulfillmentId}/advance", h.DriverOps.Advance)
			r.Post("/driver/stops/{fulfillmentId}/complete", h.DriverOps.Complete)
			r.Post("/driver/stops/{fulfillmentId}/fail", h.DriverOps.Fail)
			r.Post("/driver/location", h.DriverOps.Ping)
			r.Get("/driver/cash", h.DriverOps.Cash)
			r.Post("/driver/cash/remittances", h.DriverOps.DeclareRemittance)

			// The dispatch board. Vendor-facing, branch-scoped.
			r.Get("/branches/{branchId}/dispatch", h.DriverOps.Board)
			r.Post("/branches/{branchId}/dispatch/plan", h.DriverOps.BuildRoutes)
			r.Post("/routes/{routeId}/assign", h.DriverOps.AssignRoute)
		}

		// Phase 7 — the vendor console.
		if h.Console != nil {
			r.Get("/me/memberships", h.Console.Memberships)
			r.Get("/branches/{branchId}/summary", h.Console.Summary)
			r.Get("/branches/{branchId}/queue", h.Console.Queue)
			r.Get("/branches/{branchId}/calendar", h.Console.Calendar)
			r.Get("/branches/{branchId}/roster", h.Console.Roster)

			r.Get("/vendors/{vendorId}/statements", h.Console.Statements)
			r.Get("/vendors/{vendorId}/statements/{statementId}", h.Console.StatementDetail)
			// Counted by the branch, never by the driver who declared it.
			r.Post("/cash/remittances/{remittanceId}/settle", h.Console.SettleRemittance)

			r.Post("/disputes", h.Console.OpenDispute)
			r.Get("/disputes", h.Console.DisputeQueue)
			r.Post("/disputes/{disputeId}/resolve", h.Console.ResolveDispute)
		}

		// Phase 8 — market readiness.
		//
		// Mounted outside the media nil-check below for the same reason the
		// classifieds routes are: none of it depends on the media stack. A
		// deployment with no object store still has to send delivery codes,
		// issue tax invoices and honour a marketing opt-out — those are legal
		// obligations rather than features, and gating them behind a bucket
		// would be an odd place to lose them.
		if h.Compliance != nil {
			r.Get("/me/notifications", h.Compliance.Inbox)
			r.Post("/me/notifications/{notificationId}/read", h.Compliance.MarkNotificationRead)
			// The opt-out. Required to work in both launch markets, which is
			// why it is a first-class endpoint rather than a support action.
			r.Put("/me/notification-preferences", h.Compliance.SetPreference)

			r.Get("/me/invoices", h.Compliance.MyInvoices)
			// An author's own record. Self-scoped: there is no path here that
			// takes someone else's id.
			r.Get("/me/standing", h.Compliance.Standing)
			r.Post("/appeals", h.Compliance.OpenAppeal)

			// Vendor compliance. Both are vendor-scoped and authorise through
			// `vendor_users`, so a vendor sees why *they* cannot be paid and
			// never why anyone else cannot.
			r.Get("/vendors/{vendorId}/compliance", h.Compliance.VendorCompliance)
			r.Post("/vendors/{vendorId}/payout-account", h.Compliance.AddPayoutAccount)

			// Ops. Authorised against platform_admins inside the handler on
			// every call, never from a token claim.
			r.Post("/kyc/cases/{caseId}/decide", h.Compliance.DecideKYC)
			r.Post("/payout-accounts/{accountId}/verify", h.Compliance.VerifyPayoutAccount)
			// Overdue only. The failure mode of an appeals process is that
			// appeals are never decided, and a queue showing every open one
			// buries the breached ones among them.
			r.Get("/moderation/appeals", h.Compliance.AppealQueue)
			r.Post("/moderation/appeals/{appealId}/decide", h.Compliance.DecideAppeal)
		}

		// Phase 9 — boosts.
		//
		// Nil in every deployment that has not switched advertising on, which
		// PLAN-VIDEO-FIRST §7 says is correct until there is enough organic
		// demand that reach is worth buying. Selling reach into a thin feed
		// sells nothing and teaches the first advertisers that it does not
		// work.
		if h.BoostOps != nil {
			r.Post("/boosts", h.BoostOps.Create)
			r.Get("/boosts", h.BoostOps.Report)
			r.Post("/boosts/{campaignId}/status", h.BoostOps.SetStatus)
			// Staff. Not a second content review — the reel was moderated
			// before it could be published — but a review of the campaign.
			r.Post("/moderation/boosts/{campaignId}/decide", h.BoostOps.Review)
		}

		if h.Reels == nil {
			return
		}

		// Authoring. The upload endpoint creates a draft and hands back a
		// presigned PUT; bytes never pass through this process.
		r.Post("/reels/uploads", h.Reels.CreateUpload)
		// Seller reels (Phase 6). Separate endpoint, different authority: a
		// vendor reel authorises through branch membership, a personal one
		// through owning the subject.
		r.Post("/reels/uploads/personal", h.Reels.CreatePersonalUpload)
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

		// Phase 4 — social.
		r.Put("/reels/{reelId}/like", h.Reels.LikeReel)
		r.Delete("/reels/{reelId}/like", h.Reels.UnlikeReel)
		r.Post("/reels/{reelId}/comments", h.Reels.PostComment)
		r.Delete("/comments/{commentId}", h.Reels.DeleteComment)
		r.Post("/comments/{commentId}/report", h.Reels.ReportComment)

		// The vendor's own conversion numbers, scoped by authorship.
		r.Get("/reels/{reelId}/funnel", h.Reels.ReelFunnel)

		// Admin. Authorisation is checked in the service against
		// platform_admins on every call, not carried in a token claim.
		r.Get("/moderation/queue", h.Reels.ModerationQueue)
		r.Post("/moderation/{reelId}/decide", h.Reels.DecideModeration)

		// Comments get their own queue rather than sharing the reel one. They
		// arrive at a rate orders of magnitude higher and are judged in seconds
		// rather than minutes; interleaving them would bury the reel queue,
		// which is the one with a publish SLA on it.
		r.Get("/moderation/comments", h.Reels.CommentQueue)
		r.Post("/moderation/comments/{commentId}/decide", h.Reels.DecideComment)

		// What is ranking the feed right now. Staff-only, and it exists because
		// "the feed looked wrong at 14:20" is otherwise a question that gets
		// discussed rather than answered.
		r.Get("/ranking/profile", h.Reels.RankingProfile)

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
		r.Get("/reels/{reelId}/comments", h.ListComments)
		// The click endpoint belongs here too. It is on the same hot path as
		// the feed — every CTA press on a served page hits it — and separating
		// them would put the highest-intent event in the product behind the
		// transactional core's scaling profile.
		r.Post("/actions/{actionId}/click", h.ClickAction)
	})
}
