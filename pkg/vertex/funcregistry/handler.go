package funcregistry

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rid"
)

// FunctionLookup is the slim slice of oms.Repository the Vertex
// Function registry handler depends on. Splitting it from the full
// Repository surface keeps the test fakes minimal — none of the
// non-Function CRUD has to be stubbed — and makes the dependency
// direction explicit at the call site in cmd/server/main.go.
type FunctionLookup interface {
	GetFunction(ctx context.Context, rid string) (*oms.Function, error)
	GetFunctionByName(ctx context.Context, ontologyRID, name string) (*oms.Function, error)
	ListFunctionVersionsByName(ctx context.Context, ontologyRID, name string) ([]oms.Function, error)
	CreateFunction(ctx context.Context, fn *oms.Function) error
}

// OntologyResolver translates the {ontologyApiName} URL segment into an
// ontology RID. Mirrors the pattern oms.OMSHandler.resolveOntologyRID
// uses internally; lifted to an interface here so the test stubs can
// avoid standing up a full PG / mem repository.
type OntologyResolver interface {
	ResolveOntologyRID(ctx context.Context, apiName string) (string, error)
}

// Handler serves the Vertex Function registry endpoints (VTX-048):
//   - GET  /api/vertex/v1/functions/{rid}                                     — metadata + I/O signature
//   - GET  /api/vertex/v1/ontologies/{ontologyApiName}/functions/{name}/resolve?range=... — semver-range resolver
//   - POST /api/vertex/v1/ontologies/{ontologyApiName}/functions/register     — registration with strict param-type allowlist
type Handler struct {
	lookup   FunctionLookup
	ontology OntologyResolver
}

// NewHandler constructs a Handler over the supplied dependencies. Both
// are required — a nil dependency at construction time signals a wiring
// bug rather than a runtime degraded mode.
func NewHandler(lookup FunctionLookup, ontology OntologyResolver) *Handler {
	return &Handler{lookup: lookup, ontology: ontology}
}

// RegisterRoutes mounts the VTX-048 endpoints on r.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/vertex/v1/functions/{rid}", h.getFunction)
	r.Get("/api/vertex/v1/ontologies/{ontologyApiName}/functions/{name}/resolve", h.resolveFunction)
	r.Get("/api/vertex/v1/ontologies/{ontologyApiName}/functions/{name}/versions", h.listFunctionVersions)
	r.Post("/api/vertex/v1/ontologies/{ontologyApiName}/functions/register", h.registerFunction)
}

// functionResponse is the wire shape returned by getFunction and
// resolveFunction. It carries the canonical Function row plus the
// parsed I/O signature (params + returns) so SDK clients don't have to
// re-parse the raw `signature` JSON themselves.
type functionResponse struct {
	*oms.Function
	Params  []oms.FunctionParam `json:"params,omitempty"`
	Returns *oms.FunctionReturn `json:"returns,omitempty"`
}

func buildFunctionResponse(fn *oms.Function) functionResponse {
	parsed, _ := oms.ParseFunctionSignature(fn.Signature)
	return functionResponse{
		Function: fn,
		Params:   parsed.Params,
		Returns:  parsed.Returns,
	}
}

