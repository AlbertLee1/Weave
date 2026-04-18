package oms

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
)

// OMSHandler provides HTTP handlers for OMS V2 and admin endpoints.
type OMSHandler struct {
	repo              Repository
	queryExecutor     QueryExecutor
	actorFn           ActorFunc
	linkPropertyStore LinkPropertyStore
	linkEdgeStore     LinkEdgeStore
}

// NewOMSHandler creates a new OMSHandler with the given repository.
func NewOMSHandler(repo Repository) *OMSHandler {
	return &OMSHandler{repo: repo}
}

// SetQueryExecutor wires an optional QueryExecutor for function-backed query
// dispatch. When set, ExecuteQueryType routes QueryTypes with a non-empty
// FunctionRID through this executor instead of returning raw metadata.
func (h *OMSHandler) SetQueryExecutor(qe QueryExecutor) {
	h.queryExecutor = qe
}

// SetLinkPropertyStore wires the narrow LinkPropertyStore used by the
// link-property admin handlers (US-210). When unset the corresponding CRUD
// endpoints respond with 503 NotConfigured so degraded-mode test routers that
// do not supply the store still boot cleanly.
func (h *OMSHandler) SetLinkPropertyStore(s LinkPropertyStore) {
	h.linkPropertyStore = s
}

// SetLinkEdgeStore wires the narrow LinkEdgeStore used by the edge-value
// handlers (US-210) and by the searchAround enrichment path. When unset the
// PUT edge-properties endpoint responds with 503 NotConfigured.
func (h *OMSHandler) SetLinkEdgeStore(s LinkEdgeStore) {
	h.linkEdgeStore = s
}

// SetActorFunc sets a function that extracts the user ID from request context.
// Used by notification handlers to determine the current user.
func (h *OMSHandler) SetActorFunc(fn ActorFunc) {
	h.actorFn = fn
}

// resolveRepo returns a Repository for the request. If ?branch= is set, it
// wraps h.repo with a BranchedRepository that overlays branch changes on reads.
// Returns (repo, true) on success or (nil, false) if an error was written.
func (h *OMSHandler) resolveRepo(w http.ResponseWriter, r *http.Request) (Repository, bool) {
	branchID := r.URL.Query().Get("branch")
	if branchID == "" {
		return h.repo, true
	}
	branch, err := h.repo.GetBranch(r.Context(), branchID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("BranchNotFound", map[string]string{
				"branchId": branchID,
			}))
			return nil, false
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetBranchFailed", nil))
		return nil, false
	}
	_ = branch // existence check sufficient; branch may be in any status for reads
	return NewBranchedRepository(h.repo, branchID), true
}

// ListOntologies handles GET /api/v2/ontologies.
func (h *OMSHandler) ListOntologies(w http.ResponseWriter, r *http.Request) {
	list, err := h.repo.ListOntologies(r.Context())
	if err != nil {
		apierror.WriteJSON(w, apierror.NewNotFound("ListOntologiesFailed", nil))
		return
	}
	if list == nil {
		list = []Ontology{}
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": list})
}

// GetOntology handles GET /api/v2/ontologies/{ontologyApiName}.
func (h *OMSHandler) GetOntology(w http.ResponseWriter, r *http.Request) {
	rid := chi.URLParam(r, "ontologyApiName")
	o, err := h.repo.GetOntology(r.Context(), rid)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{"ontologyApiName": rid}))
			return
		}
		apierror.WriteJSON(w, apierror.NewNotFound("GetOntologyFailed", nil))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, o)
}

// ListObjectTypes handles GET /api/v2/ontologies/{ontologyApiName}/objectTypes.
func (h *OMSHandler) ListObjectTypes(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r)
	if !ok {
		return
	}
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	list, err := repo.ListObjectTypes(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewNotFound("ListObjectTypesFailed", nil))
		return
	}
	if list == nil {
		list = []ObjectType{}
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": list})
}

