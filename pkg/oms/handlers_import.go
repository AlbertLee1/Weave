package oms

import (
	"context"
	"errors"
	"net/http"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/rid"
)

// ImportOntologyV2Request is the request body for POST /api/v2/ontologies/import.
type ImportOntologyV2Request struct {
	Mode             string           `json:"mode"` // "merge" or "replace"
	Ontology         Ontology         `json:"ontology"`
	ObjectTypes      []ObjectType     `json:"objectTypes"`
	LinkTypes        []LinkType       `json:"linkTypes"`
	ActionTypes      []ActionType     `json:"actionTypes"`
	Interfaces       []Interface      `json:"interfaces"`
	SharedProperties []SharedProperty `json:"sharedProperties"`
	ValueTypes       []ValueType      `json:"valueTypes"`
	TypeGroups       []TypeGroup      `json:"typeGroups"`
	Functions        []Function       `json:"functions"`
	QueryTypes       []QueryType      `json:"queryTypes"`
}

// ImportResult is the response for a successful import.
type ImportResult struct {
	Ontology Ontology     `json:"ontology"`
	Imported ImportCounts `json:"imported"`
	Message  string       `json:"message"`
}

// ImportCounts tracks how many entities were processed during import.
type ImportCounts struct {
	ObjectTypes      int `json:"objectTypes"`
	Properties       int `json:"properties"`
	LinkTypes        int `json:"linkTypes"`
	ActionTypes      int `json:"actionTypes"`
	Interfaces       int `json:"interfaces"`
	SharedProperties int `json:"sharedProperties"`
	ValueTypes       int `json:"valueTypes"`
	TypeGroups       int `json:"typeGroups"`
	Functions        int `json:"functions"`
	QueryTypes       int `json:"queryTypes"`
}

// ImportOntologyV2 handles POST /api/v2/ontologies/import.
func (h *OMSHandler) ImportOntologyV2(w http.ResponseWriter, r *http.Request) {
	var req ImportOntologyV2Request
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	if req.Mode != "merge" && req.Mode != "replace" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:mode", map[string]string{
			"parameter": "mode",
			"reason":    "mode must be 'merge' or 'replace'",
		}))
		return
	}

	if req.Ontology.APIName == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:apiName", map[string]string{
			"parameter": "ontology.apiName",
			"reason":    "ontology apiName is required",
		}))
		return
	}

	ctx := r.Context()
	counts := ImportCounts{}

	// Resolve or create target ontology
	ontology, isExisting, err := h.resolveImportOntology(ctx, &req)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("OntologyResolutionFailed", nil))
		return
	}

	// For replace mode on existing ontology: delete all entities first.
	// DOG-003: also drop the Bleve index for every prior ObjectType so the
	// recreated index does not inherit stale rows from the previous schema.
	if req.Mode == "replace" && isExisting {
		if priorOTs, err := h.repo.ListObjectTypes(ctx, ontology.RID); err == nil {
			for _, ot := range priorOTs {
				_ = h.dropObjectTypeIndex(ontology.APIName, ot.APIName)
			}
		}
		h.deleteOntologyEntities(ctx, ontology.RID)
	}

	// RID maps for cross-reference remapping
	spRIDMap := make(map[string]string)
	fnRIDMap := make(map[string]string)
	ifaceRIDMap := make(map[string]string)

	// 1. Import SharedProperties (Properties may reference SharedPropertyRID)
	counts.SharedProperties = h.importSharedProperties(ctx, ontology.RID, req.Mode, req.SharedProperties, spRIDMap)

	// 2. Import Functions (ActionTypes/QueryTypes may reference FunctionRID)
	counts.Functions = h.importFunctions(ctx, ontology.RID, req.Mode, req.Functions, fnRIDMap)

	// 3. Import ObjectTypes with Properties
	otCounts, propCounts := h.importObjectTypes(ctx, ontology.RID, req.Mode, req.ObjectTypes, spRIDMap)
	counts.ObjectTypes = otCounts
	counts.Properties = propCounts

	// 4. Import Interfaces (may reference each other via ExtendsRID)
	counts.Interfaces = h.importInterfaces(ctx, ontology.RID, req.Mode, req.Interfaces, ifaceRIDMap)

	// 5. Import LinkTypes (SourceObjectType/TargetObjectType are API names, no remapping needed)
	counts.LinkTypes = h.importLinkTypes(ctx, ontology.RID, req.Mode, req.LinkTypes)

	// 6. Import ActionTypes (remap FunctionRID)
	counts.ActionTypes = h.importActionTypes(ctx, ontology.RID, req.Mode, req.ActionTypes, fnRIDMap)

	// 7. Import QueryTypes (remap FunctionRID)
	counts.QueryTypes = h.importQueryTypes(ctx, ontology.RID, req.Mode, req.QueryTypes, fnRIDMap)

	// 8. Import TypeGroups
	counts.TypeGroups = h.importTypeGroups(ctx, ontology.RID, req.Mode, req.TypeGroups)

	// 9. Import ValueTypes (global, not ontology-scoped)
	counts.ValueTypes = h.importValueTypes(ctx, req.Mode, req.ValueTypes)

	httputil.WriteJSON(w, http.StatusCreated, ImportResult{
		Ontology: *ontology,
		Imported: counts,
		Message:  "import successful",
	})
}

