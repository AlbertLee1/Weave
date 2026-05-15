// VTX-039 — + Add input or output（参数勾选）的纯逻辑层。
//
// 驱动 Scenario Pane 的 "+ Add input or output" 对话框：用户选定某个
// Action/Model 行后，本模块按其 parameters 列出候选源（time series /
// object property / measure），用户可逐个勾选或一键 Add all parameters；
// 对 Function-backed Action，parameters 的 autoBound 让 source 在 open
// 时即预填，无需用户手动绑定（调研报告 §3.2）。React 接线层负责把
// dialog state 串到 Modal + checkbox 列表，再用 buildInputOutputColumns
// 把 Apply 出来的 ScenarioPaneInputOutputColumn[] 喂给
// scenarioPane.addInputOutputColumn。

import type { ScenarioPaneInputOutputColumn } from './scenarioPane';

export type ParameterSourceKind = 'time_series' | 'object_property' | 'measure';

export interface ParameterSource {
  kind: ParameterSourceKind;
  // sourceRid 形态因 kind 而异：time_series 是 ri.timeseries.{...}，
  // object_property 是 {ObjectType}.{propertyName}，measure 是
  // ri.functions.{...}.measure.{...}。本模块不做语义校验，只透传。
  rid: string;
  label: string;
}

export interface AddInputOutputParameter {
  name: string;
  displayName?: string;
  direction: 'input' | 'output';
  required?: boolean;
  candidateSources: ParameterSource[];
  // autoBound 仅在 Function-backed Action 上提供：dialog 打开时即把它
  // 写入 selections[name].source，用户随手勾选即可 Apply，无需手动选源。
  autoBound?: ParameterSource;
}

export interface AddInputOutputRowRef {
  rowRid: string;
  kind: 'action' | 'model';
  label: string;
  parameters: AddInputOutputParameter[];
}

export interface ParamSelection {
  checked: boolean;
  source: ParameterSource | null;
}

export interface AddInputOutputDialogState {
  open: boolean;
  scenarioRid: string | null;
  row: AddInputOutputRowRef | null;
  // selections 是参数维度的 map，key=参数名。即使 row 切换后我们也会
  // 全量重建，避免幽灵字段。
  selections: Record<string, ParamSelection>;
  submitting: boolean;
  error: string | null;
}

export type AddInputOutputValidation =
  | { valid: true }
  | { valid: false; reason: 'not_open' }
  | { valid: false; reason: 'no_params_checked' }
  | { valid: false; reason: 'unbound_param'; param: string };

function requireNonBlank(value: string, field: string): string {
  if (typeof value !== 'string' || value.trim().length === 0) {
    throw new Error(`${field} is required`);
  }
  return value.trim();
}

export function createAddInputOutputDialogState(): AddInputOutputDialogState {
  return {
    open: false,
    scenarioRid: null,
    row: null,
    selections: {},
    submitting: false,
    error: null,
  };
}

// open 不读 prior state —— 每次打开都把 selections 重建一次：所有参数
// checked=false，source 优先取 param.autoBound（Function-backed 自动关联），
// 否则 null。这样标准 Action 的源由用户手填，Function-backed Action 一键
// 勾选即可 Apply。
export function openAddInputOutputDialog(
  scenarioRid: string,
  row: AddInputOutputRowRef,
): AddInputOutputDialogState {
  const rid = requireNonBlank(scenarioRid, 'scenario rid');
  const selections: Record<string, ParamSelection> = {};
  for (const param of row.parameters) {
    selections[param.name] = {
      checked: false,
      source: param.autoBound ?? null,
    };
  }
  return {
    open: true,
    scenarioRid: rid,
    row,
    selections,
    submitting: false,
    error: null,
  };
}

export function closeAddInputOutputDialog(): AddInputOutputDialogState {
  return createAddInputOutputDialogState();
}

function hasParam(state: AddInputOutputDialogState, name: string): boolean {
  return state.row !== null && Object.prototype.hasOwnProperty.call(state.selections, name);
}

// toggleAddInputOutputParam 翻转单个参数的 checked，不动 source（用户
// 撤销勾选时保留已选的 source，再次勾选无需重新选源）。未知参数 / 未
// open 直接返回同引用，让 React useMemo 不会重渲。
export function toggleAddInputOutputParam(
  state: AddInputOutputDialogState,
  name: string,
): AddInputOutputDialogState {
  if (!hasParam(state, name)) return state;
  const prev = state.selections[name];
  return {
    ...state,
    selections: {
      ...state.selections,
      [name]: { ...prev, checked: !prev.checked },
    },
  };
}

