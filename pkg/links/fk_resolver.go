package links

import (
	"context"
	"fmt"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/liyang/weave/pkg/oms"
)

// resolveFK resolves a foreign-key based link type.
func (r *Resolver) resolveFK(ctx context.Context, lt *oms.LinkType, sourcePKs []string) ([]string, error) {
	if len(lt.ForeignKeyConfig) == 0 {
		return nil, fmt.Errorf("link type %q has no foreign key config (M2M not yet supported)", lt.APIName)
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