func (h *OMSHandler) resolveImportOntology(ctx context.Context, req *ImportOntologyV2Request) (*Ontology, bool, error) {
	existing, err := h.repo.GetOntology(ctx, req.Ontology.APIName)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}
	if existing != nil {
		if req.Ontology.DisplayName != "" {
			existing.DisplayName = req.Ontology.DisplayName
		}
		if req.Ontology.Description != "" {
			existing.Description = req.Ontology.Description
		}
		_ = h.repo.UpdateOntology(ctx, existing)
		return existing, true, nil
	}

	ontology := &Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     req.Ontology.APIName,
		DisplayName: req.Ontology.DisplayName,
		Description: req.Ontology.Description,
	}
	if err := h.repo.CreateOntology(ctx, ontology); err != nil {
		return nil, false, err
	}
	return ontology, false, nil
}

func (h *OMSHandler) deleteOntologyEntities(ctx context.Context, ontologyRID string) {
	// Delete in reverse dependency order

	if qts, err := h.repo.ListQueryTypes(ctx, ontologyRID); err == nil {
		for _, qt := range qts {
			_ = h.repo.DeleteQueryType(ctx, qt.RID)
		}
	}
	if ats, err := h.repo.ListActionTypes(ctx, ontologyRID); err == nil {
		for _, at := range ats {
			_ = h.repo.DeleteActionType(ctx, at.RID)
		}
	}
	if lts, err := h.repo.ListLinkTypes(ctx, ontologyRID); err == nil {
		for _, lt := range lts {
			_ = h.repo.DeleteLinkType(ctx, lt.RID)
		}
	}
	if ifaces, err := h.repo.ListInterfaces(ctx, ontologyRID); err == nil {
		for _, iface := range ifaces {
			_ = h.repo.DeleteInterface(ctx, iface.RID)
		}
	}
	if ots, err := h.repo.ListObjectTypes(ctx, ontologyRID); err == nil {
		for _, ot := range ots {
			if props, err := h.repo.ListProperties(ctx, ot.RID); err == nil {
				for _, p := range props {
					_ = h.repo.DeleteProperty(ctx, p.RID)
				}
			}
			_ = h.repo.DeleteObjectType(ctx, ot.RID)
		}
	}
	if sps, err := h.repo.ListSharedProperties(ctx, ontologyRID); err == nil {
		for _, sp := range sps {
			_ = h.repo.DeleteSharedProperty(ctx, sp.RID)
		}
	}
	if fns, err := h.repo.ListFunctions(ctx, ontologyRID); err == nil {
		for _, fn := range fns {
			_ = h.repo.DeleteFunction(ctx, fn.RID)
		}
	}
	if tgs, err := h.repo.ListTypeGroups(ctx, ontologyRID); err == nil {
		for _, tg := range tgs {
			_ = h.repo.DeleteTypeGroup(ctx, tg.RID)
		}
	}
}

func (h *OMSHandler) importSharedProperties(ctx context.Context, ontologyRID, mode string, sps []SharedProperty, ridMap map[string]string) int {
	count := 0
	for _, sp := range sps {
		oldRID := sp.RID
		if mode == "merge" {
			if existing, err := h.repo.ListSharedProperties(ctx, ontologyRID); err == nil {
				if found := findSharedPropertyByAPIName(existing, sp.APIName); found != nil {
					ridMap[oldRID] = found.RID
					count++
					continue
				}
			}
		}
		newSP := &SharedProperty{
			RID: rid.NewSharedPropertyRID(), OntologyRID: ontologyRID,
			APIName: sp.APIName, DisplayName: sp.DisplayName, Description: sp.Description,
			BaseType: sp.BaseType, TypeConfig: sp.TypeConfig, IsArray: sp.IsArray,
		}
		if err := h.repo.CreateSharedProperty(ctx, newSP); err != nil {
			continue
		}
		ridMap[oldRID] = newSP.RID
		count++
	}
	return count
}

