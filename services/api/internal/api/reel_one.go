package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/actions"
	"github.com/marketplace/api/internal/modules/feed"
	"github.com/marketplace/api/internal/modules/social"
	"github.com/marketplace/api/internal/seam/action"
	"github.com/marketplace/api/internal/seam/subject"
)

// FeedReel serves one reel, enriched exactly as the feed enriches a page.
//
// # Why the app needs this
//
// Saved was a dead end. Tapping a row navigated to the feed *in general* —
// wherever it happened to be, showing whatever the ranker had chosen — which
// is the one thing the viewer did not ask for. They kept a specific car; the
// tap should open that car.
//
// Search results had the same shape of problem from the other direction: they
// went straight into a conversation, skipping the video entirely, on a platform
// whose whole premise is that people decide by watching.
//
// # Why it is not just a database read
//
// Everything that makes a reel *mean* something is resolved rather than stored:
// the CTA comes from the action seam, the badge and price line from the subject
// seam, the countdown from the time signal, and whether the heart is filled
// from the relation tables. A handler that read the row and returned it would
// produce a reel that plays and does nothing — no button, no price, no clock.
//
// So this shares [ReelHandlers.enrich] with the feed. Not for brevity: two
// copies of that resolution would drift, and the way it would drift is a CTA
// that works in the feed and is missing here, which nobody would notice for
// months because both screens "work".
func (h *ReelHandlers) FeedReel(w http.ResponseWriter, r *http.Request) {
	reelID, err := uuid.Parse(chi.URLParam(r, "reelId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	item, ok, err := h.Feed.One(r.Context(), reelID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if !ok {
		// A reel that was taken down, or whose author was removed. 404 rather
		// than an empty object: the Saved row that pointed here is stale, and
		// the client greys it out on this answer.
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	uid, hasUser := httpx.UserIDFromContext(r.Context())
	viewerID := uuid.NullUUID{UUID: uid, Valid: hasUser}

	// Optional here, unlike the feed, where a point is mandatory because
	// "what is near me" has no answer without one. A viewer opening a reel they
	// saved has already named it; the only thing the coordinates change is
	// whether the CTA can say "we do not deliver there", and a missing point
	// makes that check permissive rather than making the screen impossible.
	lat, _ := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
	lng, _ := strconv.ParseFloat(r.URL.Query().Get("lng"), 64)

	// No impression recorded, deliberately.
	//
	// An impression is the denominator of the feed's conversion rate, and it
	// means "this reel was served to someone who did not ask for it". A viewer
	// opening something they saved last week is a different event, and counting
	// it here would inflate the denominator with the most engaged traffic on
	// the platform — making the feed look worse the better it works. The view
	// itself is still reported by the player through the normal event path.
	items := h.enrich(r, []feed.Item{item}, viewerID, lat, lng)
	if len(items) == 0 {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", items[0])
}

// enrich resolves CTAs, viewer state and subject summaries for a set of items.
//
// Extracted from the feed handler so the single-reel endpoint cannot drift from
// it. Every failure inside is best-effort and logged, matching what the feed
// already did: a provider that fails costs its reels their badge, never the
// viewer their screen.
func (h *ReelHandlers) enrich(
	r *http.Request,
	source []feed.Item,
	viewerID uuid.NullUUID,
	lat, lng float64,
) []map[string]any {
	reelIDs := make([]uuid.UUID, len(source))
	for i, it := range source {
		reelIDs[i] = it.ID
	}

	var resolved map[uuid.UUID][]actions.Resolved
	if h.Actions != nil && len(reelIDs) > 0 {
		var err error
		resolved, err = h.Actions.ResolvePage(r.Context(), reelIDs, action.Viewer{
			UserID:  viewerID,
			Lat:     lat,
			Lng:     lng,
			Locale:  httpx.LocaleFromContext(r.Context()),
			Country: "AE",
		})
		if err != nil {
			logIfErr(r, "action resolution failed", err)
			resolved = nil
		}
	}

	var state social.ViewerState
	if h.Likes != nil && len(reelIDs) > 0 {
		var err error
		state, err = h.Likes.StateFor(r.Context(), viewerID, reelIDs)
		if err != nil {
			logIfErr(r, "viewer state lookup failed", err)
		}
	}

	var summaries map[subject.Ref]subject.Summary
	if h.Subjects != nil {
		refs := make([]subject.Ref, 0, len(source))
		for _, it := range source {
			if it.SubjectType == nil || it.SubjectID == nil {
				continue
			}
			refs = append(refs, subject.Ref{
				Type: subject.Type(*it.SubjectType),
				ID:   *it.SubjectID,
			})
		}
		if len(refs) > 0 {
			var err error
			summaries, err = h.Subjects.SummarizePage(r.Context(), refs, subject.Viewer{
				UserID:  viewerID,
				Locale:  httpx.LocaleFromContext(r.Context()),
				Country: "AE",
			})
			if err != nil {
				logIfErr(r, "subject summary resolution failed", err)
				summaries = nil
			}
		}
	}

	out := make([]map[string]any, 0, len(source))
	for _, it := range source {
		item := feedItemResponse(it, h.MediaBase)
		item["actions"] = actionsResponse(resolved[it.ID])
		item["viewer"] = map[string]any{
			"liked":    state.Liked[it.ID],
			"saved":    state.Saved[it.ID],
			"followed": state.Followed[it.ID],
		}
		item["summary"] = nil
		if it.SubjectType != nil && it.SubjectID != nil {
			ref := subject.Ref{Type: subject.Type(*it.SubjectType), ID: *it.SubjectID}
			if s, ok := summaries[ref]; ok {
				item["summary"] = summaryResponse(s)
			}
		}
		out = append(out, item)
	}
	return out
}
