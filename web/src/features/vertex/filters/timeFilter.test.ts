import { describe, it, expect } from 'vitest';
import { applyTimeFilter, type TimeFilter, type TimedNode } from './timeFilter';

const t = (s: string) => new Date(s).getTime();

const nodes: TimedNode[] = [
  { id: 'n1', timestamps: { createdAt: t('2026-01-01T00:00:00Z') } },
  { id: 'n2', timestamps: { createdAt: t('2026-02-01T00:00:00Z') } },
  { id: 'n3', timestamps: { createdAt: t('2026-03-01T00:00:00Z') } },
  { id: 'n4', timestamps: { createdAt: t('2026-04-01T00:00:00Z') } },
  { id: 'n5', timestamps: {} },
];

describe('VTX-068 applyTimeFilter', () => {
  it('given_NoFilter_when_Apply_then_AllPass', () => {
    const r = applyTimeFilter(nodes, null);
    expect(r.passing.size).toBe(5);
  });

  it('given_RangeCoversFebMar_when_Apply_then_TwoPass', () => {
    const f: TimeFilter = {
      property: 'createdAt',
      from: t('2026-02-01T00:00:00Z'),
      to: t('2026-03-31T23:59:59Z'),
    };
    const r = applyTimeFilter(nodes, f);
    expect([...r.passing].sort()).toEqual(['n2', 'n3']);
    expect(r.dimmed.has('n5')).toBe(true);
  });

  it('given_EdgeInclusiveBound_when_NodeAtBoundary_then_Passes', () => {
    const f: TimeFilter = {
      property: 'createdAt',
      from: t('2026-01-01T00:00:00Z'),
      to: t('2026-01-01T00:00:00Z'),
    };
    const r = applyTimeFilter(nodes, f);
    expect([...r.passing]).toEqual(['n1']);
  });

  it('given_NodeMissingTimestamp_when_Apply_then_Dimmed', () => {
    const f: TimeFilter = {
      property: 'createdAt',
      from: t('2026-01-01T00:00:00Z'),
      to: t('2026-12-31T23:59:59Z'),
    };
    const r = applyTimeFilter(nodes, f);
    expect(r.dimmed.has('n5')).toBe(true);
  });

  it('given_InvertedRange_when_Apply_then_NoneInRange', () => {
    const f: TimeFilter = {
      property: 'createdAt',
      from: t('2026-12-01T00:00:00Z'),
      to: t('2026-01-01T00:00:00Z'),
    };
    const r = applyTimeFilter(nodes, f);
    expect(r.passing.size).toBe(0);
  });
});
