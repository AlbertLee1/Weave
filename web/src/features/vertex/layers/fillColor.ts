import {
  aggregateAtTime,
  type AggregationMethod,
  type TimePoint,
} from '../timeSeries/aggregateAtTime';

export type FillColorConfig =
  | { by: 'static'; color: string }
  | {
      by: 'property';
      property: string;
      scale: 'rainbow';
      domain?: [number, number];
    }
  | {
      by: 'property';
      property: string;
      scale: 'threshold';
      thresholds: Array<{ lt: number; color: string }>;
    }
  | {
      by: 'timeSeries';
      property: string;
      scale: 'rainbow';
      domain?: [number, number];
      selectedTime: number;
      windowMs: number;
      agg: AggregationMethod;
    }
  | {
      by: 'timeSeries';
      property: string;
      scale: 'threshold';
      thresholds: Array<{ lt: number; color: string }>;
      selectedTime: number;
      windowMs: number;
      agg: AggregationMethod;
    };

const FALLBACK = '#9CA3AF';
const RAINBOW_HUE_START = 0;
const RAINBOW_HUE_END = 300;

export function computeFillColor(
  node: Record<string, unknown>,
  cfg: FillColorConfig,
): string {
  if (cfg.by === 'static') return cfg.color;

  let raw: unknown;
  if (cfg.by === 'timeSeries') {
    const series = node[cfg.property] as TimePoint[] | undefined;
    if (!Array.isArray(series)) return FALLBACK;
    const agg = aggregateAtTime(series, {
      selectedTime: cfg.selectedTime,
      windowMs: cfg.windowMs,
      agg: cfg.agg,
    });
    if (agg === null) return FALLBACK;
    raw = agg;
  } else {
    raw = node[cfg.property];
  }
  if (typeof raw !== 'number' || !Number.isFinite(raw)) return FALLBACK;

  if (cfg.scale === 'rainbow') {
    const [min, max] = cfg.domain ?? [0, 1];
    const span = max - min;
    if (span <= 0) return `hsl(${RAINBOW_HUE_START}, 80%, 50%)`;
    const clamped = Math.max(min, Math.min(max, raw));
    const t = (clamped - min) / span;
    const hue = Math.round(RAINBOW_HUE_START + t * (RAINBOW_HUE_END - RAINBOW_HUE_START));
    return `hsl(${hue}, 80%, 50%)`;
  }

  // threshold scale: pick first bucket whose `lt` is greater than value;
  // values >= last threshold fall through to the last bucket's color.
  for (const bucket of cfg.thresholds) {
    if (raw < bucket.lt) return bucket.color;
  }
  return cfg.thresholds[cfg.thresholds.length - 1]?.color ?? FALLBACK;
}
