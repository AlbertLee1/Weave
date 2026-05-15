// VTX-042 — Override 单元格（时间序列窗口聚合值）的纯逻辑层。
//
// 在 VTX-040 (scalar) / VTX-041 (object property) 之后的第三条 override
// 路径：把标的从 (rowRid, paramName) 或 (objectType, primaryKey, property)
// 换成 (scenarioRid, objectType, primaryKey, property, atTimestamp,
// windowMs)。该 override 仅在所配置时窗内生效 —— 用户拖动 selectedTime
// 到别的时刻，override 不再 apply（spec §5.4：应保存 atTimestamp）。
//
// 本模块交付：
//   - TimeSeriesWindowOverride 类型 + 6 段 cell key
//   - OverrideMap CRUD（不 mutate）+ isHighlighted / findForWindow /
//     applyTimeSeriesWindowOverride 三个派生 helper
//   - POST /api/vertex/v1/scenarios/{rid}/time-series-overrides 和
//     DELETE /api/vertex/v1/time-series-overrides/{id} 请求构造
//   - smoothSeriesByBucket(series, bucketMs) — 5-min smoothing 实现
//   - computeTimeSeriesWindowAggregate(series, params, override?) —
//     "override 优先，否则 smoothing + aggregateAtTime"
//   - resolveTimeSeriesWindowCellEdit — blur 五态决策（与 VTX-040 同构）
//   - assertTimeSeriesWindowCellEditable —— immutable scenario 守卫（复用
//     ScenarioFrozenError）

import {
  aggregateAtTime,
  type AggregationMethod,
  type TimePoint,
} from '../timeSeries/aggregateAtTime';
import { isScenarioImmutable, type ScenarioLike } from './createDialogs';
import { parseScalarInput, ScenarioFrozenError } from './overrideCell';

// 与 pkg/timeseries.AggregationMethod 同名同形（'avg'|'sum'|'max'|'min'|
// 'last'|'count'）—— 前后端共用一组字面值，减少翻译层。
export type TimeSeriesAggregationMethod = AggregationMethod;

export interface TimeSeriesWindowOverride {
  id: string;
  scenarioRid: string;
  objectType: string;
  primaryKey: string;
  property: string;
  atTimestamp: number;
  windowMs: number;
  agg: TimeSeriesAggregationMethod;
  smoothingMs?: number;
  value: number;
}

// 6 段 key：scenarioRid::objectType::primaryKey::property::atTimestamp::
// windowMs。比 VTX-041 多两段 (atTimestamp + windowMs) —— 时窗 override 的
// 身份必须含时窗本身，移动 selectedTime 自然命中不同 key、不同 override。
// agg 故意不进 key：override 表达"用户对该窗的聚合代表值的认定"，与该窗
// 当前显示的 agg 方法解耦；切换 agg view 时仍读同一个 override（"我说这
// 个窗就该是 1500"）。
export type TimeSeriesWindowOverrideMap = Record<string, TimeSeriesWindowOverride>;

export interface TimeSeriesWindowOverrideApiRequest {
  method: 'POST' | 'DELETE';
  path: string;
  body: {
    objectType: string;
    primaryKey: string;
    property: string;
    atTimestamp: number;
    windowMs: number;
    agg: TimeSeriesAggregationMethod;
    smoothingMs?: number;
    value: number;
  } | null;
}

export type TimeSeriesWindowCellEditDecision =
  | { kind: 'noop' }
  | { kind: 'create'; request: TimeSeriesWindowOverrideApiRequest }
  | { kind: 'update'; previousId: string; request: TimeSeriesWindowOverrideApiRequest }
  | { kind: 'delete'; previousId: string; request: TimeSeriesWindowOverrideApiRequest }
  | { kind: 'invalid'; reason: 'not_a_number' };

export interface WindowQuery {
  scenarioRid: string;
  objectType: string;
  primaryKey: string;
  property: string;
  atTimestamp: number;
  windowMs: number;
}

export interface ResolveTimeSeriesWindowCellEditInput extends WindowQuery {
  agg: TimeSeriesAggregationMethod;
  smoothingMs?: number;
  rawInput: string;
  existing: TimeSeriesWindowOverride | null;
}

export interface ComputeWindowAggregateParams {
  selectedTime: number;
  windowMs: number;
  agg: TimeSeriesAggregationMethod;
  smoothingMs?: number;
}

function requireNonBlank(value: string, field: string): string {
  if (typeof value !== 'string' || value.trim().length === 0) {
    throw new Error(`${field} is required`);
  }
  return value.trim();
}

function requirePositive(value: number, field: string): number {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) {
    throw new Error(`${field} must be a positive finite number`);
  }
  return value;
}