func (h *OMSHandler) importFunctions(ctx context.Context, ontologyRID, mode string, fns []Function, ridMap map[string]string) int {
	count := 0
	for _, fn := range fns {
		oldRID := fn.RID
		if mode == "merge" {
			if found, err := h.repo.GetFunctionByName(ctx, ontologyRID, fn.Name); err == nil {
				ridMap[oldRID] = found.RID
				count++
				continue
			}
		}
		newFn := &Function{
			RID: rid.NewFunctionRID(), OntologyRID: ontologyRID,
			Name: fn.Name, Version: fn.NormalisedVersion(), SourceCode: fn.SourceCode, CreatedBy: fn.CreatedBy,
		}
		if err := h.repo.CreateFunction(ctx, newFn); err != nil {
			continue
		}
		ridMap[oldRID] = newFn.RID
		count++
	}
	return count
}

func (h *OMSHandler) importObjectTypes(ctx context.Context, ontologyRID, mode string, ots []ObjectType, spRIDMap map[string]string) (int, int) {
	otCount, propCount := 0, 0
	// DOG-003: ImportOntologyV2 hands us an OntologyRID but the
	// IndexBootstrapper hook lives at the apiName layer (the Bleve scoped
	// key is "{ontologyApiName}__{objectTypeApiName}"). Resolve once up
	// front so each ensure call below stays O(1).
	ontologyAPIName := ""
	if h.indexBootstrapper != nil {
		if ont, err := h.repo.GetOntology(ctx, ontologyRID); err == nil {
			ontologyAPIName = ont.APIName
		}
	}
	for _, ot := range ots {
		var targetOTRID string

		if mode == "merge" {
			if found, err := h.repo.GetObjectTypeByAPIName(ctx, ontologyRID, ot.APIName); err == nil {
				// Update existing ObjectType
				found.DisplayName = ot.DisplayName
				found.PluralDisplayName = ot.PluralDisplayName
				found.Description = ot.Description
				found.PrimaryKey = ot.PrimaryKey
				found.TitleProperty = ot.TitleProperty
				if ot.Status != "" {
					found.Status = ot.Status
				}
				if ot.Visibility != "" {
					found.Visibility = ot.Visibility
				}
				found.IconName = ot.IconName
				found.Color = ot.Color
				_ = h.repo.UpdateObjectType(ctx, found)
				targetOTRID = found.RID
				otCount++

				// Upsert properties for existing ObjectType
				propCount += h.importProperties(ctx, targetOTRID, mode, ot.Properties, spRIDMap)
				// DOG-003: refresh the index shell so an updated property
				// schema (e.g. new searchable field) is reflected before any
				// subsequent ingest. The bootstrapper is a no-op when nil.
				if ontologyAPIName != "" {
					if latestProps, err := h.repo.ListProperties(ctx, targetOTRID); err == nil {
						_ = h.ensureObjectTypeIndex(ontologyAPIName, ot.APIName, latestProps)
					}
				}
				continue
			}
		}

		newOT := &ObjectType{
			RID: rid.NewObjectTypeRID(), OntologyRID: ontologyRID,
			APIName: ot.APIName, DisplayName: ot.DisplayName, PluralDisplayName: ot.PluralDisplayName,
			Description: ot.Description, PrimaryKey: ot.PrimaryKey, TitleProperty: ot.TitleProperty,
			Status: ot.Status, Visibility: ot.Visibility, IconName: ot.IconName, Color: ot.Color,
		}
		if newOT.Status == "" {
			newOT.Status = "ACTIVE"
		}
		if newOT.Visibility == "" {
			newOT.Visibility = "NORMAL"
		}
		if err := h.repo.CreateObjectType(ctx, newOT); err != nil {
			continue
		}
		targetOTRID = newOT.RID
		otCount++

		// Create properties for new ObjectType
		for _, p := range ot.Properties {
			newProp := &Property{
				RID: rid.NewPropertyRID(), ObjectTypeRID: targetOTRID,
				APIName: p.APIName, DisplayName: p.DisplayName, Description: p.Description,
				BaseType: p.BaseType, TypeConfig: p.TypeConfig,
				IsArray: p.IsArray, IsNullable: p.IsNullable, IsSearchable: p.IsSearchable, IsSortable: p.IsSortable,
				Status: p.Status, SharedPropertyRID: remapRID(spRIDMap, p.SharedPropertyRID),
			}
			if newProp.Status == "" {
				newProp.Status = "ACTIVE"
			}
			if err := h.repo.CreateProperty(ctx, newProp); err != nil {
				continue
			}
			propCount++
		}

		// DOG-003: bootstrap the Bleve index shell synchronously so the
		// follow-up stream ingest does not race against a missing index.
		// Re-list properties so we pass the persisted, schema-faithful set
		// (with new RIDs) instead of the request payload.
		if ontologyAPIName != "" {
			if persisted, err := h.repo.ListProperties(ctx, targetOTRID); err == nil {
				_ = h.ensureObjectTypeIndex(ontologyAPIName, ot.APIName, persisted)
			}
		}
	}
	return otCount, propCount
}

