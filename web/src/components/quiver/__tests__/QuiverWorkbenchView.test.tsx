import { describe, it, expect } from 'vitest';
import {
  buildChartSeries,
  type BranchedSeriesSpec,
} from '../../../utils/quiverAggregation';

const baseSpec = (
  id: string,
  branch?: string,
  color = '#22d3ee',
): BranchedSeriesSpec => ({
  id,
  label: 'CPU',
  color,
  ...(branch !== undefined ? { branch } : {}),
});

describe('buildChartSeries (US-404)', () => {
  it('renders the default branch as a solid line (no dash)', () => {
    const out = buildChartSeries([baseSpec('a')], { a: [] });
    expect(out).toHaveLength(1);
    expect(out[0].dash).toBeUndefined();
    expect(out[0].label).toBe('CPU');
  });

  it('treats explicit branch="main" as solid (default branch)', () => {
    const out = buildChartSeries([baseSpec('a', 'main')], { a: [] });
    expect(out[0].dash).toBeUndefined();
    expect(out[0].label).toBe('CPU (main)');
  });

  it('renders a non-default-branch series as dashed', () => {
    const out = buildChartSeries([baseSpec('a', 'feature-x')], { a: [] });
    expect(out[0].dash).toEqual([8, 4]);
    expect(out[0].label).toBe('CPU (feature-x)');
  });

  it('preserves the slot color across branches so dashed/solid reads as one series', () => {
    const out = buildChartSeries(
      [baseSpec('a', 'main', '#22d3ee'), baseSpec('b', 'feature-x', '#22d3ee')],
      { a: [], b: [] },
    );
    expect(out[0].color).toBe('#22d3ee');
    expect(out[1].color).toBe('#22d3ee');
    expect(out[0].dash).toBeUndefined();
    expect(out[1].dash).toEqual([8, 4]);
  });

  it('falls back to an empty point array when no entry is present', () => {
    const out = buildChartSeries([baseSpec('a')], {});
    expect(out[0].points).toEqual([]);
  });

  it('treats whitespace-only branch as the default branch', () => {
    const out = buildChartSeries([baseSpec('a', '   ')], { a: [] });
    expect(out[0].dash).toBeUndefined();
    // Whitespace-only branch trims to empty so the label keeps the bare form
    expect(out[0].label).toBe('CPU');
  });
});
