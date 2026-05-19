// VTX-044 — Scenario Run（异步路径，runRid + SSE）的纯逻辑层。
//
// 调研报告 §4.4 / 附录 B：当 Scenario 含 ≥ 2 models 或调用方显式
// forceAsync=true，后端 POST /scenarios/{rid}/runs 立即返回 202
// {runRid}。前端按 runRid 打开 SSE GET /scenarios/{rid}/runs/{runRid}/stream，
// 后端推送 progress{model,pct} / result{model,output} / done{durationMs}
// / error{model,message} / cancelled 事件。Pane 行按 progress 显示
// spinner+百分比，done 翻 ✓ + duration，error 翻 × + tooltip，cancel
// 通过 POST /runs/{runRid}/cancel 触发后端推 cancelled 事件 + cleanup。
//
// 本模块交付：
//   - 异步触发判定 shouldRunAsync（modelCount >= 2 OR forceAsync）
//   - 异步 Run 请求构造 buildAsyncRunScenarioRequest（forceAsync 透传）
//   - SSE URL 构造 buildScenarioRunStreamUrl（runRid 走 encodeURIComponent）
//   - Cancel 请求构造 buildCancelScenarioRunRequest
//   - 202 响应解析 parseAcceptedScenarioRunResponse
//   - SSE 事件解析 parseScenarioRunSseEvent（5 类事件 + clamp + fallback）
//   - 每个 scenario 一份 ScenarioRunJobState（runRid, status, startedAt,
//     durationMs, error, progressByModel, resultsByModel）
//   - 状态机迁移 applyAcceptedScenarioRunJob / applyScenarioRunSseEvent
//   - UI 派生 getOverallProgressPct / getModelProgressPct /
//     getScenarioRunJobStatusIcon / getScenarioRunJobStatusTooltip
//
// React 接线层负责开 EventSource、把 messageEvent.data 喂给 parseSseEvent、
// 把事件喂给 applySseEvent，在 Pane Run 列按 status 渲染 spinner+pct /
// ✓ / × / ⊘ / —，tooltip 显示 progress 百分比或最终结果。

import type { ScenarioRef } from './scenarioPane';

export type ScenarioRunJobStatus =
  | 'idle'
  | 'pending'
  | 'running'
  | 'success'
  | 'error'
  | 'cancelled';

export interface ScenarioRunJobState {
  runRid: string;
  status: ScenarioRunJobStatus;
  startedAt: number | null;
  durationMs: number | null;
  error: string | null;
  progressByModel: Record<string, number>;
  resultsByModel: Record<string, unknown>;
}

export type ScenarioRunJobMap = Record<string, ScenarioRunJobState>;

export type ScenarioRunSseEvent =
  | { type: 'progress'; model: string; pct: number }
  | { type: 'result'; model: string; output: unknown }
  | { type: 'done'; durationMs: number }
  | { type: 'error'; model?: string; message: string }
  | { type: 'cancelled' };

export interface ShouldRunAsyncInput {
  modelCount: number;
  forceAsync?: boolean;
}

export interface BuildAsyncRunScenarioRequestInput {
  scenarioRid: string;
  forceAsync?: boolean;
}

export interface BuildScenarioRunStreamUrlInput {
  scenarioRid: string;
  runRid: string;
}

export interface BuildCancelScenarioRunRequestInput {
  scenarioRid: string;
  runRid: string;
}

export interface AcceptedScenarioRunResponse {
  status: 'pending' | 'running';
  runRid: string;
}

export interface ParsedAcceptedScenarioRunResponse {
  status: AcceptedScenarioRunResponse['status'];
  runRid: string;
}

export interface AsyncRunScenarioApiRequest {
  method: 'POST';
  path: string;
  body: Record<string, never> | { forceAsync: true };
}

export interface CancelScenarioRunApiRequest {
  method: 'POST';
  path: string;
  body: Record<string, never>;
}

export type ScenarioRunJobStatusIcon = '—' | 'spinner' | '✓' | '×' | '⊘';

const SCENARIO_RUN_FALLBACK_MESSAGE = 'Scenario run failed';

function requireNonBlank(value: string, field: string): string {
  if (typeof value !== 'string' || value.trim().length === 0) {
    throw new Error(`${field} is required`);
  }
  return value.trim();
}

// shouldRunAsync —— spec §4.4 触发条件："Scenario 含 ≥ 2 models OR
// forceAsync=true"。modelCount 在 Pane 表 model 行数等于；forceAsync 由
// 调用方按 UX 选项（Scenario options "Always run async"）注入。
export function shouldRunAsync(input: ShouldRunAsyncInput): boolean {
  return input.forceAsync === true || input.modelCount >= 2;
}

