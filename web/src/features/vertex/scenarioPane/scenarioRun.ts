// VTX-043 — Scenario Run（单 Function start-and-poll 路径）的纯逻辑层。
//
// 调研报告 §4.4 / 附录 B：用户在 Scenario Pane 点 Run，前端 POST
// /api/vertex/v1/scenarios/{rid}/runs。已挂载后端合同返回 202
// {runRid,status}；终态由 GET /runs/{runRid} polling 返回 persisted run
// record。前端收到终态 success 后把 scenario 列状态变绿 ✓ + 显示执行
// 耗时 + 标记 scenario.immutable=true。function 抛错则返回 error payload，
// 前端列状态变红 × + tooltip 显示错误消息。
//
// 本模块交付：
//   - 每个 scenario 一份 ScenarioRunState（status / startedAt /
//     durationMs / error），map 形 Record<scenarioRid, state>
//   - 状态机迁移 helper：applyScenarioRunStart / Success / Error
//   - POST /runs 请求构造 buildRunScenarioRequest
//   - 202 响应解析 parseScenarioRunAcceptedResponse
//   - GET /runs/{runRid} 请求构造 buildGetRunScenarioRequest
//   - error payload 解析 parseScenarioRunErrorResponse
//   - preflight 校验 validateScenarioRunRequest +
//     assertScenarioRunnable（throws ScenarioRunNotEditableError）
//   - Run 按钮 enabled/disabled 决策 resolveRunButtonState
//   - status icon / tooltip 派生 getScenarioRunStatusIcon /
//     getScenarioRunStatusTooltip
//   - immutable 转换 immutableScenario —— terminal success 后把 scenario freeze
//
// React 接线层负责发 fetch、把 accepted response 喂给 parse* helper、
// 再交给 polling helper 读取 terminal record；Run 列单元格按 status 渲染
// spinner/✓/×/—，tooltip 显示 duration 或错误消息。

import type { ScenarioRef } from './scenarioPane';

export type ScenarioRunStatus = 'idle' | 'running' | 'success' | 'error';

export interface ScenarioRunState {
  status: ScenarioRunStatus;
  startedAt: number | null;
  durationMs: number | null;
  error: string | null;
}

export type ScenarioRunMap = Record<string, ScenarioRunState>;

export interface RunScenarioApiRequest {
  method: 'POST';
  path: string;
  body: Record<string, never>;
}

export interface RunScenarioStatusApiRequest {
  method: 'GET';
  path: string;
}

export interface RunScenarioAcceptedResponse {
  status: 'pending' | 'running';
  runRid: string;
}

export interface RunScenarioErrorResponse {
  status: 'error';
  message?: string;
}

export interface ParsedRunScenarioAccepted {
  status: RunScenarioAcceptedResponse['status'];
  runRid: string;
}

export interface ParsedRunScenarioError {
  message: string;
}

export interface ScenarioRunValidationInput {
  scenario: ScenarioRef;
  actionCount: number;
  overrideCount: number;
}

export type ScenarioRunValidation =
  | { valid: true }
  | { valid: false; reason: 'frozen' | 'empty_payload' };

export type RunButtonReason = 'running' | 'frozen' | 'empty_payload';

export type RunButtonState =
  | { enabled: true }
  | { enabled: false; reason: RunButtonReason };

export interface ResolveRunButtonStateInput {
  scenario: ScenarioRef;
  runStatus: ScenarioRunStatus;
  actionCount: number;
  overrideCount: number;
}

// Run 列状态变绿 ✓ / 红 × / spinner 时的图标占位。React 接线层把这些
// 串到 lucide-react 图标（CheckCircle / XCircle / Loader2）或 emoji。
export type ScenarioRunStatusIcon = '—' | 'spinner' | '✓' | '×';

// 用户在 frozen scenario 上误点 Run 时，throw 的错误前缀。React 错误
// 边界 / toast 路径 `if (err instanceof ScenarioRunNotEditableError)` 同
// 时捕获 frozen 与 empty_payload 两类，前缀让 message 一眼可读。
export const RUN_FROZEN_TOOLTIP_PREFIX = 'Scenario cannot be run';

