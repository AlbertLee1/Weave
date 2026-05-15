// VTX-054 — Add Model（模型版本管理 UI）的纯逻辑层。
//
// 状态模型驱动 Scenario Pane 的 "Add new model" 对话框：列出已发布的
// Live Model deployments，让操作者先选 model，再从两个下拉里选 model
// version 与 configuration version，最后转成 ScenarioPaneModelRow 加到
// Scenario 行集合。React 接线层负责拉 deployment 列表、调
// scenarioPane.addModelRow，以及在 capacity 触顶时弹 toast。

import type { ScenarioPaneModelRow } from './scenarioPane';

export const MAX_MODELS_PER_SCENARIO = 50;
export const MAX_MODELS_MESSAGE = 'Max 50 models per scenario';

// AddModelLiveModel 是 web/src/api/types.ts ModelDeployment 的最小投影 +
// 一个用于过滤 Live deployment 的 kind 字段。modelVersions / configVersions
// 在后端是「同名 deployment 的多个 row」聚合得到（见 VTX-050
// modelfunctions.Deployment.ModelVersion 注释），UI 拿到时已是数组形式。
export interface AddModelLiveModel {
  rid: string;
  name: string;
  modelVersions: string[];
  configVersions: string[];
  kind?: 'live' | 'standard';
}

export interface AddModelDialogState {
  open: boolean;
  scenarioRid: string | null;
  models: AddModelLiveModel[];
  loading: boolean;
  selectedModelRid: string | null;
  selectedModelVersion: string | null;
  selectedConfigVersion: string | null;
  submitting: boolean;
  error: string | null;
}

export type AddModelValidation =
  | { valid: true }
  | { valid: false; reason: 'no_selection' }
  | { valid: false; reason: 'unknown_selection' }
  | { valid: false; reason: 'no_versions_available' }
  | { valid: false; reason: 'missing_model_version' }
  | { valid: false; reason: 'missing_config_version' };

export class ScenarioModelCapacityError extends Error {
  readonly limit: number;
  constructor() {
    super(MAX_MODELS_MESSAGE);
    this.name = 'ScenarioModelCapacityError';
    this.limit = MAX_MODELS_PER_SCENARIO;
  }
}

function requireNonBlank(value: string, field: string): string {
  if (typeof value !== 'string' || value.trim().length === 0) {
    throw new Error(`${field} is required`);
  }
  return value.trim();
}

export function createAddModelDialogState(): AddModelDialogState {
  return {
    open: false,
    scenarioRid: null,
    models: [],
    loading: false,
    selectedModelRid: null,
    selectedModelVersion: null,
    selectedConfigVersion: null,
    submitting: false,
    error: null,
  };
}

// open 与 createDialogs / addAction 同模式：每次打开都从 blank 起手 + 注入
// scenarioRid。Pane 层每次"+ Add Model"都会重新拉 deployment 列表，所以
// 不缓存上一次的 models / 选择。
export function openAddModelDialog(scenarioRid: string): AddModelDialogState {
  const rid = requireNonBlank(scenarioRid, 'scenario rid');
  return { ...createAddModelDialogState(), open: true, scenarioRid: rid };
}

export function closeAddModelDialog(): AddModelDialogState {
  return createAddModelDialogState();
}

export function setAddModelLoading(
  state: AddModelDialogState,
  loading: boolean,
): AddModelDialogState {
  return { ...state, loading };
}

// setAddModelList 在替换 models 时分两步保留 / 清空 selection：
//   * 旧 selection 仍在新列表 → 保留 selectedModelRid。
//   * 但旧 modelVersion / configVersion 必须仍出现在新 model 的版本列表里
//     才保留；否则清空（旧版本号在新 deployment 里可能已下线）。
//   * 旧 selection 不在新列表 → 三个字段全清空。
// 同时 loading 翻回 false（典型路径：fetch resolve 后的 then）。
export function setAddModelList(
  state: AddModelDialogState,
  models: AddModelLiveModel[],
): AddModelDialogState {
  const next = [...models];
  if (state.selectedModelRid === null) {
    return { ...state, models: next, loading: false };
  }
  const stillSelected = next.find(m => m.rid === state.selectedModelRid) ?? null;
  if (stillSelected === null) {
    return {
      ...state,
      models: next,
      loading: false,
      selectedModelRid: null,
      selectedModelVersion: null,
      selectedConfigVersion: null,
    };
  }
  const keepModelVersion =
    state.selectedModelVersion !== null &&
    stillSelected.modelVersions.includes(state.selectedModelVersion);
  const keepConfigVersion =
    state.selectedConfigVersion !== null &&
    stillSelected.configVersions.includes(state.selectedConfigVersion);
  return {
    ...state,
    models: next,
    loading: false,
    selectedModelVersion: keepModelVersion ? state.selectedModelVersion : null,
    selectedConfigVersion: keepConfigVersion ? state.selectedConfigVersion : null,
  };
}

