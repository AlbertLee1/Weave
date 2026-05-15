// VTX-045 — 自动跑 Baseline 对照的纯逻辑层。
//
// 调研报告 §4.1 step 5.4 + Scenario options：用户在 Scenario 顶部勾选
// "Run baseline"，点 Run Scenario 时前端并行 dispatch 两份请求：
//   (a) VTX-043/VTX-044 路径：scenario fork 含 override/action，跑用户改过
//       的副本；
//   (b) 本 story 路径：baseline fork 无 override/无 action，跑当前
//       ontology snapshot 的对照副本。
//
// baseline 完成后：
//   - Pane Baseline 列 cell renderer 调 getBaselineOutput(map, caseStudyRid,
//     buildPaneBaselineOutputKey(rowRid, paramName))。
//   - System Graph 上每个 Extended Label 用 getBaselineOutput +
//     buildObjectBaselineOutputKey 拿 baseline 值，combine simulated 后通过
//     computeBaselineCompare + formatBaselineCompareLabel 得到 "1500
//     (baseline 1000, +50.0%)" + 颜色 hint。
//
// 本模块交付：
//   - ScenarioBaselineOptions：runBaseline 开关 + setter + helper。
//   - BaselineRunMap：每个 CaseStudy 一份 BaselineRunState（status /
//     startedAt / durationMs / outputs / error）。Baseline 是 per-CaseStudy
//     概念（同 CaseStudy 下所有 Scenario 共享同一份 baseline），所以 map key
//     是 caseStudyRid 而非 scenarioRid。
//   - 状态机迁移：applyBaselineRunStart / Success / Error，applyStart 时
//     **清** 旧 outputs（rerun 期间 UI 不应显示陈旧 baseline 列值），
//     applyError 时 **保留** 旧 outputs（连接闪断后 UI 仍能显示上次成功
//     的 baseline 直到重试）。
//   - 请求构造：buildBaselineRunRequest → POST
//     /api/vertex/v1/case-studies/{rid}/baseline/run。
//   - 响应解析：parseBaselineRunSuccessResponse（durationMs + outputs Map）/
//     parseBaselineRunErrorResponse（message 兜底）。
//   - 输出读取 helper：buildPaneBaselineOutputKey（Pane 列 cell key 由 row
//     rid + paramName 构成）/ buildObjectBaselineOutputKey（Extended Label
//     key 由 objectType + primaryKey + property 构成）/ getBaselineOutput /
//     applyBaselineOutputs（merge 而非替换，让 React 层可分批写入）。
//   - dispatch 决策：shouldDispatchBaseline（options.runBaseline + 当前
//     status + 可选 forceRerun）—— 避免 success 状态重复 dispatch。
//   - 对比派生：computeBaselineCompare（simulated/baseline → delta/deltaPct/
//     colorHint，baseline=0 时 deltaPct=null 防除零）/ getBaselineColorHint /
//     formatBaselineCompareLabel（数字 → "1500"/"baseline 1000"/"+50.0%" 字
//     符串三件套，让 React 层按 colorHint 套 tailwind class）。

export interface ScenarioBaselineOptions {
  runBaseline: boolean;
}

export function createScenarioBaselineOptions(
  init?: Partial<ScenarioBaselineOptions>,
): ScenarioBaselineOptions {
  return { runBaseline: init?.runBaseline ?? false };
}

export function setRunBaseline(
  options: ScenarioBaselineOptions,
  enabled: boolean,
): ScenarioBaselineOptions {
  if (options.runBaseline === enabled) return options;
  return { ...options, runBaseline: enabled };
}

export function shouldRunBaseline(options: ScenarioBaselineOptions): boolean {
  return options.runBaseline === true;
}

export type BaselineRunStatus = 'idle' | 'running' | 'success' | 'error';

export type BaselineOutputValue = string | number | boolean | null;
export type BaselineOutputs = Record<string, BaselineOutputValue>;

