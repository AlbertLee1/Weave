import { describe, it, expect } from 'vitest';
import { buildAggregationRequest } from '../dataSource';

describe('buildAggregationRequest (US-428)', () => {
  it('includes only aggregation when groupBy is omitted', () => {
    const req = buildAggregationRequest({
      kind: 'aggregation',
      ontology: 'crm',
      objectType: 'customer',
      metric: 'count',
    });
    expect(req.aggregation).toEqual([{ type: 'count', name: 'count' }]);
    expect(req.groupBy).toBeUndefined();
  });

  it('attaches a groupBy clause when set', () => {
    const req = buildAggregationRequest({
      kind: 'aggregation',
      ontology: 'crm',
      objectType: 'customer',
      metric: 'sum',
      property: 'revenue',
      groupBy: 'country',
    });
    expect(req.aggregation).toEqual([
      { type: 'sum', name: 'sum_revenue', field: 'revenue' },
    ]);
    expect(req.groupBy).toEqual([{ field: 'country', type: 'exact' }]);
  });

  it('omits the field when metric is count even if property is supplied', () => {
    const req = buildAggregationRequest({
      kind: 'aggregation',
      ontology: 'crm',
      objectType: 'customer',
      metric: 'count',
      property: 'should-be-ignored',
    });
    expect(req.aggregation[0]).not.toHaveProperty('field');
  });
});
