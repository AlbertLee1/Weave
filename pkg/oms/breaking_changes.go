package oms

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

// Breaking change kinds. Stable identifiers for the wire payload — clients
// dispatch on these to render risk callouts in the proposal review UI.
const (
	BreakingChangeKindPropertyDeleted       = "PROPERTY_DELETED"
	BreakingChangeKindPropertyTypeNarrowed  = "PROPERTY_TYPE_NARROWED"
	BreakingChangeKindPropertyRequiredAdded = "PROPERTY_REQUIRED_ADDED"
	BreakingChangeKindPrimaryKeyChanged     = "PRIMARY_KEY_CHANGED"
)

// BreakingChange describes a single risk that the branch overlay introduces
// against the main ontology. The payload is purely advisory — the merge path
// is unaffected.
type BreakingChange struct {
	Kind                    string   `json:"kind"`
	ObjectTypeRID           string   `json:"objectTypeRid,omitempty"`
	ObjectTypeAPIName       string   `json:"objectTypeApiName,omitempty"`
	PropertyAPIName         string   `json:"propertyApiName,omitempty"`
	Detail                  string   `json:"detail,omitempty"`
	AffectedActionTypes     []string `json:"affectedActionTypes,omitempty"`
	AffectedSavedObjectSets []string `json:"affectedSavedObjectSets,omitempty"`
}

// BreakingChangesReport is the response payload of the detector endpoint.
type BreakingChangesReport struct {
	BranchID string           `json:"branchId"`
	Changes  []BreakingChange `json:"changes"`
}

// SavedObjectSetRef is the minimal projection of a saved object set the
// detector needs: stable ID + raw definition tree. Adapters around the real
// SavedStore (oss/objectset.PGSavedStore) translate to this type so the OMS
// package stays free of an oss import.
type SavedObjectSetRef struct {
	ID         string
	Definition json.RawMessage
}

// SavedObjectSetLister returns the saved object sets visible under an
// ontology so the detector can scan their definitions for property/object
// references. Implementations may return nil/empty in degraded modes; the
// detector simply omits saved-set impacts when the lister itself is nil.
type SavedObjectSetLister interface {
	ListSavedObjectSets(ctx context.Context, ontologyAPIName string) ([]SavedObjectSetRef, error)
}

// DetectBreakingChanges compares a branch overlay against main and returns
// the BreakingChangesReport. ontologyRID identifies the ontology whose main
// state we read; ontologyAPIName is the apiName used for SavedObjectSet
// lookups. The ontologyAPIName parameter may be empty when no lister is
// supplied. branchID identifies which branch to inspect.
func DetectBreakingChanges(
	ctx context.Context,
	repo Repository,
	savedSets SavedObjectSetLister,
	ontologyRID, ontologyAPIName, branchID string,
) (BreakingChangesReport, error) {
	report := BreakingChangesReport{BranchID: branchID, Changes: []BreakingChange{}}

	mainObjectTypes, err := repo.ListObjectTypes(ctx, ontologyRID)
	if err != nil {
		return report, fmt.Errorf("list object types: %w", err)
	}

	allChanges, err := repo.ListBranchChanges(ctx, branchID)
	if err != nil {
		return report, fmt.Errorf("list branch changes: %w", err)
	}

	objectTypeChanges := indexChanges(allChanges, "objectType")
	propertyChanges := indexChanges(allChanges, "property")

	for _, ot := range mainObjectTypes {
		report.Changes = append(report.Changes, detectObjectTypeChanges(ot, objectTypeChanges[ot.RID])...)

		props, err := repo.ListProperties(ctx, ot.RID)
		if err != nil {
			return report, fmt.Errorf("list properties for %s: %w", ot.RID, err)
		}
		for _, p := range props {
			report.Changes = append(report.Changes, detectPropertyChanges(ot, p, propertyChanges[p.RID])...)
		}
	}

	if len(report.Changes) == 0 {
		return report, nil
	}

	actionTypes, err := repo.ListActionTypes(ctx, ontologyRID)
	if err != nil {
		return report, fmt.Errorf("list action types: %w", err)
	}

	var savedRefs []SavedObjectSetRef
	if savedSets != nil {
		savedRefs, err = savedSets.ListSavedObjectSets(ctx, ontologyAPIName)
		if err != nil {
			return report, fmt.Errorf("list saved object sets: %w", err)
		}
	}

	for i := range report.Changes {
		report.Changes[i].AffectedActionTypes = sortedUnique(findAffectedActionTypes(report.Changes[i], actionTypes))
		report.Changes[i].AffectedSavedObjectSets = sortedUnique(findAffectedSavedSets(report.Changes[i], savedRefs))
	}

	return report, nil
}