export function setAddInputOutputParamSource(
  state: AddInputOutputDialogState,
  name: string,
  source: ParameterSource | null,
): AddInputOutputDialogState {
  if (!hasParam(state, name)) return state;
  const prev = state.selections[name];
  return {
    ...state,
    selections: {
      ...state.selections,
      [name]: { ...prev, source },
    },
  };
}

// setAddInputOutputAllChecked 一键 check/uncheck 全部参数。check 不动
// source（保留 autoBound / 之前手填的源），uncheck 也保留 source 以便
// 用户改主意时回来不需要重新选。未 open 返回同引用。
export function setAddInputOutputAllChecked(
  state: AddInputOutputDialogState,
  checked: boolean,
): AddInputOutputDialogState {
  if (state.row === null) return state;
  const selections: Record<string, ParamSelection> = {};
  for (const param of state.row.parameters) {
    const prev = state.selections[param.name] ?? { checked: false, source: null };
    selections[param.name] = { ...prev, checked };
  }
  return { ...state, selections };
}

export function setAddInputOutputSubmitting(
  state: AddInputOutputDialogState,
  submitting: boolean,
): AddInputOutputDialogState {
  if (submitting) {
    return { ...state, submitting: true, error: null };
  }
  return { ...state, submitting: false };
}

export function setAddInputOutputError(
  state: AddInputOutputDialogState,
  error: string | null,
): AddInputOutputDialogState {
  return { ...state, error, submitting: false };
}

// getCheckedParamNames 返回参数名数组，顺序与 row.parameters 一致（用户
// Apply 后 Pane 列也按这个顺序加进去，避免每次勾选顺序不同导致列乱序）。
export function getCheckedParamNames(state: AddInputOutputDialogState): string[] {
  if (state.row === null) return [];
  return state.row.parameters
    .map(p => p.name)
    .filter(name => state.selections[name]?.checked === true);
}

export function isAllChecked(state: AddInputOutputDialogState): boolean {
  if (state.row === null || state.row.parameters.length === 0) return false;
  return state.row.parameters.every(p => state.selections[p.name]?.checked === true);
}

export function isAllUnchecked(state: AddInputOutputDialogState): boolean {
  if (state.row === null) return true;
  return state.row.parameters.every(p => state.selections[p.name]?.checked !== true);
}

export function validateAddInputOutput(
  state: AddInputOutputDialogState,
): AddInputOutputValidation {
  if (!state.open || state.row === null) {
    return { valid: false, reason: 'not_open' };
  }
  const checked = getCheckedParamNames(state);
  if (checked.length === 0) {
    return { valid: false, reason: 'no_params_checked' };
  }
  for (const name of checked) {
    if (state.selections[name].source === null) {
      return { valid: false, reason: 'unbound_param', param: name };
    }
  }
  return { valid: true };
}

// buildInputOutputColumnKey 用 `${rowRid}::${paramName}` 组合键作为 Pane
// 的 input/output 列 key，避免不同 Action/Model 同名参数（e.g. 多个
// model 都叫 demand）冲撞。React 接线层渲染单元格时用此 key 反查 row +
// param 拿到 source binding。
export function buildInputOutputColumnKey(rowRid: string, paramName: string): string {
  return `${rowRid}::${paramName}`;
}

export function buildInputOutputColumns(
  state: AddInputOutputDialogState,
): ScenarioPaneInputOutputColumn[] {
  const v = validateAddInputOutput(state);
  if (!v.valid) {
    throw new Error(`cannot build columns: ${v.reason}`);
  }
  // validate 通过保证 state.row 非空，但 TS narrowing 不跨函数边界，所以
  // 这里再断言一次。
  const row = state.row;
  if (row === null) {
    throw new Error('cannot build columns: row missing');
  }
  const checkedNames = new Set(getCheckedParamNames(state));
  return row.parameters
    .filter(p => checkedNames.has(p.name))
    .map(p => ({
      key: buildInputOutputColumnKey(row.rowRid, p.name),
      label: p.displayName ?? p.name,
    }));
}