export function buildAsyncRunScenarioRequest(
  input: BuildAsyncRunScenarioRequestInput,
): AsyncRunScenarioApiRequest {
  const scenarioRid = requireNonBlank(input.scenarioRid, 'scenarioRid');
  const path = `/api/vertex/v1/scenarios/${encodeURIComponent(scenarioRid)}/runs`;
  if (input.forceAsync === true) {
    return { method: 'POST', path, body: { forceAsync: true } };
  }
  return { method: 'POST', path, body: {} };
}

export function buildScenarioRunStreamUrl(
  input: BuildScenarioRunStreamUrlInput,
): string {
  const scenarioRid = requireNonBlank(input.scenarioRid, 'scenarioRid');
  const runRid = requireNonBlank(input.runRid, 'runRid');
  return `/api/vertex/v1/scenarios/${encodeURIComponent(scenarioRid)}/runs/${encodeURIComponent(runRid)}/stream`;
}

export function buildCancelScenarioRunRequest(
  input: BuildCancelScenarioRunRequestInput,
): CancelScenarioRunApiRequest {
  const scenarioRid = requireNonBlank(input.scenarioRid, 'scenarioRid');
  const runRid = requireNonBlank(input.runRid, 'runRid');
  return {
    method: 'POST',
    path: `/api/vertex/v1/scenarios/${encodeURIComponent(scenarioRid)}/runs/${encodeURIComponent(runRid)}/cancel`,
    body: {},
  };
}

export function parseAcceptedScenarioRunResponse(
  response: AcceptedScenarioRunResponse,
): ParsedAcceptedScenarioRunResponse {
  if (typeof response.runRid !== 'string') {
    throw new Error('runRid is required');
  }
  const runRid = response.runRid.trim();
  if (runRid.length === 0) {
    throw new Error('runRid is required');
  }
  return { runRid, status: response.status };
}

function clampPct(value: number): number {
  if (value < 0) return 0;
  if (value > 100) return 100;
  return value;
}

// parseScenarioRunSseEvent —— 把后端推送的单条 SSE message data 字符串
// 解析为 ScenarioRunSseEvent。JSON.parse 失败由内部抛出；类型不匹配
// 抛出明确字段错误。progress.pct clamp 到 [0,100] 防止后端漂；error
// 缺失 message 兜底 SCENARIO_RUN_FALLBACK_MESSAGE。
export function parseScenarioRunSseEvent(rawData: string): ScenarioRunSseEvent {
  const parsed = JSON.parse(rawData) as unknown;
  if (!parsed || typeof parsed !== 'object') {
    throw new Error('event payload must be an object');
  }
  const obj = parsed as Record<string, unknown>;
  const type = obj.type;
  if (typeof type !== 'string') {
    throw new Error('event type is required');
  }
  switch (type) {
    case 'progress': {
      const model = obj.model;
      if (typeof model !== 'string' || model.trim().length === 0) {
        throw new Error('progress.model is required');
      }
      const pct = obj.pct;
      if (typeof pct !== 'number' || !Number.isFinite(pct)) {
        throw new Error('progress.pct must be a finite number');
      }
      return { type: 'progress', model, pct: clampPct(pct) };
    }
    case 'result': {
      const model = obj.model;
      if (typeof model !== 'string' || model.trim().length === 0) {
        throw new Error('result.model is required');
      }
      return { type: 'result', model, output: obj.output ?? null };
    }
    case 'done': {
      const durationMs = obj.durationMs;
      if (typeof durationMs !== 'number' || !Number.isFinite(durationMs)) {
        throw new Error('done.durationMs must be a finite number');
      }
      if (durationMs < 0) {
        throw new Error('done.durationMs must be non-negative');
      }
      return { type: 'done', durationMs };
    }
    case 'error': {
      const rawMessage = obj.message;
      const trimmed =
        typeof rawMessage === 'string' ? rawMessage.trim() : '';
      const message =
        trimmed.length > 0 ? trimmed : SCENARIO_RUN_FALLBACK_MESSAGE;
      const model = obj.model;
      if (typeof model === 'string' && model.trim().length > 0) {
        return { type: 'error', model, message };
      }
      return { type: 'error', message };
    }
    case 'cancelled':
      return { type: 'cancelled' };
    default:
      throw new Error(`unknown event type: ${type}`);
  }
}

export function createScenarioRunJobMap(): ScenarioRunJobMap {
  return {};
}

export interface CreateScenarioRunJobStateInput {
  runRid: string;
  startedAt?: number | null;
}

export function createScenarioRunJobState(
  input: CreateScenarioRunJobStateInput,
): ScenarioRunJobState {
  return {
    runRid: input.runRid,
    status: 'pending',
    startedAt: input.startedAt ?? null,
    durationMs: null,
    error: null,
    progressByModel: {},
    resultsByModel: {},
  };
}

export function setScenarioRunJobState(
  map: ScenarioRunJobMap,
  scenarioRid: string,
  state: ScenarioRunJobState,
): ScenarioRunJobMap {
  return { ...map, [scenarioRid]: state };
}

