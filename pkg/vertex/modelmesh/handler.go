package modelmesh

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/oms"
)

// OntologyResolver translates the {ontologyApiName} URL segment into
// an ontology RID. The interface is defined locally — same shape as
// pkg/vertex/funcregistry / functionactions / modelfunctions — so
// modelmesh does not pick up a transitive dependency on those packages
// merely to share a type.
type OntologyResolver interface {
	ResolveOntologyRID(ctx context.Context, apiName string) (string, error)
}

// Handler serves the VTX-052 model-mesh endpoints:
//
//	POST /api/vertex/v1/ontologies/{ontologyApiName}/model-mesh/plan
//	POST /api/vertex/v1/ontologies/{ontologyApiName}/model-mesh/run
//
// The plan endpoint returns the layered topological order (or a 400
// CycleDetected) without executing anything; it is intended for the
// Vertex Scenario UI to preview the mesh before commit. The run
// endpoint additionally invokes the wired ModelExecutor for each
// node, returning RunResults in topological order.
type Handler struct {
	ontology OntologyResolver
	exec     ModelExecutor
}

// NewHandler wires a Handler. exec may be nil — the plan endpoint
// remains usable, while the run endpoint replies 500
// ModelMeshExecutorNotConfigured. main.go is expected to plug in the
// real executor (the Function dispatcher + scenario_edits writer)
// once the wiring story lands.
func NewHandler(ontology OntologyResolver, exec ModelExecutor) *Handler {
	return &Handler{ontology: ontology, exec: exec}
}

// RegisterRoutes mounts the plan and run endpoints on r.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/vertex/v1/ontologies/{ontologyApiName}/model-mesh/plan", h.plan)
	r.Post("/api/vertex/v1/ontologies/{ontologyApiName}/model-mesh/run", h.run)
}

type meshRequest struct {
	Models      []ModelNode `json:"models"`
	ScenarioRID string      `json:"scenarioRid,omitempty"`
	Concurrency int         `json:"concurrency,omitempty"`
}

type planResponse struct {
	Layers [][]string `json:"layers"`
}

type runResponse struct {
	Layers  [][]string  `json:"layers"`
	Results []RunResult `json:"results"`
}

func (h *Handler) plan(w http.ResponseWriter, r *http.Request) {
	if h.ontology == nil {
		apierror.WriteJSON(w, apierror.NewInternal("ModelMeshNotConfigured", nil))
		return
	}
	apiName := chi.URLParam(r, "ontologyApiName")
	if _, err := h.ontology.ResolveOntologyRID(r.Context(), apiName); err != nil {
		writeOntologyError(w, apiName, err)
		return
	}
	var req meshRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{"reason": "invalid JSON"}))
		return
	}
	layers, err := TopologicalLayers(req.Models)
	if err != nil {
		writePlanError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, planResponse{Layers: layersToSlice(layers)})
}

func (h *Handler) run(w http.ResponseWriter, r *http.Request) {
	if h.ontology == nil {
		apierror.WriteJSON(w, apierror.NewInternal("ModelMeshNotConfigured", nil))
		return
	}
	apiName := chi.URLParam(r, "ontologyApiName")
	if _, err := h.ontology.ResolveOntologyRID(r.Context(), apiName); err != nil {
		writeOntologyError(w, apiName, err)
		return
	}
	var req meshRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{"reason": "invalid JSON"}))
		return
	}
	layers, err := TopologicalLayers(req.Models)
	if err != nil {
		writePlanError(w, err)
		return
	}
	if h.exec == nil {
		apierror.WriteJSON(w, apierror.NewInternal("ModelMeshExecutorNotConfigured", nil))
		return
	}
	runner := &Runner{Concurrency: req.Concurrency}
	results, runErr := runner.Run(r.Context(), req.Models, h.exec)
	if runErr != nil && !errors.Is(runErr, ErrCycleDetected) {
		// Cycle was already caught by the explicit TopologicalLayers
		// call above; this branch only fires if a model executor
		// returned an error mid-run. Surface it as 500 — the per-
		// model failure is also visible inside results[].error.
		apierror.WriteJSON(w, apierror.NewInternal("ModelMeshRunFailed", map[string]string{"error": runErr.Error()}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, runResponse{
		Layers:  layersToSlice(layers),
		Results: results,
	})
}

func writeOntologyError(w http.ResponseWriter, apiName string, err error) {
	if errors.Is(err, oms.ErrNotFound) {
		apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{"ontologyApiName": apiName}))
		return
	}
	apierror.WriteJSON(w, apierror.NewInternal("ResolveOntologyFailed", map[string]string{"error": err.Error()}))
}

func writePlanError(w http.ResponseWriter, err error) {
	var ce *CycleError
	if errors.As(err, &ce) {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("CycleDetected", map[string]string{
			"cycle":  strings.Join(ce.Cycle, " -> "),
			"reason": "model mesh contains a dependency cycle",
		}))
		return
	}
	if errors.Is(err, ErrEmptyModelID) {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:id", map[string]string{
			"reason": "model id is required",
		}))
		return
	}
	if errors.Is(err, ErrDuplicateModelID) {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:id", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidModelMesh", map[string]string{"reason": err.Error()}))
}

func layersToSlice(layers []Layer) [][]string {
	out := make([][]string, len(layers))
	for i, layer := range layers {
		out[i] = append([]string(nil), layer...)
	}
	return out
}