// setAddModelSelected 切换选择时清空 modelVersion / configVersion —— 不同
// model 的版本完全独立，保留旧值会让 dropdown 出现"幽灵选项"。无变化时
// 返回原引用让 React useMemo 不会触发重渲。
export function setAddModelSelected(
  state: AddModelDialogState,
  rid: string | null,
): AddModelDialogState {
  if (state.selectedModelRid === rid) return state;
  return {
    ...state,
    selectedModelRid: rid,
    selectedModelVersion: null,
    selectedConfigVersion: null,
  };
}

export function setAddModelVersion(
  state: AddModelDialogState,
  version: string | null,
): AddModelDialogState {
  return { ...state, selectedModelVersion: version };
}

export function setAddModelConfigVersion(
  state: AddModelDialogState,
  version: string | null,
): AddModelDialogState {
  return { ...state, selectedConfigVersion: version };
}

// setSubmitting(true) 顺手清 error；setError 顺手关 submitting —— 与
// addAction / createDialogs 一致，保证 submitting + error 不会同时为 truthy。
export function setAddModelSubmitting(
  state: AddModelDialogState,
  submitting: boolean,
): AddModelDialogState {
  if (submitting) {
    return { ...state, submitting: true, error: null };
  }
  return { ...state, submitting: false };
}

export function setAddModelError(
  state: AddModelDialogState,
  error: string | null,
): AddModelDialogState {
  return { ...state, error, submitting: false };
}

export function isLiveModel(model: AddModelLiveModel): boolean {
  return model.kind === 'live';
}

export function filterLiveModels(models: AddModelLiveModel[]): AddModelLiveModel[] {
  return models.filter(isLiveModel);
}

export function getSelectedModel(state: AddModelDialogState): AddModelLiveModel | null {
  if (state.selectedModelRid === null) return null;
  return state.models.find(m => m.rid === state.selectedModelRid) ?? null;
}

export function getAvailableModelVersions(state: AddModelDialogState): string[] {
  const model = getSelectedModel(state);
  return model?.modelVersions ?? [];
}

export function getAvailableConfigVersions(state: AddModelDialogState): string[] {
  const model = getSelectedModel(state);
  return model?.configVersions ?? [];
}

export function validateAddModel(state: AddModelDialogState): AddModelValidation {
  if (state.selectedModelRid === null) {
    return { valid: false, reason: 'no_selection' };
  }
  const selected = getSelectedModel(state);
  if (selected === null) {
    return { valid: false, reason: 'unknown_selection' };
  }
  if (selected.modelVersions.length === 0 || selected.configVersions.length === 0) {
    return { valid: false, reason: 'no_versions_available' };
  }
  if (
    state.selectedModelVersion === null ||
    !selected.modelVersions.includes(state.selectedModelVersion)
  ) {
    return { valid: false, reason: 'missing_model_version' };
  }
  if (
    state.selectedConfigVersion === null ||
    !selected.configVersions.includes(state.selectedConfigVersion)
  ) {
    return { valid: false, reason: 'missing_config_version' };
  }
  return { valid: true };
}

export function isScenarioAtModelCapacity(currentCount: number): boolean {
  return currentCount >= MAX_MODELS_PER_SCENARIO;
}

export function assertScenarioModelCapacity(currentCount: number): void {
  if (isScenarioAtModelCapacity(currentCount)) {
    throw new ScenarioModelCapacityError();
  }
}

export interface BuildAddModelRowInput {
  state: AddModelDialogState;
  rid: string;
}

// buildAddModelRow 把 dialog state 翻译成 scenarioPane 模块可接受的
// ScenarioPaneModelRow。rid 由调用方提供 —— 通常是 crypto.randomUUID()
// 或后端 POST 响应里的 rid；本模块刻意不引入随机源以保持纯。
// 调用前 React 层应 validateAddModel(state).valid === true，以下三处 throw
// 只是兜底防御（assertion 风格）。
export function buildAddModelRow(input: BuildAddModelRowInput): ScenarioPaneModelRow {
  const rid = requireNonBlank(input.rid, 'row rid');
  const selected = getSelectedModel(input.state);
  if (selected === null) {
    throw new Error('cannot build row without a selected model');
  }
  if (input.state.selectedModelVersion === null) {
    throw new Error('cannot build row without a model version');
  }
  if (input.state.selectedConfigVersion === null) {
    throw new Error('cannot build row without a config version');
  }
  return {
    kind: 'model',
    rid,
    label: selected.name,
    modelRid: selected.rid,
    modelVersion: input.state.selectedModelVersion,
    configVersion: input.state.selectedConfigVersion,
  };
}
