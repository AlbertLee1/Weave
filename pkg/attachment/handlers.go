package attachment

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
)

// Handler serves the 4 Foundry OSv2 global attachment endpoints:
//
//	POST /api/v2/ontologies/attachments/upload?filename=X
//	POST /api/v2/ontologies/attachments/upload/{attachmentRid}?filename=X
//	GET  /api/v2/ontologies/attachments/{attachmentRid}
//	GET  /api/v2/ontologies/attachments/{attachmentRid}/content
//
// Attachment paths are global — no {ontology} segment in the URL — because
// Foundry models attachment blobs as a cross-ontology resource.
type Handler struct {
	store BlobStore
}

// NewHandler constructs a Handler backed by the given BlobStore.
func NewHandler(store BlobStore) *Handler {
	return &Handler{store: store}
}

// RegisterRoutes mounts the 4 attachment endpoints on r.
//
// Note: the static segment "upload" must be registered before the wildcard
// "{attachmentRid}" GET route so chi resolves POST .../upload correctly.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/v2/ontologies/attachments/upload", h.Upload)
	r.Post("/api/v2/ontologies/attachments/upload/{attachmentRid}", h.UploadWithRID)
	r.Get("/api/v2/ontologies/attachments/{attachmentRid}", h.GetMetadata)
	r.Get("/api/v2/ontologies/attachments/{attachmentRid}/content", h.GetContent)
}

// Upload handles POST /api/v2/ontologies/attachments/upload.
func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	filename := strings.TrimSpace(r.URL.Query().Get("filename"))
	if filename == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingFilename", map[string]string{
			"parameter": "filename",
		}))
		return
	}

	att, err := h.store.Put(r.Context(), BlobMeta{
		Filename:  filename,
		MediaType: resolveMediaType(r),
	}, r.Body)
	if err != nil {
		h.writeStoreError(w, err)
		return
	}
	writeAttachmentJSON(w, att)
}

// UploadWithRID handles POST /api/v2/ontologies/attachments/upload/{attachmentRid}.
func (h *Handler) UploadWithRID(w http.ResponseWriter, r *http.Request) {
	rid := chi.URLParam(r, "attachmentRid")
	filename := strings.TrimSpace(r.URL.Query().Get("filename"))
	if filename == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingFilename", map[string]string{
			"parameter": "filename",
		}))
		return
	}

	att, err := h.store.PutWithRID(r.Context(), rid, BlobMeta{
		Filename:  filename,
		MediaType: resolveMediaType(r),
	}, r.Body)
	if err != nil {
		h.writeStoreError(w, err)
		return
	}
	writeAttachmentJSON(w, att)
}

// GetMetadata handles GET /api/v2/ontologies/attachments/{attachmentRid}.
func (h *Handler) GetMetadata(w http.ResponseWriter, r *http.Request) {
	rid := chi.URLParam(r, "attachmentRid")
	att, err := h.store.Stat(r.Context(), rid)
	if err != nil {
		h.writeStoreError(w, err)
		return
	}
	writeAttachmentJSON(w, att)
}

// GetContent handles GET /api/v2/ontologies/attachments/{attachmentRid}/content.
func (h *Handler) GetContent(w http.ResponseWriter, r *http.Request) {
	rid := chi.URLParam(r, "attachmentRid")
	att, err := h.store.Stat(r.Context(), rid)
	if err != nil {
		h.writeStoreError(w, err)
		return
	}
	body, err := h.store.Get(r.Context(), rid)
	if err != nil {
		h.writeStoreError(w, err)
		return
	}
	defer body.Close()

	mediaType := att.MediaType
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mediaType)
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, body)
}

// resolveMediaType returns the request Content-Type if set, otherwise
// application/octet-stream.
func resolveMediaType(r *http.Request) string {
	ct := strings.TrimSpace(r.Header.Get("Content-Type"))
	if ct == "" {
		return "application/octet-stream"
	}
	return ct
}

func writeAttachmentJSON(w http.ResponseWriter, att *Attachment) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(att)
}

// writeStoreError maps BlobStore sentinel errors to Palantir-style
// APIErrors.
func (h *Handler) writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		apierror.WriteJSON(w, apierror.NewNotFound("AttachmentNotFound", nil))
	case errors.Is(err, ErrAlreadyExists):
		apierror.WriteJSON(w, apierror.NewConflict("AttachmentAlreadyExists", nil))
	case errors.Is(err, ErrInvalidRID):
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidAttachmentRid", nil))
	default:
		apierror.WriteJSON(w, apierror.NewInternal("AttachmentStoreError", map[string]string{
			"message": err.Error(),
		}))
	}
}

