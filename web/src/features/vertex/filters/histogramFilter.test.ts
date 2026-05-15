import { describe, it, expect } from 'vitest';
import {
  applyHistogramFilters,
  clearFilters,
  setRangeFilter,
  type HistogramFilters,
  type FilterableNode,
} from './histogramFilter';

const nodes: FilterableNode[] = [
  { id: 'n1', properties: { alertScore: 10, age: 1 } },
  { id: 'n2', properties: { alertScore: 60, age: 5 } },
  { id: 'n3', properties: { alertScore: 80, age: 10 } },
  { id: 'n4', properties: { alertScore: 120, age: 3 } },
  { id: 'n5', properties: { alertScore: 30, age: null } },
];

describe('VTX-067 applyHistogramFilters', () => {
  it('given_NoFilters_when_Apply_then_AllPass', () => {
    const r = applyHistogramFilters(nodes, {});
    expect(r.passing.size).toBe(5);
    expect(r.dimmed.size).toBe(0);
  });

  it('given_SingleRangeFilter50to100_when_Apply_then_OnlyInRangePass', () => {
    const filters: HistogramFilters = { alertScore: { min: 50, max: 100 } };
    const r = applyHistogramFilters(nodes, filters);
    expect([...r.passing].sort()).toEqual(['n2', 'n3']);
    expect([...r.dimmed].sort()).toEqual(['n1', 'n4', 'n5']);
  });

  it('given_TwoFilters_when_Apply_then_Intersection', () => {
    const filters: HistogramFilters = {
      alertScore: { min: 50, max: 100 },
      age: { min: 4, max: 6 },
    };
    const r = applyHistogramFilters(nodes, filters);
    expect([...r.passing]).toEqual(['n2']);
  });

  it('given_FilterOnProperty_when_NodeMissingProp_then_Dimmed', () => {
    const filters: HistogramFilters = { age: { min: 0, max: 100 } };
    const r = applyHistogramFilters(nodes, filters);
    expect(r.passing.has('n5')).toBe(false);
    expect(r.dimmed.has('n5')).toBe(true);
  });

  it('given_FilterRangeEdgeInclusive_when_ValueExactlyAtBound_then_Passes', () => {
    const filters: HistogramFilters = { alertScore: { min: 10, max: 10 } };
    const r = applyHistogramFilters(nodes, filters);
    expect([...r.passing]).toEqual(['n1']);
  });
});

describe('VTX-067 setRangeFilter / clearFilters', () => {
  it('given_EmptyFilters_when_SetRange_then_AddedAtProperty', () => {
    const f = setRangeFilter({}, 'alertScore', { min: 0, max: 100 });
    expect(f.alertScore).toEqual({ min: 0, max: 100 });
  });

  it('given_ExistingFilters_when_SetRangeOnNewProp_then_Coexists', () => {
    const f0: HistogramFilters = { alertScore: { min: 0, max: 50 } };
    const f1 = setRangeFilter(f0, 'age', { min: 1, max: 10 });
    expect(Object.keys(f1).sort()).toEqual(['age', 'alertScore']);
  });

  it('given_SetRangeReplacesSameProp_then_Overwrites', () => {
    const f0: HistogramFilters = { alertScore: { min: 0, max: 50 } };
    const f1 = setRangeFilter(f0, 'alertScore', { min: 100, max: 200 });
    expect(f1.alertScore).toEqual({ min: 100, max: 200 });
  });

  it('given_AnyFilters_when_Clear_then_Empty', () => {
    const f: HistogramFilters = { alertScore: { min: 0, max: 50 }, age: { min: 1, max: 10 } };
    expect(clearFilters(f)).toEqual({});
  });
});
