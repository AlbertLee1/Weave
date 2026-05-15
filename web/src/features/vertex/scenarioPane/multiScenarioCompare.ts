// VTX-047 — 多 Scenario 横向并列对比的纯逻辑层。
//
// 在 VTX-036 ScenarioPane（列定义）+ VTX-045 BaselineRun（baseline 输出与
// compare 派生）之上引入主视图状态 ActiveScenarioState，让：
//   - Pane 同时显示 Baseline + N Scenarios 的多列读数；
//   - System Graph Extended Label 在同一节点上叠加所有列的读数；
//   - 用户点 Scenario 列头切换"主视图"（节点着色按该 Scenario 数据）。
//
// 三件 React 接线层关心的事各自由一组 helper 承担：
//   1. 列定义：getMultiScenarioColumns + setActiveColumnFromHeader；
//   2. 单元格读数：getMultiScenarioPaneRowReadings / getMultiScenarioObjectReadings；
//   3. 节点着色：getNodeColoringSource / getNodeColoringOutputs。

import {
  type BaselineCompareColor,
  type BaselineCompareResult,
  type BaselineOutputValue,
  type BaselineOutputs,
  buildObjectBaselineOutputKey,
  buildPaneBaselineOutputKey,
  computeBaselineCompare,
} from './baselineRun';
import type { ScenarioPaneState } from './scenarioPane';

// ------------- Active scenario state -------------

export interface ActiveScenarioState {
  /** null 表示 Baseline 是主视图。 */
  activeScenarioRid: string | null;
}

export interface CreateActiveScenarioStateInit {
  activeScenarioRid?: string | null;
}

export function createActiveScenarioState(
  init?: CreateActiveScenarioStateInit,
): ActiveScenarioState {
  return { activeScenarioRid: init?.activeScenarioRid ?? null };
}

export function setActiveScenario(
  state: ActiveScenarioState,
  scenarioRid: string | null,
): ActiveScenarioState {
  if (state.activeScenarioRid === scenarioRid) return state;
  return { activeScenarioRid: scenarioRid };
}

export function isBaselineActive(state: ActiveScenarioState): boolean {
  return state.activeScenarioRid === null;
}

export function isScenarioActive(
  state: ActiveScenarioState,
  scenarioRid: string,
): boolean {
  return state.activeScenarioRid === scenarioRid;
}

/**
 * 当 VTX-036 removeScenario 把当前主视图 scenario 删掉时，调用方调本 helper
 * 把 activeScenarioRid 回退到 Baseline，避免悬挂引用导致 Pane 列头全无 active
 * 标记。Baseline 已激活时返回同引用。
 */
export function clearActiveIfMissing(
  state: ActiveScenarioState,
  presentScenarioRids: string[],
): ActiveScenarioState {
  const rid = state.activeScenarioRid;
  if (rid === null) return state;
  if (presentScenarioRids.includes(rid)) return state;
  return { activeScenarioRid: null };
}

// ------------- Column derivation -------------

export type MultiScenarioColumnKind = 'baseline' | 'scenario';

export const BASELINE_COLUMN_KEY = 'baseline';
export const BASELINE_COLUMN_LABEL = 'Baseline';

export interface MultiScenarioColumn {
  key: string;
  label: string;
  kind: MultiScenarioColumnKind;
  scenarioRid?: string;
  isActive: boolean;
}

export function getMultiScenarioColumns(
  paneState: ScenarioPaneState,
  activeState: ActiveScenarioState,
): MultiScenarioColumn[] {
  if (paneState.caseStudy === null) return [];
  const columns: MultiScenarioColumn[] = [
    {
      key: BASELINE_COLUMN_KEY,
      label: BASELINE_COLUMN_LABEL,
      kind: 'baseline',
      isActive: activeState.activeScenarioRid === null,
    },
  ];
  for (const scenario of paneState.scenarios) {
    columns.push({
      key: scenario.rid,
      label: scenario.name,
      kind: 'scenario',
      scenarioRid: scenario.rid,
      isActive: activeState.activeScenarioRid === scenario.rid,
    });
  }
  return columns;
}

/**
 * 列头点击 → 主视图切换。'baseline' 字面值切回 Baseline，其它视为 scenarioRid。
 * 同值返回同引用。
 */
export function setActiveColumnFromHeader(
  state: ActiveScenarioState,
  columnKey: string,
): ActiveScenarioState {
  if (columnKey === BASELINE_COLUMN_KEY) {
    return setActiveScenario(state, null);
  }
  return setActiveScenario(state, columnKey);
}

// ------------- Scenario outputs map -------------

export type ScenarioOutputs = Record<string, BaselineOutputValue>;
export type ScenarioOutputsByRid = Record<string, ScenarioOutputs>;

