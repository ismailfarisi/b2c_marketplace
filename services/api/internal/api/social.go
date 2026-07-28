package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/comments"
	"github.com/marketplace/api/internal/modules/moderation"
)

// --- likes -------------------------------------------------------------------

func (h *ReelHandlers) LikeReel(w http.ResponseWriter, r *http.Request) {
	userID, reelID, ok := userAndReel(w, r)
	if !ok {
		return
	}
	if h.Likes == nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	created, err := h.Likes.Like(r.Context(), userID, reelID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	// 200 either way. A double-tap and a retry on a flaky connection must not
	// be distinguishable to the client, or clients start writing code to tell
	// them apart and get it wrong.
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"liked": true, "changed": created,
	})
}

func (h *ReelHandlers) UnlikeReel(w http.ResponseWriter, r *http.Request) {
	userID, reelID, ok := userAndReel(w, r)
	if !ok {
		return
	}
	if h.Likes == nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	removed, err := h.Likes.Unlike(r.Context(), userID, reelID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"liked": false, "changed": removed,
	})
}

// --- comments ----------------------------------------------------------------

type commentBody struct {
	Body     string     `json:"body"`
	ParentID *uuid.UUID `json:"parent_id"`
}

func (h *ReelHandlers) PostComment(w http.ResponseWriter, r *http.Request) {
	userID, reelID, ok := userAndReel(w, r)
	if !ok {
		return
	}
	if h.Comments == nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	var body commentBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	c, err := h.Comments.Post(r.Context(), userID, reelID, body.ParentID, body.Body)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	// 201 for a published comment, 202 for one held for review. The distinction
	// is the whole reason the state exists: a client that renders a pending
	// comment as live shows its author something nobody else can see, and the
	// author finds out days later that their question was never asked.
	status := http.StatusCreated
	if c.State != comments.StatePublished {
		status = http.StatusAccepted
	}
	httpx.WriteJSON(w, status, "", commentResponse(*c))
}

func (h *ReelHandlers) ListComments(w http.ResponseWriter, r *http.Request) {
	reelID, err := uuid.Parse(chi.URLParam(r, "reelId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	if h.Comments == nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))

	var before time.Time
	if raw := q.Get("before"); raw != "" {
		t, perr := time.Parse(time.RFC3339, raw)
		if perr != nil {
			httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "before", Code: "invalid_timestamp",
			}}})
			return
		}
		before = t
	}

	list, err := h.Comments.List(r.Context(), reelID, before, limit)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	items := make([]map[string]any, 0, len(list))
	var oldest time.Time
	for _, c := range list {
		items = append(items, commentResponse(c))
		if c.Depth == 0 && (oldest.IsZero() || c.CreatedAt.Before(oldest)) {
			oldest = c.CreatedAt
		}
	}

	out := map[string]any{"items": items}
	if !oldest.IsZero() {
		// Keyset on the oldest *root*, not on the oldest row. Replies are
		// fetched under their parent, so paginating on a reply's timestamp
		// would skip every root published between it and its parent.
		out["next_before"] = oldest.Format(time.RFC3339Nano)
	}
	httpx.WriteJSON(w, http.StatusOK, "", out)
}

func (h *ReelHandlers) DeleteComment(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	commentID, err := uuid.Parse(chi.URLParam(r, "commentId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	if h.Comments == nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	if err := h.Comments.Delete(r.Context(), userID, commentID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ReelHandlers) ReportComment(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	commentID, err := uuid.Parse(chi.URLParam(r, "commentId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	if h.Comments == nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	var body reportBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.Comments.Report(r.Context(), userID, commentID, body.Reason, body.Detail); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, "", map[string]any{"status": "received"})
}

// --- comment moderation (admin) ----------------------------------------------

func (h *ReelHandlers) CommentQueue(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	if h.Comments == nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	items, err := h.Comments.Queue(r.Context(), userID, limit)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		out = append(out, map[string]any{
			"id":          it.ID,
			"reel_id":     it.ReelID,
			"author_id":   it.AuthorID,
			"author_name": it.AuthorName,
			"body":        it.Body,
			"state":       it.State,
			"reports":     it.Reports,
			"created_at":  it.CreatedAt.Format(time.RFC3339),
			// Surfaced for the same reason the reel queue surfaces it: a queue
			// breaching its SLA has to be visible in the tool the moderator
			// already has open, not only in a dashboard nobody watches.
			"waiting_seconds": int(it.WaitingFor.Seconds()),
			"sla_breached":    it.WaitingFor > moderation.ReviewSLA,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": out})
}

func (h *ReelHandlers) DecideComment(w http.ResponseWriter, r *http.Request) {
	moderatorID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	commentID, err := uuid.Parse(chi.URLParam(r, "commentId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	if h.Comments == nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	var body decideBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.Comments.Decide(r.Context(), moderatorID, commentID,
		moderation.Verdict(body.Decision), body.Notes); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"status": "recorded"})
}

// --- ranking (admin) ---------------------------------------------------------

// RankingProfile reports which weights are ranking the feed right now.
//
// Staff-only, and it exists because "the feed looked wrong at 14:20" is a
// question that otherwise gets discussed rather than answered. A ranking system
// whose configuration is invisible is one where every regression turns into an
// argument about whether anything changed.
func (h *ReelHandlers) RankingProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	if h.Moderation == nil || h.Ranking == nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	if err := h.Moderation.RequireStaff(r.Context(), userID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	weights := h.Ranking.Weights(r.Context())
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"profile": h.Ranking.Active(),
		"weights": weights,
		"notes": "Edit ranking_profiles to change these. Exactly one row may be " +
			"active; an invalid profile is refused and the previous weights stay " +
			"in force. Cached for up to one minute.",
	})
}

// --- helpers -----------------------------------------------------------------

func userAndReel(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return uuid.Nil, uuid.Nil, false
	}
	reelID, err := uuid.Parse(chi.URLParam(r, "reelId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return uuid.Nil, uuid.Nil, false
	}
	return userID, reelID, true
}

func commentResponse(c comments.Comment) map[string]any {
	out := map[string]any{
		"id":          c.ID,
		"reel_id":     c.ReelID,
		"parent_id":   c.ParentID,
		"depth":       c.Depth,
		"author_id":   c.AuthorID,
		"author_name": c.AuthorName,
		"body":        c.Body,
		"state":       c.State,
		"created_at":  c.CreatedAt.Format(time.RFC3339),
	}
	if c.Depth == 0 {
		out["reply_count"] = c.ReplyCount
	}
	// The author is told which policy their comment fell foul of. "Your comment
	// was removed" with no reason converts a correct decision into a grievance.
	if c.Reason != "" {
		out["reason"] = c.Reason
	}
	return out
}
