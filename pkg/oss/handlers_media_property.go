package oss

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/attachment"
	"github.com/liyang/weave/pkg/oms"
)

// Foundry MediaReference maps each media reference to a media-set item. For
// the single-machine implementation we reuse the attachment BlobStore and
// advertise a single default media set so clients see a wire shape that is
// a faithful subset of Foundry's MediaReference.
const (
	defaultMediaSetRid     = "ri.mio.main.media-set.default"
	defaultMediaSetViewRid = "ri.mio.main.media-set-view.default"
)

// mediaMetadataResponse mirrors Foundry's MediaMetadata wire shape
// ({path, sizeBytes, mediaType}). We expose the stored filename as the
// path since the single-machine store has no hierarchical layout.
type mediaMetadataResponse struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
	MediaType string `json:"mediaType"`
}

// mediaReferenceResponse mirrors Foundry's MediaReference wire shape.
type mediaReferenceResponse struct {
	MimeType  string               `json:"mimeType"`
	Reference mediaReferenceTarget `json:"reference"`
}

type mediaReferenceTarget struct {
	Type             string               `json:"type"`
	MediaSetViewItem mediaSetViewItemWire `json:"mediaSetViewItem"`
}

type mediaSetViewItemWire struct {
	MediaSetRid     string `json:"mediaSetRid"`
	MediaSetViewRid string `json:"mediaSetViewRid"`
	MediaItemRid    string `json:"mediaItemRid"`
}

// GetMediaPropertyMetadata handles
// GET /api/v2/ontologies/{o}/objects/{type}/{pk}/media/{property}/metadata.
func (h *Handler) GetMediaPropertyMetadata(w http.ResponseWriter, r *http.Request) {
	rid, ok := h.resolveMediaPropertyRID(w, r)
	if !ok {
		return
	}
	att, err := h.attachmentStore.Stat(r.Context(), rid)
	if err != nil {
		writeMediaStoreError(w, err)
		return
	}
	writeJSONOK(w, mediaMetadataResponse{
		Path:      att.Filename,
		SizeBytes: att.SizeBytes,
		MediaType: att.MediaType,
	})
}

// GetMediaPropertyContent handles
// GET /api/v2/ontologies/{o}/objects/{type}/{pk}/media/{property}/content.
func (h *Handler) GetMediaPropertyContent(w http.ResponseWriter, r *http.Request) {
	rid, ok := h.resolveMediaPropertyRID(w, r)
	if !ok {
		return
	}
	att, err := h.attachmentStore.Stat(r.Context(), rid)
	if err != nil {
		writeMediaStoreError(w, err)
		return
	}
	body, err := h.attachmentStore.Get(r.Context(), rid)
	if err != nil {
		writeMediaStoreError(w, err)
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

// UploadMediaProperty handles
// POST /api/v2/ontologies/{o}/objectTypes/{objectType}/media/{property}/upload.
// The request body is the raw media bytes; mediaItemPath must be supplied as
// a query parameter. The response is a Foundry MediaReference pointing at
// the newly created blob.
func (h *Handler) UploadMediaProperty(w http.ResponseWriter, r *http.Request) {
	if h.attachmentStore == nil {
		apierror.WriteJSON(w, apierror.NewInternal("MediaStoreNotConfigured", nil))
		return
	}

	mediaItemPath := strings.TrimSpace(r.URL.Query().Get("mediaItemPath"))
	if mediaItemPath == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingMediaItemPath", map[string]string{
			"parameter": "mediaItemPath",
		}))
		return
	}

	mediaType := strings.TrimSpace(r.Header.Get("Content-Type"))
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}

	att, err := h.attachmentStore.Put(r.Context(), attachment.BlobMeta{
		Filename:  mediaItemPath,
		MediaType: mediaType,
	}, r.Body)
	if err != nil {
		writeMediaStoreError(w, err)
		return
	}

	writeJSONOK(w, mediaReferenceResponse{
		MimeType: att.MediaType,
		Reference: mediaReferenceTarget{
			Type: "mediaSetViewItem",
			MediaSetViewItem: mediaSetViewItemWire{
				MediaSetRid:     defaultMediaSetRid,
				MediaSetViewRid: defaultMediaSetViewRid,
				MediaItemRid:    att.RID,
			},
		},
	})
}

// resolveMediaPropertyRID loads the addressed object and extracts the
// media reference RID stored under the {property} URL segment. Property
// values may be a single string RID or a nested MediaReference object
// (matching the Foundry wire shape), both resolved to the underlying
// attachment RID.
func (h *Handler) resolveMediaPropertyRID(w http.ResponseWriter, r *http.Request) (string, bool) {
	if h.attachmentStore == nil {
		apierror.WriteJSON(w, apierror.NewInternal("MediaStoreNotConfigured", nil))
		return "", false
	}

	ontologyRID := chi.URLParam(r, "ontologyApiName")
	objectType := chi.URLParam(r, "objectType")
	primaryKey := chi.URLParam(r, "primaryKey")
	propertyName := chi.URLParam(r, "property")

	obj, err := h.svc.GetObject(r.Context(), GetObjectRequest{
		OntologyRID: ontologyRID,
		ObjectType:  objectType,
		PrimaryKey:  primaryKey,
	})
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ObjectNotFound", map[string]string{
				"objectType": objectType,
				"primaryKey": primaryKey,
			}))
			return "", false
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetObjectFailed", map[string]string{
			"reason": err.Error(),
		}))
		return "", false
	}

	rawValue, ok := obj.Properties[propertyName]
	if !ok || rawValue == nil {
		apierror.WriteJSON(w, apierror.NewNotFound("PropertyNotFound", map[string]string{
			"property": propertyName,
		}))
		return "", false
	}

	rid, err := extractMediaReferenceRID(rawValue)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidMediaProperty", map[string]string{
			"property": propertyName,
			"reason":   err.Error(),
		}))
		return "", false
	}
	return rid, true
}

// extractMediaReferenceRID pulls the underlying blob RID out of a stored
// media property value. It accepts either a bare string RID or a nested
// map matching the Foundry MediaReference shape
// ({reference: {mediaSetViewItem: {mediaItemRid}}}).
func extractMediaReferenceRID(value interface{}) (string, error) {
	switch v := value.(type) {
	case string:
		if v == "" {
			return "", errors.New("media property value is empty")
		}
		return v, nil
	case map[string]interface{}:
		ref, ok := v["reference"].(map[string]interface{})
		if !ok {
			return "", errors.New("media property missing reference field")
		}
		item, ok := ref["mediaSetViewItem"].(map[string]interface{})
		if !ok {
			return "", errors.New("media property missing mediaSetViewItem")
		}
		rid, _ := item["mediaItemRid"].(string)
		if rid == "" {
			return "", errors.New("media property missing mediaItemRid")
		}
		return rid, nil
	default:
		return "", errors.New("media property value is not a string or object")
	}
}

func writeMediaStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, attachment.ErrNotFound):
		apierror.WriteJSON(w, apierror.NewNotFound("MediaItemNotFound", nil))
	case errors.Is(err, attachment.ErrInvalidRID):
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidMediaItemRid", nil))
	default:
		apierror.WriteJSON(w, apierror.NewInternal("MediaStoreError", map[string]string{
			"message": err.Error(),
		}))
	}
}
