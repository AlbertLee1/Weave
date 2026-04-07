package oss

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/oms"
)

const (
	// historyDefaultLimit is the page size used when the caller does not
	// supply ?limit=. Matches the default in the PG repo.
	historyDefaultLimit = 50
	// historyMaxLimit is the upper bound enforced on ?limit= regardless of
	// what the caller asks for, to keep history responses bounded.
	historyMaxLimit = 500
)

// SetHistoryRepo wires an OMS repository so the handler can serve the
// /objects/{objectType}/{primaryKey}/history endpoint. Pass nil (or never
// call) to disable the history route. The OMS service interface does not
// know about history; it is read directly from the OMS repository.
func (h *Handler) SetHistoryRepo(repo oms.Repository) {
	h.historyRepo = repo
}

// objectHistoryResponse is the JSON shape returned by GetObjectHistory.
type objectHistoryResponse struct {
	History       []oms.ObjectHistory `json:"history"`
	TotalVersions int64               `json:"totalVersions"`
}

// GetObjectHistory handles
//
//	GET /api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/history
//
// Returns the most recent N revisions for a single object plus the total
// number of versions recorded. Query params:
//
//	limit  default 50, max 500
//
// Responses: 200 with the response body, 400 on bad limit, 404 if the
// object type is unknown, 503 when the history repo has not been wired.
func (h *Handler) GetObjectHistory(w http.ResponseWriter, r *http.Request) {
	if h.historyRepo == nil {
		apierror.WriteJSON(w, apierror.NewInternal("HistoryNotConfigured", map[string]string{
			"reason": "object history is not enabled on this server",
		}))
		return
	}

	ontologyAPI := chi.URLParam(r, "ontologyApiName")
	objectType := chi.URLParam(r, "objectType")
	primaryKey := chi.URLParam(r, "primaryKey")

	limit := historyDefaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidLimit", map[string]string{
				"limit": raw,
			}))
			return
		}
		if n > historyMaxLimit {
			n = historyMaxLimit
		}
		limit = n
	}

	ot, err := h.historyRepo.GetObjectTypeByAPIName(r.Context(), ontologyAPI, objectType)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewNotFound("ObjectTypeNotFound", map[string]string{
			"objectType": objectType,
		}))
		return
	}

	rows, err := h.historyRepo.ListObjectHistory(r.Context(), ot.RID, primaryKey, limit)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListObjectHistoryFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	total, err := h.historyRepo.GetObjectVersionCount(r.Context(), ot.RID, primaryKey)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("GetObjectVersionCountFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	if rows == nil {
		rows = []oms.ObjectHistory{}
	}
	httputil.WriteJSON(w, http.StatusOK, objectHistoryResponse{
		History:       rows,
		TotalVersions: total,
	})
}
