package media

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/oms"
)

// DefaultMaxUploadBytes caps a single multipart upload at 10 MiB, the
// US-204 PRD limit.
const DefaultMaxUploadBytes int64 = 10 * 1024 * 1024

// Handler serves the three Foundry-style media endpoints:
//
//	POST   /api/v2/media           multipart upload, field "file"
//	GET    /api/v2/media/{rid}     download, sets Content-Disposition
//	DELETE /api/v2/media/{rid}     decrement refcount, reclaim blob if 0
//
// The catalog (oms.MediaAssetStore) holds one row per logical upload. Bytes
// live in the content-addressed Store; multiple rows MAY share a single
// physical file when SHA-256 collides (dedup), so DELETE only reclaims the
// blob after CountBySHA256 reports zero remaining references.
type Handler struct {
	store          *Store
	catalog        oms.MediaAssetStore
	maxUploadBytes int64
}

// NewHandler constructs a Handler bound to a content-addressed Store and a
// MediaAssetStore catalog (typically *oms.PGRepository).
func NewHandler(store *Store, catalog oms.MediaAssetStore) *Handler {
	return &Handler{
		store:          store,
		catalog:        catalog,
		maxUploadBytes: DefaultMaxUploadBytes,
	}
}

// SetMaxUploadBytes overrides the per-upload byte cap. Values <= 0 are
// ignored so callers cannot accidentally disable the limit.
func (h *Handler) SetMaxUploadBytes(n int64) {
	if n > 0 {
		h.maxUploadBytes = n
	}
}

// RegisterRoutes mounts the three media endpoints on r.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/v2/media", h.Upload)
	r.Get("/api/v2/media/{rid}", h.Download)
	r.Delete("/api/v2/media/{rid}", h.Delete)
}

// Upload reads a multipart/form-data body with one "file" part and an
// optional "realm" form field, persists the bytes through the content-
// addressed Store, then writes a media_assets row.
func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxUploadBytes)

	if err := r.ParseMultipartForm(h.maxUploadBytes + 1024); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			h.writeTooLarge(w)
			return
		}
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidMultipart", map[string]string{
			"message": err.Error(),
		}))
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingFile", map[string]string{
			"field": "file",
		}))
		return
	}
	defer file.Close()

	realm := strings.TrimSpace(r.FormValue("realm"))
	mime := strings.TrimSpace(header.Header.Get("Content-Type"))
	var createdBy string
	if u := auth.UserFromContext(r.Context()); u != nil {
		createdBy = u.ID
	}

	asset, err := h.store.Put(r.Context(), PutOptions{
		Realm:     realm,
		Filename:  header.Filename,
		MIME:      mime,
		CreatedBy: createdBy,
	}, file)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			h.writeTooLarge(w)
			return
		}
		if errors.Is(err, ErrInvalidRealm) {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRealm", map[string]string{
				"realm": realm,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("MediaStoreError", map[string]string{
			"message": err.Error(),
		}))
		return
	}

	row := &oms.MediaAsset{
		RID:       asset.RID,
		Realm:     asset.Realm,
		Filename:  asset.Filename,
		MIME:      asset.MIME,
		SizeBytes: asset.SizeBytes,
		SHA256:    asset.SHA256,
		Path:      asset.Path,
		CreatedBy: asset.CreatedBy,
		CreatedAt: asset.CreatedAt,
	}
	if err := h.catalog.CreateMediaAsset(r.Context(), row); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("MediaCatalogError", map[string]string{
			"message": err.Error(),
		}))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(row)
}

// Download streams the bytes for a media asset and sets a Content-Disposition
// header so browsers prompt with the original filename.
func (h *Handler) Download(w http.ResponseWriter, r *http.Request) {
	rid := chi.URLParam(r, "rid")
	asset, err := h.catalog.GetMediaAsset(r.Context(), rid)
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("MediaAssetNotFound", map[string]string{"rid": rid}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("MediaCatalogError", map[string]string{"message": err.Error()}))
		return
	}

	body, err := h.store.Open(r.Context(), asset.Path)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("MediaBlobNotFound", map[string]string{"rid": rid}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("MediaStoreError", map[string]string{"message": err.Error()}))
		return
	}
	defer body.Close()

	mime := asset.MIME
	if mime == "" {
		mime = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", asset.SizeBytes))
	w.Header().Set("Content-Disposition", contentDisposition(asset.Filename))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, body)
}

// Delete removes the catalog row for rid. If no other rows still reference
// the same (realm, sha256) the underlying blob is reclaimed too; otherwise
// the file is left untouched so the surviving references stay readable.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	rid := chi.URLParam(r, "rid")
	asset, err := h.catalog.GetMediaAsset(r.Context(), rid)
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("MediaAssetNotFound", map[string]string{"rid": rid}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("MediaCatalogError", map[string]string{"message": err.Error()}))
		return
	}
	if err := h.catalog.DeleteMediaAsset(r.Context(), rid); err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("MediaAssetNotFound", map[string]string{"rid": rid}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("MediaCatalogError", map[string]string{"message": err.Error()}))
		return
	}
	count, err := h.catalog.CountBySHA256(r.Context(), asset.Realm, asset.SHA256)
	if err == nil && count == 0 {
		_ = h.store.DeletePath(r.Context(), asset.Path)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) writeTooLarge(w http.ResponseWriter) {
	apierror.WriteJSON(w, apierror.NewInvalidParameter("MediaUploadTooLarge", map[string]string{
		"maxBytes": fmt.Sprintf("%d", h.maxUploadBytes),
	}))
}

// contentDisposition produces an RFC 5987-compatible header that sets both
// the legacy filename= and filename*= UTF-8 forms so browsers across vintages
// recover the original filename verbatim.
func contentDisposition(filename string) string {
	if filename == "" {
		return "attachment"
	}
	safe := strings.NewReplacer("\"", "", "\\", "", "\n", " ", "\r", " ").Replace(filename)
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, safe, url.PathEscape(filename))
}
