// VTX-038 — + Add Action（Function-backed Action 选择）的纯逻辑层。
//
// 状态模型驱动 Scenario Pane 的 "+ Add Action" 对话框：列出当前 ontology
// 已发布的 Action（包含 function-backed 与普通），让用户选定一个、填参，
// 然后转成 ScenarioPaneActionRow 加到 Scenario。本模块仅承担纯状态 +
// 派生 + 校验；React 接线层负责拉 Action 列表（OMS list API）、调
// 用 scenarioPane.addActionRow，以及在 capacity 触顶时弹 toast。

import type { ScenarioPaneActionRow } from './scenarioPane';

export const MAX_ACTIONS_PER_SCENARIO = 50;
export const MAX_ACTIONS_MESSAGE = 'Max 50 actions per scenario';

// 与 pkg/oms ActionType.status 字面值对齐：ACTIVE / ENDORSED 视为已发布
// 可供 Vertex 调用；EXPERIMENTAL / DEPRECATED / DRAFT 排除。
const PUBLISHED_STATUSES: ReadonlySet<string> = new Set(['ACTIVE', 'ENDORSED']);

export interface AddActionParameterSpec {
  required?: boolean;
}

// AddActionActionType 是 web/src/api/types.ts ActionType 的最小投影 + 两
// 个 VTX-051 / VTX-038 引入的可选字段（kind / functionRid）。本模块刻意
// 不复用 web/src/api/types.ts.ActionType —— 那里 parameters 是
// Record<string, ActionParameterV2>，强绑 dataType；这里只在乎 required，
// 解耦让 vitest 不需要构造完整的 DataType payload。
export interface AddActionActionType {
  rid: string;
  apiName: string;
  displayName: string;
  status: string;
  kind?: 'standard' | 'function_backed';
  functionRid?: string;
  parameters?: Record<string, AddActionParameterSpec>;
}

export interface AddActionDialogState {
  open: boolean;
  scenarioRid: string | null;
  actions: AddActionActionType[];
  loading: boolean;
  selectedActionRid: string | null;
  paramValues: Record<string, string>;
  submitting: boolean;
  error: string | null;
}

export type AddActionValidation =
  | { valid: true }
  | { valid: false; reason: 'no_selection' }
  | { valid: false; reason: 'unknown_selection' }
  | { valid: false; reason: 'missing_required_param'; param: string };

export class ScenarioActionCapacityError extends Error {
  readonly limit: number;
  constructor() {
    super(MAX_ACTIONS_MESSAGE);
    this.name = 'ScenarioActionCapacityError';
    this.limit = MAX_ACTIONS_PER_SCENARIO;
  }
}

function requireNonBlank(value: string, field: string): string {
  if (typeof value !== 'string' || value.trim().length === 0) {
    throw new Error(`${field} is required`);
  }
  return value.trim();
}

export function createAddActionDialogState(): AddActionDialogState {
  return {
    open: false,
    scenarioRid: null,
    actions: [],
    loading: false,
    selectedActionRid: null,
    paramValues: {},
    submitting: false,
    error: null,
  };
}

// open 与 createDialogs 系一致：不读 prior state，永远从 blank 起手 +
// 注入 scenarioRid。React 层每次"打开 + Add Action"都重新拉一遍 Action
// 列表，所以这里也不缓存上次的 actions。
export function openAddActionDialog(scenarioRid: string): AddActionDialogState {
  const rid = requireNonBlank(scenarioRid, 'scenario rid');
  return { ...createAddActionDialogState(), open: true, scenarioRid: rid };
}

export function closeAddActionDialog(): AddActionDialogState {
  return createAddActionDialogState();
}

export function setAddActionLoading(
  state: AddActionDialogState,
  loading: boolean,
): AddActionDialogState {
  return { ...state, loading };
}