export class ScenarioRunNotEditableError extends Error {
  readonly scenarioRid: string;
  readonly reason: 'frozen' | 'empty_payload';
  constructor(scenarioRid: string, reason: 'frozen' | 'empty_payload') {
    const detail =
      reason === 'frozen'
        ? 'scenario is frozen'
        : 'scenario has no actions or overrides';
    super(`${RUN_FROZEN_TOOLTIP_PREFIX}: ${detail}`);
    this.name = 'ScenarioRunNotEditableError';
    this.scenarioRid = scenarioRid;
    this.reason = reason;
  }
}

function requireNonBlank(value: string, field: string): string {
  if (typeof value !== 'string' || value.trim().length === 0) {
    throw new Error(`${field} is required`);
  }
  return value.trim();
}

export function createScenarioRunMap(): ScenarioRunMap {
  return {};
}

export function createScenarioRunState(): ScenarioRunState {
  return { status: 'idle', startedAt: null, durationMs: null, error: null };
}

// setScenarioRunState 用 spread + key 替换：只动 target key，其他
// scenario 的 state 引用保持不变（React useMemo 友好）。
export function setScenarioRunState(
  map: ScenarioRunMap,
  scenarioRid: string,
  state: ScenarioRunState,
): ScenarioRunMap {
  return { ...map, [scenarioRid]: state };
}

export function getScenarioRunStatus(
  map: ScenarioRunMap,
  scenarioRid: string,
): ScenarioRunStatus {
  return map[scenarioRid]?.status ?? 'idle';
}

export function isScenarioRunning(
  map: ScenarioRunMap,
  scenarioRid: string,
): boolean {
  return getScenarioRunStatus(map, scenarioRid) === 'running';
}

// applyScenarioRunStart 把 scenario 从 idle/success/error 翻到 running。
// 顺手清掉 error（上次失败的错误不应在重试 spinner 期间还显示）+ 清
// duration（旧 duration 与新 run 无关）。startedAt 由调用方提供（通常
// 是 Date.now()），便于测试用 fake clock 注入稳定时间戳。
export function applyScenarioRunStart(
  map: ScenarioRunMap,
  scenarioRid: string,
  startedAt: number,
): ScenarioRunMap {
  return setScenarioRunState(map, scenarioRid, {
    status: 'running',
    startedAt,
    durationMs: null,
    error: null,
  });
}

export function applyScenarioRunSuccess(
  map: ScenarioRunMap,
  scenarioRid: string,
  durationMs: number,
): ScenarioRunMap {
  const prior = map[scenarioRid] ?? createScenarioRunState();
  return setScenarioRunState(map, scenarioRid, {
    status: 'success',
    startedAt: prior.startedAt,
    durationMs,
    error: null,
  });
}

export function applyScenarioRunError(
  map: ScenarioRunMap,
  scenarioRid: string,
  message: string,
): ScenarioRunMap {
  const prior = map[scenarioRid] ?? createScenarioRunState();
  return setScenarioRunState(map, scenarioRid, {
    status: 'error',
    startedAt: prior.startedAt,
    durationMs: null,
    error: message,
  });
}

export interface BuildRunScenarioRequestInput {
  scenarioRid: string;
}

export interface BuildGetRunScenarioRequestInput {
  scenarioRid: string;
  runRid: string;
}

export function buildRunScenarioRequest(
  input: BuildRunScenarioRequestInput,
): RunScenarioApiRequest {
  const scenarioRid = requireNonBlank(input.scenarioRid, 'scenarioRid');
  return {
    method: 'POST',
    path: `/api/vertex/v1/scenarios/${encodeURIComponent(scenarioRid)}/runs`,
    body: {},
  };
}

export function buildGetRunScenarioRequest(
  input: BuildGetRunScenarioRequestInput,
): RunScenarioStatusApiRequest {
  const scenarioRid = requireNonBlank(input.scenarioRid, 'scenarioRid');
  const runRid = requireNonBlank(input.runRid, 'runRid');
  return {
    method: 'GET',
    path: `/api/vertex/v1/scenarios/${encodeURIComponent(scenarioRid)}/runs/${encodeURIComponent(runRid)}`,
  };
}

// hasRunnablePayload —— 一个 scenario 至少有 1 个 action 或 1 个 override
// 才值得发 Run 请求。完全空的 scenario 没有 fork 内容，POST 会被后端
// 401/400 拒，前端先短路省一次往返。
export function hasRunnablePayload(input: {
  actionCount: number;
  overrideCount: number;
}): boolean {
  return input.actionCount > 0 || input.overrideCount > 0;
}

