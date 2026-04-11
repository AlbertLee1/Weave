package oss

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/attachment"
	"github.com/liyang/weave/pkg/oms"
)

// SetAttachmentStore wires the attachment blob store so the handler can serve
// the object-path attachment read endpoints. When nil, those routes return
// AttachmentStoreNotConfigured.
func (h *Handler) SetAttachmentStore(store attachment.BlobStore) {
	h.attachmentStore = store
}

// GetAttachmentPropertyMetadata handles
// GET /api/v2/ontologies/{o}/objects/{type}/{pk}/attachments/{property}.
func (h *Handler) GetAttachmentPropertyMetadata(w http.ResponseWriter, r *http.Request) {
	rid, ok := h.resolveAttachmentPropertyRID(w, r, "")
	if !ok {
		return
	}
	att, err := h.attachmentStore.Stat(r.Context(), rid)
	if err != nil {
		writeAttachmentStoreError(w, err)
		return
	}
	writeJSONOK(w, att)
}

// GetAttachmentPropertyContent handles
// GET /api/v2/ontologies/{o}/objects/{type}/{pk}/attachments/{property}/content.
func (h *Handler) GetAttachmentPropertyContent(w http.ResponseWriter, r *http.Request) {
	rid, ok := h.resolveAttachmentPropertyRID(w, r, "")
	if !ok {
		return
	}
	h.streamAttachmentContent(w, r, rid)
}

// GetAttachmentPropertyMetadataByRID handles
// GET /api/v2/ontologies/{o}/objects/{type}/{pk}/attachments/{property}/{attachmentRid}.
func (h *Handler) GetAttachmentPropertyMetadataByRID(w http.ResponseWriter, r *http.Request) {
	rid, ok := h.resolveAttachmentPropertyRID(w, r, chi.URLParam(r, "attachmentRid"))
	if !ok {
		return
	}
	att, err := h.attachmentStore.Stat(r.Context(), rid)
	if err != nil {
		writeAttachmentStoreError(w, err)
		return
	}
	writeJSONOK(w, att)
}

// GetAttachmentPropertyContentByRID handles
// GET /api/v2/ontologies/{o}/objects/{type}/{pk}/attachments/{property}/{attachmentRid}/content.
func (h *Handler) GetAttachmentPropertyContentByRID(w http.ResponseWriter, r *http.Request) {
	rid, ok := h.resolveAttachmentPropertyRID(w, r, chi.URLParam(r, "attachmentRid"))
	if !ok {
		return
	}
	h.streamAttachmentContent(w, r, rid)
}

// resolveAttachmentPropertyRID loads the addressed object, extracts the
// attachment RID stored under the {property} URL segment, and (optionally)
// validates it against an explicit {attachmentRid} path parameter. It writes
// the appropriate error response and returns ok=false on any failure.
func (h *Handler) resolveAttachmentPropertyRID(w http.ResponseWriter, r *http.Request, expectRID string) (string, bool) {
	if h.attachmentStore == nil {
		apierror.WriteJSON(w, apierror.NewInternal("AttachmentStoreNotConfigured", nil))
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
		apierror.WriteJSON(w, apierror.NewInvalidParameter("GetObjectFailed", map[string]string{
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

	storedRID, err := extractAttachmentRID(rawValue, expectRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidAttachmentProperty", map[string]string{
			"property": propertyName,
			"reason":   err.Error(),
		}))
		return "", false
	}
	if storedRID == "" {
		// expectRID supplied but no matching RID stored in the property.
		apierror.WriteJSON(w, apierror.NewNotFound("AttachmentNotFound", map[string]string{
			"attachmentRid": expectRID,
		}))
		return "", false
	}
	return storedRID, true
}

// extractAttachmentRID returns the attachment RID stored in the property
// value. The property may be a single string, or an array of strings (for
// attachmentArray properties). When expectRID is non-empty, the function
// returns that RID only if it is present in the value; otherwise it returns
// "" (not found). When expectRID is empty, it returns the single stored RID,
// or an error if the property holds zero or multiple attachments.
func extractAttachmentRID(value interface{}, expectRID string) (string, error) {
	switch v := value.(type) {
	case string:
		if expectRID != "" {
			if v == expectRID {
				return v, nil
			}
			return "", nil
		}
		return v, nil
	case []interface{}:
		rids := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return "", errors.New("attachment array element is not a string")
			}
			rids = append(rids, s)
		}
		if expectRID != "" {
			for _, s := range rids {
				if s == expectRID {
					return s, nil
				}
			}
			return "", nil
		}
		if len(rids) == 0 {
			return "", errors.New("attachment array property is empty")
		}
		if len(rids) > 1 {
			return "", errors.New("attachment array property holds multiple attachments; specify attachmentRid")
		}
		return rids[0], nil
	default:
		return "", errors.New("attachment property value is not a string or array of strings")
	}
}

func (h *Handler) streamAttachmentContent(w http.ResponseWriter, r *http.Request, rid string) {
	att, err := h.attachmentStore.Stat(r.Context(), rid)
	if err != nil {
		writeAttachmentStoreError(w, err)
		return
	}
	body, err := h.attachmentStore.Get(r.Context(), rid)
	if err != nil {
		writeAttachmentStoreError(w, err)
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

func writeAttachmentStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, attachment.ErrNotFound):
		apierror.WriteJSON(w, apierror.NewNotFound("AttachmentNotFound", nil))
	case errors.Is(err, attachment.ErrInvalidRID):
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidAttachmentRid", nil))
	default:
		apierror.WriteJSON(w, apierror.NewInternal("AttachmentStoreError", map[string]string{
			"message": err.Error(),
		}))
	}
}

func writeJSONOK(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(v)
}
