// VTX-060 — TimeSeries Extended Label 渲染的纯逻辑层。
//
// 与 VTX-059 propertyLabel 并列的第二种 Extended Label：节点 DOM overlay
// 在当前 selectedTime / time window 下显示一个 scalar（窗口聚合 + 可选
// smoothing bucket）。本模块提供：
//   - TimeSeriesExtendedLabelSpec 与 wire schema 对齐（VTX-058 ExtendedLabel.
//     kind/timeSeriesRid，displayName 是 React 接线层从 ObjectType / TimeSeries
//     metadata 注入的渲染时增强，与 propertyLabel 同模式）
//   - buildTimeSeriesLabelRequest —— OSS time series scalar 端点的 URL builder，
//     对齐 pkg/oss/handlers_vertex_timeseries.go 的
//     /api/v2/ontologies/{o}/objects/{ot}/{pk}/timeseries/{name}?from=&to=&agg=&bucket=
//   - computeTimeSeriesLabelScalar —— 客户端 series → scalar 计算（reuse
//     VTX-042 smoothSeriesByBucket + VTX-065 aggregateAtTime），用于已经拿
//     到 series（如 sparkline 数据复用）的场景，避免再发一次请求
//   - renderTimeSeriesExtendedLabel —— scalar → {status, labelName, valueText,
//     text}，status 增加 'error' 态（VTX-061 一致，让 React 层共用渲染层）
//   - createTimeSeriesLabelDebouncer —— selectedTime 拖动 100ms 防抖（与 VTX-031
//     makeDebouncedNotifier 同形态，但默认 delay 锁在 TIMESERIES_LABEL_DEBOUNCE_MS）
//
// 不依赖 React / DOM；wire 层的 agg 字面值与 backend (pkg/timeseries 的
// AggAvg 等 enum string) 保持大写映射，让 URL 直接是 ?agg=AVG。

import { aggregateAtTime, type AggregationMethod, type TimePoint } from '../timeSeries/aggregateAtTime';
import { smoothSeriesByBucket } from '../scenarioPane/timeSeriesOverride';
import { MISSING_VALUE_PLACEHOLDER } from './propertyLabel';

export { MISSING_VALUE_PLACEHOLDER };

export const TIMESERIES_LABEL_DEBOUNCE_MS = 100;
export const ERROR_PLACEHOLDER = '!';

export type TimeSeriesLabelStatus = 'present' | 'missing' | 'error';

export interface TimeSeriesExtendedLabelSpec {
  kind: 'timeSeries';
  timeSeriesRid: string;
  // displayName 非 wire 字段；React 接线层从 ObjectType.timeSeries[rid].
  // displayName 取后注入。留空 / 仅空白 → fallback 到 timeSeriesRid。
  displayName?: string;
}

export interface TimeSeriesLabelRequestContext {
  ontology: string;
  objectType: string;
  primaryKey: string;
}

export interface TimeSeriesLabelWindow {
  from: number;
  to: number;
  agg: AggregationMethod;
  // 5-min smoothing 对应 5*60*1000；undefined / 0 → 不做 smoothing。
  smoothingMs?: number;
}

export interface TimeSeriesLabelRequest {
  method: 'GET';
  path: string;
}

export interface ComputeTimeSeriesLabelScalarParams {
  selectedTime: number;
  windowMs: number;
  agg: AggregationMethod;
  smoothingMs?: number;
}

export interface TimeSeriesLabelRenderResult {
  status: TimeSeriesLabelStatus;
  labelName: string;
  valueText: string;
  text: string;
  errorMessage?: string;
}

export interface RenderTimeSeriesExtendedLabelOptions {
  // 自定义 scalar 格式化；返回空字符串视为 missing。React 接线层注入
  // toFixed / 千分位 / 单位后缀等专用 formatter。
  formatValue?: (raw: number) => string;
  missingPlaceholder?: string;
  errorPlaceholder?: string;
  // 设置后 status 强制 'error'，valueText 用 errorPlaceholder（默认 '!'）。
  // 调用方传 error 字符串表示 fetch / 解析失败；message 透传给 tooltip。
  error?: string;
}

// AggregationMethod (lower-case) → backend URL query (UPPER-CASE)，与
// pkg/timeseries/agg.go (AggAvg='AVG' 等) 对齐。
const AGG_TO_BACKEND: Record<AggregationMethod, string> = {
  avg: 'AVG',
  sum: 'SUM',
  max: 'MAX',
  min: 'MIN',
  last: 'LAST',
  count: 'COUNT',
};

function requireNonBlank(value: string, field: string): string {
  if (typeof value !== 'string' || value.trim().length === 0) {
    throw new Error(`${field} is required`);
  }
  return value;
}

export function isTimeSeriesExtendedLabel(label: { kind: string }): boolean {
  return label.kind === 'timeSeries';
}

