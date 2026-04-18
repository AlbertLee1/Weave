package oms

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/rid"
)

// US-214 Interface Method Signatures: admin CRUD for interface_methods and
// the polymorphic invoke dispatcher that routes a method call to whichever
// ActionType (by implementsMethodRid) targets the caller-supplied
// ObjectType.

// CreateInterfaceMethodRequest is the request body for POST
// /api/v2/ontologies/{o}/interfaces/{interfaceRid}/methods.
type CreateInterfaceMethodRequest struct {
	Name        string                 `json:"name"`
	Params      []InterfaceMethodParam `json:"params"`
	Returns     InterfaceMethodReturns `json:"returns"`
	Description string                 `json:"description,omitempty"`
}

// UpdateInterfaceMethodRequest is the request body for PUT
// /api/v2/ontologies/{o}/interfaces/methods/byRid/{methodRid}.
// Every field fully replaces the corresponding stored value — partial
// updates would leave params / returns ambiguous across callers.
type UpdateInterfaceMethodRequest struct {
	Name        string                 `json:"name"`
	Params      []InterfaceMethodParam `json:"params"`
	Returns     InterfaceMethodReturns `json:"returns"`
	Description string                 `json:"description,omitempty"`
}

// InvokeInterfaceMethodRequest is the request body for POST
// /api/v2/ontologies/{o}/interfaces/methods/{methodRid}/invoke. The caller
// specifies which concrete ObjectType apiName to dispatch on; the handler
// finds an ActionType that both implements this method AND has a rule
// targeting that ObjectType.
type InvokeInterfaceMethodRequest struct {
	ObjectType string                 `json:"objectType"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}

// InvokeInterfaceMethodResponse wraps the dispatched ActionType identity so
// the caller can see which concrete implementation ran. The actual
// per-action execution happens via the pkg/actions Executor injected by
// cmd/server; this handler is purely a resolver + forwarder.
type InvokeInterfaceMethodResponse struct {
	ActionTypeRID     string          `json:"actionTypeRid"`
	ActionTypeAPIName string          `json:"actionTypeApiName"`
	ObjectType        string          `json:"objectType"`
	MethodRID         string          `json:"methodRid"`
	Result            json.RawMessage `json:"result,omitempty"`
}

// InterfaceMethodActionDispatcher is the narrow interface a forwarder must
// satisfy to actually run the resolved ActionType. Defined here (rather
// than depending on pkg/actions) so oms does not cycle through its own
// Repository. cmd/server supplies a tiny adapter around actions.Executor.
type InterfaceMethodActionDispatcher interface {
	Dispatch(ctx context.Context, ontologyAPINameOrRID string, actionAPIName string, parameters map[string]interface{}) (json.RawMessage, error)
}

// SetInterfaceMethodStore wires the narrow InterfaceMethodStore used by the
// method CRUD handlers + ActionType.implementsMethodRid validation (US-214).
// When unset the corresponding endpoints respond with 503 NotConfigured and
// the ActionType-level validation short-circuits to a clean error so
// degraded-mode test routers can boot without the store.
func (h *OMSHandler) SetInterfaceMethodStore(s InterfaceMethodStore) {
	h.interfaceMethodStore = s
}

// SetInterfaceMethodDispatcher wires the optional dispatcher for the
// polymorphic invoke endpoint. Without it the invoke path still resolves
// the target ActionType but returns the resolution result with an empty
// Result payload — callers can still confirm the dispatch decision without
// a full execution pipeline (useful in tests).
func (h *OMSHandler) SetInterfaceMethodDispatcher(d InterfaceMethodActionDispatcher) {
	h.interfaceMethodDispatcher = d
}

func (h *OMSHandler) interfaceMethodStoreOrError(w http.ResponseWriter) (InterfaceMethodStore, bool) {
	if h.interfaceMethodStore == nil {
		apierror.WriteJSON(w, apierror.NewInternal("InterfaceMethodStoreNotConfigured", nil))
		return nil, false
	}
	return h.interfaceMethodStore, true
}

// --- CRUD handlers ---

// CreateInterfaceMethod handles POST
// /api/v2/ontologies/{o}/interfaces/{interfaceRid}/methods.
func (h *OMSHandler) CreateInterfaceMethod(w http.ResponseWriter, r *http.Request) {
	store, ok := h.interfaceMethodStoreOrError(w)
	if !ok {
		return
	}
	interfaceRID := chi.URLParam(r, "interfaceRid")
	if _, err := h.repo.GetInterface(r.Context(), interfaceRID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("InterfaceNotFound", map[string]string{
				"interfaceRid": interfaceRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetInterfaceFailed", nil))
		return
	}

	var req CreateInterfaceMethodRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}
	if req.Name == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:name", map[string]string{
			"parameter": "name",
			"reason":    "name is required",
		}))
		return
	}

	im := &InterfaceMethod{
		RID:          rid.New("ontology", "main", "interface-method"),
		InterfaceRID: interfaceRID,
		Name:         req.Name,
		Params:       req.Params,
		Returns:      req.Returns,
		Description:  req.Description,
	}
	if err := im.Validate(); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:method", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if err := store.CreateInterfaceMethod(r.Context(), im); err != nil {
		if errors.Is(err, ErrDuplicate) {
			apierror.WriteJSON(w, apierror.NewConflict("InterfaceMethodAlreadyExists", map[string]string{
				"name": req.Name,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CreateInterfaceMethodFailed", nil))
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, im)
}

// ListInterfaceMethods handles GET
// /api/v2/ontologies/{o}/interfaces/{interfaceRid}/methods.
func (h *OMSHandler) ListInterfaceMethods(w http.ResponseWriter, r *http.Request) {
	store, ok := h.interfaceMethodStoreOrError(w)
	if !ok {
		return
	}
	interfaceRID := chi.URLParam(r, "interfaceRid")
	if _, err := h.repo.GetInterface(r.Context(), interfaceRID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("InterfaceNotFound", map[string]string{
				"interfaceRid": interfaceRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetInterfaceFailed", nil))
		return
	}
	methods, err := store.ListInterfaceMethods(r.Context(), interfaceRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListInterfaceMethodsFailed", nil))
		return
	}
	if methods == nil {
		methods = []InterfaceMethod{}
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": methods})
}

// GetInterfaceMethod handles GET
// /api/v2/ontologies/{o}/interfaces/methods/byRid/{methodRid}.
func (h *OMSHandler) GetInterfaceMethod(w http.ResponseWriter, r *http.Request) {
	store, ok := h.interfaceMethodStoreOrError(w)
	if !ok {
		return
	}
	methodRID := chi.URLParam(r, "methodRid")
	im, err := store.GetInterfaceMethod(r.Context(), methodRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("InterfaceMethodNotFound", map[string]string{
				"methodRid": methodRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetInterfaceMethodFailed", nil))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, im)
}

// UpdateInterfaceMethod handles PUT
// /api/v2/ontologies/{o}/interfaces/methods/byRid/{methodRid}.
func (h *OMSHandler) UpdateInterfaceMethod(w http.ResponseWriter, r *http.Request) {
	store, ok := h.interfaceMethodStoreOrError(w)
	if !ok {
		return
	}
	methodRID := chi.URLParam(r, "methodRid")
	existing, err := store.GetInterfaceMethod(r.Context(), methodRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("InterfaceMethodNotFound", map[string]string{
				"methodRid": methodRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetInterfaceMethodFailed", nil))
		return
	}

	var req UpdateInterfaceMethodRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	updated := *existing
	if req.Name != "" {
		updated.Name = req.Name
	}
	updated.Params = req.Params
	updated.Returns = req.Returns
	updated.Description = req.Description
	if err := updated.Validate(); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:method", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if err := store.UpdateInterfaceMethod(r.Context(), &updated); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("InterfaceMethodNotFound", map[string]string{
				"methodRid": methodRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("UpdateInterfaceMethodFailed", nil))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, &updated)
}

// DeleteInterfaceMethod handles DELETE
// /api/v2/ontologies/{o}/interfaces/methods/byRid/{methodRid}.
func (h *OMSHandler) DeleteInterfaceMethod(w http.ResponseWriter, r *http.Request) {
	store, ok := h.interfaceMethodStoreOrError(w)
	if !ok {
		return
	}
	methodRID := chi.URLParam(r, "methodRid")
	if err := store.DeleteInterfaceMethod(r.Context(), methodRID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("InterfaceMethodNotFound", map[string]string{
				"methodRid": methodRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DeleteInterfaceMethodFailed", nil))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Polymorphic dispatch ---

// InvokeInterfaceMethod handles POST
// /api/v2/ontologies/{o}/interfaces/methods/{methodRid}/invoke. Resolves a
// concrete ActionType that (a) has implementsMethodRid == methodRid and (b)
// has a rule targeting req.ObjectType's apiName. The ObjectType must
// actually implement the method's owning Interface.
func (h *OMSHandler) InvokeInterfaceMethod(w http.ResponseWriter, r *http.Request) {
	store, ok := h.interfaceMethodStoreOrError(w)
	if !ok {
		return
	}
	ontologyAPINameOrRID := chi.URLParam(r, "ontologyApiName")
	methodRID := chi.URLParam(r, "methodRid")

	method, err := store.GetInterfaceMethod(r.Context(), methodRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("InterfaceMethodNotFound", map[string]string{
				"methodRid": methodRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetInterfaceMethodFailed", nil))
		return
	}

	var req InvokeInterfaceMethodRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}
	if req.ObjectType == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:objectType", map[string]string{
			"parameter": "objectType",
			"reason":    "objectType is required",
		}))
		return
	}

	ontologyRID, err := h.resolveOntologyRID(r.Context(), ontologyAPINameOrRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{
				"ontologyApiName": ontologyAPINameOrRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ResolveOntologyFailed", nil))
		return
	}

	ot, err := h.repo.GetObjectTypeByAPIName(r.Context(), ontologyRID, req.ObjectType)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ObjectTypeNotFound", map[string]string{
				"objectType": req.ObjectType,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetObjectTypeFailed", nil))
		return
	}
	if apiErr := h.requireObjectTypeImplementsInterface(r.Context(), ot.RID, method.InterfaceRID); apiErr != nil {
		apierror.WriteJSON(w, apiErr)
		return
	}

	action, apiErr := h.findImplementingAction(r.Context(), ontologyRID, methodRID, req.ObjectType)
	if apiErr != nil {
		apierror.WriteJSON(w, apiErr)
		return
	}

	resp := InvokeInterfaceMethodResponse{
		ActionTypeRID:     action.RID,
		ActionTypeAPIName: action.APIName,
		ObjectType:        req.ObjectType,
		MethodRID:         methodRID,
	}
	if h.interfaceMethodDispatcher != nil {
		result, err := h.interfaceMethodDispatcher.Dispatch(r.Context(), ontologyRID, action.APIName, req.Parameters)
		if err != nil {
			if typed := asTypedAPIError(err); typed != nil {
				apierror.WriteJSON(w, typed)
				return
			}
			apierror.WriteJSON(w, apierror.NewInternal("InterfaceMethodDispatchFailed", map[string]string{
				"reason": err.Error(),
			}))
			return
		}
		resp.Result = result
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}

// validateImplementsMethodRID verifies that a method referenced from
// ActionType.ImplementsMethodRID exists AND belongs to an Interface in the
// same ontology. Returns a typed APIError on failure so both the create
// and update handlers can forward it unchanged.
func (h *OMSHandler) validateImplementsMethodRID(ctx context.Context, ontologyRID, methodRID string) *apierror.APIError {
	if h.interfaceMethodStore == nil {
		return apierror.NewInternal("InterfaceMethodStoreNotConfigured", nil)
	}
	method, err := h.interfaceMethodStore.GetInterfaceMethod(ctx, methodRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return apierror.NewNotFound("InterfaceMethodNotFound", map[string]string{
				"methodRid": methodRID,
			})
		}
		return apierror.NewInternal("GetInterfaceMethodFailed", nil)
	}
	iface, err := h.repo.GetInterface(ctx, method.InterfaceRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return apierror.NewNotFound("InterfaceNotFound", map[string]string{
				"interfaceRid": method.InterfaceRID,
			})
		}
		return apierror.NewInternal("GetInterfaceFailed", nil)
	}
	if iface.OntologyRID != ontologyRID {
		return apierror.NewInvalidParameter("InvalidParameter:implementsMethodRid", map[string]string{
			"parameter": "implementsMethodRid",
			"reason":    "method belongs to a different ontology",
		})
	}
	return nil
}

// requireObjectTypeImplementsInterface walks the object_type_interfaces
// attachments and fails with 400 if the ObjectType does not declare the
// target Interface. Keeps the dispatcher honest: an action that happens to
// match a method rid but whose ObjectType did not opt in cannot be called
// via the polymorphic path.
func (h *OMSHandler) requireObjectTypeImplementsInterface(ctx context.Context, objectTypeRID, interfaceRID string) *apierror.APIError {
	attached, err := h.repo.ListObjectTypeInterfaces(ctx, objectTypeRID)
	if err != nil {
		return apierror.NewInternal("ListObjectTypeInterfacesFailed", nil)
	}
	for _, oti := range attached {
		if oti.InterfaceRID == interfaceRID {
			return nil
		}
	}
	return apierror.NewInvalidParameter("InvalidParameter:objectType", map[string]string{
		"parameter": "objectType",
		"reason":    "ObjectType does not implement the method's Interface",
	})
}

// findImplementingAction scans the ontology's ActionTypes for one whose
// ImplementsMethodRID matches AND whose rule targets the requested
// ObjectType apiName. First match wins (admins are responsible for keeping
// at most one implementation per (method, objectType) pair; a
// ImplementsMethodAmbiguous error could be added later but would be a
// stricter contract than Foundry ships).
func (h *OMSHandler) findImplementingAction(ctx context.Context, ontologyRID, methodRID, objectTypeAPIName string) (*ActionType, *apierror.APIError) {
	actionTypes, err := h.repo.ListActionTypes(ctx, ontologyRID)
	if err != nil {
		return nil, apierror.NewInternal("ListActionTypesFailed", nil)
	}
	for _, at := range actionTypes {
		if at.ImplementsMethodRID != methodRID {
			continue
		}
		if actionTargetsObjectType(at, objectTypeAPIName) {
			copyAT := at
			return &copyAT, nil
		}
	}
	return nil, apierror.NewNotFound("NoImplementingAction", map[string]string{
		"methodRid":  methodRID,
		"objectType": objectTypeAPIName,
	})
}

// actionTargetsObjectType decodes the ActionType rules JSON and reports
// whether any rule declares `objectType == wanted`. Duplicates the tiny
// parse shape kept local to pkg/oms.breaking_changes to avoid importing
// pkg/actions (which would cycle through pkg/oms.Repository).
func actionTargetsObjectType(at ActionType, wanted string) bool {
	if wanted == "" {
		return false
	}
	if len(at.Rules) == 0 {
		return false
	}
	var rules []struct {
		ObjectType string `json:"objectType"`
	}
	if err := json.Unmarshal(at.Rules, &rules); err != nil {
		return false
	}
	for _, r := range rules {
		if r.ObjectType == wanted {
			return true
		}
	}
	return false
}

// asTypedAPIError mirrors pkg/actions.typedAPIError for handlers in this
// package that forward executor-level errors; kept tiny to avoid a cycle.
func asTypedAPIError(err error) *apierror.APIError {
	var apiErr *apierror.APIError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return nil
}
