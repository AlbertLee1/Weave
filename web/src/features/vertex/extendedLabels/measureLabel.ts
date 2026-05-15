// VTX-061 — Measure / Derived Property Function Label 渲染的纯逻辑层。
//
// 第三种 Extended Label：节点 DOM overlay 把 measure 函数的 scalar 结果展示
// 为 `{labelName}: {valueText}`。measure 是 OMS Function 的一个语义角色——
// 输入一个对象（或对象 RID + 任意 link traversal 由 function 自己 SDK 端做），
// 输出一个 scalar。本模块只关心前端的 4 件事：
//   1. spec 校验（与 wire schema VTX-058 ExtendedLabel { kind:'measure',
//      measureRid:'...' } 对齐；displayName 是 React 接线层渲染时增强）
//   2. buildMeasureLabelRequest —— OMS Function execute 端点的 URL / body
//      builder，对齐 pkg/oms/handlers_function.go ExecuteFunction:
//      POST /api/v2/ontologies/{o}/functions/{rid}/execute
//      body: { parameters: { <inputParamName>: <objectRid> } }
//   3. renderMeasureExtendedLabel —— scalar → {status, labelName, valueText,
//      text}，error 态用红色 ! + tooltip（status='error'，与 VTX-060
//      TimeSeriesLabel 同形态：error > missing > present）
//   4. isMeasureExtendedLabel —— kind discriminator helper
//
// BDD #2（"measure function 涉及多跳 link When 调用 Then function 用 SDK
// link traversal API"）在前端层是 **opaque**：请求 body 只携带对象 RID，
// link traversal 完全是 Python 端 function 自己的事。本模块的测试用
// "请求 body 只含 object RID" 来体现这条契约。

import { MISSING_VALUE_PLACEHOLDER } from './propertyLabel';
import { ERROR_PLACEHOLDER } from './timeSeriesLabel';

export { ERROR_PLACEHOLDER, MISSING_VALUE_PLACEHOLDER };

export type MeasureLabelStatus = 'present' | 'missing' | 'error';

// MEASURE_LABEL_DEFAULT_INPUT_PARAM —— measure function 的默认输入参数名。
// 真实 function signature 由后端 OMS 维护；前端不解析 signature，按约定用
// `object` 作为输入参数名。React 接线层若 function 用不同的参数名，传
// inputParamName 覆盖；这避免每个节点渲染前都先 GET function metadata。
export const MEASURE_LABEL_DEFAULT_INPUT_PARAM = 'object';

export interface MeasureExtendedLabelSpec {
  kind: 'measure';
  measureRid: string;
  // displayName 非 wire 字段；React 接线层从 Function metadata 或 ObjectType
  // measure binding 取后注入。留空 / 仅空白 → fallback 到 measureRid。
  displayName?: string;
}

export interface MeasureLabelRequestContext {
  ontology: string;
  objectRid: string;
}

export interface BuildMeasureLabelRequestOptions {
  // 覆盖默认输入参数名。Function signature 不在前端可见，接线层若知道某个
  // measure 用其它参数名（如 `airport`）就传进来。
  inputParamName?: string;
}

export interface MeasureLabelRequest {
  method: 'POST';
  path: string;
  body: {
    parameters: Record<string, string>;
  };
}

export interface MeasureLabelRenderResult {
  status: MeasureLabelStatus;
  labelName: string;
  valueText: string;
  text: string;
  errorMessage?: string;
}

export interface RenderMeasureExtendedLabelOptions {
  // 自定义 scalar 格式化；返回空字符串视为 missing。React 接线层注入
  // toFixed / 千分位 / 单位后缀等专用 formatter。
  formatValue?: (raw: number) => string;
  missingPlaceholder?: string;
  errorPlaceholder?: string;
  // 设置后 status 强制 'error'，valueText 用 errorPlaceholder（默认 '!'）。
  // 空字符串视为"无错误"，回退到 scalar 渲染（避免接线层用 '' 默认值
  // 误触 error 态）。
  error?: string;
}