export interface BaselineRunState {
  status: BaselineRunStatus;
  startedAt: number | null;
  durationMs: number | null;
  outputs: BaselineOutputs;
  error: string | null;
}

export type BaselineRunMap = Record<string, BaselineRunState>;

export const BASELINE_RUN_FAILED_MESSAGE = 'Baseline run failed';

function requireNonBlank(value: string, field: string): string {
  if (typeof value !== 'string' || value.trim().length === 0) {
    throw new Error(`${field} is required`);
  }
  return value.trim();
}

export function createBaselineRunMap(): BaselineRunMap {
  return {};
}

export function createBaselineRunState(): BaselineRunState {
  return {
    status: 'idle',
    startedAt: null,
    durationMs: null,
    outputs: {},
    error: null,
  };
}

export function setBaselineRunState(
  map: BaselineRunMap,
  caseStudyRid: string,
  state: BaselineRunState,
): BaselineRunMap {
  return { ...map, [caseStudyRid]: state };
}

export function getBaselineRunStatus(
  map: BaselineRunMap,
  caseStudyRid: string,
): BaselineRunStatus {
  return map[caseStudyRid]?.status ?? 'idle';
}

export function isBaselineRunning(
  map: BaselineRunMap,
  caseStudyRid: string,
): boolean {
  return getBaselineRunStatus(map, caseStudyRid) === 'running';
}

// applyBaselineRunStart —— idle/success/error → running。清旧 outputs（让
// Pane Baseline 列在 rerun 期间显示 spinner / 空，而非陈旧的上次值），清
// 旧 error/duration。startedAt 由调用方提供（通常 Date.now()），便于测试用
// fake clock 注入稳定时间戳。
export function applyBaselineRunStart(
  map: BaselineRunMap,
  caseStudyRid: string,
  startedAt: number,
): BaselineRunMap {
  return setBaselineRunState(map, caseStudyRid, {
    status: 'running',
    startedAt,
    durationMs: null,
    outputs: {},
    error: null,
  });
}

export function applyBaselineRunSuccess(
  map: BaselineRunMap,
  caseStudyRid: string,
  durationMs: number,
  outputs: BaselineOutputs,
): BaselineRunMap {
  const prior = map[caseStudyRid] ?? createBaselineRunState();
  return setBaselineRunState(map, caseStudyRid, {
    status: 'success',
    startedAt: prior.startedAt,
    durationMs,
    outputs: { ...outputs },
    error: null,
  });
}

// applyBaselineRunError —— rerun 失败时**保留**旧 outputs：上次 success
// 已经填充了 baseline 列 / Extended Label，连接闪断后用户看着 staleness
// 比看着"全空 + 红×"友好；error 字段单独承载失败信号，UI 层叠 tooltip
// 即可。如果调用方想强制清，传 `forceClearOutputs: true`。
export function applyBaselineRunError(
  map: BaselineRunMap,
  caseStudyRid: string,
  message: string,
): BaselineRunMap {
  const prior = map[caseStudyRid] ?? createBaselineRunState();
  return setBaselineRunState(map, caseStudyRid, {
    status: 'error',
    startedAt: prior.startedAt,
    durationMs: null,
    outputs: prior.outputs,
    error: message,
  });
}

export interface BaselineRunApiRequest {
  method: 'POST';
  path: string;
  body: Record<string, never>;
}

export function buildBaselineRunRequest(input: {
  caseStudyRid: string;
}): BaselineRunApiRequest {
  const caseStudyRid = requireNonBlank(input.caseStudyRid, 'caseStudyRid');
  return {
    method: 'POST',
    path: `/api/vertex/v1/case-studies/${encodeURIComponent(caseStudyRid)}/baseline/run`,
    body: {},
  };
}

export interface BaselineRunSuccessResponse {
  status: 'success';
  durationMs: number;
  outputs?: BaselineOutputs;
}

export interface BaselineRunErrorResponse {
  status: 'error';
  message?: string;
}

