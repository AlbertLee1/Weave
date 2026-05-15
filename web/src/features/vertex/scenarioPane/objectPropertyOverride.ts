// VTX-041 — Override 单元格（对象属性）的纯逻辑层。
//
// 扩展 VTX-040 的 scalar override 概念，把标的从 (rowRid, paramName) 换成
// (objectType, primaryKey, property)。该 override 仅在 fork（scenario）内
// 生效：base ontology 查询不变；带 scenarioId 的查询返回 fork overlay。
//
// 本模块交付：
//   - ObjectPropertyOverride 类型 + 4 段 cell key
//   - OverrideMap CRUD（不 mutate）+ isHighlighted 派生
//   - POST /api/vertex/v1/scenarios/{rid}/object-property-overrides
//     和 DELETE /api/vertex/v1/object-property-overrides/{id} 请求构造
//   - withScenarioId(path, scenarioRid?) —— GET /objects 查询参数注入
//   - applyObjectPropertyOverrides(obj, overrides) —— 客户端 read overlay
//   - resolveObjectPropertyCellEdit —— blur 时五态决策（与 VTX-040 同构）
//   - assertObjectPropertyCellEditable —— immutable scenario 守卫（复用
//     ScenarioFrozenError）

import { isScenarioImmutable, type ScenarioLike } from './createDialogs';
import {
  parseScalarInput,
  ScenarioFrozenError,
  type ScalarOverrideValue,
  type ScalarValueType,
} from './overrideCell';

export interface ObjectPropertyOverride {
  id: string;
  scenarioRid: string;
  objectType: string;
  primaryKey: string;
  property: string;
  value: ScalarOverrideValue;
}

// 4 段 key：scenarioRid::objectType::primaryKey::property。Pane 同时显示
// 多 scenario 列，每对 (objectType, pk, prop) 在每个 scenario 下都是独立
// 槽位，scenarioRid 必须进 key 否则跨列覆盖。
export type ObjectPropertyOverrideMap = Record<string, ObjectPropertyOverride>;

export interface ObjectPropertyOverrideApiRequest {
  method: 'POST' | 'DELETE';
  path: string;
  body: {
    objectType: string;
    primaryKey: string;
    property: string;
    value: ScalarOverrideValue;
  } | null;
}

export type ObjectPropertyCellEditDecision =
  | { kind: 'noop' }
  | { kind: 'create'; request: ObjectPropertyOverrideApiRequest }
  | { kind: 'update'; previousId: string; request: ObjectPropertyOverrideApiRequest }
  | { kind: 'delete'; previousId: string; request: ObjectPropertyOverrideApiRequest }
  | { kind: 'invalid'; reason: 'not_a_number' | 'not_a_boolean' };

export interface ResolveObjectPropertyCellEditInput {
  scenarioRid: string;
  objectType: string;
  primaryKey: string;
  property: string;
  rawInput: string;
  valueType: ScalarValueType;
  existing: ObjectPropertyOverride | null;
}

// WireObjectLike 是 web/src/api/types.ts WireObject 的最小投影：保留 3 个
// __meta 字段 + 任意 property bag。模块内不强绑 WireObject 类型避免循环
// 依赖（applyObjectPropertyOverrides 是给 React 接线层 + SDK 都可用的纯
// 工具）。
export interface WireObjectLike {
  __rid: string;
  __primaryKey: string | number;
  __apiName: string;
  [property: string]: unknown;
}

function requireNonBlank(value: string, field: string): string {
  if (typeof value !== 'string' || value.trim().length === 0) {
    throw new Error(`${field} is required`);
  }
  return value.trim();
}

export function buildObjectPropertyOverrideKey(
  scenarioRid: string,
  objectType: string,
  primaryKey: string,
  property: string,
): string {
  return `${scenarioRid}::${objectType}::${primaryKey}::${property}`;
}

export function getObjectPropertyOverride(
  map: ObjectPropertyOverrideMap,
  key: string,
): ObjectPropertyOverride | null {
  return map[key] ?? null;
}

export function setObjectPropertyOverride(
  map: ObjectPropertyOverrideMap,
  override: ObjectPropertyOverride,
): ObjectPropertyOverrideMap {
  const key = buildObjectPropertyOverrideKey(
    override.scenarioRid,
    override.objectType,
    override.primaryKey,
    override.property,
  );
  return { ...map, [key]: override };
}

export function removeObjectPropertyOverride(
  map: ObjectPropertyOverrideMap,
  key: string,
): ObjectPropertyOverrideMap {
  if (!(key in map)) return map;
  const next = { ...map };
  delete next[key];
  return next;
}

export function isObjectPropertyCellHighlighted(
  map: ObjectPropertyOverrideMap,
  key: string,
): boolean {
  return key in map;
}