// indexChanges groups branch changes of the given EntityType by EntityRID.
// The same RID can carry multiple changes (e.g. ADDED then MODIFIED on a
// branch); we keep them all so callers can inspect the latest state.
func indexChanges(all []BranchChange, entityType string) map[string][]BranchChange {
	out := map[string][]BranchChange{}
	for _, c := range all {
		if c.EntityType != entityType {
			continue
		}
		out[c.EntityRID] = append(out[c.EntityRID], c)
	}
	return out
}

func detectObjectTypeChanges(main ObjectType, changes []BranchChange) []BreakingChange {
	var out []BreakingChange
	for _, c := range changes {
		if c.ChangeType != "MODIFIED" {
			continue
		}
		if len(c.AfterState) == 0 {
			continue
		}
		var after ObjectType
		if err := json.Unmarshal(c.AfterState, &after); err != nil {
			continue
		}
		mainPKs := main.EffectivePrimaryKeys()
		afterPKs := after.EffectivePrimaryKeys()
		if !equalStringSlices(mainPKs, afterPKs) {
			out = append(out, BreakingChange{
				Kind:              BreakingChangeKindPrimaryKeyChanged,
				ObjectTypeRID:     main.RID,
				ObjectTypeAPIName: main.APIName,
				Detail:            fmt.Sprintf("primary key changed from %v to %v", mainPKs, afterPKs),
			})
		}
	}
	return out
}

func detectPropertyChanges(ot ObjectType, main Property, changes []BranchChange) []BreakingChange {
	var out []BreakingChange
	for _, c := range changes {
		switch c.ChangeType {
		case "DELETED":
			out = append(out, BreakingChange{
				Kind:              BreakingChangeKindPropertyDeleted,
				ObjectTypeRID:     ot.RID,
				ObjectTypeAPIName: ot.APIName,
				PropertyAPIName:   main.APIName,
				Detail:            fmt.Sprintf("property %q deleted from %q", main.APIName, ot.APIName),
			})
		case "MODIFIED":
			if len(c.AfterState) == 0 {
				continue
			}
			var after Property
			if err := json.Unmarshal(c.AfterState, &after); err != nil {
				continue
			}
			if main.BaseType != after.BaseType || main.IsArray != after.IsArray {
				out = append(out, BreakingChange{
					Kind:              BreakingChangeKindPropertyTypeNarrowed,
					ObjectTypeRID:     ot.RID,
					ObjectTypeAPIName: ot.APIName,
					PropertyAPIName:   main.APIName,
					Detail: fmt.Sprintf("type changed from %s (array=%v) to %s (array=%v)",
						main.BaseType, main.IsArray, after.BaseType, after.IsArray),
				})
			}
			if main.IsNullable && !after.IsNullable {
				out = append(out, BreakingChange{
					Kind:              BreakingChangeKindPropertyRequiredAdded,
					ObjectTypeRID:     ot.RID,
					ObjectTypeAPIName: ot.APIName,
					PropertyAPIName:   main.APIName,
					Detail:            "property became required (nullable→not nullable)",
				})
			}
		}
	}
	return out
}

func findAffectedActionTypes(change BreakingChange, actionTypes []ActionType) []string {
	var hits []string
	for _, at := range actionTypes {
		if actionTypeReferences(at, change) {
			hits = append(hits, at.RID)
		}
	}
	return hits
}