export function getScenarioOutput(
  outputsByRid: ScenarioOutputsByRid,
  scenarioRid: string,
  key: string,
): BaselineOutputValue {
  const outputs = outputsByRid[scenarioRid];
  if (!outputs) return null;
  return key in outputs ? outputs[key] : null;
}

// ------------- Cell readings -------------

export interface MultiScenarioReading {
  columnKey: string;
  kind: MultiScenarioColumnKind;
  scenarioRid?: string;
  value: BaselineOutputValue;
  /** 与 Baseline 的比较；baseline 列自身或任一侧非有限数时为 null。 */
  compare: BaselineCompareResult | null;
  colorHint: BaselineCompareColor | null;
}

function buildReading(
  column: MultiScenarioColumn,
  value: BaselineOutputValue,
  baselineValue: BaselineOutputValue,
): MultiScenarioReading {
  let compare: BaselineCompareResult | null = null;
  let colorHint: BaselineCompareColor | null = null;
  if (
    column.kind === 'scenario'
    && typeof value === 'number'
    && Number.isFinite(value)
    && typeof baselineValue === 'number'
    && Number.isFinite(baselineValue)
  ) {
    compare = computeBaselineCompare({ simulated: value, baseline: baselineValue });
    colorHint = compare.colorHint;
  }
  const reading: MultiScenarioReading = {
    columnKey: column.key,
    kind: column.kind,
    value,
    compare,
    colorHint,
  };
  if (column.scenarioRid !== undefined) reading.scenarioRid = column.scenarioRid;
  return reading;
}

function lookupBaselineValue(
  baselineOutputs: BaselineOutputs,
  key: string,
): BaselineOutputValue {
  return key in baselineOutputs ? baselineOutputs[key] : null;
}

function buildReadingsForKey(
  columns: MultiScenarioColumn[],
  key: string,
  baselineOutputs: BaselineOutputs,
  scenarioOutputsByRid: ScenarioOutputsByRid,
): MultiScenarioReading[] {
  const baselineValue = lookupBaselineValue(baselineOutputs, key);
  return columns.map((column) => {
    if (column.kind === 'baseline') {
      return buildReading(column, baselineValue, baselineValue);
    }
    const scenarioRid = column.scenarioRid ?? '';
    const value = getScenarioOutput(scenarioOutputsByRid, scenarioRid, key);
    return buildReading(column, value, baselineValue);
  });
}

export interface MultiScenarioPaneRowReadingsInput {
  paneState: ScenarioPaneState;
  activeState: ActiveScenarioState;
  baselineOutputs: BaselineOutputs;
  scenarioOutputsByRid: ScenarioOutputsByRid;
  rowRid: string;
  paramName: string;
}

export function getMultiScenarioPaneRowReadings(
  input: MultiScenarioPaneRowReadingsInput,
): MultiScenarioReading[] {
  const columns = getMultiScenarioColumns(input.paneState, input.activeState);
  if (columns.length === 0) return [];
  const key = buildPaneBaselineOutputKey(input.rowRid, input.paramName);
  return buildReadingsForKey(
    columns,
    key,
    input.baselineOutputs,
    input.scenarioOutputsByRid,
  );
}

export interface MultiScenarioObjectReadingsInput {
  paneState: ScenarioPaneState;
  activeState: ActiveScenarioState;
  baselineOutputs: BaselineOutputs;
  scenarioOutputsByRid: ScenarioOutputsByRid;
  objectType: string;
  primaryKey: string;
  property: string;
}

export function getMultiScenarioObjectReadings(
  input: MultiScenarioObjectReadingsInput,
): MultiScenarioReading[] {
  const columns = getMultiScenarioColumns(input.paneState, input.activeState);
  if (columns.length === 0) return [];
  const key = buildObjectBaselineOutputKey(
    input.objectType,
    input.primaryKey,
    input.property,
  );
  return buildReadingsForKey(
    columns,
    key,
    input.baselineOutputs,
    input.scenarioOutputsByRid,
  );
}

// ------------- Node coloring -------------

export type NodeColoringSource =
  | { kind: 'baseline' }
  | { kind: 'scenario'; scenarioRid: string };

export function getNodeColoringSource(
  state: ActiveScenarioState,
): NodeColoringSource {
  if (state.activeScenarioRid === null) return { kind: 'baseline' };
  return { kind: 'scenario', scenarioRid: state.activeScenarioRid };
}

const EMPTY_OUTPUTS: BaselineOutputs = Object.freeze({}) as BaselineOutputs;

export function getNodeColoringOutputs(input: {
  activeState: ActiveScenarioState;
  baselineOutputs: BaselineOutputs;
  scenarioOutputsByRid: ScenarioOutputsByRid;
}): BaselineOutputs {
  const source = getNodeColoringSource(input.activeState);
  if (source.kind === 'baseline') return input.baselineOutputs;
  return input.scenarioOutputsByRid[source.scenarioRid] ?? EMPTY_OUTPUTS;
}
