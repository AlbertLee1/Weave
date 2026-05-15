import { describe, it, expect } from 'vitest';
import {
  SPARKLINE_DEFAULT_DIMS,
  toAlignedData,
  findNearestIndex,
  computeSelectedMark,
  selectedTimeRatio,
  buildSparklineOptions,
  createSparklineScheduler,
} from './sparkline';
import type { TimePoint } from '../timeSeries/aggregateAtTime';

const t = (s: string) => new Date(s).getTime();

const SERIES: TimePoint[] = [
  { t: t('2026-05-01T00:00:00Z'), v: 10 },
  { t: t('2026-05-01T01:00:00Z'), v: 20 },
  { t: t('2026-05-01T02:00:00Z'), v: 30 },
  { t: t('2026-05-01T03:00:00Z'), v: 40 },
];

describe('VTX-032 toAlignedData', () => {
  it('given_TimePointSeries_when_Convert_then_AlignedXsInSecondsAndYsParallel', () => {
    const [xs, ys] = toAlignedData(SERIES);
    expect(xs).toHaveLength(4);
    expect(xs[0]).toBe(t('2026-05-01T00:00:00Z') / 1000);
    expect(xs[3]).toBe(t('2026-05-01T03:00:00Z') / 1000);
    expect(Array.from(ys)).toEqual([10, 20, 30, 40]);
  });

  it('given_EmptySeries_when_Convert_then_EmptyAlignedData', () => {
    const [xs, ys] = toAlignedData([]);
    expect(Array.from(xs)).toEqual([]);
    expect(Array.from(ys)).toEqual([]);
  });
});

describe('VTX-032 findNearestIndex', () => {
  it('given_EmptySeries_when_Lookup_then_ReturnsMinusOne', () => {
    expect(findNearestIndex([], t('2026-05-01T00:00:00Z'))).toBe(-1);
  });

  it('given_ExactMatch_when_Lookup_then_ReturnsThatIndex', () => {
    expect(findNearestIndex(SERIES, t('2026-05-01T02:00:00Z'))).toBe(2);
  });

  it('given_BeforeFirstPoint_when_Lookup_then_ReturnsFirstIndex', () => {
    expect(findNearestIndex(SERIES, t('2026-04-01T00:00:00Z'))).toBe(0);
  });

  it('given_AfterLastPoint_when_Lookup_then_ReturnsLastIndex', () => {
    expect(findNearestIndex(SERIES, t('2026-06-01T00:00:00Z'))).toBe(3);
  });

  it('given_BetweenTwoPointsCloserToLater_when_Lookup_then_ReturnsLaterIndex', () => {
    // 35min past t1, only 25min from t2 → returns t2 (index 1)
    expect(findNearestIndex(SERIES, t('2026-05-01T00:35:00Z'))).toBe(1);
  });

  it('given_BetweenTwoPointsCloserToEarlier_when_Lookup_then_ReturnsEarlierIndex', () => {
    // 25min past t1, 35min from t2 → returns t1 (index 0)
    expect(findNearestIndex(SERIES, t('2026-05-01T00:25:00Z'))).toBe(0);
  });

  it('given_EquidistantBetweenTwo_when_Lookup_then_ReturnsEarlierIndex', () => {
    // exactly 30 min past t1, exactly 30 min before t2 → tie → earlier (index 0)
    expect(findNearestIndex(SERIES, t('2026-05-01T00:30:00Z'))).toBe(0);
  });
});

describe('VTX-032 computeSelectedMark', () => {
  it('given_EmptySeries_when_Compute_then_ReturnsNull', () => {
    expect(computeSelectedMark([], t('2026-05-01T00:00:00Z'))).toBeNull();
  });

  it('given_SelectedAtPoint_when_Compute_then_ReturnsThatMark', () => {
    expect(computeSelectedMark(SERIES, t('2026-05-01T02:00:00Z'))).toEqual({
      index: 2,
      t: t('2026-05-01T02:00:00Z'),
      v: 30,
    });
  });

  it('given_SelectedAfterAll_when_Compute_then_ClampsToLastMark', () => {
    expect(computeSelectedMark(SERIES, t('2026-06-01T00:00:00Z'))).toEqual({
      index: 3,
      t: t('2026-05-01T03:00:00Z'),
      v: 40,
    });
  });
});

describe('VTX-032 selectedTimeRatio', () => {
  it('given_EmptySeries_when_Compute_then_ReturnsNull', () => {
    expect(selectedTimeRatio([], t('2026-05-01T00:00:00Z'))).toBeNull();
  });

  it('given_SingleSamplePoint_when_Compute_then_ReturnsZero', () => {
    expect(selectedTimeRatio([SERIES[0]], t('2026-05-01T05:00:00Z'))).toBe(0);
  });

  it('given_SelectedAtFirst_when_Compute_then_ReturnsZero', () => {
    expect(selectedTimeRatio(SERIES, t('2026-05-01T00:00:00Z'))).toBe(0);
  });

  it('given_SelectedAtLast_when_Compute_then_ReturnsOne', () => {
    expect(selectedTimeRatio(SERIES, t('2026-05-01T03:00:00Z'))).toBe(1);
  });

  it('given_SelectedAtMiddle_when_Compute_then_ReturnsHalf', () => {
    // (1.5h) / (3h) = 0.5
    expect(selectedTimeRatio(SERIES, t('2026-05-01T01:30:00Z'))).toBeCloseTo(0.5, 6);
  });

  it('given_SelectedBeforeFirst_when_Compute_then_ClampsToZero', () => {
    expect(selectedTimeRatio(SERIES, t('2026-04-01T00:00:00Z'))).toBe(0);
  });

  it('given_SelectedAfterLast_when_Compute_then_ClampsToOne', () => {
    expect(selectedTimeRatio(SERIES, t('2026-06-01T00:00:00Z'))).toBe(1);
  });
});

