package links

import (
	"context"
	"fmt"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/liyang/weave/pkg/oms"
)

// resolveFK resolves a foreign-key based link type in the forward direction
// (source -> target).
func (r *Resolver) resolveFK(ctx context.Context, lt *oms.LinkType, sourcePKs []string) ([]string, error) {
	if len(lt.ForeignKeyConfig) == 0 {
		return nil, fmt.Errorf("link type %q has no foreign key config", lt.APIName)
	}

	fkConfig, err := parseFKConfig(lt.ForeignKeyConfig)
	if err != nil {
		return nil, fmt.Errorf("parse FK config: %w", err)
	}

	// Get source and target object types to learn their API names and primary keys.
	sourceOT, err := r.repo.GetObjectType(ctx, lt.SourceObjectType)
	if err != nil {
		return nil, fmt.Errorf("get source object type: %w", err)
	}

	targetOT, err := r.repo.GetObjectType(ctx, lt.TargetObjectType)
	if err != nil {
		return nil, fmt.Errorf("get target object type: %w", err)
	}

	// Step 1: Get the FK values from source objects.
	fkValues, err := r.getFKValues(sourceOT.APIName, sourcePKs, sourceOT.PrimaryKey, fkConfig.SourceProperty)
	if err != nil {
		return nil, fmt.Errorf("get FK values: %w", err)
	}

	if len(fkValues) == 0 {
		return nil, nil
	}

	// Step 2: Find target objects where targetProperty matches any FK value.
	targetPKs, err := r.findTargetsByFK(targetOT.APIName, targetOT.PrimaryKey, fkConfig.TargetProperty, fkValues)
	if err != nil {
		return nil, fmt.Errorf("find targets: %w", err)
	}

	return targetPKs, nil
}

// getFKValues retrieves the foreign key property values from source objects by their primary keys.
func (r *Resolver) getFKValues(objectType string, pks []string, pkField, fkField string) ([]string, error) {
	var values []string
	seen := make(map[string]bool)

	for _, pk := range pks {
		q := bleve.NewTermQuery(pk)
		q.SetField(pkField)
		req := bleve.NewSearchRequest(q)
		req.Fields = []string{fkField}
		req.Size = 1

		result, err := r.indexMgr.Search(objectType, req)
		if err != nil {
			return nil, err
		}

		for _, hit := range result.Hits {
			if val, ok := hit.Fields[fkField]; ok {
				strVal := fmt.Sprintf("%v", val)
				if !seen[strVal] {
					values = append(values, strVal)
					seen[strVal] = true
				}
			}
		}
	}

	return values, nil
}

// resolveFKReverse resolves a foreign-key based link type in the reverse
// direction (target -> source). Given target object primary keys, return source
// object primary keys whose FK property value matches any of the given targets.
//
// Example: Order.customerID -> Customer.id.
// Given customer PKs, return orders whose customerID equals one of them.
// This is simpler than forward FK because we only need one Bleve lookup on the
// source index, not two.
func (r *Resolver) resolveFKReverse(ctx context.Context, lt *oms.LinkType, targetPKs []string) ([]string, error) {
	if len(lt.ForeignKeyConfig) == 0 {
		return nil, fmt.Errorf("link type %q has no foreign key config", lt.APIName)
	}

	fkConfig, err := parseFKConfig(lt.ForeignKeyConfig)
	if err != nil {
		return nil, fmt.Errorf("parse FK config: %w", err)
	}

	if len(targetPKs) == 0 {
		return nil, nil
	}

	// Get source and target object types to learn their API names and primary keys.
	sourceOT, err := r.repo.GetObjectType(ctx, lt.SourceObjectType)
	if err != nil {
		return nil, fmt.Errorf("get source object type: %w", err)
	}

	targetOT, err := r.repo.GetObjectType(ctx, lt.TargetObjectType)
	if err != nil {
		return nil, fmt.Errorf("get target object type: %w", err)
	}

	// Step 1: Resolve target PKs to their targetProperty values.
	// For most links targetProperty == primaryKey so we can skip this lookup,
	// but when they differ (e.g. composite keys) we must fetch the field.
	var fkValues []string
	if fkConfig.TargetProperty == targetOT.PrimaryKey {
		fkValues = targetPKs
	} else {
		fkValues, err = r.getFKValues(targetOT.APIName, targetPKs, targetOT.PrimaryKey, fkConfig.TargetProperty)
		if err != nil {
			return nil, fmt.Errorf("get reverse FK values: %w", err)
		}
	}

	if len(fkValues) == 0 {
		return nil, nil
	}

	// Step 2: Find source objects whose sourceProperty matches any of the target values.
	sourcePKs, err := r.findTargetsByFK(sourceOT.APIName, sourceOT.PrimaryKey, fkConfig.SourceProperty, fkValues)
	if err != nil {
		return nil, fmt.Errorf("find reverse sources: %w", err)
	}

	return sourcePKs, nil
}

// findTargetsByFK finds target objects where the FK field matches any of the given values.
func (r *Resolver) findTargetsByFK(objectType, pkField, fkField string, fkValues []string) ([]string, error) {
	queries := make([]query.Query, 0, len(fkValues))
	for _, val := range fkValues {
		q := bleve.NewTermQuery(val)
		q.SetField(fkField)
		queries = append(queries, q)
	}

	var bq query.Query
	if len(queries) == 1 {
		bq = queries[0]
	} else {
		bq = bleve.NewDisjunctionQuery(queries...)
	}

	req := bleve.NewSearchRequest(bq)
	req.Fields = []string{pkField}
	req.Size = 10000

	result, err := r.indexMgr.Search(objectType, req)
	if err != nil {
		return nil, err
	}

	var pks []string
	seen := make(map[string]bool)
	for _, hit := range result.Hits {
		if val, ok := hit.Fields[pkField]; ok {
			strVal := fmt.Sprintf("%v", val)
			if !seen[strVal] {
				pks = append(pks, strVal)
				seen[strVal] = true
			}
		}
	}

	return pks, nil
}