// setAddActionList 在替换 actions 时尝试保留 selection：
//   * 如果旧 selection 仍存在于新列表 → 保留 selection + paramValues。
//   * 否则清空 selection + paramValues（旧 action 的参数对新 action 没意义）。
// 同时把 loading 翻回 false（典型调用路径是 fetch resolve 后的 then）。
export function setAddActionList(
  state: AddActionDialogState,
  actions: AddActionActionType[],
): AddActionDialogState {
  const next = [...actions];
  const stillSelected =
    state.selectedActionRid !== null &&
    next.some(a => a.rid === state.selectedActionRid);
  return {
    ...state,
    actions: next,
    loading: false,
    selectedActionRid: stillSelected ? state.selectedActionRid : null,
    paramValues: stillSelected ? state.paramValues : {},
  };
}

// setAddActionSelected 切换选择时清空 paramValues —— 不同 Action 的参数集
// 完全不同，保留旧值会导致表单出现"幽灵字段"。无变化时返回原引用让
// React useMemo 不会触发重渲。
export function setAddActionSelected(
  state: AddActionDialogState,
  rid: string | null,
): AddActionDialogState {
  if (state.selectedActionRid === rid) return state;
  return {
    ...state,
    selectedActionRid: rid,
    paramValues: {},
  };
}

export function setAddActionParam(
  state: AddActionDialogState,
  name: string,
  value: string,
): AddActionDialogState {
  return {
    ...state,
    paramValues: { ...state.paramValues, [name]: value },
  };
}

// setSubmitting(true) 顺手清 error，setError 顺手关 submitting —— 与
// createDialogs 一致，保证 submitting + error 二者不会同时为 truthy。
export function setAddActionSubmitting(
  state: AddActionDialogState,
  submitting: boolean,
): AddActionDialogState {
  if (submitting) {
    return { ...state, submitting: true, error: null };
  }
  return { ...state, submitting: false };
}

export function setAddActionError(
  state: AddActionDialogState,
  error: string | null,
): AddActionDialogState {
  return { ...state, error, submitting: false };
}

export function isActionPublished(action: AddActionActionType): boolean {
  return PUBLISHED_STATUSES.has(action.status);
}

export function isFunctionBackedAction(action: AddActionActionType): boolean {
  return action.kind === 'function_backed';
}

export function filterPublishedActions(
  actions: AddActionActionType[],
): AddActionActionType[] {
  return actions.filter(isActionPublished);
}

export function getSelectedAction(
  state: AddActionDialogState,
): AddActionActionType | null {
  if (state.selectedActionRid === null) return null;
  return state.actions.find(a => a.rid === state.selectedActionRid) ?? null;
}

export function getRequiredParamNames(action: AddActionActionType): string[] {
  if (!action.parameters) return [];
  return Object.entries(action.parameters)
    .filter(([, spec]) => spec.required === true)
    .map(([name]) => name);
}

export function validateAddAction(state: AddActionDialogState): AddActionValidation {
  if (state.selectedActionRid === null) {
    return { valid: false, reason: 'no_selection' };
  }
  const selected = getSelectedAction(state);
  if (selected === null) {
    return { valid: false, reason: 'unknown_selection' };
  }
  for (const name of getRequiredParamNames(selected)) {
    const raw = state.paramValues[name];
    if (typeof raw !== 'string' || raw.trim().length === 0) {
      return { valid: false, reason: 'missing_required_param', param: name };
    }
  }
  return { valid: true };
}

export function isScenarioAtActionCapacity(currentCount: number): boolean {
  return currentCount >= MAX_ACTIONS_PER_SCENARIO;
}

export function assertScenarioActionCapacity(currentCount: number): void {
  if (isScenarioAtActionCapacity(currentCount)) {
    throw new ScenarioActionCapacityError();
  }
}

export interface BuildAddActionRowInput {
  state: AddActionDialogState;
  rid: string;
}

// buildAddActionRow 把 dialog state 翻译成 scenarioPane 模块可接受的
// ScenarioPaneActionRow。rid 由调用方提供 —— 通常是 crypto.randomUUID()
// 或后端 POST 响应里的 rid；本模块刻意不引入随机源以保持纯。
export function buildAddActionRow(input: BuildAddActionRowInput): ScenarioPaneActionRow {
  const rid = requireNonBlank(input.rid, 'row rid');
  const selected = getSelectedAction(input.state);
  if (selected === null) {
    throw new Error('cannot build row without a selected action');
  }
  return {
    kind: 'action',
    rid,
    label: selected.displayName,
    actionTypeId: selected.rid,
  };
}
