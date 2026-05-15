// VTX-037 — 创建 Case Study / Scenario 对话框的纯逻辑层。
//
// 提供两个独立的对话框状态机（CaseStudy / Scenario），名称校验，POST
// 请求体 + 路径构造器，以及 immutable Scenario 编辑守卫。React 接线层
// 负责把状态机串到 Modal 组件 + fetch 调用，再用返回的 CaseStudy /
// Scenario payload 喂给 scenarioPane reducer（setCaseStudy / addScenario）。

export const IMMUTABLE_SCENARIO_MESSAGE = 'Scenario is immutable. Duplicate to modify.';

// 单个对话框输入文字的硬上限。与 Pane 列头/侧栏宽度无直接关系，纯粹
// 防止 UI 把超长名字塞进 POST body。Trim 后才参与判断。
export const DIALOG_NAME_MAX_LENGTH = 128;

export interface CreateCaseStudyDialogState {
  open: boolean;
  name: string;
  submitting: boolean;
  error: string | null;
}

export interface CreateScenarioDialogState {
  open: boolean;
  caseStudyRid: string | null;
  name: string;
  submitting: boolean;
  error: string | null;
}

export interface ApiRequest {
  method: 'POST';
  path: string;
  body: Record<string, string>;
}

export interface CreateCaseStudyInput {
  name: string;
  ontologyRid: string;
}

export interface CreateScenarioInput {
  caseStudyRid: string;
  name: string;
  parentOntologyCommit: string;
}

export interface ScenarioLike {
  rid: string;
  name?: string;
  immutable?: boolean;
}

export type DialogNameValidation =
  | { valid: true }
  | { valid: false; reason: 'required' | 'too_long' };

export class ScenarioImmutableError extends Error {
  readonly scenarioRid: string;
  constructor(scenarioRid: string) {
    super(IMMUTABLE_SCENARIO_MESSAGE);
    this.name = 'ScenarioImmutableError';
    this.scenarioRid = scenarioRid;
  }
}

function requireNonBlank(value: string, fieldName: string): string {
  if (typeof value !== 'string' || value.trim().length === 0) {
    throw new Error(`${fieldName} is required`);
  }
  return value.trim();
}

export function createCaseStudyDialogState(): CreateCaseStudyDialogState {
  return { open: false, name: '', submitting: false, error: null };
}

// open 会清空 name / error / submitting，确保旧 session 的残留不会带到
// 新一次创建里（关闭对话框后再次打开是常见路径）。无 state 入参 —
// 打开/关闭与之前的状态无关。
export function openCreateCaseStudyDialog(): CreateCaseStudyDialogState {
  return { open: true, name: '', submitting: false, error: null };
}

export function closeCreateCaseStudyDialog(): CreateCaseStudyDialogState {
  return createCaseStudyDialogState();
}

export function setCreateCaseStudyName(
  state: CreateCaseStudyDialogState,
  name: string,
): CreateCaseStudyDialogState {
  return { ...state, name };
}

// setSubmitting(true) 顺手清掉 error —— 进入请求阶段意味着之前的错误已
// 经被用户看到并尝试纠正。setSubmitting(false) 不动 error，因为
// setError 通常已经在请求失败时被调用过（setError 也会顺手把 submitting
// 翻回 false）。
export function setCreateCaseStudySubmitting(
  state: CreateCaseStudyDialogState,
  submitting: boolean,
): CreateCaseStudyDialogState {
  if (submitting) {
    return { ...state, submitting: true, error: null };
  }
  return { ...state, submitting: false };
}

export function setCreateCaseStudyError(
  state: CreateCaseStudyDialogState,
  error: string | null,
): CreateCaseStudyDialogState {
  return { ...state, error, submitting: false };
}

export function createScenarioDialogState(): CreateScenarioDialogState {
  return {
    open: false,
    caseStudyRid: null,
    name: '',
    submitting: false,
    error: null,
  };
}

export function openCreateScenarioDialog(
  caseStudyRid: string,
): CreateScenarioDialogState {
  const rid = requireNonBlank(caseStudyRid, 'case study rid');
  return { open: true, caseStudyRid: rid, name: '', submitting: false, error: null };
}

export function closeCreateScenarioDialog(): CreateScenarioDialogState {
  return createScenarioDialogState();
}

export function setCreateScenarioName(
  state: CreateScenarioDialogState,
  name: string,
): CreateScenarioDialogState {
  return { ...state, name };
}

export function setCreateScenarioSubmitting(
  state: CreateScenarioDialogState,
  submitting: boolean,
): CreateScenarioDialogState {
  if (submitting) {
    return { ...state, submitting: true, error: null };
  }
  return { ...state, submitting: false };
}

export function setCreateScenarioError(
  state: CreateScenarioDialogState,
  error: string | null,
): CreateScenarioDialogState {
  return { ...state, error, submitting: false };
}

export function validateDialogName(name: string): DialogNameValidation {
  const trimmed = typeof name === 'string' ? name.trim() : '';
  if (trimmed.length === 0) return { valid: false, reason: 'required' };
  if (trimmed.length > DIALOG_NAME_MAX_LENGTH) {
    return { valid: false, reason: 'too_long' };
  }
  return { valid: true };
}

export function buildCreateCaseStudyRequest(
  input: CreateCaseStudyInput,
): ApiRequest {
  const name = requireNonBlank(input.name, 'name');
  const ontologyRid = requireNonBlank(input.ontologyRid, 'ontologyRid');
  return {
    method: 'POST',
    path: '/api/vertex/v1/case-studies',
    body: { name, ontologyRid },
  };
}

export function buildCreateScenarioRequest(
  input: CreateScenarioInput,
): ApiRequest {
  const caseStudyRid = requireNonBlank(input.caseStudyRid, 'caseStudyRid');
  const name = requireNonBlank(input.name, 'name');
  const parentOntologyCommit = requireNonBlank(
    input.parentOntologyCommit,
    'parentOntologyCommit',
  );
  return {
    method: 'POST',
    path: `/api/vertex/v1/case-studies/${encodeURIComponent(caseStudyRid)}/scenarios`,
    body: { name, parentOntologyCommit },
  };
}

export function isScenarioImmutable(scenario: ScenarioLike): boolean {
  return scenario.immutable === true;
}

export function assertScenarioMutable(scenario: ScenarioLike): void {
  if (isScenarioImmutable(scenario)) {
    throw new ScenarioImmutableError(scenario.rid);
  }
}