export function buildTimeSeriesLabelRequest(
  spec: TimeSeriesExtendedLabelSpec,
  ctx: TimeSeriesLabelRequestContext,
  window: TimeSeriesLabelWindow,
): TimeSeriesLabelRequest {
  if (spec.kind !== 'timeSeries') {
    throw new Error(`buildTimeSeriesLabelRequest: expected kind="timeSeries", got "${spec.kind}"`);
  }
  requireNonBlank(spec.timeSeriesRid, 'timeSeriesRid');
  requireNonBlank(ctx.ontology, 'ontology');
  requireNonBlank(ctx.objectType, 'objectType');
  requireNonBlank(ctx.primaryKey, 'primaryKey');
  if (!Number.isFinite(window.from) || !Number.isFinite(window.to)) {
    throw new Error('window.from / window.to must be finite numbers');
  }
  if (window.to < window.from) {
    throw new Error('window.from must be ≤ window.to');
  }

  const aggParam = AGG_TO_BACKEND[window.agg];
  if (!aggParam) {
    throw new Error(`unknown agg: ${window.agg}`);
  }

  const segments = [
    'api',
    'v2',
    'ontologies',
    encodeURIComponent(ctx.ontology),
    'objects',
    encodeURIComponent(ctx.objectType),
    encodeURIComponent(ctx.primaryKey),
    'timeseries',
    encodeURIComponent(spec.timeSeriesRid),
  ];
  const path = `/${segments.join('/')}`;

  const params = new URLSearchParams();
  params.set('from', String(window.from));
  params.set('to', String(window.to));
  params.set('agg', aggParam);
  if (typeof window.smoothingMs === 'number' && window.smoothingMs > 0) {
    // backend bucket 用 Go time.ParseDuration 解析（pkg/oss/handlers_vertex_
    // timeseries.go:135）—— "<n>ms" 形态是合法解析串；前端不假设 5min →
    // '5m' 这种人类可读时间单位，直接传毫秒数字，绕开 round 误差。
    params.set('bucket', `${window.smoothingMs}ms`);
  }

  return { method: 'GET', path: `${path}?${params.toString()}` };
}

export function computeTimeSeriesLabelScalar(
  series: readonly TimePoint[],
  params: ComputeTimeSeriesLabelScalarParams,
): number | null {
  const points =
    typeof params.smoothingMs === 'number' && params.smoothingMs > 0
      ? smoothSeriesByBucket(series, params.smoothingMs)
      : series.slice();
  return aggregateAtTime(points, {
    selectedTime: params.selectedTime,
    windowMs: params.windowMs,
    agg: params.agg,
  });
}

function isMissingScalar(v: number | null | undefined): boolean {
  if (v === null || v === undefined) return true;
  if (typeof v !== 'number' || !Number.isFinite(v)) return true;
  return false;
}

function formatScalar(v: number): string {
  return String(v);
}

export function renderTimeSeriesExtendedLabel(
  spec: TimeSeriesExtendedLabelSpec,
  scalar: number | null,
  options: RenderTimeSeriesExtendedLabelOptions = {},
): TimeSeriesLabelRenderResult {
  if (spec.kind !== 'timeSeries') {
    throw new Error(`renderTimeSeriesExtendedLabel: expected kind="timeSeries", got "${spec.kind}"`);
  }
  requireNonBlank(spec.timeSeriesRid, 'timeSeriesRid');

  const labelName = (() => {
    const dn = spec.displayName;
    if (typeof dn === 'string' && dn.trim().length > 0) return dn;
    return spec.timeSeriesRid;
  })();

  const missingPlaceholder = options.missingPlaceholder ?? MISSING_VALUE_PLACEHOLDER;
  const errorPlaceholder = options.errorPlaceholder ?? ERROR_PLACEHOLDER;

  // Error 态优先于 present / missing：上游 fetch 失败时即使 scalar 有值也
  // 走 error 路径（避免显示陈旧数据让用户误以为成功）。
  if (typeof options.error === 'string' && options.error.length > 0) {
    return {
      status: 'error',
      labelName,
      valueText: errorPlaceholder,
      text: `${labelName}: ${errorPlaceholder}`,
      errorMessage: options.error,
    };
  }

  if (isMissingScalar(scalar)) {
    return {
      status: 'missing',
      labelName,
      valueText: missingPlaceholder,
      text: `${labelName}: ${missingPlaceholder}`,
    };
  }

  const valueText = (() => {
    const n = scalar as number;
    if (options.formatValue) {
      const formatted = options.formatValue(n);
      if (typeof formatted !== 'string' || formatted.length === 0) return '';
      return formatted;
    }
    return formatScalar(n);
  })();

  if (valueText === '') {
    return {
      status: 'missing',
      labelName,
      valueText: missingPlaceholder,
      text: `${labelName}: ${missingPlaceholder}`,
    };
  }

  return {
    status: 'present',
    labelName,
    valueText,
    text: `${labelName}: ${valueText}`,
  };
}

export interface DebouncedTimeSeriesLabelNotifier {
  (selectedTime: number): void;
  cancel: () => void;
}

// createTimeSeriesLabelDebouncer —— BDD #3：selectedTime 拖动时 100ms 防抖
// 再触发"全部 timeSeries label 重算"回调。形态与 VTX-031 makeDebounced
// Notifier 一致，但默认 delay 锁定 TIMESERIES_LABEL_DEBOUNCE_MS 让接线
// 层不必各处复制 100 这个数字。
export function createTimeSeriesLabelDebouncer(
  callback: (selectedTime: number) => void,
  delayMs: number = TIMESERIES_LABEL_DEBOUNCE_MS,
): DebouncedTimeSeriesLabelNotifier {
  let handle: ReturnType<typeof setTimeout> | null = null;
  let pending: { selectedTime: number } | null = null;

  const notify = ((selectedTime: number) => {
    pending = { selectedTime };
    if (handle !== null) clearTimeout(handle);
    handle = setTimeout(() => {
      handle = null;
      const p = pending;
      pending = null;
      if (p) callback(p.selectedTime);
    }, delayMs);
  }) as DebouncedTimeSeriesLabelNotifier;

  notify.cancel = () => {
    if (handle !== null) {
      clearTimeout(handle);
      handle = null;
    }
    pending = null;
  };

  return notify;
}
