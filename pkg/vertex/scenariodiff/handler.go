package scenariodiff

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/scenarios"
)

// EditsReader is the slim slice of scenarios.Repo the diff handler needs.
// It mirrors scenarios.Repo.ListEdits exactly so a real *scenarios.Repo can
// be passed in without an adapter. Splitting it out keeps the handler test
// fakes minimal — no need to stub the full Repo surface.
type EditsReader interface {
	ListEdits(ctx context.Context, scenarioRID string) ([]scenarios.ScenarioEdit, error)
}

// Handler serves GET /api/vertex/v1/scenarios/{rid}/diff.
type Handler struct {
	reader EditsReader
	base   BaseLoader
}

// NewHandler wires a Handler over an EditsReader + BaseLoader. A nil
// BaseLoader is replaced with NoBaseLoader so callers that just need
// created/deleted bookkeeping don't have to provide one.
func NewHandler(reader EditsReader, base BaseLoader) *Handler {
	if base == nil {
		base = NoBaseLoader{}
	}
	return &Handler{reader: reader, base: base}
}

// RegisterRoutes mounts the VTX-046 endpoint on r.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/vertex/v1/scenarios/{rid}/diff", h.getDiff)
}

func (h *Handler) getDiff(w http.ResponseWriter, r *http.Request) {
	if h.reader == nil {
		apierror.WriteJSON(w, apierror.NewInternal("RepoNotConfigured", nil))
		return
	}
	ridStr := chi.URLParam(r, "rid")
	edits, err := h.reader.ListEdits(r.Context(), ridStr)
	if err != nil {
		if errors.Is(err, scenarios.ErrScenarioNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ScenarioNotFound", map[string]string{"rid": ridStr}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ListEditsFailed", map[string]string{"error": err.Error()}))
		return
	}
	diff, err := Compute(r.Context(), edits, h.base)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ComputeDiffFailed", map[string]string{"error": err.Error()}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, diff)
}
