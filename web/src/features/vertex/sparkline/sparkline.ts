// VTX-032 — 节点 Sparkline Extended Label（uPlot）的纯逻辑层。
//
// 不依赖 React / DOM / uPlot 实例 —— 只产出 uPlot.Options 和驱动批量重绘
// 的调度器。React 接线层在 mount 时 dynamic import uPlot、把这里的 Options
// 传给构造器，并用 SparklineScheduler 把 selectedTime 变化引起的 50+ 节点
// 重绘合并到单帧，保证调研报告 §10.6 的 ≥ 30 fps 目标。

import type uPlotCtor from 'uplot';
import type { TimePoint } from '../timeSeries/aggregateAtTime';

export interface SparklineDims {
  width: number;
  height: number;
}

export const SPARKLINE_DEFAULT_DIMS: SparklineDims = { width: 80, height: 20 };

const DEFAULT_STROKE = '#3B82F6';

export interface SparklineConfig {
  series: TimePoint[];
  selectedTime: number;
  dims?: SparklineDims;
  stroke?: string;
}

export type AlignedData = [number[], (number | null)[]];

export function toAlignedData(series: TimePoint[]): AlignedData {
  const xs = new Array<number>(series.length);
  const ys = new Array<number | null>(series.length);
  for (let i = 0; i < series.length; i++) {
    xs[i] = series[i].t / 1000;
    ys[i] = series[i].v;
  }
  return [xs, ys];
}

// findNearestIndex returns the index of the TimePoint whose `t` is closest to
// `selectedTime`. On a tie it returns the earlier index (matches the React
// layer's "snap to the most-recent recorded value" expectation).
export function findNearestIndex(series: TimePoint[], selectedTime: number): number {
  if (series.length === 0) return -1;
  if (selectedTime <= series[0].t) return 0;
  if (selectedTime >= series[series.length - 1].t) return series.length - 1;

  let lo = 0;
  let hi = series.length - 1;
  while (lo < hi) {
    const mid = (lo + hi) >>> 1;
    if (series[mid].t < selectedTime) lo = mid + 1;
    else hi = mid;
  }
  if (lo === 0) return 0;
  const beforeDiff = selectedTime - series[lo - 1].t;
  const afterDiff = series[lo].t - selectedTime;
  return beforeDiff <= afterDiff ? lo - 1 : lo;
}

export interface SparklineMark {
  index: number;
  t: number;
  v: number;
}

export function computeSelectedMark(
  series: TimePoint[],
  selectedTime: number,
): SparklineMark | null {
  const i = findNearestIndex(series, selectedTime);
  if (i < 0) return null;
  return { index: i, t: series[i].t, v: series[i].v };
}

// selectedTimeRatio returns the 0..1 horizontal position of selectedTime inside
// the series' time span — useful for absolute-positioning a highlight overlay
// without needing a uPlot instance.
export function selectedTimeRatio(series: TimePoint[], selectedTime: number): number | null {
  if (series.length === 0) return null;
  const first = series[0].t;
  const last = series[series.length - 1].t;
  if (last <= first) return 0;
  if (selectedTime <= first) return 0;
  if (selectedTime >= last) return 1;
  return (selectedTime - first) / (last - first);
}

export function buildSparklineOptions(config: SparklineConfig): uPlotCtor.Options {
  const dims = config.dims ?? SPARKLINE_DEFAULT_DIMS;
  return {
    width: dims.width,
    height: dims.height,
    padding: [1, 1, 1, 1],
    legend: { show: false },
    cursor: { show: false, drag: { x: false, y: false } },
    axes: [{ show: false }, { show: false }],
    scales: {
      x: { time: true },
      y: { auto: true },
    },
    series: [
      {},
      {
        label: 'v',
        stroke: config.stroke ?? DEFAULT_STROKE,
        width: 1,
        spanGaps: true,
        points: { show: false },
      },
    ],
  };
}

// SparklineScheduler coalesces N redraw requests into a single
// requestAnimationFrame so 50+ node sparklines refreshing on a selectedTime
// change cost one frame's worth of layout/paint instead of 50.
export interface SparklineScheduler {
  schedule(id: string, redraw: () => void): void;
  flushSync(): void;
  cancel(id: string): void;
  stop(): void;
}

export function createSparklineScheduler(
  raf: (cb: FrameRequestCallback) => number = requestAnimationFrame,
  cancelRaf: (handle: number) => void = cancelAnimationFrame,
): SparklineScheduler {
  const pending = new Map<string, () => void>();
  let handle: number | null = null;

  const drain = () => {
    const snapshot = Array.from(pending.values());
    pending.clear();
    for (const fn of snapshot) fn();
  };

  return {
    schedule(id, redraw) {
      pending.set(id, redraw);
      if (handle === null) {
        handle = raf(() => {
          handle = null;
          drain();
        });
      }
    },
    flushSync() {
      if (handle !== null) {
        cancelRaf(handle);
        handle = null;
      }
      drain();
    },
    cancel(id) {
      pending.delete(id);
    },
    stop() {
      if (handle !== null) {
        cancelRaf(handle);
        handle = null;
      }
      pending.clear();
    },
  };
}