describe('VTX-032 buildSparklineOptions', () => {
  const baseConfig = { series: SERIES, selectedTime: t('2026-05-01T01:30:00Z') };

  it('given_DefaultDims_when_Build_then_80x20', () => {
    expect(SPARKLINE_DEFAULT_DIMS).toEqual({ width: 80, height: 20 });
    const opts = buildSparklineOptions(baseConfig);
    expect(opts.width).toBe(80);
    expect(opts.height).toBe(20);
  });

  it('given_CustomDims_when_Build_then_UsesProvidedDims', () => {
    const opts = buildSparklineOptions({ ...baseConfig, dims: { width: 120, height: 32 } });
    expect(opts.width).toBe(120);
    expect(opts.height).toBe(32);
  });

  it('given_AnyConfig_when_Build_then_LegendCursorAxesHidden', () => {
    const opts = buildSparklineOptions(baseConfig);
    expect(opts.legend?.show).toBe(false);
    expect(opts.cursor?.show).toBe(false);
    expect(opts.axes && opts.axes.every((ax) => ax.show === false)).toBe(true);
  });

  it('given_AnyConfig_when_Build_then_HasTwoSeriesXAndY', () => {
    const opts = buildSparklineOptions(baseConfig);
    expect(opts.series).toHaveLength(2);
    const ySeries = opts.series?.[1];
    expect(ySeries).toBeDefined();
    expect(ySeries?.points?.show).toBe(false);
    expect(typeof ySeries?.stroke).toBe('string');
    expect(ySeries?.width).toBe(1);
  });

  it('given_AnyConfig_when_Build_then_XScaleIsTime', () => {
    const opts = buildSparklineOptions(baseConfig);
    expect(opts.scales?.x?.time).toBe(true);
  });

  it('given_CustomStroke_when_Build_then_StrokeAppliedToYSeries', () => {
    const opts = buildSparklineOptions({ ...baseConfig, stroke: '#FF00FF' });
    expect(opts.series?.[1].stroke).toBe('#FF00FF');
  });
});

describe('VTX-032 createSparklineScheduler', () => {
  function makeFakeRaf() {
    let next = 1;
    const pending = new Map<number, FrameRequestCallback>();
    return {
      raf: (cb: FrameRequestCallback) => {
        const h = next++;
        pending.set(h, cb);
        return h;
      },
      cancel: (h: number) => {
        pending.delete(h);
      },
      flush: () => {
        const cbs = Array.from(pending.values());
        pending.clear();
        for (const cb of cbs) cb(0);
      },
      pendingCount: () => pending.size,
    };
  }

  it('given_50ConcurrentSchedules_when_FlushOneFrame_then_AllExecuteInSingleRaf', () => {
    const fake = makeFakeRaf();
    const sched = createSparklineScheduler(fake.raf, fake.cancel);
    const ran: string[] = [];
    for (let i = 0; i < 50; i++) sched.schedule(`n${i}`, () => ran.push(`n${i}`));
    expect(fake.pendingCount()).toBe(1);
    fake.flush();
    expect(ran).toHaveLength(50);
  });

  it('given_SchedulePreviousIdAgain_when_FlushOnce_then_OnlyLatestRedrawRuns', () => {
    const fake = makeFakeRaf();
    const sched = createSparklineScheduler(fake.raf, fake.cancel);
    const ran: string[] = [];
    sched.schedule('a', () => ran.push('old'));
    sched.schedule('a', () => ran.push('new'));
    fake.flush();
    expect(ran).toEqual(['new']);
  });

  it('given_Cancel_when_Flush_then_RedrawNotExecuted', () => {
    const fake = makeFakeRaf();
    const sched = createSparklineScheduler(fake.raf, fake.cancel);
    const ran: string[] = [];
    sched.schedule('a', () => ran.push('a'));
    sched.cancel('a');
    fake.flush();
    expect(ran).toEqual([]);
  });

  it('given_FlushSync_when_PendingScheduled_then_ExecutesImmediatelyAndCancelsRaf', () => {
    const fake = makeFakeRaf();
    const sched = createSparklineScheduler(fake.raf, fake.cancel);
    const ran: string[] = [];
    sched.schedule('a', () => ran.push('a'));
    sched.flushSync();
    expect(ran).toEqual(['a']);
    expect(fake.pendingCount()).toBe(0);
  });

  it('given_Stop_when_Flush_then_PendingDiscardedAndRafCancelled', () => {
    const fake = makeFakeRaf();
    const sched = createSparklineScheduler(fake.raf, fake.cancel);
    const ran: string[] = [];
    sched.schedule('a', () => ran.push('a'));
    sched.stop();
    expect(fake.pendingCount()).toBe(0);
    fake.flush();
    expect(ran).toEqual([]);
  });

  it('given_NoSchedules_when_FlushSync_then_NoOp', () => {
    const fake = makeFakeRaf();
    const sched = createSparklineScheduler(fake.raf, fake.cancel);
    expect(() => sched.flushSync()).not.toThrow();
    expect(fake.pendingCount()).toBe(0);
  });
});
