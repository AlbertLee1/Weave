package aggregation

import "fmt"

// resolveMetricPropertyIdentifiers normalises every metric in the request that
// names its target via propertyIdentifier instead of a bare field (syntax ref
// L461). It runs before any metric or facet so all downstream paths
// (computeMetrics, name resolution, ordering) see a populated Field. The two
// forms are mutually exclusive: setting both a non-matching field and a
// propertyIdentifier is a 400. Walks subAggregations recursively so nested
// metrics get the same treatment.
func resolveMetricPropertyIdentifiers(req *AggregationRequest) error {
	if err := resolveAggregationSpecPIs(req.Aggregations); err != nil {
		return err
	}
	return resolveSubAggregationPIs(req.SubAggregations)
}

func resolveAggregationSpecPIs(specs []AggregationSpec) error {
	for i := range specs {
		s := &specs[i]
		if s.PropertyIdentifier == nil {
			continue
		}
		api := s.PropertyIdentifier.Property.APIName
		if api == "" {
			return fmt.Errorf("aggregation[%d]: propertyIdentifier.property.apiName is required", i)
		}
		if s.Field != "" && s.Field != api {
			return fmt.Errorf("aggregation[%d]: field and propertyIdentifier are mutually exclusive", i)
		}
		s.Field = api
	}
	return nil
}

func resolveSubAggregationPIs(subs []SubAggregationSpec) error {
	for i := range subs {
		if err := resolveAggregationSpecPIs(subs[i].Aggregations); err != nil {
			return err
		}
		if err := resolveSubAggregationPIs(subs[i].SubAggregations); err != nil {
			return err
		}
	}
	return nil
}
