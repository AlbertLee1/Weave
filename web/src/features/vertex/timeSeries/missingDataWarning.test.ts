import { describe, it, expect } from 'vitest';
import { computeMissingDataWarning } from './missingDataWarning';
import type { TimePoint } from './aggregateAtTime';

const t = (s: string) => new Date(s).getTime();

describe('VTX-080 computeMissingDataWarning', () => {
  it('given_LastObservationWithinThreshold_then_NoWarning', () => {
    const series: TimePoint[] = [
      { t: t('2026-05-15T00:00:00Z'), v: 1 },
      { t: t('2026-05-15T01:00:00Z'), v: 2 },
    ];
    const r = computeMissingDataWarning(series, {
      selectedTime: t('2026-05-15T02:00:00Z'),
      missingDataWarningHours: 24,
    });
    expect(r.warn).toBe(false);
    expect(r.lastObservedMs).toBe(t('2026-05-15T01:00:00Z'));
  });

  it('given_LastObservationOlderThanThreshold_then_Warn', () => {
    const series: TimePoint[] = [
      { t: t('2026-05-13T00:00:00Z'), v: 1 },
    ];
    const r = computeMissingDataWarning(series, {
      selectedTime: t('2026-05-15T00:00:00Z'),
      missingDataWarningHours: 24,
    });
    expect(r.warn).toBe(true);
    expect(r.lastObservedMs).toBe(t('2026-05-13T00:00:00Z'));
    expect(r.gapHours).toBe(48);
  });

  it('given_EmptySeries_then_WarnWithNoLastObserved', () => {
    const r = computeMissingDataWarning([], {
      selectedTime: t('2026-05-15T00:00:00Z'),
      missingDataWarningHours: 1,
    });
    expect(r.warn).toBe(true);
    expect(r.lastObservedMs).toBeNull();
  });

  it('given_FutureObservation_then_NoWarn', () => {
    // If a point lies after selectedTime, consider it as "future not yet
    // arrived" — the warning is about staleness, not future data. Pick
    // the latest point at or before selectedTime.
    const series: TimePoint[] = [
      { t: t('2026-05-15T05:00:00Z'), v: 1 },
      { t: t('2026-05-15T15:00:00Z'), v: 2 },
    ];
    const r = computeMissingDataWarning(series, {
      selectedTime: t('2026-05-15T10:00:00Z'),
      missingDataWarningHours: 24,
    });
    expect(r.lastObservedMs).toBe(t('2026-05-15T05:00:00Z'));
    expect(r.warn).toBe(false);
  });

  it('given_AllObservationsInFuture_then_WarnWithNoLastObserved', () => {
    const series: TimePoint[] = [
      { t: t('2026-05-15T20:00:00Z'), v: 1 },
    ];
    const r = computeMissingDataWarning(series, {
      selectedTime: t('2026-05-15T10:00:00Z'),
      missingDataWarningHours: 1,
    });
    expect(r.warn).toBe(true);
    expect(r.lastObservedMs).toBeNull();
  });

  it('given_GapExactlyEqualToThreshold_then_NoWarn', () => {
    const series: TimePoint[] = [
      { t: t('2026-05-15T00:00:00Z'), v: 1 },
    ];
    const r = computeMissingDataWarning(series, {
      selectedTime: t('2026-05-16T00:00:00Z'),
      missingDataWarningHours: 24,
    });
    expect(r.warn).toBe(false);
    expect(r.gapHours).toBe(24);
  });

  it('given_GapHoursReturnedRoundedTo2Decimals', () => {
    const series: TimePoint[] = [
      { t: t('2026-05-15T00:00:00Z'), v: 1 },
    ];
    const r = computeMissingDataWarning(series, {
      selectedTime: t('2026-05-15T01:30:00Z'),
      missingDataWarningHours: 1,
    });
    expect(r.gapHours).toBe(1.5);
  });
});
