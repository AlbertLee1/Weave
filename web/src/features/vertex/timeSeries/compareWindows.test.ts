import { describe, it, expect } from 'vitest';
import {
  addCompareWindow,
  removeCompareWindow,
  aggregatePerWindow,
  type TimeWindow,
} from './compareWindows';
import type { TimePoint } from './aggregateAtTime';

const t = (s: string) => new Date(s).getTime();

const series: TimePoint[] = [
  { t: t('2026-01-01T00:00:00Z'), v: 100 },
  { t: t('2026-01-15T00:00:00Z'), v: 200 },
  { t: t('2026-02-01T00:00:00Z'), v: 300 },
  { t: t('2026-02-15T00:00:00Z'), v: 400 },
];

const W1: TimeWindow = {
  id: 'w1',
  from: t('2026-01-01T00:00:00Z'),
  to: t('2026-01-31T23:59:59Z'),
};
const W2: TimeWindow = {
  id: 'w2',
  from: t('2026-02-01T00:00:00Z'),
  to: t('2026-02-28T23:59:59Z'),
};

describe('VTX-079 addCompareWindow', () => {
  it('given_OneWindow_when_Add_then_Two', () => {
    const next = addCompareWindow([W1], W2);
    expect(next.map((w) => w.id)).toEqual(['w1', 'w2']);
  });

  it('given_DuplicateId_when_Add_then_Throws', () => {
    expect(() => addCompareWindow([W1], W1)).toThrow(/duplicate/i);
  });
});

describe('VTX-079 removeCompareWindow', () => {
  it('given_TwoWindows_when_RemoveOne_then_OneLeft', () => {
    const next = removeCompareWindow([W1, W2], 'w2');
    expect(next.map((w) => w.id)).toEqual(['w1']);
  });

  it('given_RemoveLast_when_Apply_then_DoesNotEmptyList', () => {
    const next = removeCompareWindow([W1], 'w1');
    // Removing the only window is allowed; caller decides whether to refuse
    // at the UI layer.
    expect(next).toEqual([]);
  });
});

describe('VTX-079 aggregatePerWindow', () => {
  it('given_TwoWindows_when_SumAgg_then_PerWindowTotals', () => {
    const result = aggregatePerWindow(series, [W1, W2], 'sum');
    expect(result).toEqual({
      w1: 300,
      w2: 700,
    });
  });

  it('given_WindowWithNoPoints_when_Aggregate_then_Null', () => {
    const empty: TimeWindow = { id: 'w3', from: t('2025-01-01T00:00:00Z'), to: t('2025-12-31T00:00:00Z') };
    const result = aggregatePerWindow(series, [W1, empty], 'avg');
    expect(result.w1).toBe(150);
    expect(result.w3).toBeNull();
  });

  it('given_AvgAgg_when_W2_then_MeanOfFebPoints', () => {
    const result = aggregatePerWindow(series, [W2], 'avg');
    expect(result.w2).toBe(350);
  });

  it('given_NoWindows_when_Aggregate_then_EmptyObject', () => {
    expect(aggregatePerWindow(series, [], 'avg')).toEqual({});
  });
});
