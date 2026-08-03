package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/chat"
	"github.com/marketplace/api/internal/modules/moderation"
)

// ChatHandlers is buyer↔seller messaging.
//
// Mounted only with the vehicles vertical. Chat is not a platform-wide feature
// in this system and deliberately so: an order has a support channel and a
// subscription has an account manager, but a classified listing has two
// strangers and no transaction to hang a dispute on. That is a different
// product with different safety requirements, and giving it its own handler set
// keeps those requirements from leaking into the order surface.
type ChatHandlers struct {
	Chat       *chat.Service
	Moderation *moderation.Service
}

func (h *ChatHandlers) Inbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}

	entries, err := h.Chat.Inbox(r.Context(), userID, atoiOr(r.URL.Query().Get("limit"), 50))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	items := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		items = append(items, map[string]any{
			"id":              e.ConversationID,
			"listing_id":      e.ListingID,
			"listing_title":   e.ListingTitle,
			"counterparty_id": e.CounterpartyID,
			"unread":          e.Unread,
			"last_message_at": e.LastMessageAt,
			"state":           e.State,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func (h *ChatHandlers) History(w http.ResponseWriter, r *http.Request) {
	userID, convID, ok := userAndConversation(w, r)
	if !ok {
		return
	}

	messages, err := h.Chat.History(r.Context(), convID, userID,
		atoiOr(r.URL.Query().Get("limit"), 50))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": renderMessages(messages, userID)})
}

func (h *ChatHandlers) Send(w http.ResponseWriter, r *http.Request) {
	userID, convID, ok := userAndConversation(w, r)
	if !ok {
		return
	}

	var body struct {
		Body string `json:"body"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	msg, err := h.Chat.Send(r.Context(), convID, userID, body.Body)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	// 201 delivered, 202 held — the same distinction the comment endpoint makes
	// in Phase 4, and for the same reason. A held message that returns 201 tells
	// the sender it arrived; they see no reply, assume the recipient is
	// ignoring them, and send it again through whatever channel they can find.
	status := http.StatusCreated
	if msg.State == "held" {
		status = http.StatusAccepted
	}

	out := map[string]any{
		"id":    msg.ID,
		"state": msg.State,
		"body":  msg.Body,
		// Always true — the sender is the viewer by definition here — but stated
		// rather than defaulted, because the client appends this object straight
		// into the thread it is rendering. Leaving it absent would make the
		// message the viewer just sent the one bubble on the screen drawn as
		// though it came from the other side.
		"mine":       true,
		"sender_id":  userID,
		"created_at": msg.CreatedAt,
	}
	switch msg.State {
	case "held":
		out["message"] = "This message is being reviewed and has not been delivered yet."
	case "redacted":
		// Told plainly, with the reason. A silently altered message is the
		// single most alienating thing a messaging product can do, and the
		// sender will simply retry until something gets through.
		out["message"] = "Contact details were removed. Keep the conversation here so we can help if something goes wrong."
		out["removed"] = msg.Findings
	}
	httpx.WriteJSON(w, status, "", out)
}

func (h *ChatHandlers) MarkRead(w http.ResponseWriter, r *http.Request) {
	userID, convID, ok := userAndConversation(w, r)
	if !ok {
		return
	}
	if err := h.Chat.MarkRead(r.Context(), convID, userID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- moderation -------------------------------------------------------------

func (h *ChatHandlers) Queue(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	if err := h.Moderation.RequireStaff(r.Context(), userID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	items, err := h.Chat.Queue(r.Context(), atoiOr(r.URL.Query().Get("limit"), 50))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		out = append(out, map[string]any{
			"id":              m.ID,
			"conversation_id": m.ConversationID,
			"listing_id":      m.ListingID,
			"sender_id":       m.SenderID,
			// The original, unredacted. A reviewer judging whether a message is
			// a scam needs the text that triggered the hold, not a version with
			// the evidence removed.
			"body":       m.Body,
			"findings":   m.Findings,
			"reason":     m.Reason,
			"created_at": m.CreatedAt,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": out})
}

func (h *ChatHandlers) Decide(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	if err := h.Moderation.RequireStaff(r.Context(), userID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	messageID, err := uuid.Parse(chi.URLParam(r, "messageId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	var body struct {
		Release bool `json:"release"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.Chat.Decide(r.Context(), messageID, body.Release); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	// Releasing delivers the redacted body, never the original: judging that a
	// message is not a scam is not the same decision as publishing the phone
	// number inside it, and only one of them was asked for.
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"released": body.Release,
		"note":     "released messages are still redacted for contact details",
	})
}

// renderMessages projects a thread for one participant.
//
// `mine` is computed here rather than left to the client, and that is not
// convenience. A chat where every bubble looks the same is not a chat — it is a
// transcript with the speaker labels torn off, and that is exactly what the app
// rendered: `sender_id` was on the wire and the client had no idea which uuid
// was its own, because nothing in the token exchange ever told it.
//
// The alternatives were worse. Returning the viewer's user id somewhere for the
// client to compare against means publishing an identifier the product
// otherwise never needs, and asking the client to derive it from a JWT means
// parsing a token the client is supposed to treat as opaque. The server already
// knows who is asking, on every request, from the authenticated context.
func renderMessages(messages []chat.Message, viewer uuid.UUID) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, m := range messages {
		out = append(out, map[string]any{
			"id":        m.ID,
			"sender_id": m.SenderID,
			// Whether this side of the conversation wrote it.
			"mine":       m.SenderID == viewer,
			"body":       m.Body,
			"state":      m.State,
			"created_at": m.CreatedAt,
		})
	}
	return out
}

func userAndConversation(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return uuid.Nil, uuid.Nil, false
	}
	convID, err := uuid.Parse(chi.URLParam(r, "conversationId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return uuid.Nil, uuid.Nil, false
	}
	return userID, convID, true
}
