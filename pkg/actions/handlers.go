package actions

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
)

// Handler handles action HTTP requests.
type Handler struct {
	executor *Executor
}

// NewHandler creates a new action handler.
func NewHandler(executor *Executor) *Handler {
	return &Handler{executor: executor}
}

// Apply handles POST /api/v2/ontologies/{ontologyApiName}/actions/apply
func (h *Handler) Apply(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")

	var req ApplyRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{"error": err.Error()}))
		return
	}

	if req.ActionType == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingActionType", nil))
		return
	}

	result, err := h.executor.Apply(r.Context(), ontologyRID, &req)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("ActionFailed", map[string]string{"error": err.Error()}))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, result)
}

// ApplyBatch handles POST /api/v2/ontologies/{ontologyApiName}/actions/applyBatch
func (h *Handler) ApplyBatch(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")

	var reqs struct {
		Actions []ApplyRequest `json:"actions"`
	}
	if err := httputil.ReadJSON(r, &reqs); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{"error": err.Error()}))
		return
	}

	var results []*ApplyResult
	for _, req := range reqs.Actions {
		result, err := h.executor.Apply(r.Context(), ontologyRID, &req)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("ActionFailed", map[string]string{
				"actionType": req.ActionType,
				"error":      err.Error(),
			}))
			return
		}
		results = append(results, result)
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"results": results})
}
