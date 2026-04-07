package oss

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
)

// createLinkBody is the request body for POST /links/{linkTypeApiName}.
type createLinkBody struct {
	SourcePK   string                 `json:"sourcePk"`
	TargetPK   string                 `json:"targetPk"`
	Properties map[string]interface{} `json:"properties,omitempty"`
}

// deleteLinkBody is the request body for DELETE /links/{linkTypeApiName}.
type deleteLinkBody struct {
	SourcePK string `json:"sourcePk"`
	TargetPK string `json:"targetPk"`
}

// CreateLink handles POST /api/v2/ontologies/{ontologyApiName}/links/{linkTypeApiName}.
// Upserts a single many-to-many link edge. Responses: 201 on success,
// 400 on invalid body or missing PK fields, 404 if the link type is unknown
// or declared with a non-M2M cardinality, 500 on internal errors.
func (h *Handler) CreateLink(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	linkTypeAPIName := chi.URLParam(r, "linkTypeApiName")

	var body createLinkBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidLinkRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if body.SourcePK == "" || body.TargetPK == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingLinkPrimaryKey", map[string]string{
			"sourcePk": body.SourcePK,
			"targetPk": body.TargetPK,
		}))
		return
	}

	err := h.svc.CreateLink(r.Context(), CreateLinkRequest{
		OntologyRID:     ontologyRID,
		LinkTypeAPIName: linkTypeAPIName,
		SourcePK:        body.SourcePK,
		TargetPK:        body.TargetPK,
		Properties:      body.Properties,
	})
	if err != nil {
		h.writeLinkError(w, linkTypeAPIName, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":          "created",
		"linkType":        linkTypeAPIName,
		"sourcePrimaryKey": body.SourcePK,
		"targetPrimaryKey": body.TargetPK,
	})
}

// DeleteLink handles DELETE /api/v2/ontologies/{ontologyApiName}/links/{linkTypeApiName}.
// Removes a single M2M link edge. Idempotent — deleting a non-existent edge
// returns 200 with status=deleted. Responses: 200 on success, 400 on invalid
// body or missing PK fields, 404 if the link type is unknown or non-M2M.
func (h *Handler) DeleteLink(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	linkTypeAPIName := chi.URLParam(r, "linkTypeApiName")

	var body deleteLinkBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidLinkRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if body.SourcePK == "" || body.TargetPK == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingLinkPrimaryKey", map[string]string{
			"sourcePk": body.SourcePK,
			"targetPk": body.TargetPK,
		}))
		return
	}

	err := h.svc.DeleteLink(r.Context(), DeleteLinkRequest{
		OntologyRID:     ontologyRID,
		LinkTypeAPIName: linkTypeAPIName,
		SourcePK:        body.SourcePK,
		TargetPK:        body.TargetPK,
	})
	if err != nil {
		h.writeLinkError(w, linkTypeAPIName, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":          "deleted",
		"linkType":        linkTypeAPIName,
		"sourcePrimaryKey": body.SourcePK,
		"targetPrimaryKey": body.TargetPK,
	})
}

// writeLinkError maps domain errors to API error responses for the link CRUD
// handlers. ErrLinkTypeNotFound and ErrUnsupportedCardinality both produce
// 404 because from the caller's perspective the targeted resource (a mutable
// M2M link type with that API name) does not exist.
func (h *Handler) writeLinkError(w http.ResponseWriter, linkTypeAPIName string, err error) {
	if errors.Is(err, ErrLinkTypeNotFound) || errors.Is(err, ErrUnsupportedCardinality) {
		apierror.WriteJSON(w, apierror.NewNotFound("LinkTypeNotFound", map[string]string{
			"linkType": linkTypeAPIName,
			"reason":   err.Error(),
		}))
		return
	}
	apierror.WriteJSON(w, apierror.NewInternal("LinkMutationFailed", map[string]string{
		"reason": err.Error(),
	}))
}
