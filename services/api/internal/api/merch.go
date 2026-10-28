package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/admin"
	"github.com/marketplace/api/internal/modules/merch"
)

// ---- storefront ---------------------------------------------------------

func (h *MealsHandlers) Home(w http.ResponseWriter, r *http.Request) {
	lat, lng, ok := point(w, r)
	if !ok {
		return
	}
	home, err := h.Merch.Home(r.Context(), lat, lng)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", home)
}

func (h *MealsHandlers) Collection(w http.ResponseWriter, r *http.Request) {
	lat, lng, ok := point(w, r)
	if !ok {
		return
	}
	c, err := h.Merch.PublicCollection(r.Context(), chi.URLParam(r, "slug"), lat, lng)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", c)
}

// ---- admin --------------------------------------------------------------
//
// Merchandising: reads need commerce.read, edits commerce.write. Every write
// is audited; publishing is what customers see, so it carries a note.

func (h *AdminHandlers) merchID(w http.ResponseWriter, r *http.Request, key string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, key))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func (h *AdminHandlers) MerchCollections(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermCommerceRead); !ok {
		return
	}
	items, err := h.Merch.Collections(r.Context())
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func (h *AdminHandlers) MerchCollection(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermCommerceRead); !ok {
		return
	}
	id, ok := h.merchID(w, r, "collectionId")
	if !ok {
		return
	}
	c, err := h.Merch.Collection(r.Context(), id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", c)
}

func (h *AdminHandlers) SaveMerchCollection(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermCommerceWrite)
	if !ok {
		return
	}
	var id *uuid.UUID
	if chi.URLParam(r, "collectionId") != "" {
		parsed, ok := h.merchID(w, r, "collectionId")
		if !ok {
			return
		}
		id = &parsed
	}
	var in merch.CollectionInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	c, err := h.Merch.SaveCollection(r.Context(), id, in)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	action, status := "merch_collection.update", http.StatusOK
	if id == nil {
		action, status = "merch_collection.create", http.StatusCreated
	}
	h.record(r, grant, action, "merch_collection", c.ID, "", map[string]any{"slug": c.Slug, "rules": c.Rules})
	httpx.WriteJSON(w, status, "", c)
}

func (h *AdminHandlers) SetMerchCollectionItems(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermCommerceWrite)
	if !ok {
		return
	}
	id, ok := h.merchID(w, r, "collectionId")
	if !ok {
		return
	}
	var body struct {
		Items []merch.Item `json:"items"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	c, err := h.Merch.SetItems(r.Context(), id, body.Items)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	h.record(r, grant, "merch_collection.items", "merch_collection", id, "", map[string]any{"items": c.Items})
	httpx.WriteJSON(w, http.StatusOK, "", c)
}

func (h *AdminHandlers) DeleteMerchCollection(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermCommerceWrite)
	if !ok {
		return
	}
	id, ok := h.merchID(w, r, "collectionId")
	if !ok {
		return
	}
	if err := h.Merch.DeleteCollection(r.Context(), id); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	h.record(r, grant, "merch_collection.delete", "merch_collection", id, "", nil)
	w.WriteHeader(http.StatusNoContent)
}

// PreviewMerchCollection shows what a collection resolves to at a point,
// before it is placed on the home page.
func (h *AdminHandlers) PreviewMerchCollection(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermCommerceRead); !ok {
		return
	}
	id, ok := h.merchID(w, r, "collectionId")
	if !ok {
		return
	}
	lat, lng, ok := point(w, r)
	if !ok {
		return
	}
	c, err := h.Merch.Collection(r.Context(), id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	ks, err := h.Merch.Resolve(r.Context(), c, lat, lng)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": ks})
}

func (h *AdminHandlers) MerchBanners(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermCommerceRead); !ok {
		return
	}
	items, err := h.Merch.Banners(r.Context())
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func (h *AdminHandlers) SaveMerchBanner(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermCommerceWrite)
	if !ok {
		return
	}
	var id *uuid.UUID
	if chi.URLParam(r, "bannerId") != "" {
		parsed, ok := h.merchID(w, r, "bannerId")
		if !ok {
			return
		}
		id = &parsed
	}
	var in merch.BannerInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	b, err := h.Merch.SaveBanner(r.Context(), id, in)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	action, status := "merch_banner.update", http.StatusOK
	if id == nil {
		action, status = "merch_banner.create", http.StatusCreated
	}
	h.record(r, grant, action, "merch_banner", b.ID, "", map[string]any{"title": b.Title})
	httpx.WriteJSON(w, status, "", b)
}

func (h *AdminHandlers) DeleteMerchBanner(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermCommerceWrite)
	if !ok {
		return
	}
	id, ok := h.merchID(w, r, "bannerId")
	if !ok {
		return
	}
	if err := h.Merch.DeleteBanner(r.Context(), id); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	h.record(r, grant, "merch_banner.delete", "merch_banner", id, "", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (h *AdminHandlers) MerchLayout(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermCommerceRead); !ok {
		return
	}
	st, err := h.Merch.Layout(r.Context(), merch.HomeLayout)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", st)
}

func (h *AdminHandlers) SaveMerchDraft(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermCommerceWrite)
	if !ok {
		return
	}
	var body struct {
		Sections []merch.Section `json:"sections"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	v, err := h.Merch.SaveDraft(r.Context(), merch.HomeLayout, body.Sections)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	h.record(r, grant, "merch_layout.draft", "merch_layout_version", v.ID, "", map[string]any{"sections": len(v.Sections)})
	httpx.WriteJSON(w, http.StatusOK, "", v)
}

func (h *AdminHandlers) PublishMerchLayout(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermCommerceWrite)
	if !ok {
		return
	}
	var body struct {
		PublishAt *time.Time `json:"publish_at"`
		Note      string     `json:"note"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	v, err := h.Merch.Publish(r.Context(), merch.HomeLayout, body.PublishAt, grant.UserID, body.Note)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	h.record(r, grant, "merch_layout.publish", "merch_layout_version", v.ID, body.Note,
		map[string]any{"version": v.VersionNumber, "publish_at": v.PublishAt})
	httpx.WriteJSON(w, http.StatusOK, "", v)
}

func (h *AdminHandlers) RestoreMerchVersion(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermCommerceWrite)
	if !ok {
		return
	}
	id, ok := h.merchID(w, r, "versionId")
	if !ok {
		return
	}
	v, err := h.Merch.Restore(r.Context(), merch.HomeLayout, id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	h.record(r, grant, "merch_layout.restore", "merch_layout_version", id, "", map[string]any{"draft": v.ID})
	httpx.WriteJSON(w, http.StatusOK, "", v)
}

// PreviewMerchLayout resolves posted sections at a point exactly as the
// storefront would, without saving them.
func (h *AdminHandlers) PreviewMerchLayout(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermCommerceRead); !ok {
		return
	}
	lat, lng, ok := point(w, r)
	if !ok {
		return
	}
	var body struct {
		Sections []merch.Section `json:"sections"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	sections, err := merch.NormaliseSections(body.Sections)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	home, err := h.Merch.Preview(r.Context(), sections, lat, lng)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", home)
}