export interface ParsedBaselineRunSuccess {
  durationMs: number;
  outputs: BaselineOutputs;
}

export interface ParsedBaselineRunError {
  message: string;
}

function isScalar(v: unknown): v is BaselineOutputValue {
  if (v === null) return true;
  const t = typeof v;
  return t === 'string' || t === 'number' || t === 'boolean';
}

export function parseBaselineRunSuccessResponse(
  response: BaselineRunSuccessResponse,
): ParsedBaselineRunSuccess {
  if (typeof response.durationMs !== 'number' || !Number.isFinite(response.durationMs)) {
    throw new Error('durationMs is required');
  }
  if (response.durationMs < 0) {
    throw new Error('durationMs must be non-negative');
  }
  const outputs: BaselineOutputs = {};
  if (response.outputs) {
    for (const [k, v] of Object.entries(response.outputs)) {
      if (!isScalar(v)) {
        throw new Error(`output ${k} must be a scalar (string | number | boolean | null)`);
      }
      outputs[k] = v;
    }
  }
  return { durationMs: response.durationMs, outputs };
}

export function parseBaselineRunErrorResponse(
  response: BaselineRunErrorResponse,
): ParsedBaselineRunError {
  const raw = typeof response.message === 'string' ? response.message.trim() : '';
  return { message: raw.length > 0 ? raw : BASELINE_RUN_FAILED_MESSAGE };
}

// Output key builders —— 两条 keying 路径共存：
//   - Pane Baseline 列 cell：(rowRid, paramName) 二段 key，与 VTX-040 cell
//     key 中 row + param 部分形态一致。
//   - System Graph Extended Label：(objectType, primaryKey, property) 三段
//     key，与 VTX-041 objectPropertyOverride 一致。
// 一份 outputs map 里两种 key 不会冲突（一个 2 段、一个 3 段，且语义域不同），
// React 层按渲染上下文选 builder。

export function buildPaneBaselineOutputKey(rowRid: string, paramName: string): string {
  const rid = requireNonBlank(rowRid, 'rowRid');
  const name = requireNonBlank(paramName, 'paramName');
  return `${rid}::${name}`;
}

export function buildObjectBaselineOutputKey(
  objectType: string,
  primaryKey: string,
  property: string,
): string {
  const ot = requireNonBlank(objectType, 'objectType');
  const pk = requireNonBlank(primaryKey, 'primaryKey');
  const prop = requireNonBlank(property, 'property');
  return `${ot}::${pk}::${prop}`;
}

export function getBaselineOutput(
  map: BaselineRunMap,
  caseStudyRid: string,
  key: string,
): BaselineOutputValue {
  const state = map[caseStudyRid];
  if (!state) return null;
  const v = state.outputs[key];
  return v === undefined ? null : v;
}

// applyBaselineOutputs —— 批量 merge 新输出到现有 outputs（不替换其他 key）。
// 用于 SSE 流式 baseline run（VTX-044 风格）逐步喂结果；同 key 后写覆盖前
// 写。若该 caseStudyRid 还没 state（极少见，正常顺序是 applyStart →
// applySuccess），就用 createBaselineRunState 兜底（idle status 不动）。
export function applyBaselineOutputs(
  map: BaselineRunMap,
  caseStudyRid: string,
  outputs: BaselineOutputs,
): BaselineRunMap {
  const prior = map[caseStudyRid] ?? createBaselineRunState();
  return setBaselineRunState(map, caseStudyRid, {
    ...prior,
    outputs: { ...prior.outputs, ...outputs },
  });
}

export interface ShouldDispatchBaselineInput {
  options: ScenarioBaselineOptions;
  currentStatus: BaselineRunStatus;
  forceRerun?: boolean;
}

