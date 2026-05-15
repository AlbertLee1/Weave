// VTX-040 — Override 单元格（标量）的纯逻辑层。
//
// 驱动 Scenario Pane 表 input 单元格的 click → edit → blur 流程：
//   - 把 raw text 输入按 scalar 类型解析（string / number / boolean）
//   - 与 existing override 对比，决定 noop / create / update / delete
//   - 生成 POST /api/vertex/v1/scenarios/{rid}/overrides 或
//     DELETE /api/vertex/v1/overrides/{id} 请求
//   - 暴露"单元格是否高亮（黄色边框）"和"scenario 冻结时禁用 + tooltip"
//     两个派生 helper 给 React 渲染层
//
// 模块内仅保存 OverrideMap（cell key → ScalarOverride），server 返回的
// override.id 由调用方喂回 setOverride，本模块不发起 fetch。

import { isScenarioImmutable, type ScenarioLike } from './createDialogs';

// 调研报告 §4.3 只覆盖 scalar 三类：string / number / boolean；其余
// （array / object）留给后续 story（VTX-041 object property 一类已经
// 偏离 scalar 范畴，会走另一条 override 路径）。
export type ScalarOverrideValue = string | number | boolean;
export type ScalarValueType = 'string' | 'number' | 'boolean';

export interface ScalarOverride {
  id: string;
  scenarioRid: string;
  rowRid: string;
  paramName: string;
  value: ScalarOverrideValue;
}

// OverrideMap 以 `${scenarioRid}::${rowRid}::${paramName}` 为 key。React
// 接线层在用户首次打开 Scenario 时调 GET /api/vertex/v1/scenarios/{rid}/
// overrides 拉一次，存进 Zustand store；之后每次 cell blur 都先用
// resolveCellEdit 决策，再调对应 API，最后用 setOverride / removeOverride
// 更新本地 map。
export type OverrideMap = Record<string, ScalarOverride>;

// 单元格 disabled 时的 tooltip 文案。spec：scenario.immutable=true →
// 单元格禁用 + tooltip scenario is frozen。
export const FROZEN_SCENARIO_TOOLTIP = 'Scenario is frozen.';

export interface OverrideApiRequest {
  method: 'POST' | 'DELETE';
  path: string;
  body: { rowRid: string; paramName: string; value: ScalarOverrideValue } | null;
}

export type ParseScalarResult =
  | { ok: true; empty: true }
  | { ok: true; empty: false; value: ScalarOverrideValue }
  | { ok: false; reason: 'not_a_number' | 'not_a_boolean' };

export type CellEditDecision =
  | { kind: 'noop' }
  | { kind: 'create'; request: OverrideApiRequest }
  | { kind: 'update'; previousId: string; request: OverrideApiRequest }
  | { kind: 'delete'; previousId: string; request: OverrideApiRequest }
  | { kind: 'invalid'; reason: 'not_a_number' | 'not_a_boolean' };

export interface ResolveCellEditInput {
  scenarioRid: string;
  rowRid: string;
  paramName: string;
  rawInput: string;
  valueType: ScalarValueType;
  existing: ScalarOverride | null;
}

export class ScenarioFrozenError extends Error {
  readonly scenarioRid: string;
  constructor(scenarioRid: string) {
    super(FROZEN_SCENARIO_TOOLTIP);
    this.name = 'ScenarioFrozenError';
    this.scenarioRid = scenarioRid;
  }
}

function requireNonBlank(value: string, field: string): string {
  if (typeof value !== 'string' || value.trim().length === 0) {
    throw new Error(`${field} is required`);
  }
  return value.trim();
}

// buildOverrideCellKey 用三段组合键。与 VTX-039
// buildInputOutputColumnKey 不同的是这里额外把 scenarioRid 也拼进去
// —— Pane 同时显示多个 Scenario 列，不同 scenario 的同名 cell 不能共用
// 一个 override 槽位。
export function buildOverrideCellKey(
  scenarioRid: string,
  rowRid: string,
  paramName: string,
): string {
  return `${scenarioRid}::${rowRid}::${paramName}`;
}

export function getOverride(map: OverrideMap, key: string): ScalarOverride | null {
  return map[key] ?? null;
}

export function setOverride(map: OverrideMap, override: ScalarOverride): OverrideMap {
  const key = buildOverrideCellKey(
    override.scenarioRid,
    override.rowRid,
    override.paramName,
  );
  return { ...map, [key]: override };
}

export function removeOverride(map: OverrideMap, key: string): OverrideMap {
  if (!(key in map)) return map;
  const next = { ...map };
  delete next[key];
  return next;
}

// isCellHighlighted —— Pane 单元格"已有 override → 黄色边框"。React 层
// 直接用此 bool 切 className（border-yellow-400 / 无边框）。
export function isCellHighlighted(map: OverrideMap, key: string): boolean {
  return key in map;
}