func (h *OMSHandler) importProperties(ctx context.Context, objectTypeRID, mode string, props []Property, spRIDMap map[string]string) int {
	count := 0
	existingProps, _ := h.repo.ListProperties(ctx, objectTypeRID)
	existingMap := make(map[string]*Property, len(existingProps))
	for i := range existingProps {
		existingMap[existingProps[i].APIName] = &existingProps[i]
	}

	for _, p := range props {
		if existing, ok := existingMap[p.APIName]; ok {
			// Update existing property
			existing.DisplayName = p.DisplayName
			existing.Description = p.Description
			existing.BaseType = p.BaseType
			existing.TypeConfig = p.TypeConfig
			existing.IsArray = p.IsArray
			existing.IsNullable = p.IsNullable
			existing.IsSearchable = p.IsSearchable
			existing.IsSortable = p.IsSortable
			existing.SharedPropertyRID = remapRID(spRIDMap, p.SharedPropertyRID)
			_ = h.repo.UpdateProperty(ctx, existing)
			count++
		} else {
			// Create new property
			newProp := &Property{
				RID: rid.NewPropertyRID(), ObjectTypeRID: objectTypeRID,
				APIName: p.APIName, DisplayName: p.DisplayName, Description: p.Description,
				BaseType: p.BaseType, TypeConfig: p.TypeConfig,
				IsArray: p.IsArray, IsNullable: p.IsNullable, IsSearchable: p.IsSearchable, IsSortable: p.IsSortable,
				Status: p.Status, SharedPropertyRID: remapRID(spRIDMap, p.SharedPropertyRID),
			}
			if newProp.Status == "" {
				newProp.Status = "ACTIVE"
			}
			if err := h.repo.CreateProperty(ctx, newProp); err != nil {
				continue
			}
			count++
		}
	}
	return count
}

func (h *OMSHandler) importInterfaces(ctx context.Context, ontologyRID, mode string, ifaces []Interface, ridMap map[string]string) int {
	count := 0
	for _, iface := range ifaces {
		oldRID := iface.RID
		if mode == "merge" {
			if found, err := h.repo.GetInterfaceByAPIName(ctx, ontologyRID, iface.APIName); err == nil {
				ridMap[oldRID] = found.RID
				count++
				continue
			}
		}
		newIface := &Interface{
			RID: rid.NewInterfaceRID(), OntologyRID: ontologyRID,
			APIName: iface.APIName, DisplayName: iface.DisplayName,
			ExtendsRID:       remapRID(ridMap, iface.ExtendsRID),
			SharedProperties: iface.SharedProperties, OutgoingLinkTypes: iface.OutgoingLinkTypes,
		}
		if err := h.repo.CreateInterface(ctx, newIface); err != nil {
			continue
		}
		ridMap[oldRID] = newIface.RID
		count++
	}
	return count
}

func (h *OMSHandler) importLinkTypes(ctx context.Context, ontologyRID, mode string, lts []LinkType) int {
	count := 0
	for _, lt := range lts {
		if mode == "merge" {
			if _, err := h.repo.GetLinkTypeByAPIName(ctx, ontologyRID, lt.APIName); err == nil {
				count++
				continue
			}
		}
		newLT := &LinkType{
			RID: rid.NewLinkTypeRID(), OntologyRID: ontologyRID,
			APIName: lt.APIName, DisplayName: lt.DisplayName, Description: lt.Description,
			SourceObjectType: lt.SourceObjectType, TargetObjectType: lt.TargetObjectType,
			Cardinality: lt.Cardinality, ForeignKeyConfig: lt.ForeignKeyConfig,
			JoinTableConfig: lt.JoinTableConfig, IsRequired: lt.IsRequired,
		}
		if err := h.repo.CreateLinkType(ctx, newLT); err != nil {
			continue
		}
		count++
	}
	return count
}

