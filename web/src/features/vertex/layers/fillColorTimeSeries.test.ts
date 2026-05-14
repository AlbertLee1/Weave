import { describe, it, expect } from 'vitest';
import { computeFillColor, type FillColorConfig } from './fillColor';
import type { TimePoint } from '../timeSeries/aggregateAtTime';

const t = (s: string) => new Date(s).getTime();

const series: TimePoint[] = [
  { t: t('2026-01-01T00:00:00Z'), v: 10 },
  { t: t('2026-01-01T01:00:00Z'), v: 50 },
  { t: t('2026-01-01T02:00:00Z'), v: 100 },
];

describe('VTX-065 computeFillColor by timeSeries', () => {
  const cfg: FillColorConfig = {
    by: 'timeSeries',
    property: 'load',
    scale: 'rainbow',
    domain: [0, 100],
    selectedTime: t('2026-01-01T01:00:00Z'),
    windowMs: 30 * 60_000,
    agg: 'last',
  };

  it('given_TimeSeriesAtT1_when_Last_then_ColorByValue50', () => {
    const c = computeFillColor({ load: series }, cfg);
    expect(c).toBe('hsl(150, 80%, 50%)');
  });

  it('given_TimeSeriesAtT2_when_Last_then_ColorByValue100', () => {
    const c = computeFillColor({ load: series }, { ...cfg, selectedTime: t('2026-01-01T02:00:00Z') });
    expect(c).toBe('hsl(300, 80%, 50%)');
  });

  it('given_NoPointInWindow_then_ReturnsFallback', () => {
    const c = computeFillColor({ load: series }, { ...cfg, selectedTime: t('2026-02-01T00:00:00Z') });
    expect(c).toBe('#9CA3AF');
  });

  it('given_TimeSeriesPropertyMissing_then_ReturnsFallback', () => {
    const c = computeFillColor({}, cfg);
    expect(c).toBe('#9CA3AF');
  });

  it('given_TimeSeriesAggAvgWindow2h_when_T2_then_AveragesAllPoints', () => {
    const c = computeFillColor({ load: series }, {
      ...cfg,
      selectedTime: t('2026-01-01T02:00:00Z'),
      windowMs: 2 * 3600_000,
      agg: 'avg',
    });
    // avg(10,50,100) = 53.33 → hue 160
    expect(c).toBe('hsl(160, 80%, 50%)');
  });
});