export function isCellDisabled(scenario: ScenarioLike): boolean {
  return isScenarioImmutable(scenario);
}

// getCellTooltip —— immutable scenario 返回 FROZEN_SCENARIO_TOOLTIP，否则
// 返回 null（不显示 tooltip）。Pane React 层用 <td title={tooltip ?? ''}>
// 或 Radix Tooltip 包裹。
export function getCellTooltip(scenario: ScenarioLike): string | null {
  return isScenarioImmutable(scenario) ? FROZEN_SCENARIO_TOOLTIP : null;
}

// assertCellEditable —— 用户输入路径上的最后防线。React 层 button
// disabled + tooltip 是软提示，键盘 Enter / programmatic mutate 可能绕过；
// 调 fetch 之前再 assert 一次保证不写入 frozen scenario。
export function assertCellEditable(scenario: ScenarioLike): void {
  if (isScenarioImmutable(scenario)) {
    throw new ScenarioFrozenError(scenario.rid);
  }
}

// parseScalarInput —— 把 raw text input 按 valueType 解析。
//   - trim 后为空 → { ok: true, empty: true }（调用方据此决定 delete 还是 noop）
//   - string → 直接透传 trim 结果
//   - number → Number(trimmed)；NaN / Infinity / -Infinity 视为 invalid
//   - boolean → 字面值 "true" / "false"（大小写不敏感）
// 不接受 0/1 当 boolean、不接受 "1.5e10" 之外的科学计数法外的奇异写法；
// 解析失败给 not_a_number / not_a_boolean，调用方喂给 toast 提示用户。
export function parseScalarInput(
  raw: string,
  valueType: ScalarValueType,
): ParseScalarResult {
  const text = typeof raw === 'string' ? raw.trim() : '';
  if (text.length === 0) return { ok: true, empty: true };

  switch (valueType) {
    case 'string':
      return { ok: true, empty: false, value: text };
    case 'number': {
      const n = Number(text);
      if (!Number.isFinite(n)) {
        return { ok: false, reason: 'not_a_number' };
      }
      return { ok: true, empty: false, value: n };
    }
    case 'boolean': {
      const lower = text.toLowerCase();
      if (lower === 'true') return { ok: true, empty: false, value: true };
      if (lower === 'false') return { ok: true, empty: false, value: false };
      return { ok: false, reason: 'not_a_boolean' };
    }
  }
}

export function buildCreateOverrideRequest(input: {
  scenarioRid: string;
  rowRid: string;
  paramName: string;
  value: ScalarOverrideValue;
}): OverrideApiRequest {
  const scenarioRid = requireNonBlank(input.scenarioRid, 'scenarioRid');
  const rowRid = requireNonBlank(input.rowRid, 'rowRid');
  const paramName = requireNonBlank(input.paramName, 'paramName');
  return {
    method: 'POST',
    path: `/api/vertex/v1/scenarios/${encodeURIComponent(scenarioRid)}/overrides`,
    body: { rowRid, paramName, value: input.value },
  };
}

export function buildDeleteOverrideRequest(overrideId: string): OverrideApiRequest {
  const id = requireNonBlank(overrideId, 'overrideId');
  return {
    method: 'DELETE',
    path: `/api/vertex/v1/overrides/${encodeURIComponent(id)}`,
    body: null,
  };
}

// resolveCellEdit —— 单元格 blur 时的决策中枢。
//   - 输入解析失败 → invalid（React 层 toast 错误、保留原单元格值不动）
//   - empty input：
//       existing == null → noop
//       existing != null → delete + DELETE 请求
//   - non-empty input：
//       existing == null → create + POST
//       existing != null && value 未变 → noop（避免冗余写）
//       existing != null && value 变了 → update + POST（服务端 upsert）
// "update" 和 "create" 都走 POST，区分仅给上层做 UX 文案（"Override
// updated" vs "Override added"）和分析埋点。
export function resolveCellEdit(input: ResolveCellEditInput): CellEditDecision {
  const parsed = parseScalarInput(input.rawInput, input.valueType);
  if (!parsed.ok) {
    return { kind: 'invalid', reason: parsed.reason };
  }

  if (parsed.empty) {
    if (input.existing === null) return { kind: 'noop' };
    return {
      kind: 'delete',
      previousId: input.existing.id,
      request: buildDeleteOverrideRequest(input.existing.id),
    };
  }

  if (input.existing === null) {
    return {
      kind: 'create',
      request: buildCreateOverrideRequest({
        scenarioRid: input.scenarioRid,
        rowRid: input.rowRid,
        paramName: input.paramName,
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
    request: buildCreateOverrideRequest({
      scenarioRid: input.scenarioRid,
      rowRid: input.rowRid,
      paramName: input.paramName,
      value: parsed.value,
    }),
  };
}