export function getScenarioRunJobStatus(
  map: ScenarioRunJobMap,
  scenarioRid: string,
): ScenarioRunJobStatus {
  return map[scenarioRid]?.status ?? 'idle';
}

export function isScenarioRunJobActive(
  map: ScenarioRunJobMap,
  scenarioRid: string,
): boolean {
  const s = getScenarioRunJobStatus(map, scenarioRid);
  return s === 'pending' || s === 'running';
}

// applyAcceptedScenarioRunJob —— 202 accepted 后初始化 job state。
// 顺手清 prior 的 progress/result/error/duration，重试场景下旧数据不
// 干扰新 job UI。
export function applyAcceptedScenarioRunJob(
  map: ScenarioRunJobMap,
  scenarioRid: string,
  runRid: string,
  startedAt: number,
): ScenarioRunJobMap {
  return setScenarioRunJobState(map, scenarioRid, {
    runRid,
    status: 'pending',
    startedAt,
    durationMs: null,
    error: null,
    progressByModel: {},
    resultsByModel: {},
  });
}

// applyScenarioRunSseEvent —— 把 SSE 事件应用到 job state。
// 若该 scenario 还没有 job state（事件先于 202 response 到达 / 用户已
// 离开 Pane 等场景），直接返回原 map 引用，让 React 不重渲。
export function applyScenarioRunSseEvent(
  map: ScenarioRunJobMap,
  scenarioRid: string,
  event: ScenarioRunSseEvent,
): ScenarioRunJobMap {
  const prior = map[scenarioRid];
  if (!prior) return map;
  switch (event.type) {
    case 'progress': {
      return setScenarioRunJobState(map, scenarioRid, {
        ...prior,
        status: 'running',
        progressByModel: { ...prior.progressByModel, [event.model]: event.pct },
      });
    }
    case 'result': {
      return setScenarioRunJobState(map, scenarioRid, {
        ...prior,
        status: 'running',
        resultsByModel: { ...prior.resultsByModel, [event.model]: event.output },
      });
    }
    case 'done': {
      return setScenarioRunJobState(map, scenarioRid, {
        ...prior,
        status: 'success',
        durationMs: event.durationMs,
        error: null,
      });
    }
    case 'error': {
      const message =
        event.model !== undefined
          ? `${event.model}: ${event.message}`
          : event.message;
      return setScenarioRunJobState(map, scenarioRid, {
        ...prior,
        status: 'error',
        error: message,
      });
    }
    case 'cancelled': {
      return setScenarioRunJobState(map, scenarioRid, {
        ...prior,
        status: 'cancelled',
      });
    }
  }
}

// getModelProgressPct —— React 接线层渲染 model 行 progress bar 时取
// 该 model 的 pct；未知 model 返回 null（React 可显示 spinner 而非 0）。
export function getModelProgressPct(
  state: ScenarioRunJobState,
  model: string,
): number | null {
  const pct = state.progressByModel[model];
  return typeof pct === 'number' ? pct : null;
}

// getOverallProgressPct —— Pane Run 列单 cell tooltip 用整体百分比。
// 成功态强制 100；无模型时 0；其他取已知模型平均。
export function getOverallProgressPct(state: ScenarioRunJobState): number {
  if (state.status === 'success') return 100;
  const values = Object.values(state.progressByModel);
  if (values.length === 0) return 0;
  const sum = values.reduce((acc, v) => acc + v, 0);
  return Math.round(sum / values.length);
}

export function getScenarioRunJobStatusIcon(
  status: ScenarioRunJobStatus,
): ScenarioRunJobStatusIcon {
  switch (status) {
    case 'idle':
      return '—';
    case 'pending':
    case 'running':
      return 'spinner';
    case 'success':
      return '✓';
    case 'error':
      return '×';
    case 'cancelled':
      return '⊘';
  }
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms} ms`;
  const seconds = ms / 1000;
  return `${seconds.toFixed(1)} s`;
}

export function getScenarioRunJobStatusTooltip(
  map: ScenarioRunJobMap,
  scenarioRid: string,
): string | null {
  const state = map[scenarioRid];
  if (!state || state.status === 'idle') return null;
  switch (state.status) {
    case 'pending':
      return 'Queued…';
    case 'running': {
      const values = Object.values(state.progressByModel);
      if (values.length === 0) return 'Running…';
      return `Running… ${getOverallProgressPct(state)}%`;
    }
    case 'success':
      if (state.durationMs === null) return 'Run completed';
      return `Run completed in ${formatDuration(state.durationMs)}`;
    case 'error':
      return state.error ?? SCENARIO_RUN_FALLBACK_MESSAGE;
    case 'cancelled':
      return 'Cancelled';
  }
}

// 给 React 接线层一个可用的 ScenarioRef immutable helper —— async run
// 成功后也要 freeze scenario。
export function immutableScenario(scenario: ScenarioRef): ScenarioRef {
  return { ...scenario, immutable: true };
}
