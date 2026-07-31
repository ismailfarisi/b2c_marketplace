package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/documents"
	"github.com/marketplace/api/internal/modules/moderation"
	"github.com/marketplace/api/internal/seam/subject"
)

// DocumentHandlers is the trust-document surface: upload, confirm, review.
type DocumentHandlers struct {
	Documents  *documents.Service
	Moderation *moderation.Service
	// Subjects answers "does this person own the thing this document is about".
	//
	// Through the seam rather than by importing vehicles and property, for the
	// obvious reason: the third vertical must not require an edit here.
	Subjects *subject.Registry
}

type documentUploadBody struct {
	SubjectType string    `json:"subject_type"`
	SubjectID   uuid.UUID `json:"subject_id"`
	Kind        string    `json:"kind"`
	ContentType string    `json:"content_type"`
}

// RequestDocumentUpload presigns a PUT for a trust document.
//
// The ownership check is the whole reason this handler exists rather than the
// service being called directly. Without it, any authenticated user could
// attach a document to any listing — which for property means attaching a
// permit to somebody else's advert and pushing it live.
func (h *DocumentHandlers) RequestDocumentUpload(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}

	var body documentUploadBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	owns, err := h.Subjects.OwnedBy(r.Context(),
		subject.Type(body.SubjectType), body.SubjectID, userID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if !owns {
		// Not-found rather than forbidden. Distinguishing "this is not yours"
		// from "this does not exist" turns the endpoint into a way to test
		// whether a listing id is real.
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	upload, err := h.Documents.RequestUpload(r.Context(), documents.UploadRequest{
		UploadedBy:  userID,
		SubjectType: body.SubjectType,
		SubjectID:   body.SubjectID,
		Kind:        body.Kind,
		ContentType: body.ContentType,
	})
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, "", map[string]any{
		"document_id":    upload.DocumentID,
		"upload_url":     upload.URL,
		"upload_headers": upload.Headers,
		"expires_at":     upload.ExpiresAt,
	})
}

// ConfirmDocument records that the bytes arrived.
func (h *DocumentHandlers) ConfirmDocument(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	documentID, err := uuid.Parse(chi.URLParam(r, "documentId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	doc, err := h.Documents.Confirm(r.Context(), userID, documentID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"id":     doc.ID,
		"kind":   doc.Kind,
		"status": doc.Status,
		// Said at the point of upload, for the same reason the reel upload says
		// it: a seller who expects a badge in ninety seconds and gets it in two
		// hours writes a one-star review about a working system.
		"review_note": "a person checks every document before it counts",
	})
}

// SubjectDocuments lists what has been uploaded against one subject, so the
// sell flow can render the trust row's real state rather than always asking.
func (h *DocumentHandlers) SubjectDocuments(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	subjectID, err := uuid.Parse(chi.URLParam(r, "subjectId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	subjectType := chi.URLParam(r, "subjectType")

	owns, err := h.Subjects.OwnedBy(r.Context(), subject.Type(subjectType), subjectID, userID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if !owns {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	docs, err := h.Documents.ForSubject(r.Context(), subjectType, subjectID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": renderDocuments(docs)})
}

// --- review -------------------------------------------------------------------

func (h *DocumentHandlers) DocumentQueue(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	if err := h.Moderation.RequireStaff(r.Context(), userID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	docs, err := h.Documents.Queue(r.Context(), atoiOr(r.URL.Query().Get("limit"), 50))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	items := renderDocuments(docs)
	// A read link per row, minted here rather than stored.
	//
	// Sixty seconds each, and never on the seller-facing projection above: a
	// mulkiya carries a chassis number, an owner's name and an address, and the
	// only person who needs to see one is the reviewer deciding about it.
	for i, d := range docs {
		url, err := h.Documents.ReadURL(r.Context(), d.ID)
		if err != nil {
			logIfErr(r, "document read url failed", err)
			continue
		}
		items[i]["url"] = url
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func (h *DocumentHandlers) DecideDocument(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	if err := h.Moderation.RequireStaff(r.Context(), userID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	documentID, err := uuid.Parse(chi.URLParam(r, "documentId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	var body struct {
		Approve bool   `json:"approve"`
		Reason  string `json:"reason"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.Documents.Decide(r.Context(), userID, documentID, body.Approve, body.Reason); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"approved": body.Approve})
}

func renderDocuments(docs []documents.Document) []map[string]any {
	out := make([]map[string]any, 0, len(docs))
	for _, d := range docs {
		out = append(out, map[string]any{
			"id":           d.ID,
			"subject_type": d.SubjectType,
			"subject_id":   d.SubjectID,
			"kind":         d.Kind,
			"status":       d.Status,
			"content_type": d.ContentType,
			"byte_size":    d.ByteSize,
			// The rejection reason, shown to the seller. A document refused
			// with no explanation is a seller who uploads the same photograph
			// again.
			"reason":     d.Reason,
			"created_at": d.CreatedAt,
		})
	}
	return out
}
