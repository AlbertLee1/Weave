import { describe, it, expect } from 'vitest';
import {
  aggregateRange,
  buildAlignedData,
  pickColor,
  QUIVER_PALETTE,
  EMPTY_AGGREGATE,
} from './quiverAggregation';

describe('aggregateRange', () => {
  const points = [
    { time: '2026-04-18T10:00:00Z', value: 10 },
    { time: '2026-04-18T11:00:00Z', value: 20 },
    { time: '2026-04-18T12:00:00Z', value: 30 },
    { time: '2026-04-18T13:00:00Z', value: 40 },
  ];

  it('aggregates all points when range covers them', () => {
    const start = Date.parse('2026-04-18T09:00:00Z');
    const end = Date.parse('2026-04-18T14:00:00Z');
    expect(aggregateRange(points, start, end)).toEqual({
      count: 4,
      sum: 100,
      avg: 25,
      min: 10,
      max: 40,
    });
  });

  it('uses a half-open window [start, end)', () => {
    const start = Date.parse('2026-04-18T11:00:00Z');
    const end = Date.parse('2026-04-18T13:00:00Z');
    const out = aggregateRange(points, start, end);
    expect(out.count).toBe(2);
    expect(out.sum).toBe(50);
    expect(out.min).toBe(20);
    expect(out.max).toBe(30);
  });

  it('normalises swapped start/end', () => {
    const start = Date.parse('2026-04-18T13:00:00Z');
    const end = Date.parse('2026-04-18T11:00:00Z');
    const out = aggregateRange(points, start, end);
    expect(out.count).toBe(2);
    expect(out.avg).toBe(25);
  });

  it('returns EMPTY_AGGREGATE when no point falls in range', () => {
    const start = Date.parse('2026-04-19T00:00:00Z');
    const end = Date.parse('2026-04-19T01:00:00Z');
    expect(aggregateRange(points, start, end)).toBe(EMPTY_AGGREGATE);
  });

  it('skips non-numeric and unparseable values', () => {
    const mixed = [
      { time: '2026-04-18T10:00:00Z', value: 5 },
      { time: '2026-04-18T11:00:00Z', value: 'not-a-number' },
      { time: 'not-a-date', value: 100 },
      { time: '2026-04-18T12:00:00Z', value: 15 },
    ];
    const start = Date.parse('2026-04-18T00:00:00Z');
    const end = Date.parse('2026-04-19T00:00:00Z');
    expect(aggregateRange(mixed, start, end)).toEqual({
      count: 2,
      sum: 20,
      avg: 10,
      min: 5,
      max: 15,
    });
  });

  it('returns EMPTY_AGGREGATE for non-finite range bounds', () => {
    expect(aggregateRange(points, NaN, 0)).toBe(EMPTY_AGGREGATE);
    expect(aggregateRange(points, 0, Infinity)).toBe(EMPTY_AGGREGATE);
  });
});

describe('buildAlignedData', () => {
  it('produces a sorted union x-axis and parallel y-arrays', () => {
    const series = [
      {
        id: 'a',
        points: [
          { time: '2026-04-18T10:00:00Z', value: 1 },
          { time: '2026-04-18T12:00:00Z', value: 3 },
        ],
      },
      {
        id: 'b',
        points: [
          { time: '2026-04-18T11:00:00Z', value: 2 },
          { time: '2026-04-18T12:00:00Z', value: 4 },
        ],
      },
    ];
    const { xs, ys } = buildAlignedData(series);
    expect(xs).toHaveLength(3);
    expect(xs).toEqual([...xs].sort((p, q) => p - q));
    expect(ys).toHaveLength(2);
    expect(ys[0]).toEqual([1, null, 3]);
    expect(ys[1]).toEqual([null, 2, 4]);
  });

  it('drops non-numeric and unparseable points', () => {
    const series = [
      {
        id: 'a',
        points: [
          { time: '2026-04-18T10:00:00Z', value: 1 },
          { time: 'bad', value: 2 },
          { time: '2026-04-18T11:00:00Z', value: 'NaN' },
        ],
      },
    ];
    const { xs, ys } = buildAlignedData(series);
    expect(xs).toEqual([Math.floor(Date.parse('2026-04-18T10:00:00Z') / 1000)]);
    expect(ys[0]).toEqual([1]);
  });

  it('handles empty input', () => {
    const out = buildAlignedData([]);
    expect(out.xs).toEqual([]);
    expect(out.ys).toEqual([]);
  });
});

describe('pickColor', () => {
  it('cycles through the palette by index', () => {
    expect(pickColor(0)).toBe(QUIVER_PALETTE[0]);
    expect(pickColor(QUIVER_PALETTE.length)).toBe(QUIVER_PALETTE[0]);
    expect(pickColor(QUIVER_PALETTE.length + 1)).toBe(QUIVER_PALETTE[1]);
  });
});
