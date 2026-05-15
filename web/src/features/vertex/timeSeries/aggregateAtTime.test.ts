import { describe, it, expect } from 'vitest';
import { aggregateAtTime, type TimePoint } from './aggregateAtTime';

const t = (s: string) => new Date(s).getTime();

const series: TimePoint[] = [
  { t: t('2026-01-01T00:00:00Z'), v: 10 },
  { t: t('2026-01-01T01:00:00Z'), v: 20 },
  { t: t('2026-01-01T02:00:00Z'), v: 30 },
  { t: t('2026-01-01T03:00:00Z'), v: 40 },
];

describe('VTX-065 aggregateAtTime', () => {
  it('given_WindowCoversAllPoints_when_AvgAgg_then_ReturnsMean', () => {
    const r = aggregateAtTime(series, {
      selectedTime: t('2026-01-01T03:00:00Z'),
      windowMs: 4 * 3600_000,
      agg: 'avg',
    });
    expect(r).toBe(25);
  });

  it('given_WindowMissesAll_when_AvgAgg_then_ReturnsNull', () => {
    const r = aggregateAtTime(series, {
      selectedTime: t('2026-02-01T00:00:00Z'),
      windowMs: 1 * 3600_000,
      agg: 'avg',
    });
    expect(r).toBeNull();
  });

  it('given_OnlyOnePointInWindow_when_SumAgg_then_ReturnsThatValue', () => {
    const r = aggregateAtTime(series, {
      selectedTime: t('2026-01-01T01:30:00Z'),
      windowMs: 30 * 60_000,
      agg: 'sum',
    });
    expect(r).toBe(20);
  });

  it('given_MultiPointsInWindow_when_SumAgg_then_ReturnsTotal', () => {
    const r = aggregateAtTime(series, {
      selectedTime: t('2026-01-01T02:00:00Z'),
      windowMs: 2 * 3600_000,
      agg: 'sum',
    });
    expect(r).toBe(10 + 20 + 30);
  });

  it('given_MaxAgg_when_MultiPoints_then_ReturnsMax', () => {
    const r = aggregateAtTime(series, {
      selectedTime: t('2026-01-01T03:00:00Z'),
      windowMs: 4 * 3600_000,
      agg: 'max',
    });
    expect(r).toBe(40);
  });

  it('given_LastAgg_when_MultiPoints_then_ReturnsLatestObservation', () => {
    const r = aggregateAtTime(series, {
      selectedTime: t('2026-01-01T02:30:00Z'),
      windowMs: 3 * 3600_000,
      agg: 'last',
    });
    expect(r).toBe(30);
  });

  it('given_EmptySeries_then_ReturnsNull', () => {
    const r = aggregateAtTime([], {
      selectedTime: t('2026-01-01T00:00:00Z'),
      windowMs: 3600_000,
      agg: 'avg',
    });
    expect(r).toBeNull();
  });

  it('given_WindowEdgeInclusive_when_PointExactlyAtStart_then_Included', () => {
    const r = aggregateAtTime(series, {
      selectedTime: t('2026-01-01T01:00:00Z'),
      windowMs: 1 * 3600_000,
      agg: 'avg',
    });
    // window = [00:00, 01:00], both inclusive → (10 + 20) / 2
    expect(r).toBe(15);
  });
});