// actionRule mirrors the persistence shape of a single action rule. Kept
// local to the detector so we don't pull in a dependency on pkg/actions
// (which would cycle through pkg/oms via Repository).
type actionRule struct {
	Type             string                            `json:"type"`
	ObjectType       string                            `json:"objectType"`
	PropertyBindings map[string]map[string]interface{} `json:"propertyBindings"`
}

func actionTypeReferences(at ActionType, change BreakingChange) bool {
	if change.ObjectTypeAPIName == "" {
		return false
	}
	rules := parseActionRules(at.Rules)
	for _, r := range rules {
		if r.ObjectType != change.ObjectTypeAPIName {
			continue
		}
		if change.Kind == BreakingChangeKindPrimaryKeyChanged {
			return true
		}
		if change.PropertyAPIName == "" {
			continue
		}
		if _, ok := r.PropertyBindings[change.PropertyAPIName]; ok {
			return true
		}
	}
	return false
}

func parseActionRules(raw json.RawMessage) []actionRule {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var rules []actionRule
	if err := json.Unmarshal(raw, &rules); err != nil {
		return nil
	}
	return rules
}

func findAffectedSavedSets(change BreakingChange, sets []SavedObjectSetRef) []string {
	var hits []string
	for _, sos := range sets {
		if savedSetReferences(sos, change) {
			hits = append(hits, sos.ID)
		}
	}
	return hits
}

func savedSetReferences(sos SavedObjectSetRef, change BreakingChange) bool {
	if len(sos.Definition) == 0 {
		return false
	}
	var node map[string]interface{}
	if err := json.Unmarshal(sos.Definition, &node); err != nil {
		return false
	}
	return walkSavedSetNode(node, change)
}

func walkSavedSetNode(node map[string]interface{}, change BreakingChange) bool {
	if node == nil {
		return false
	}
	if ot, ok := node["objectType"].(string); ok && ot == change.ObjectTypeAPIName {
		// Any reference to the affected ObjectType makes a PK change relevant.
		if change.Kind == BreakingChangeKindPrimaryKeyChanged {
			return true
		}
	}
	if change.PropertyAPIName != "" {
		if props, ok := node["properties"].([]interface{}); ok {
			for _, p := range props {
				if s, ok := p.(string); ok && s == change.PropertyAPIName {
					return true
				}
			}
		}
		if dps, ok := node["derivedProperties"].([]interface{}); ok {
			for _, dp := range dps {
				if m, ok := dp.(map[string]interface{}); ok {
					if f, ok := m["field"].(string); ok && f == change.PropertyAPIName {
						return true
					}
				}
			}
		}
		if where, ok := node["where"].(map[string]interface{}); ok {
			if walkWhereNode(where, change.PropertyAPIName) {
				return true
			}
		}
	}
	if inner, ok := node["objectSet"].(map[string]interface{}); ok {
		if walkSavedSetNode(inner, change) {
			return true
		}
	}
	if list, ok := node["objectSets"].([]interface{}); ok {
		for _, item := range list {
			if m, ok := item.(map[string]interface{}); ok {
				if walkSavedSetNode(m, change) {
					return true
				}
			}
		}
	}
	return false
}

func walkWhereNode(node map[string]interface{}, propAPIName string) bool {
	if node == nil {
		return false
	}
	if f, ok := node["field"].(string); ok && f == propAPIName {
		return true
	}
	switch node["type"] {
	case "and", "or":
		if vs, ok := node["value"].([]interface{}); ok {
			for _, v := range vs {
				if m, ok := v.(map[string]interface{}); ok {
					if walkWhereNode(m, propAPIName) {
						return true
					}
				}
			}
		}
	case "not":
		if v, ok := node["value"].(map[string]interface{}); ok {
			if walkWhereNode(v, propAPIName) {
				return true
			}
		}
	}
	return false
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sortedUnique(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
