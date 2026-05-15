// VTX-062 — baseline vs simulated 对比 Extended Label 渲染的纯逻辑层。
//
// 调研报告 §2.2 + §4.5：当 Scenario 和 Baseline 都已 Run 时，节点 DOM
// overlay 的 Extended Label 自动从单值 "1500" 升级为对比形态：
//
//   totalAlerts: 1500 (baseline 1000, +50.0%)
//                                       ^^^^^^ 上色 (positive=绿 / negative=红)
//
// 本模块在 VTX-045 baselineRun (computeBaselineCompare / formatBaseline
// CompareLabel) + VTX-047 multiScenarioCompare (ActiveScenarioState /
// ScenarioOutputsByRid) 之上新增两件事：
//   1. selectBaselineCompareValues —— 把当前 active scenario 列对应的
//      simulated 值与 baseline 值从 outputs map 拣出来。当 active=null
//      （主视图是 Baseline）时 simulated=null，渲染走 baseline-only。
//   2. renderBaselineCompareExtendedLabel —— 状态机：
//        - explicit error 优先 → 'error'，simulated/baseline 段空，valueText='!'
//        - active=null → 'baseline-only'：仅显示 baseline scalar，无 delta
//          (主视图是 Baseline，不存在比较基线)
//        - active≠null + 两侧都是 finite number → 'present'：完整对比串
//        - active≠null + simulated 缺/baseline 缺 / NaN/string → 'missing-*'
//          细分子状态，方便 React 接线层切换字体颜色 / tooltip。
//
// BDD #2 ("用户切换 active scenario 列 → label 切换对比基线") 在本模块是
// 纯派生：activeState 改了，selectBaselineCompareValues 重算 simulated；
// React 层只需在 activeState 变化时 re-render 同一个节点 overlay。
//
// 与 VTX-059/060/061 同 namespace 复用 MISSING_VALUE_PLACEHOLDER /
// ERROR_PLACEHOLDER，让 React overlay 组件不必按 kind 写多套字段。

import {
  type BaselineCompareColor,
  type BaselineCompareResult,
  type BaselineOutputValue,
  type BaselineOutputs,
  type FormatBaselineCompareOpts,
  buildObjectBaselineOutputKey,
  computeBaselineCompare,
  formatBaselineCompareLabel,
} from '../scenarioPane/baselineRun';
import type {
  ActiveScenarioState,
  ScenarioOutputsByRid,
} from '../scenarioPane/multiScenarioCompare';
import { MISSING_VALUE_PLACEHOLDER } from './propertyLabel';
import { ERROR_PLACEHOLDER } from './timeSeriesLabel';

export { ERROR_PLACEHOLDER, MISSING_VALUE_PLACEHOLDER };

export type BaselineCompareLabelStatus =
  | 'present'
  | 'baseline-only'
  | 'missing'
  | 'missing-baseline'
  | 'missing-simulated'
  | 'error';

export interface BaselineCompareLabelSpec {
  /**
   * Display label name (e.g. property name, timeSeries display name, measure
   * function display name)。React 接线层从对应 spec/metadata 取后注入；本模块
   * 不与具体 ExtendedLabel kind 耦合，避免每加一种 kind 就要改这里。
   */
  labelName: string;
  /** 渲染时增强；留空 / 仅空白 → fallback 到 labelName。 */
  displayName?: string;
}

export interface BaselineCompareLabelObjectContext {
  objectType: string;
  primaryKey: string;
  property: string;
}

export interface BaselineCompareLabelInput {
  activeState: ActiveScenarioState;
  baselineOutputs: BaselineOutputs;
  scenarioOutputsByRid: ScenarioOutputsByRid;
  context: BaselineCompareLabelObjectContext;
}

export interface BaselineCompareLabelValues {
  simulated: BaselineOutputValue;
  baseline: BaselineOutputValue;
  activeScenarioRid: string | null;
}

export function selectBaselineCompareValues(
  input: BaselineCompareLabelInput,
): BaselineCompareLabelValues {
  const key = buildObjectBaselineOutputKey(
    input.context.objectType,
    input.context.primaryKey,
    input.context.property,
  );
  const baseline = key in input.baselineOutputs ? input.baselineOutputs[key] : null;
  const activeRid = input.activeState.activeScenarioRid;
  let simulated: BaselineOutputValue = null;
  if (activeRid !== null) {
    const scenarioOutputs = input.scenarioOutputsByRid[activeRid];
    if (scenarioOutputs && key in scenarioOutputs) {
      simulated = scenarioOutputs[key];
    }
  }
  return { simulated, baseline, activeScenarioRid: activeRid };
}

export interface BaselineCompareLabelRenderResult {
  status: BaselineCompareLabelStatus;
  labelName: string;
  /** 主视图当前列的 scalar 文本（baseline-only 时是 baseline 值）。 */
  simulatedText: string;
  /** "baseline NNN" 段；baseline-only / error 时为空字符串。 */
  baselineText: string;
  /** delta 段（"+50.0%" / "-5"）；缺 simulated/baseline / hideDelta / error 时为空。 */
  deltaText: string;
  /** 完整一行文本，React 层可直接插 overlay。 */
  text: string;
  /** 仅 'present' 状态下非 null（绿/红/中性）。其它状态由 React 用统一灰色渲染。 */
  colorHint: BaselineCompareColor | null;
  /** error 时的 tooltip 文本；其它状态 undefined。 */
  errorMessage?: string;
  /** 'present' 时携带原始数值 + delta，方便 React 直接渲染高级图表。 */
  compare?: BaselineCompareResult;
}