function requireNonBlank(value: string, field: string): string {
  if (typeof value !== 'string' || value.trim().length === 0) {
    throw new Error(`${field} is required`);
  }
  return value;
}

export function isMeasureExtendedLabel(label: { kind: string; [key: string]: unknown }): boolean {
  return label.kind === 'measure';
}

export function buildMeasureLabelRequest(
  spec: MeasureExtendedLabelSpec,
  ctx: MeasureLabelRequestContext,
  options: BuildMeasureLabelRequestOptions = {},
): MeasureLabelRequest {
  if (spec.kind !== 'measure') {
    throw new Error(`buildMeasureLabelRequest: expected kind="measure", got "${spec.kind}"`);
  }
  requireNonBlank(spec.measureRid, 'measureRid');
  requireNonBlank(ctx.ontology, 'ontology');
  requireNonBlank(ctx.objectRid, 'objectRid');

  const inputParamName =
    options.inputParamName !== undefined
      ? requireNonBlank(options.inputParamName, 'inputParamName')
      : MEASURE_LABEL_DEFAULT_INPUT_PARAM;

  const segments = [
    'api',
    'v2',
    'ontologies',
    encodeURIComponent(ctx.ontology),
    'functions',
    encodeURIComponent(spec.measureRid),
    'execute',
  ];
  const path = `/${segments.join('/')}`;

  return {
    method: 'POST',
    path,
    body: {
      parameters: { [inputParamName]: ctx.objectRid },
    },
  };
}

function isMissingScalar(v: number | null | undefined): boolean {
  if (v === null || v === undefined) return true;
  if (typeof v !== 'number' || !Number.isFinite(v)) return true;
  return false;
}

function formatScalar(v: number): string {
  return String(v);
}

export function renderMeasureExtendedLabel(
  spec: MeasureExtendedLabelSpec,
  scalar: number | null,
  options: RenderMeasureExtendedLabelOptions = {},
): MeasureLabelRenderResult {
  if (spec.kind !== 'measure') {
    throw new Error(`renderMeasureExtendedLabel: expected kind="measure", got "${spec.kind}"`);
  }
  requireNonBlank(spec.measureRid, 'measureRid');

  const labelName = (() => {
    const dn = spec.displayName;
    if (typeof dn === 'string' && dn.trim().length > 0) return dn;
    return spec.measureRid;
  })();

  const missingPlaceholder = options.missingPlaceholder ?? MISSING_VALUE_PLACEHOLDER;
  const errorPlaceholder = options.errorPlaceholder ?? ERROR_PLACEHOLDER;

  // Error 态优先于 present / missing：上游 fetch / 函数执行失败时即使 scalar
  // 有值（陈旧数据）也走 error 路径。空字符串视为"无错误"。
  if (typeof options.error === 'string' && options.error.length > 0) {
    return {
      status: 'error',
      labelName,
      valueText: errorPlaceholder,
      text: `${labelName}: ${errorPlaceholder}`,
      errorMessage: options.error,
    };
  }

  if (isMissingScalar(scalar)) {
    return {
      status: 'missing',
      labelName,
      valueText: missingPlaceholder,
      text: `${labelName}: ${missingPlaceholder}`,
    };
  }

  const valueText = (() => {
    const n = scalar as number;
    if (options.formatValue) {
      const formatted = options.formatValue(n);
      if (typeof formatted !== 'string' || formatted.length === 0) return '';
      return formatted;
    }
    return formatScalar(n);
  })();

  if (valueText === '') {
    return {
      status: 'missing',
      labelName,
      valueText: missingPlaceholder,
      text: `${labelName}: ${missingPlaceholder}`,
    };
  }

  return {
    status: 'present',
    labelName,
    valueText,
    text: `${labelName}: ${valueText}`,
  };
}
