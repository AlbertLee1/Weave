package modelfunctions

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/vertex/funcregistry"
)

// DeploymentRepo persists Deployment rows. The interface is intentionally
// narrow — Create is all the register endpoint exercises today — so the
// handler tests can stub it without standing up a full PostgreSQL repo.
type DeploymentRepo interface {
	Create(ctx context.Context, dep *Deployment) error
}

// FunctionCreator is the slice of oms.Repository the handler needs to
// persist the auto-generated wrapper. Splitting it out matches the
// FunctionLookup convention from pkg/vertex/funcregistry and keeps the
// test fakes minimal.
type FunctionCreator interface {
	CreateFunction(ctx context.Context, fn *oms.Function) error
}

// OntologyResolver translates the {ontologyApiName} URL segment into an
// ontology RID. Identical to funcregistry.OntologyResolver in shape —
// the duplication is deliberate so the modelfunctions package doesn't
// transitively depend on funcregistry for its public types.
type OntologyResolver interface {
	ResolveOntologyRID(ctx context.Context, apiName string) (string, error)
}

// Handler serves the VTX-050 register endpoint:
//
//	POST /api/vertex/v1/ontologies/{ontologyApiName}/model-functions/register
//
// On success the deployment is persisted and an oms.Function wrapper
// row is created so the live model becomes addressable through the
// same Function APIs (VTX-048) the rest of Vertex already uses.
type Handler struct {
	deployments DeploymentRepo
	functions   FunctionCreator
	ontology    OntologyResolver
}

// NewHandler wires a Handler over its three dependencies. A nil
// dependency at construction time signals a wiring bug, not a runtime
// degraded mode — main.go is expected to plumb in real implementations
// before mounting the routes.
func NewHandler(deployments DeploymentRepo, functions FunctionCreator, ontology OntologyResolver) *Handler {
	return &Handler{deployments: deployments, functions: functions, ontology: ontology}
}

// RegisterRoutes mounts the VTX-050 endpoint on r.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/vertex/v1/ontologies/{ontologyApiName}/model-functions/register", h.register)
}

// registerRequest is the wire body the register endpoint accepts. The
// shape mirrors Deployment for the user-supplied fields and leaves
// server-assigned fields (RID, CreatedAt, FunctionRID) implicit so the
// caller can't forge them.
type registerRequest struct {
	Name         string        `json:"name"`
	EndpointURL  string        `json:"endpointUrl"`
	ModelVersion string        `json:"modelVersion,omitempty"`
	Inputs       []SchemaParam `json:"inputs,omitempty"`
	Output       *SchemaReturn `json:"output,omitempty"`
	CreatedBy    string        `json:"createdBy,omitempty"`
}

// registerResponse is the 201 body the register endpoint returns. The
// deployment record carries the server-assigned RID + FunctionRID; the
// function record carries the wrapper that downstream Action wiring
// (VTX-051) will reference.
type registerResponse struct {
	Deployment Deployment   `json:"deployment"`
	Function   oms.Function `json:"function"`
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	if h.deployments == nil || h.functions == nil || h.ontology == nil {
		apierror.WriteJSON(w, apierror.NewInternal("ModelFunctionsNotConfigured", nil))
		return
	}

	apiName := chi.URLParam(r, "ontologyApiName")
	ontologyRID, err := h.ontology.ResolveOntologyRID(r.Context(), apiName)
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{"ontologyApiName": apiName}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ResolveOntologyFailed", map[string]string{"error": err.Error()}))
		return
	}

	var req registerRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{"reason": "invalid JSON"}))
		return
	}
	if req.Name == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:name", map[string]string{
			"parameter": "name", "reason": "name is required",
		}))
		return
	}
	if req.EndpointURL == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:endpointUrl", map[string]string{
			"parameter": "endpointUrl", "reason": "endpointUrl is required",
		}))
		return
	}

	dep := Deployment{
		RID:          NewDeploymentRID(),
		OntologyRID:  ontologyRID,
		Name:         req.Name,
		EndpointURL:  req.EndpointURL,
		ModelVersion: req.ModelVersion,
		Inputs:       req.Inputs,
		Output:       req.Output,
		CreatedBy:    req.CreatedBy,
		CreatedAt:    time.Now().UTC(),
	}

	fn, err := BuildWrapperFunction(dep, req.CreatedBy)
	if err != nil {
		// Map the typed wrapper-build failures back to 400s with a
		// structured payload. ParamTypeError carries field-level
		// detail so SDK callers can pinpoint the bad I/O entry.
		var pe *funcregistry.ParamTypeError
		if errors.As(err, &pe) {
			payload := map[string]string{"reason": err.Error(), "type": pe.Type}
			if pe.IsReturn {
				payload["target"] = "return"
			} else {
				payload["parameter"] = pe.Parameter
				payload["target"] = "parameter"
			}
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:schema", payload))
			return
		}
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:deployment", map[string]string{"reason": err.Error()}))
		return
	}
	dep.FunctionRID = fn.RID

	// Persist the deployment first so a function-create failure leaves
	// a forensic row the operator can inspect (and we don't risk
	// stranding the function pointing at a deployment that never
	// landed). If deployment-write fails we surface 500 without ever
	// touching the function repo.
	if err := h.deployments.Create(r.Context(), &dep); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("CreateDeploymentFailed", map[string]string{"error": err.Error()}))
		return
	}

	if err := h.functions.CreateFunction(r.Context(), fn); err != nil {
		if errors.Is(err, oms.ErrDuplicate) {
			apierror.WriteJSON(w, apierror.NewConflict("FunctionAlreadyExists", map[string]string{
				"name": fn.Name, "version": fn.Version,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CreateFunctionFailed", map[string]string{"error": err.Error()}))
		return
	}

	resp := registerResponse{Deployment: dep, Function: *fn}
	httputil.WriteJSON(w, http.StatusCreated, resp)
}