function requireFiniteNumber(value: number, field: string): number {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    throw new Error(`${field} must be a finite number`);
  }
  return value;
}

export function buildTimeSeriesWindowOverrideKey(
  scenarioRid: string,
  objectType: string,
  primaryKey: string,
  property: string,
  atTimestamp: number,
  windowMs: number,
): string {
  return `${scenarioRid}::${objectType}::${primaryKey}::${property}::${atTimestamp}::${windowMs}`;
}

export function getTimeSeriesWindowOverride(
  map: TimeSeriesWindowOverrideMap,
  key: string,
): TimeSeriesWindowOverride | null {
  return map[key] ?? null;
}

export function setTimeSeriesWindowOverride(
  map: TimeSeriesWindowOverrideMap,
  override: TimeSeriesWindowOverride,
): TimeSeriesWindowOverrideMap {
  const key = buildTimeSeriesWindowOverrideKey(
    override.scenarioRid,
    override.objectType,
    override.primaryKey,
    override.property,
    override.atTimestamp,
    override.windowMs,
  );
  return { ...map, [key]: override };
}

export function removeTimeSeriesWindowOverride(
  map: TimeSeriesWindowOverrideMap,
  key: string,
): TimeSeriesWindowOverrideMap {
  if (!(key in map)) return map;
  const next = { ...map };
  delete next[key];
  return next;
}

export function isTimeSeriesWindowCellHighlighted(
  map: TimeSeriesWindowOverrideMap,
  key: string,
): boolean {
  return key in map;
}

// findTimeSeriesWindowOverride —— 给定窗口查询，反查 map 里是否有 override。
// 等价于 getTimeSeriesWindowOverride(map, buildKey(...))，但调用方不必自
// 组装 key。React 接线层渲染 Pane 单元格时先调此 helper 决定值/边框。
export function findTimeSeriesWindowOverride(
  map: TimeSeriesWindowOverrideMap,
  q: WindowQuery,
): TimeSeriesWindowOverride | null {
  const key = buildTimeSeriesWindowOverrideKey(
    q.scenarioRid,
    q.objectType,
    q.primaryKey,
    q.property,
    q.atTimestamp,
    q.windowMs,
  );
  return getTimeSeriesWindowOverride(map, key);
}

// applyTimeSeriesWindowOverride —— 客户端 read overlay：给定窗口查询返回
// override 的 scalar，否则 null。语义"override 仅对配置时窗生效"由 6 段
// key 隐式守住：query 的 atTimestamp / windowMs 必须与 override 完全匹配
// 才命中。React 接线层算 sparkline / extended label 时用此 helper 决定
// "显示 override 还是计算值"。
export function applyTimeSeriesWindowOverride(
  map: TimeSeriesWindowOverrideMap,
  q: WindowQuery,
): number | null {
  const found = findTimeSeriesWindowOverride(map, q);
  return found === null ? null : found.value;
}

export function assertTimeSeriesWindowCellEditable(scenario: ScenarioLike): void {
  if (isScenarioImmutable(scenario)) {
    throw new ScenarioFrozenError(scenario.rid);
  }
}

export function buildCreateTimeSeriesWindowOverrideRequest(input: {
  scenarioRid: string;
  objectType: string;
  primaryKey: string;
  property: string;
  atTimestamp: number;
  windowMs: number;
  agg: TimeSeriesAggregationMethod;
  smoothingMs?: number;
  value: number;
}): TimeSeriesWindowOverrideApiRequest {
  const scenarioRid = requireNonBlank(input.scenarioRid, 'scenarioRid');
  const objectType = requireNonBlank(input.objectType, 'objectType');
  const primaryKey = requireNonBlank(input.primaryKey, 'primaryKey');
  const property = requireNonBlank(input.property, 'property');
  requireFiniteNumber(input.atTimestamp, 'atTimestamp');
  requirePositive(input.windowMs, 'windowMs');
  requireFiniteNumber(input.value, 'value');
  if (input.smoothingMs !== undefined) {
    requirePositive(input.smoothingMs, 'smoothingMs');
  }
  const body: TimeSeriesWindowOverrideApiRequest['body'] = {
    objectType,
    primaryKey,
    property,
    atTimestamp: input.atTimestamp,
    windowMs: input.windowMs,
    agg: input.agg,
    value: input.value,
  };
  if (input.smoothingMs !== undefined) {
    body.smoothingMs = input.smoothingMs;
  }
  return {
    method: 'POST',
    path: `/api/vertex/v1/scenarios/${encodeURIComponent(scenarioRid)}/time-series-overrides`,
    body,
  };
}

