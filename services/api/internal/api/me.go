package api

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/actions"
	"github.com/marketplace/api/internal/modules/vehicles"
	"github.com/marketplace/api/internal/seam/subject"
)

// MeHandlers is the viewer's own two lists: what they kept, and what they sell.
//
// A separate handler set from SellHandlers even though both are "me" screens,
// because the audiences differ and so does the nil condition. Saves belong to
// anyone with an account; listings belong to a seller, and a deployment with no
// classifieds vertical has the first and not the second.
type MeHandlers struct {
	Actions  *actions.Service
	Subjects *subject.Registry
	// Listings is nil in a deployment without the vehicles vertical.
	Listings *vehicles.Service
	// MediaBase prefixes poster keys.
	//
	// Composed here rather than sent as a bare key for the client to prefix. The
	// renditions already arrive as absolute URLs, so a client that had to build
	// one URL and not the other would carry the CDN host in two places and get
	// it wrong in the one that is only exercised on a screen nobody opens during
	// development.
	MediaBase string
}

// Saves is the Saved tab.
//
// The cards get their words from the owning vertical through the same
// SummarizePage the feed uses. That is the point: a saved car and a saved
// midnight offer render through one code path, and neither this handler nor the
// client learns what either is.
func (h *MeHandlers) Saves(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}

	cards, err := h.Actions.SavedCards(r.Context(), userID,
		atoiOr(r.URL.Query().Get("limit"), 50))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	refs := make([]subject.Ref, 0, len(cards))
	for _, c := range cards {
		if c.SubjectID != nil && c.SubjectType != "" {
			refs = append(refs, subject.Ref{Type: subject.Type(c.SubjectType), ID: *c.SubjectID})
		}
	}

	summaries := map[subject.Ref]subject.Summary{}
	if h.Subjects != nil && len(refs) > 0 {
		// Errors are swallowed, matching the feed. A provider that is down costs
		// these cards their text, not the viewer their list.
		if got, err := h.Subjects.SummarizePage(r.Context(), refs, subject.Viewer{
			UserID:  uuid.NullUUID{UUID: userID, Valid: true},
			Locale:  httpx.LocaleFromContext(r.Context()),
			Country: "AE",
		}); err == nil {
			summaries = got
		}
	}

	items := make([]map[string]any, 0, len(cards))
	for _, c := range cards {
		item := map[string]any{
			"reel_id":      c.ReelID,
			"subject_type": c.SubjectType,
			"poster_url":   h.mediaURL(c.PosterKey),
			"lqip":         c.LQIP,
			"saved_at":     c.SavedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			// Stated rather than filtered. A saved car that sold is something the
			// viewer chose to keep; deleting it from their own list silently
			// reads as the app losing their data.
			"unavailable": c.Removed,
			"summary":     nil,
		}
		if c.SubjectID != nil {
			item["subject_id"] = *c.SubjectID
			if s, present := summaries[subject.Ref{Type: subject.Type(c.SubjectType), ID: *c.SubjectID}]; present {
				item["summary"] = summaryResponse(s)
			}
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

// MyListings is the seller half of the Me tab.
//
// It reports every state, including the ones a buyer never sees. A seller whose
// listing was rejected needs to be told so on the screen they go looking for it
// on; the alternative is a car that is simply absent, which reads as a bug and
// gets reported as one.
func (h *MeHandlers) MyListings(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	if h.Listings == nil {
		// An empty list, not a 404. This deployment has no classifieds vertical,
		// so the honest answer to "what am I selling" is "nothing" rather than
		// "that screen does not exist".
		httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": []any{}})
		return
	}

	listings, err := h.Listings.MyListings(r.Context(), userID,
		atoiOr(r.URL.Query().Get("limit"), 50))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	items := make([]map[string]any, 0, len(listings))
	for _, l := range listings {
		items = append(items, map[string]any{
			"id":           l.ID,
			"reference_no": l.ReferenceNo,
			"status":       l.Status,
			"title":        l.Title,
			"year":         l.Year,
			"mileage_km":   l.MileageKM,
			"price_minor":  l.PriceMinor,
			"currency":     l.Currency,
			"city":         l.City,
			"reel_id":      l.ReelID,
			// Empty when there is no reel. The client renders the listing's own
			// status in that case, which is the correct thing to show for a car
			// listed through the web form.
			"reel_state":   l.ReelState,
			"published_at": l.PublishedAt,
			"expires_at":   l.ExpiresAt,
			"sold_at":      l.SoldAt,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func (h *MeHandlers) mediaURL(key string) string {
	if key == "" || h.MediaBase == "" {
		return ""
	}
	if strings.HasPrefix(key, "http://") || strings.HasPrefix(key, "https://") {
		return key
	}
	return strings.TrimSuffix(h.MediaBase, "/") + "/" + strings.TrimPrefix(key, "/")
}