// GetObjectType handles GET /api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}.
func (h *OMSHandler) GetObjectType(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r)
	if !ok {
		return
	}
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	apiName := chi.URLParam(r, "objectTypeApiName")

	ot, err := repo.GetObjectTypeByAPIName(r.Context(), ontologyRID, apiName)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ObjectTypeNotFound", map[string]string{
				"ontologyApiName":  ontologyRID,
				"objectTypeApiName": apiName,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewNotFound("GetObjectTypeFailed", nil))
		return
	}

	wireData, err := ot.ToWireJSON()
	if err != nil {
		apierror.WriteJSON(w, apierror.NewNotFound("SerializationFailed", nil))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(wireData)
}

// GetObjectTypeResolved handles
// GET /api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/resolved.
// US-212: returns the ObjectType with parent properties + outgoing links
// merged in (child entries override matching api_name). Surfaces a 400 for
// inheritance cycles so admin tooling can flag the misconfiguration.
func (h *OMSHandler) GetObjectTypeResolved(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r)
	if !ok {
		return
	}
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	apiName := chi.URLParam(r, "objectTypeApiName")

	ot, err := repo.GetObjectTypeByAPIName(r.Context(), ontologyRID, apiName)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ObjectTypeNotFound", map[string]string{
				"ontologyApiName":   ontologyRID,
				"objectTypeApiName": apiName,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewNotFound("GetObjectTypeFailed", nil))
		return
	}

	resolved, err := ResolveInheritedObjectType(r.Context(), repo, ot)
	if err != nil {
		if errors.Is(err, ErrInheritanceCycle) {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:extendsRid", map[string]string{
				"parameter":         "extendsRid",
				"objectTypeApiName": apiName,
				"reason":            "inheritance chain forms a cycle",
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ResolveInheritanceFailed", nil))
		return
	}

	wire := map[string]interface{}{
		"apiName":     ot.APIName,
		"displayName": ot.DisplayName,
		"status":      ot.Status,
		"primaryKey":  ot.PrimaryKey,
		"rid":         ot.RID,
		"visibility":  ot.Visibility,
	}
	if pks := ot.EffectivePrimaryKeys(); len(pks) > 0 {
		wire["primaryKeys"] = pks
	}
	if ot.PluralDisplayName != "" {
		wire["pluralDisplayName"] = ot.PluralDisplayName
	}
	if ot.Description != "" {
		wire["description"] = ot.Description
	}
	if ot.TitleProperty != "" {
		wire["titleProperty"] = ot.TitleProperty
	}
	if ot.ExtendsRID != "" {
		wire["extendsRid"] = ot.ExtendsRID
	}
	if len(resolved.ExtendsChain) > 0 {
		wire["extendsChain"] = resolved.ExtendsChain
	}

	props := make(map[string]interface{}, len(resolved.Properties))
	for _, p := range resolved.Properties {
		entry := map[string]interface{}{
			"dataType": p.DataTypeJSON(),
			"rid":      p.RID,
		}
		if p.ObjectTypeRID != "" && p.ObjectTypeRID != ot.RID {
			entry["inheritedFrom"] = p.ObjectTypeRID
		}
		if p.DisplayName != "" {
			entry["displayName"] = p.DisplayName
		}
		if p.Description != "" {
			entry["description"] = p.Description
		}
		props[p.APIName] = entry
	}
	wire["properties"] = props

	links := make([]map[string]interface{}, 0, len(resolved.OutgoingLinkTypes))
	for _, lt := range resolved.OutgoingLinkTypes {
		entry := map[string]interface{}{
			"apiName":                 lt.APIName,
			"displayName":             lt.DisplayName,
			"rid":                     lt.RID,
			"objectTypeApiName":       lt.SourceObjectType,
			"linkedObjectTypeApiName": lt.TargetObjectType,
			"cardinality":             lt.Cardinality,
			"required":                lt.IsRequired,
		}
		if lt.Description != "" {
			entry["description"] = lt.Description
		}
		if lt.SourceObjectType != "" && lt.SourceObjectType != ot.RID {
			entry["inheritedFrom"] = lt.SourceObjectType
		}
		links = append(links, entry)
	}
	wire["outgoingLinkTypes"] = links

	httputil.WriteJSON(w, http.StatusOK, wire)
}

