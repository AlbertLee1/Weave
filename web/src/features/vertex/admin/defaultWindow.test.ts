import { describe, it, expect } from 'vitest';
import { computeDefaultTimeWindow } from './defaultWindow';

describe('VTX-095 computeDefaultTimeWindow', () => {
  it('given_30Days_when_NowKnown_then_Window30DaysBack', () => {
    const now = new Date('2026-05-15T12:00:00Z').getTime();
    const w = computeDefaultTimeWindow({ defaultWindowDays: 30, now });
    expect(w.to).toBe(now);
    const expectedFrom = now - 30 * 24 * 3600 * 1000;
    expect(w.from).toBe(expectedFrom);
  });

  it('given_DefaultWindowDays0_when_Compute_then_FromEqualsTo', () => {
    const now = 1_000_000;
    expect(computeDefaultTimeWindow({ defaultWindowDays: 0, now })).toEqual({
      from: now,
      to: now,
    });
  });

  it('given_NegativeDays_when_Compute_then_Throws', () => {
    expect(() =>
      computeDefaultTimeWindow({ defaultWindowDays: -5, now: 1 }),
    ).toThrow(/defaultWindowDays/);
  });
});