func (h *OMSHandler) importActionTypes(ctx context.Context, ontologyRID, mode string, ats []ActionType, fnRIDMap map[string]string) int {
	count := 0
	for _, at := range ats {
		if mode == "merge" {
			if _, err := h.repo.GetActionTypeByAPIName(ctx, ontologyRID, at.APIName); err == nil {
				count++
				continue
			}
		}
		newAT := &ActionType{
			RID: rid.NewActionTypeRID(), OntologyRID: ontologyRID,
			APIName: at.APIName, DisplayName: at.DisplayName, Description: at.Description,
			Status: at.Status, Parameters: at.Parameters, Rules: at.Rules,
			SubmissionCriteria: at.SubmissionCriteria, SideEffects: at.SideEffects,
			FunctionRID: remapRID(fnRIDMap, at.FunctionRID), IsFunctionBacked: at.IsFunctionBacked,
			FunctionVersion: at.FunctionVersion,
		}
		if newAT.Status == "" {
			newAT.Status = "ACTIVE"
		}
		if err := h.repo.CreateActionType(ctx, newAT); err != nil {
			continue
		}
		count++
	}
	return count
}

func (h *OMSHandler) importQueryTypes(ctx context.Context, ontologyRID, mode string, qts []QueryType, fnRIDMap map[string]string) int {
	count := 0
	for _, qt := range qts {
		if mode == "merge" {
			if _, err := h.repo.GetQueryTypeByAPIName(ctx, ontologyRID, qt.APIName); err == nil {
				count++
				continue
			}
		}
		newQT := &QueryType{
			RID: rid.NewQueryTypeRID(), OntologyRID: ontologyRID,
			APIName: qt.APIName, DisplayName: qt.DisplayName, Description: qt.Description,
			Parameters: qt.Parameters, Output: qt.Output, Query: qt.Query,
			FunctionRID: remapRID(fnRIDMap, qt.FunctionRID), Status: qt.Status,
		}
		if err := h.repo.CreateQueryType(ctx, newQT); err != nil {
			continue
		}
		count++
	}
	return count
}

func (h *OMSHandler) importTypeGroups(ctx context.Context, ontologyRID, mode string, tgs []TypeGroup) int {
	count := 0
	for _, tg := range tgs {
		if mode == "merge" {
			if existing, err := h.repo.ListTypeGroups(ctx, ontologyRID); err == nil {
				if found := findTypeGroupByAPIName(existing, tg.APIName); found != nil {
					count++
					continue
				}
			}
		}
		newTG := &TypeGroup{
			RID: rid.NewTypeGroupRID(), OntologyRID: ontologyRID,
			APIName: tg.APIName, DisplayName: tg.DisplayName, Description: tg.Description,
			Color: tg.Color,
		}
		if err := h.repo.CreateTypeGroup(ctx, newTG); err != nil {
			continue
		}
		count++
	}
	return count
}

func (h *OMSHandler) importValueTypes(ctx context.Context, mode string, vts []ValueType) int {
	count := 0
	for _, vt := range vts {
		if mode == "merge" {
			if _, err := h.repo.GetValueTypeByAPIName(ctx, vt.APIName); err == nil {
				count++
				continue
			}
		}
		newVT := &ValueType{
			RID: rid.NewValueTypeRID(),
			APIName: vt.APIName, DisplayName: vt.DisplayName,
			BaseType: vt.BaseType, Constraints: vt.Constraints, Version: vt.Version,
		}
		if err := h.repo.CreateValueType(ctx, newVT); err != nil {
			continue
		}
		count++
	}
	return count
}

// remapRID looks up oldRID in the map and returns the new RID, or the original if not found.
func remapRID(ridMap map[string]string, oldRID string) string {
	if oldRID == "" {
		return ""
	}
	if newRID, ok := ridMap[oldRID]; ok {
		return newRID
	}
	return oldRID
}

func findSharedPropertyByAPIName(sps []SharedProperty, apiName string) *SharedProperty {
	for i := range sps {
		if sps[i].APIName == apiName {
			return &sps[i]
		}
	}
	return nil
}

func findTypeGroupByAPIName(tgs []TypeGroup, apiName string) *TypeGroup {
	for i := range tgs {
		if tgs[i].APIName == apiName {
			return &tgs[i]
		}
	}
	return nil
}