export function buildDeleteTimeSeriesWindowOverrideRequest(
  overrideId: string,
): TimeSeriesWindowOverrideApiRequest {
  const id = requireNonBlank(overrideId, 'overrideId');
  return {
    method: 'DELETE',
    path: `/api/vertex/v1/time-series-overrides/${encodeURIComponent(id)}`,
    body: null,
  };
}

// smoothSeriesByBucket —— 5-min smoothing：按 bucketMs 把原始点 floor 到
// 桶起点，桶内取 avg，每桶吐一个 (bucketStart, avg) 点。结果按桶起点升序。
// bucketMs ≤ 0 时不做 smoothing，返回原序列副本（保持纯逻辑：不引用同一
// 对象）。
export function smoothSeriesByBucket(
  series: readonly TimePoint[],
  bucketMs: number,
): TimePoint[] {
  if (!Number.isFinite(bucketMs) || bucketMs <= 0) {
    return series.slice();
  }
  if (series.length === 0) return [];
  const buckets = new Map<number, { sum: number; count: number }>();
  for (const p of series) {
    const bucketStart = Math.floor(p.t / bucketMs) * bucketMs;
    const cell = buckets.get(bucketStart);
    if (cell === undefined) {
      buckets.set(bucketStart, { sum: p.v, count: 1 });
    } else {
      cell.sum += p.v;
      cell.count += 1;
    }
  }
  const out: TimePoint[] = [];
  for (const [t, cell] of buckets) {
    out.push({ t, v: cell.sum / cell.count });
  }
  out.sort((a, b) => a.t - b.t);
  return out;
}

// computeTimeSeriesWindowAggregate —— Pane 单元格 / sparkline / extended
// label / 模型 input 的统一聚合入口：
//   override 存在 → 直接返回 override.value（spec §5.4 模型 input 用 override）
//   smoothing > 0 → 先桶化再 aggregateAtTime
//   都没有 → 直接 aggregateAtTime
export function computeTimeSeriesWindowAggregate(
  series: readonly TimePoint[],
  params: ComputeWindowAggregateParams,
  override: TimeSeriesWindowOverride | null,
): number | null {
  if (override !== null) return override.value;
  const points = params.smoothingMs !== undefined && params.smoothingMs > 0
    ? smoothSeriesByBucket(series, params.smoothingMs)
    : series.slice();
  return aggregateAtTime(points, {
    selectedTime: params.selectedTime,
    windowMs: params.windowMs,
    agg: params.agg,
  });
}

// resolveTimeSeriesWindowCellEdit —— Pane cell blur 时五态决策（与 VTX-040
// resolveCellEdit / VTX-041 resolveObjectPropertyCellEdit 同构）。
// 时窗 override 仅接受 number value，故 valueType 固定 'number'，invalid
// reason 只能是 'not_a_number'。
//   - invalid: rawInput 解析失败（NaN/Infinity/字母）→ 上层 toast + 回滚
//     原 cell 显示，**不静默丢 existing**
//   - empty + existing → delete + DELETE 请求
//   - empty + null → noop
//   - value + null → create + POST
//   - value + existing 相同 → noop（避免冗余 POST）
//   - value + existing 不同 → update + POST（服务端 upsert，复用 create
//     builder，区分仅给上层 UX 文案）
export function resolveTimeSeriesWindowCellEdit(
  input: ResolveTimeSeriesWindowCellEditInput,
): TimeSeriesWindowCellEditDecision {
  const parsed = parseScalarInput(input.rawInput, 'number');
  if (!parsed.ok) {
    return { kind: 'invalid', reason: 'not_a_number' };
  }

  if (parsed.empty) {
    if (input.existing === null) return { kind: 'noop' };
    return {
      kind: 'delete',
      previousId: input.existing.id,
      request: buildDeleteTimeSeriesWindowOverrideRequest(input.existing.id),
    };
  }

  const value = parsed.value as number;

  if (input.existing === null) {
    return {
      kind: 'create',
      request: buildCreateTimeSeriesWindowOverrideRequest({
        scenarioRid: input.scenarioRid,
        objectType: input.objectType,
        primaryKey: input.primaryKey,
        property: input.property,
        atTimestamp: input.atTimestamp,
        windowMs: input.windowMs,
        agg: input.agg,
        smoothingMs: input.smoothingMs,
        value,
      }),
    };
  }

  if (input.existing.value === value) {
    return { kind: 'noop' };
  }

  return {
    kind: 'update',
    previousId: input.existing.id,
    request: buildCreateTimeSeriesWindowOverrideRequest({
      scenarioRid: input.scenarioRid,
      objectType: input.objectType,
      primaryKey: input.primaryKey,
      property: input.property,
      atTimestamp: input.atTimestamp,
      windowMs: input.windowMs,
      agg: input.agg,
      smoothingMs: input.smoothingMs,
      value,
    }),
  };
}
