package oms

import (
	"context"
	"encoding/json"
)

// BranchedRepository is a decorator around a base Repository that applies
// branch overlay changes to read operations. ADDED entities are appended,
// MODIFIED entities are replaced, DELETED entities are removed.
type BranchedRepository struct {
	Repository           // embedded base for delegation of all non-overridden methods
	branchID   string
}

// NewBranchedRepository wraps base with branch overlay read behaviour.
func NewBranchedRepository(base Repository, branchID string) *BranchedRepository {
	return &BranchedRepository{Repository: base, branchID: branchID}
}

// --- helper: load branch changes filtered by entity type ---

func (br *BranchedRepository) changesForType(ctx context.Context, entityType string) ([]BranchChange, error) {
	all, err := br.Repository.ListBranchChanges(ctx, br.branchID)
	if err != nil {
		return nil, err
	}
	var filtered []BranchChange
	for _, c := range all {
		if c.EntityType == entityType {
			filtered = append(filtered, c)
		}
	}
	return filtered, nil
}

// --- ObjectType overlay ---

func (br *BranchedRepository) ListObjectTypes(ctx context.Context, ontologyRID string) ([]ObjectType, error) {
	mainList, err := br.Repository.ListObjectTypes(ctx, ontologyRID)
	if err != nil {
		return nil, err
	}
	changes, err := br.changesForType(ctx, "objectType")
	if err != nil {
		return nil, err
	}
	return applyListOverlay(mainList, changes, func(ot ObjectType) string { return ot.RID }, unmarshalObjectType)
}

func (br *BranchedRepository) GetObjectTypeByAPIName(ctx context.Context, ontologyRID, apiName string) (*ObjectType, error) {
	mainOT, mainErr := br.Repository.GetObjectTypeByAPIName(ctx, ontologyRID, apiName)
	changes, err := br.changesForType(ctx, "objectType")
	if err != nil {
		return nil, err
	}
	return applyGetOverlay(mainOT, mainErr, changes, apiName,
		func(ot ObjectType) string { return ot.RID },
		func(ot ObjectType) string { return ot.APIName },
		unmarshalObjectType)
}

// --- Property overlay ---

func (br *BranchedRepository) ListProperties(ctx context.Context, objectTypeRID string) ([]Property, error) {
	mainList, err := br.Repository.ListProperties(ctx, objectTypeRID)
	if err != nil {
		return nil, err
	}
	changes, err := br.changesForType(ctx, "property")
	if err != nil {
		return nil, err
	}
	// Note: Property.ObjectTypeRID has json:"-" so it is lost during branch
	// change serialization.  We don't filter ADDED properties by ObjectTypeRID;
	// the admin handler already ensures branch changes are scoped correctly.
	return applyListOverlay(mainList, changes,
		func(p Property) string { return p.RID },
		unmarshalProperty)
}

// --- LinkType overlay ---

func (br *BranchedRepository) ListLinkTypes(ctx context.Context, ontologyRID string) ([]LinkType, error) {
	mainList, err := br.Repository.ListLinkTypes(ctx, ontologyRID)
	if err != nil {
		return nil, err
	}
	changes, err := br.changesForType(ctx, "linkType")
	if err != nil {
		return nil, err
	}
	return applyListOverlay(mainList, changes, func(lt LinkType) string { return lt.RID }, unmarshalLinkType)
}

// --- ActionType overlay ---

func (br *BranchedRepository) ListActionTypes(ctx context.Context, ontologyRID string) ([]ActionType, error) {
	mainList, err := br.Repository.ListActionTypes(ctx, ontologyRID)
	if err != nil {
		return nil, err
	}
	changes, err := br.changesForType(ctx, "actionType")
	if err != nil {
		return nil, err
	}
	return applyListOverlay(mainList, changes, func(at ActionType) string { return at.RID }, unmarshalActionType)
}