// ListOutgoingLinkTypes handles GET /api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/outgoingLinkTypes.
func (h *OMSHandler) ListOutgoingLinkTypes(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r)
	if !ok {
		return
	}
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	apiName := chi.URLParam(r, "objectTypeApiName")

	// Resolve apiName to objectType RID
	ot, err := repo.GetObjectTypeByAPIName(r.Context(), ontologyRID, apiName)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ObjectTypeNotFound", map[string]string{
				"objectTypeApiName": apiName,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewNotFound("GetObjectTypeFailed", nil))
		return
	}

	list, err := repo.ListOutgoingLinkTypes(r.Context(), ot.RID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewNotFound("ListOutgoingLinkTypesFailed", nil))
		return
	}

	// Build wire JSON for each link type
	wireList := make([]json.RawMessage, 0, len(list))
	for i := range list {
		data, err := list[i].ToWireJSON()
		if err != nil {
			apierror.WriteJSON(w, apierror.NewNotFound("SerializationFailed", nil))
			return
		}
		wireList = append(wireList, json.RawMessage(data))
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": wireList})
}

// ListActionTypes handles GET /api/v2/ontologies/{ontologyApiName}/actionTypes.
func (h *OMSHandler) ListActionTypes(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r)
	if !ok {
		return
	}
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	list, err := repo.ListActionTypes(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewNotFound("ListActionTypesFailed", nil))
		return
	}

	wireList := make([]json.RawMessage, 0, len(list))
	for i := range list {
		data, err := list[i].ToWireJSON()
		if err != nil {
			apierror.WriteJSON(w, apierror.NewNotFound("SerializationFailed", nil))
			return
		}
		wireList = append(wireList, json.RawMessage(data))
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": wireList})
}

// GetFullMetadata handles GET /api/v2/ontologies/{ontologyApiName}/fullMetadata.
// It returns the complete ontology metadata in a single response.
func (h *OMSHandler) GetFullMetadata(w http.ResponseWriter, r *http.Request) {
	if !requirePreview(w, r) {
		return
	}
	repo, ok := h.resolveRepo(w, r)
	if !ok {
		return
	}
	ontologyRID := chi.URLParam(r, "ontologyApiName")

	ontology, err := h.repo.GetOntology(r.Context(), ontologyRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{
				"ontologyApiName": ontologyRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetOntologyFailed", nil))
		return
	}

	objectTypes, err := repo.ListObjectTypes(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListObjectTypesFailed", nil))
		return
	}
	for i := range objectTypes {
		props, err := repo.ListProperties(r.Context(), objectTypes[i].RID)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("ListPropertiesFailed", nil))
			return
		}
		objectTypes[i].Properties = props
	}

	linkTypes, err := repo.ListLinkTypes(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListLinkTypesFailed", nil))
		return
	}

	actionTypes, err := repo.ListActionTypes(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListActionTypesFailed", nil))
		return
	}

	interfaces, err := repo.ListInterfaces(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListInterfacesFailed", nil))
		return
	}

	if objectTypes == nil {
		objectTypes = []ObjectType{}
	}
	if linkTypes == nil {
		linkTypes = []LinkType{}
	}
	if actionTypes == nil {
		actionTypes = []ActionType{}
	}
	if interfaces == nil {
		interfaces = []Interface{}
	}

	httputil.WriteJSON(w, http.StatusOK, OntologyExport{
		Ontology:    *ontology,
		ObjectTypes: objectTypes,
		LinkTypes:   linkTypes,
		ActionTypes: actionTypes,
		Interfaces:  interfaces,
	})
}