// shouldDispatchBaseline —— Run Scenario 按钮按下时 React 接线层调它决
// 定是否并行 dispatch baseline 请求。语义：
//   - options.runBaseline=false → 永远 false（即使 forceRerun=true）。开关
//     是更强的"用户意愿"信号，禁用时不应有任何 baseline 流量。
//   - 已 running → false（避免重复 dispatch 撞同一份 fork）。
//   - 已 success → 默认 false（baseline 是 per-CaseStudy 缓存，ontology 没
//     变就复用上次结果；React 层可以传 forceRerun=true 显式 invalidate）。
//   - 已 error → true（重试路径，让用户改完连接再点 Run 时自动 retry）。
export function shouldDispatchBaseline(input: ShouldDispatchBaselineInput): boolean {
  if (!shouldRunBaseline(input.options)) return false;
  if (input.currentStatus === 'running') return false;
  if (input.currentStatus === 'success') return input.forceRerun === true;
  // idle | error → dispatch
  return true;
}

// ------------- Baseline vs simulated compare -------------

export type BaselineCompareColor = 'positive' | 'negative' | 'neutral';

export interface BaselineCompareResult {
  simulated: number;
  baseline: number;
  delta: number;
  deltaPct: number | null;
  colorHint: BaselineCompareColor;
}

export function computeBaselineCompare(input: {
  simulated: number;
  baseline: number;
}): BaselineCompareResult {
  const { simulated, baseline } = input;
  if (typeof simulated !== 'number' || !Number.isFinite(simulated)) {
    throw new Error('simulated must be a finite number');
  }
  if (typeof baseline !== 'number' || !Number.isFinite(baseline)) {
    throw new Error('baseline must be a finite number');
  }
  const delta = simulated - baseline;
  const deltaPct = baseline === 0 ? null : (delta / baseline) * 100;
  let colorHint: BaselineCompareColor;
  if (delta > 0) colorHint = 'positive';
  else if (delta < 0) colorHint = 'negative';
  else colorHint = 'neutral';
  return { simulated, baseline, delta, deltaPct, colorHint };
}

export function getBaselineColorHint(
  result: BaselineCompareResult,
): BaselineCompareColor {
  return result.colorHint;
}

export interface FormatBaselineCompareOpts {
  decimals?: number;
  hideDelta?: boolean;
}

export interface FormattedBaselineCompare {
  simulated: string;
  baseline: string;
  delta: string;
  colorHint: BaselineCompareColor;
}

function formatNumber(value: number, decimals: number): string {
  if (decimals === 0) {
    // Number.isInteger 对小数 toFixed(0) 自动 round；用 Math.round 避免
    // "-0" 字面值（Math.round(-0.4) → 0，避免 toFixed 的 "-0"）。
    return String(Math.round(value));
  }
  return value.toFixed(decimals);
}

function formatDeltaPct(deltaPct: number, decimals: number): string {
  // 百分比固定一位小数（与 Foundry Vertex 习惯一致），允许调用方按需
  // 覆盖。decimals 在 deltaPct=null 路径走绝对值时复用。
  void decimals;
  const sign = deltaPct > 0 ? '+' : '';
  return `${sign}${deltaPct.toFixed(1)}%`;
}

export function formatBaselineCompareLabel(
  result: BaselineCompareResult,
  opts?: FormatBaselineCompareOpts,
): FormattedBaselineCompare {
  const decimals = opts?.decimals ?? 0;
  const simulated = formatNumber(result.simulated, decimals);
  const baseline = `baseline ${formatNumber(result.baseline, decimals)}`;
  let delta = '';
  if (!opts?.hideDelta) {
    if (result.deltaPct === null) {
      // baseline === 0 —— 百分比无意义，回退到带符号的绝对 delta。
      if (result.delta === 0) {
        delta = '0';
      } else {
        const sign = result.delta > 0 ? '+' : '';
        delta = `${sign}${formatNumber(result.delta, decimals)}`;
      }
    } else {
      delta = formatDeltaPct(result.deltaPct, decimals);
    }
  }
  return { simulated, baseline, delta, colorHint: result.colorHint };
}
