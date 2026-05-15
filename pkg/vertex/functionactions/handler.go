package functionactions

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/oms"
)

// BindingRepo persists FunctionActionBinding rows. Create is the only
// surface the register endpoint needs today; the read paths used by the
// invoke wiring story live behind a separate Get method so the
// registration handler test fakes can stay minimal.
type BindingRepo interface {
	Create(ctx context.Context, b *FunctionActionBinding) error
	GetByActionType(ctx context.Context, ontologyRID, actionTypeRID string) (*FunctionActionBinding, error)
}

// ActionTypeLookup is the slim slice of oms.Repository the handler needs
// to validate that the bound action type exists, is function-backed, and
// pins the same function_rid the registration body carries.
type ActionTypeLookup interface {
	GetActionType(ctx context.Context, rid string) (*oms.ActionType, error)
}

// OntologyResolver translates the {ontologyApiName} URL segment into an
// ontology RID. Mirrors the convention used by pkg/vertex/funcregistry
// and pkg/vertex/modelfunctions — the duplication is deliberate so the
// functionactions package does not transitively depend on those packages
// just for the resolver type.
type OntologyResolver interface {
	ResolveOntologyRID(ctx context.Context, apiName string) (string, error)
}

// Handler serves the VTX-051 register endpoint:
//
//	POST /api/vertex/v1/ontologies/{ontologyApiName}/function-actions/register
//
// On success the binding is persisted (RID + CreatedAt server-assigned)
// and returned to the caller. The caller is expected to be the Vertex
// Scenario UI's "Bind function-backed Action" flow; the bound row will
// be picked up by the invoke wiring story to fold the Function's output
// onto scenario_edits rows at execute time.
type Handler struct {
	bindings BindingRepo
	actions  ActionTypeLookup
	ontology OntologyResolver
}

// NewHandler wires a Handler over its three dependencies. A nil
// dependency at construction time signals a wiring bug rather than a
// runtime degraded mode — main.go is expected to plumb in real
// implementations before mounting the routes.
func NewHandler(bindings BindingRepo, actions ActionTypeLookup, ontology OntologyResolver) *Handler {
	return &Handler{bindings: bindings, actions: actions, ontology: ontology}
}

// RegisterRoutes mounts the VTX-051 endpoint on r.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/vertex/v1/ontologies/{ontologyApiName}/function-actions/register", h.register)
}

// registerRequest is the wire body the register endpoint accepts.
// ActionTypeRID names the OMS action_types row the binding wraps;
// FunctionRID is the function_rid the binding pins (must match the
// action type's function_rid). OutputMappings carries the
// Vertex-specific Function-output → property edit rules.
type registerRequest struct {
	ActionTypeRID  string          `json:"actionTypeRid"`
	FunctionRID    string          `json:"functionRid"`
	OutputMappings []OutputMapping `json:"outputMappings"`
	CreatedBy      string          `json:"createdBy,omitempty"`
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	if h.bindings == nil || h.actions == nil || h.ontology == nil {
		apierror.WriteJSON(w, apierror.NewInternal("FunctionActionsNotConfigured", nil))
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
	if req.ActionTypeRID == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:actionTypeRid", map[string]string{
			"parameter": "actionTypeRid", "reason": "actionTypeRid is required",
		}))
		return
	}
	if req.FunctionRID == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:functionRid", map[string]string{
			"parameter": "functionRid", "reason": "functionRid is required",
		}))
		return
	}
	if err := ValidateOutputMappings(req.OutputMappings); err != nil {
		var me *MappingError
		if errors.As(err, &me) {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:outputMappings", map[string]string{
				"parameter": me.Field, "reason": me.Reason,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:outputMappings", map[string]string{"reason": err.Error()}))
		return
	}

	at, err := h.actions.GetActionType(r.Context(), req.ActionTypeRID)
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ActionTypeNotFound", map[string]string{"actionTypeRid": req.ActionTypeRID}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetActionTypeFailed", map[string]string{"error": err.Error()}))
		return
	}
	if at.OntologyRID != ontologyRID {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:actionTypeRid", map[string]string{
			"parameter": "actionTypeRid",
			"reason":    "actionType does not belong to ontology",
		}))
		return
	}
	if !at.IsFunctionBacked || at.FunctionRID == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:actionTypeRid", map[string]string{
			"parameter": "actionTypeRid",
			"reason":    "actionType is not function-backed",
		}))
		return
	}
	if at.FunctionRID != req.FunctionRID {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:functionRid", map[string]string{
			"parameter": "functionRid",
			"reason":    "functionRid does not match actionType.functionRid",
		}))
		return
	}

	binding := &FunctionActionBinding{
		RID:            NewFunctionActionRID(),
		OntologyRID:    ontologyRID,
		ActionTypeRID:  req.ActionTypeRID,
		FunctionRID:    req.FunctionRID,
		OutputMappings: req.OutputMappings,
		CreatedBy:      req.CreatedBy,
		CreatedAt:      time.Now().UTC(),
	}
	if err := h.bindings.Create(r.Context(), binding); err != nil {
		if errors.Is(err, oms.ErrDuplicate) {
			apierror.WriteJSON(w, apierror.NewConflict("FunctionActionBindingAlreadyExists", map[string]string{
				"actionTypeRid": req.ActionTypeRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CreateFunctionActionBindingFailed", map[string]string{"error": err.Error()}))
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, binding)
}