// ExportOntologyV2 handles GET /api/v2/ontologies/{ontologyApiName}/export.
// Returns the complete ontology definition including all entity types.
func (h *OMSHandler) ExportOntologyV2(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")

	ontology, err := h.repo.GetOntology(r.Context(), ontologyRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{
				"ontologyApiName": ontologyRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetOntologyFailed", nil))
		return
	}

	ctx := r.Context()

	objectTypes, err := h.repo.ListObjectTypes(ctx, ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListObjectTypesFailed", nil))
		return
	}
	for i := range objectTypes {
		props, err := h.repo.ListProperties(ctx, objectTypes[i].RID)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("ListPropertiesFailed", nil))
			return
		}
		objectTypes[i].Properties = props
	}

	linkTypes, err := h.repo.ListLinkTypes(ctx, ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListLinkTypesFailed", nil))
		return
	}

	actionTypes, err := h.repo.ListActionTypes(ctx, ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListActionTypesFailed", nil))
		return
	}

	interfaces, err := h.repo.ListInterfaces(ctx, ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListInterfacesFailed", nil))
		return
	}

	sharedProperties, err := h.repo.ListSharedProperties(ctx, ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListSharedPropertiesFailed", nil))
		return
	}

	valueTypes, err := h.repo.ListValueTypes(ctx)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListValueTypesFailed", nil))
		return
	}

	typeGroups, err := h.repo.ListTypeGroups(ctx, ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListTypeGroupsFailed", nil))
		return
	}

	functions, err := h.repo.ListFunctions(ctx, ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListFunctionsFailed", nil))
		return
	}

	queryTypes, err := h.repo.ListQueryTypes(ctx, ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListQueryTypesFailed", nil))
		return
	}

	// Ensure all arrays are non-nil for consistent JSON serialization
	if objectTypes == nil {
		objectTypes = []ObjectType{}
	}
	if linkTypes == nil {
		linkTypes = []LinkType{}
	}
	if actionTypes == nil {
		actionTypes = []ActionType{}
	}
	if interfaces == nil {
		interfaces = []Interface{}
	}
	if sharedProperties == nil {
		sharedProperties = []SharedProperty{}
	}
	if valueTypes == nil {
		valueTypes = []ValueType{}
	}
	if typeGroups == nil {
		typeGroups = []TypeGroup{}
	}
	if functions == nil {
		functions = []Function{}
	}
	if queryTypes == nil {
		queryTypes = []QueryType{}
	}

	httputil.WriteJSON(w, http.StatusOK, OntologyExport{
		Ontology:         *ontology,
		ObjectTypes:      objectTypes,
		LinkTypes:        linkTypes,
		ActionTypes:      actionTypes,
		Interfaces:       interfaces,
		SharedProperties: sharedProperties,
		ValueTypes:       valueTypes,
		TypeGroups:       typeGroups,
		Functions:        functions,
		QueryTypes:       queryTypes,
	})
}

// GetActionType handles GET /api/v2/ontologies/{ontologyApiName}/actionTypes/{actionTypeRid}.
// The {actionTypeRid} path param accepts either an apiName or a full RID.
func (h *OMSHandler) GetActionType(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r)
	if !ok {
		return
	}
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	actionIdentifier := chi.URLParam(r, "actionTypeRid")
	at, err := repo.GetActionTypeByAPIName(r.Context(), ontologyRID, actionIdentifier)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ActionTypeNotFound", map[string]string{
				"actionTypeRid": actionIdentifier,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewNotFound("GetActionTypeFailed", nil))
		return
	}

	wireData, err := at.ToWireJSON()
	if err != nil {
		apierror.WriteJSON(w, apierror.NewNotFound("SerializationFailed", nil))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(wireData)
}