export function validateScenarioRunRequest(
  input: ScenarioRunValidationInput,
): ScenarioRunValidation {
  if (input.scenario.immutable === true) {
    return { valid: false, reason: 'frozen' };
  }
  if (!hasRunnablePayload(input)) {
    return { valid: false, reason: 'empty_payload' };
  }
  return { valid: true };
}

// assertScenarioRunnable —— preflight 守卫。React 层 Run 按钮 disabled
// 是软提示，键盘 Enter / programmatic dispatch 可能绕过；调 fetch 前再
// assert 一次保证不写入 frozen scenario / 不发空 Run。
export function assertScenarioRunnable(input: ScenarioRunValidationInput): void {
  const result = validateScenarioRunRequest(input);
  if (!result.valid) {
    throw new ScenarioRunNotEditableError(input.scenario.rid, result.reason);
  }
}

// resolveRunButtonState —— Run 按钮的 UI 决策中枢。优先级：running >
// frozen > empty_payload > enabled。running 比 frozen 优先是因为按钮在
// 用户点 Run 后立即 disable（spinner 期间），即便 scenario 还未冻结也
// 不允许重复点。
export function resolveRunButtonState(
  input: ResolveRunButtonStateInput,
): RunButtonState {
  if (input.runStatus === 'running') {
    return { enabled: false, reason: 'running' };
  }
  if (input.scenario.immutable === true) {
    return { enabled: false, reason: 'frozen' };
  }
  if (!hasRunnablePayload(input)) {
    return { enabled: false, reason: 'empty_payload' };
  }
  return { enabled: true };
}

export function getScenarioRunStatusIcon(
  status: ScenarioRunStatus,
): ScenarioRunStatusIcon {
  switch (status) {
    case 'idle':
      return '—';
    case 'running':
      return 'spinner';
    case 'success':
      return '✓';
    case 'error':
      return '×';
  }
}

// formatDuration —— 1000 ms 以下显示 "{ms} ms"，1000+ ms 显示
// "{seconds.x} s"（保留一位小数）。Pane Run 列宽度有限，太长会截断。
function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms} ms`;
  const seconds = ms / 1000;
  return `${seconds.toFixed(1)} s`;
}

// getScenarioRunStatusTooltip —— hover Run 列单元格时显示的 tooltip。
// success → "Run completed in N s/ms"（duration null 兜底 "Run
// completed"）。error → 错误消息。running → "Running…"。idle → null。
export function getScenarioRunStatusTooltip(
  map: ScenarioRunMap,
  scenarioRid: string,
): string | null {
  const state = map[scenarioRid];
  if (!state || state.status === 'idle') return null;
  if (state.status === 'running') return 'Running…';
  if (state.status === 'error') {
    return state.error ?? 'Scenario run failed';
  }
  // success
  if (state.durationMs === null) return 'Run completed';
  return `Run completed in ${formatDuration(state.durationMs)}`;
}

// parseScenarioRunAcceptedResponse —— mounted POST /runs contract:
// 202 Accepted {status:'pending'|'running', runRid}. Terminal success/failure
// must come from GET /runs/{runRid}, not the POST response.
export function parseScenarioRunAcceptedResponse(
  response: RunScenarioAcceptedResponse,
): ParsedRunScenarioAccepted {
  if (typeof response.runRid !== 'string') {
    throw new Error('runRid is required');
  }
  const runRid = response.runRid.trim();
  if (runRid.length === 0) {
    throw new Error('runRid is required');
  }
  if (response.status !== 'pending' && response.status !== 'running') {
    throw new Error('status must be pending or running');
  }
  return { status: response.status, runRid };
}

export function parseScenarioRunErrorResponse(
  response: RunScenarioErrorResponse,
): ParsedRunScenarioError {
  const raw = typeof response.message === 'string' ? response.message.trim() : '';
  return { message: raw.length > 0 ? raw : 'Scenario run failed' };
}

// immutableScenario —— terminal success 后把 scenario.immutable 翻为 true 的
// 工具函数。返回新对象，不 mutate 入参。
export function immutableScenario(scenario: ScenarioRef): ScenarioRef {
  return { ...scenario, immutable: true };
}