func (h *Handler) getFunction(w http.ResponseWriter, r *http.Request) {
	if h.lookup == nil {
		apierror.WriteJSON(w, apierror.NewInternal("LookupNotConfigured", nil))
		return
	}
	fnRID := chi.URLParam(r, "rid")
	fn, err := h.lookup.GetFunction(r.Context(), fnRID)
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("FunctionNotFound", map[string]string{"rid": fnRID}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetFunctionFailed", map[string]string{"error": err.Error()}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, buildFunctionResponse(fn))
}

func (h *Handler) resolveFunction(w http.ResponseWriter, r *http.Request) {
	if h.lookup == nil || h.ontology == nil {
		apierror.WriteJSON(w, apierror.NewInternal("LookupNotConfigured", nil))
		return
	}
	apiName := chi.URLParam(r, "ontologyApiName")
	name := chi.URLParam(r, "name")

	ontologyRID, err := h.ontology.ResolveOntologyRID(r.Context(), apiName)
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{"ontologyApiName": apiName}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ResolveOntologyFailed", map[string]string{"error": err.Error()}))
		return
	}

	rng, err := ParseSemverRange(r.URL.Query().Get("range"))
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:range", map[string]string{
			"parameter": "range",
			"reason":    err.Error(),
		}))
		return
	}

	// Empty / "*" range short-circuits to the registry's default
	// "latest semver" resolution — same shape GetFunctionByName already
	// implements, just routed through the slim FunctionLookup. This
	// keeps the no-range URL behaviour identical to GET …/functions/{name}
	// on the OMS surface.
	if rng.IsAny() {
		fn, err := h.lookup.GetFunctionByName(r.Context(), ontologyRID, name)
		if err != nil {
			if errors.Is(err, oms.ErrNotFound) {
				apierror.WriteJSON(w, apierror.NewNotFound("FunctionNotFound", map[string]string{
					"ontologyApiName": apiName,
					"name":            name,
				}))
				return
			}
			apierror.WriteJSON(w, apierror.NewInternal("GetFunctionFailed", map[string]string{"error": err.Error()}))
			return
		}
		httputil.WriteJSON(w, http.StatusOK, buildFunctionResponse(fn))
		return
	}

	versions, err := h.lookup.ListFunctionVersionsByName(r.Context(), ontologyRID, name)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListFunctionVersionsFailed", map[string]string{"error": err.Error()}))
		return
	}
	winner, ok := ResolveLatestInRange(rng, versions)
	if !ok {
		apierror.WriteJSON(w, apierror.NewNotFound("FunctionVersionNotInRange", map[string]string{
			"ontologyApiName": apiName,
			"name":            name,
			"range":           r.URL.Query().Get("range"),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, buildFunctionResponse(&winner))
}

// registerFunctionRequest is the wire body POST register accepts. The
// shape is a strict subset of oms.CreateFunctionRequest — fields the
// Vertex registration flow doesn't yet need (branchId, pure, dependsOn)
// are omitted so the surface is small and easy to evolve. Adding them
// later is non-breaking because the JSON decoder is permissive about
// unknown fields by default.
type registerFunctionRequest struct {
	Name       string          `json:"name"`
	Version    string          `json:"version,omitempty"`
	SourceCode string          `json:"sourceCode"`
	Runtime    string          `json:"runtime,omitempty"`
	Signature  json.RawMessage `json:"signature,omitempty"`
	CreatedBy  string          `json:"createdBy,omitempty"`
}

// listFunctionVersions GET /api/vertex/v1/ontologies/{ontologyApiName}/
// functions/{name}/versions. Round 73. Returns every Function row
// matching (ontologyRID, name) sorted version DESC (newest first) —
// the registry's internal default ordering surfaced via
// ListFunctionVersionsByName.
//
// Unknown function name returns 200 + {versions: []} so the SPA's
// version-history panel renders cleanly against a brand-new function
// (name is a filter here, not a key). Unknown ontology slug returns
// 404 because the slug is a real lookup.
type listVersionsResponse struct {
	Versions []functionResponse `json:"versions"`
}

func (h *Handler) listFunctionVersions(w http.ResponseWriter, r *http.Request) {
	if h.lookup == nil || h.ontology == nil {
		apierror.WriteJSON(w, apierror.NewInternal("LookupNotConfigured", nil))
		return
	}
	apiName := chi.URLParam(r, "ontologyApiName")
	name := chi.URLParam(r, "name")

	ontologyRID, err := h.ontology.ResolveOntologyRID(r.Context(), apiName)
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{"ontologyApiName": apiName}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ResolveOntologyFailed", map[string]string{"error": err.Error()}))
		return
	}

	versions, err := h.lookup.ListFunctionVersionsByName(r.Context(), ontologyRID, name)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListFunctionVersionsFailed", map[string]string{"error": err.Error()}))
		return
	}
	out := make([]functionResponse, 0, len(versions))
	for i := range versions {
		out = append(out, buildFunctionResponse(&versions[i]))
	}
	httputil.WriteJSON(w, http.StatusOK, listVersionsResponse{Versions: out})
}

func (h *Handler) registerFunction(w http.ResponseWriter, r *http.Request) {
	if h.lookup == nil || h.ontology == nil {
		apierror.WriteJSON(w, apierror.NewInternal("LookupNotConfigured", nil))
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

	var req registerFunctionRequest
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
	if req.SourceCode == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:sourceCode", map[string]string{
			"parameter": "sourceCode", "reason": "sourceCode is required",
		}))
		return
	}

	// Reuse the OMS signature parser for shape errors so a payload that
	// would be rejected by the canonical CreateFunction handler is also
	// rejected here. The strict param-type allowlist runs on the parsed
	// signature and is the gating check for BDD #2.
	if err := oms.ValidateFunctionSignature(req.Signature); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:signature", map[string]string{
			"parameter": "signature", "reason": err.Error(),
		}))
		return
	}
	parsed, err := oms.ParseFunctionSignature(req.Signature)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:signature", map[string]string{
			"parameter": "signature", "reason": err.Error(),
		}))
		return
	}
	if err := ValidateParameterTypes(parsed); err != nil {
		var pe *ParamTypeError
		if errors.As(err, &pe) {
			payload := map[string]string{
				"reason": err.Error(),
				"type":   pe.Type,
			}
			if pe.IsReturn {
				payload["target"] = "return"
			} else {
				payload["parameter"] = pe.Parameter
				payload["target"] = "parameter"
			}
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:signature", payload))
			return
		}
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:signature", map[string]string{"reason": err.Error()}))
		return
	}

	version := req.Version
	if version == "" {
		version = oms.DefaultFunctionVersion
	}
	fn := &oms.Function{
		RID:         rid.NewFunctionRID(),
		OntologyRID: ontologyRID,
		Name:        req.Name,
		Version:     version,
		SourceCode:  req.SourceCode,
		Runtime:     req.Runtime,
		Signature:   req.Signature,
		CreatedBy:   req.CreatedBy,
	}
	if err := fn.Validate(); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:function", map[string]string{"reason": err.Error()}))
		return
	}
	fn.Runtime = fn.NormalisedRuntime()

	if err := h.lookup.CreateFunction(r.Context(), fn); err != nil {
		if errors.Is(err, oms.ErrDuplicate) {
			apierror.WriteJSON(w, apierror.NewConflict("FunctionAlreadyExists", map[string]string{
				"name": req.Name, "version": fn.Version,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CreateFunctionFailed", map[string]string{"error": err.Error()}))
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, buildFunctionResponse(fn))
}