func (br *BranchedRepository) GetActionTypeByAPIName(ctx context.Context, ontologyRID, apiNameOrRID string) (*ActionType, error) {
	mainAT, mainErr := br.Repository.GetActionTypeByAPIName(ctx, ontologyRID, apiNameOrRID)
	changes, err := br.changesForType(ctx, "actionType")
	if err != nil {
		return nil, err
	}
	return applyGetOverlay(mainAT, mainErr, changes, apiNameOrRID,
		func(at ActionType) string { return at.RID },
		func(at ActionType) string { return at.APIName },
		unmarshalActionType)
}

// --- generic overlay helpers ---

// applyListOverlay merges branch changes into a main entity list.
func applyListOverlay[T any](
	mainList []T,
	changes []BranchChange,
	getRID func(T) string,
	unmarshal func(json.RawMessage) (T, error),
) ([]T, error) {
	return applyListOverlayFiltered(mainList, changes, getRID, unmarshal, nil)
}

// applyListOverlayFiltered merges branch changes with an optional filter for ADDED entities.
func applyListOverlayFiltered[T any](
	mainList []T,
	changes []BranchChange,
	getRID func(T) string,
	unmarshal func(json.RawMessage) (T, error),
	addedFilter func(T) bool,
) ([]T, error) {
	// Build sets from changes
	deleted := map[string]bool{}
	modified := map[string]json.RawMessage{}
	var addedRaw []json.RawMessage

	for _, c := range changes {
		switch c.ChangeType {
		case "DELETED":
			deleted[c.EntityRID] = true
		case "MODIFIED":
			modified[c.EntityRID] = c.AfterState
		case "ADDED":
			addedRaw = append(addedRaw, c.AfterState)
		}
	}

	// Process main list: skip deleted, replace modified
	var result []T
	for _, entity := range mainList {
		rid := getRID(entity)
		if deleted[rid] {
			continue
		}
		if raw, ok := modified[rid]; ok {
			updated, err := unmarshal(raw)
			if err != nil {
				return nil, err
			}
			result = append(result, updated)
		} else {
			result = append(result, entity)
		}
	}

	// Append added entities
	for _, raw := range addedRaw {
		entity, err := unmarshal(raw)
		if err != nil {
			return nil, err
		}
		if addedFilter != nil && !addedFilter(entity) {
			continue
		}
		result = append(result, entity)
	}

	return result, nil
}

// applyGetOverlay applies branch overlay to a single-entity get operation.
func applyGetOverlay[T any](
	mainEntity *T,
	mainErr error,
	changes []BranchChange,
	apiName string,
	getRID func(T) string,
	getAPIName func(T) string,
	unmarshal func(json.RawMessage) (T, error),
) (*T, error) {
	// Check ADDED changes first (entity may not exist on main)
	for _, c := range changes {
		if c.ChangeType == "ADDED" && c.AfterState != nil {
			entity, err := unmarshal(c.AfterState)
			if err != nil {
				continue
			}
			if getAPIName(entity) == apiName {
				return &entity, nil
			}
		}
	}

	// If main entity not found, nothing more to check
	if mainErr != nil {
		return nil, mainErr
	}

	mainRID := getRID(*mainEntity)

	// Check MODIFIED and DELETED
	for _, c := range changes {
		if c.EntityRID != mainRID {
			continue
		}
		switch c.ChangeType {
		case "MODIFIED":
			if c.AfterState != nil {
				entity, err := unmarshal(c.AfterState)
				if err != nil {
					return nil, err
				}
				return &entity, nil
			}
		case "DELETED":
			return nil, ErrNotFound
		}
	}

	return mainEntity, nil
}

// --- unmarshal helpers ---

func unmarshalObjectType(data json.RawMessage) (ObjectType, error) {
	var ot ObjectType
	err := json.Unmarshal(data, &ot)
	return ot, err
}

func unmarshalProperty(data json.RawMessage) (Property, error) {
	var p Property
	err := json.Unmarshal(data, &p)
	return p, err
}

func unmarshalLinkType(data json.RawMessage) (LinkType, error) {
	var lt LinkType
	err := json.Unmarshal(data, &lt)
	return lt, err
}

func unmarshalActionType(data json.RawMessage) (ActionType, error) {
	var at ActionType
	err := json.Unmarshal(data, &at)
	return at, err
}