export interface RenderBaselineCompareLabelOptions extends FormatBaselineCompareOpts {
  missingPlaceholder?: string;
  errorPlaceholder?: string;
  /**
   * 设置后 status 强制 'error'，simulated 段用 errorPlaceholder（默认 '!'）。
   * 空字符串视为 "无错误"，回退到正常状态机；避免接线层把 '' 当默认值
   * 误触 error 态（与 VTX-060/061 同语义）。
   */
  error?: string;
}

function requireNonBlank(value: string, field: string): string {
  if (typeof value !== 'string' || value.trim().length === 0) {
    throw new Error(`${field} is required`);
  }
  return value;
}

function isFiniteNumber(v: unknown): v is number {
  return typeof v === 'number' && Number.isFinite(v);
}

function resolveLabelName(spec: BaselineCompareLabelSpec): string {
  requireNonBlank(spec.labelName, 'labelName');
  const dn = spec.displayName;
  if (typeof dn === 'string' && dn.trim().length > 0) return dn;
  return spec.labelName;
}

function formatScalarFallback(v: number, decimals: number | undefined): string {
  if (decimals === undefined) return String(v);
  if (decimals === 0) return String(Math.round(v));
  return v.toFixed(decimals);
}

export function renderBaselineCompareExtendedLabel(
  spec: BaselineCompareLabelSpec,
  values: BaselineCompareLabelValues,
  options: RenderBaselineCompareLabelOptions = {},
): BaselineCompareLabelRenderResult {
  const labelName = resolveLabelName(spec);
  const missingPlaceholder = options.missingPlaceholder ?? MISSING_VALUE_PLACEHOLDER;
  const errorPlaceholder = options.errorPlaceholder ?? ERROR_PLACEHOLDER;

  // Error 优先（与 VTX-060/061 一致）。空字符串视为无错误回退。
  if (typeof options.error === 'string' && options.error.length > 0) {
    return {
      status: 'error',
      labelName,
      simulatedText: errorPlaceholder,
      baselineText: '',
      deltaText: '',
      text: `${labelName}: ${errorPlaceholder}`,
      colorHint: null,
      errorMessage: options.error,
    };
  }

  const baselineFinite = isFiniteNumber(values.baseline);
  const simulatedFinite = isFiniteNumber(values.simulated);

  // baseline-only：主视图是 Baseline (active=null)。展示 baseline 单值即可，
  // 不挂 "(baseline N)" 重复段，也不显示 delta。
  if (values.activeScenarioRid === null) {
    if (!baselineFinite) {
      return {
        status: 'missing',
        labelName,
        simulatedText: missingPlaceholder,
        baselineText: '',
        deltaText: '',
        text: `${labelName}: ${missingPlaceholder}`,
        colorHint: null,
      };
    }
    const baselineNum = values.baseline as number;
    const valueText = formatScalarFallback(baselineNum, options.decimals);
    return {
      status: 'baseline-only',
      labelName,
      simulatedText: valueText,
      baselineText: `baseline ${valueText}`,
      deltaText: '',
      text: `${labelName}: ${valueText}`,
      colorHint: null,
    };
  }

  // active 是 scenario 但缺 simulated 或 baseline。两侧都缺 → 'missing'；
  // 缺其中一侧 → 细分子状态，让 React 层在 tooltip / 下钻 UI 上区分原因
  // (eg. baseline 还没跑完 vs scenario 数据缺失)。
  if (!simulatedFinite && !baselineFinite) {
    return {
      status: 'missing',
      labelName,
      simulatedText: missingPlaceholder,
      baselineText: '',
      deltaText: '',
      text: `${labelName}: ${missingPlaceholder}`,
      colorHint: null,
    };
  }

  if (!baselineFinite) {
    const simulatedNum = values.simulated as number;
    const simulatedText = formatScalarFallback(simulatedNum, options.decimals);
    return {
      status: 'missing-baseline',
      labelName,
      simulatedText,
      baselineText: `baseline ${missingPlaceholder}`,
      deltaText: '',
      text: `${labelName}: ${simulatedText} (baseline ${missingPlaceholder})`,
      colorHint: null,
    };
  }

  if (!simulatedFinite) {
    const baselineNum = values.baseline as number;
    const baselineText = `baseline ${formatScalarFallback(baselineNum, options.decimals)}`;
    return {
      status: 'missing-simulated',
      labelName,
      simulatedText: missingPlaceholder,
      baselineText,
      deltaText: '',
      text: `${labelName}: ${missingPlaceholder} (${baselineText})`,
      colorHint: null,
    };
  }

  // present：两侧都是 finite number → 走 VTX-045 compute + format 全套。
  const compare = computeBaselineCompare({
    simulated: values.simulated as number,
    baseline: values.baseline as number,
  });
  const formatted = formatBaselineCompareLabel(compare, {
    decimals: options.decimals,
    hideDelta: options.hideDelta,
  });

  const segments: string[] = [`(${formatted.baseline}`];
  if (formatted.delta !== '') segments[0] = `(${formatted.baseline}, ${formatted.delta}`;
  segments[0] = `${segments[0]})`;
  const tail = segments[0];
  const text = `${labelName}: ${formatted.simulated} ${tail}`;

  return {
    status: 'present',
    labelName,
    simulatedText: formatted.simulated,
    baselineText: formatted.baseline,
    deltaText: formatted.delta,
    text,
    colorHint: formatted.colorHint,
    compare,
  };
}