// assertObjectPropertyCellEditable —— 写入路径上的最后防线。immutable
// scenario 不能再加新 override；React disabled 是软提示，键盘 / 直调
// mutate 可能绕过，调 fetch 前再 assert 一次。错误类型沿用 VTX-040 的
// ScenarioFrozenError（同一类错误共享 catch 路径，UX 给同样的 tooltip）。
export function assertObjectPropertyCellEditable(scenario: ScenarioLike): void {
  if (isScenarioImmutable(scenario)) {
    throw new ScenarioFrozenError(scenario.rid);
  }
}

export function buildCreateObjectPropertyOverrideRequest(input: {
  scenarioRid: string;
  objectType: string;
  primaryKey: string;
  property: string;
  value: ScalarOverrideValue;
}): ObjectPropertyOverrideApiRequest {
  const scenarioRid = requireNonBlank(input.scenarioRid, 'scenarioRid');
  const objectType = requireNonBlank(input.objectType, 'objectType');
  const primaryKey = requireNonBlank(input.primaryKey, 'primaryKey');
  const property = requireNonBlank(input.property, 'property');
  return {
    method: 'POST',
    path: `/api/vertex/v1/scenarios/${encodeURIComponent(scenarioRid)}/object-property-overrides`,
    body: { objectType, primaryKey, property, value: input.value },
  };
}

export function buildDeleteObjectPropertyOverrideRequest(
  overrideId: string,
): ObjectPropertyOverrideApiRequest {
  const id = requireNonBlank(overrideId, 'overrideId');
  return {
    method: 'DELETE',
    path: `/api/vertex/v1/object-property-overrides/${encodeURIComponent(id)}`,
    body: null,
  };
}

// withScenarioId —— 在 GET /objects/... URL 后追加 scenarioId 查询参数让
// 服务端按 fork overlay 返回；scenarioRid 为 null/undefined/blank 时原样
// 返回（无 overlay = base ontology）。已有 ? 时追加 &，没有时追加 ?。
export function withScenarioId(path: string, scenarioRid: string | null | undefined): string {
  if (typeof scenarioRid !== 'string' || scenarioRid.trim().length === 0) {
    return path;
  }
  const trimmed = scenarioRid.trim();
  const sep = path.includes('?') ? '&' : '?';
  return `${path}${sep}scenarioId=${encodeURIComponent(trimmed)}`;
}

// applyObjectPropertyOverrides —— 客户端 read overlay：给一个 base
// WireObject + 该 scenario 的 overrides 列表，返回应用 override 后的副本。
// 不 mutate 原对象（保留 base ontology 不变的不变式 in-memory）。
// 匹配规则：override.objectType === obj.__apiName 且
// String(obj.__primaryKey) === override.primaryKey（primaryKey 可能是
// number，统一字符串化对齐）。
export function applyObjectPropertyOverrides(
  obj: WireObjectLike,
  overrides: readonly ObjectPropertyOverride[],
): WireObjectLike {
  const result: WireObjectLike = { ...obj };
  for (const ovr of overrides) {
    if (ovr.objectType !== obj.__apiName) continue;
    if (String(obj.__primaryKey) !== ovr.primaryKey) continue;
    result[ovr.property] = ovr.value;
  }
  return result;
}

// resolveObjectPropertyCellEdit —— 单元格 blur 时五态决策中枢，与 VTX-040
// resolveCellEdit 同构（noop / create / update / delete / invalid）。语义
// 重点：invalid 不静默丢 existing；value 未变 → noop 避免冗余 POST。
export function resolveObjectPropertyCellEdit(
  input: ResolveObjectPropertyCellEditInput,
): ObjectPropertyCellEditDecision {
  const parsed = parseScalarInput(input.rawInput, input.valueType);
  if (!parsed.ok) {
    return { kind: 'invalid', reason: parsed.reason };
  }

  if (parsed.empty) {
    if (input.existing === null) return { kind: 'noop' };
    return {
      kind: 'delete',
      previousId: input.existing.id,
      request: buildDeleteObjectPropertyOverrideRequest(input.existing.id),
    };
  }

  if (input.existing === null) {
    return {
      kind: 'create',
      request: buildCreateObjectPropertyOverrideRequest({
        scenarioRid: input.scenarioRid,
        objectType: input.objectType,
        primaryKey: input.primaryKey,
        property: input.property,
        value: parsed.value,
      }),
    };
  }

  if (input.existing.value === parsed.value) {
    return { kind: 'noop' };
  }

  return {
    kind: 'update',
    previousId: input.existing.id,
    request: buildCreateObjectPropertyOverrideRequest({
      scenarioRid: input.scenarioRid,
      objectType: input.objectType,
      primaryKey: input.primaryKey,
      property: input.property,
      value: parsed.value,
    }),
  };
}
