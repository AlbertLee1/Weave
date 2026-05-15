// VTX-059 — Property Extended Label 渲染的纯逻辑层。
//
// 节点 DOM overlay 上展示一条 `{labelName}: {valueText}` 文本。属性值缺失
// （字段不存在 / null / undefined / 空字符串 / NaN）时 valueText 退化为
// `—`，并把 status 标为 'missing' 供 React 接线层按状态切换字号 / 颜色。
//
// 本模块不依赖 React / DOM —— 只产出文本 + 状态枚举 + 命中状态。后端 schema
// 由 pkg/vertex/graphsvc.ExtendedLabel 定义（VTX-058 已锁 kind/property/
// timeSeriesRid/measureRid 字段名），本模块的 PropertyExtendedLabelSpec
// 与 wire 形态 1:1。displayName 字段不在后端 schema 里 —— 它是 React 接
// 线层从 ObjectType.properties[name].displayName 取的渲染时增强，本模块
// 直接接受可选 displayName，避免接线层再绕一层 helper。
//
// 与 VTX-060（TimeSeries kind）/ VTX-061（Measure kind）的渲染模块共用
// MISSING_VALUE_PLACEHOLDER / PropertyLabelStatus / PropertyLabelRenderResult
// 命名空间，让 React 节点 overlay 组件不必按 kind 写三套字段。

export const MISSING_VALUE_PLACEHOLDER = '—';

export type PropertyLabelStatus = 'present' | 'missing';

export interface PropertyExtendedLabelSpec {
  kind: 'property';
  property: string;
  // displayName 非 wire 字段；React 接线层从 ObjectType metadata 取后注入。
  // 留空 / 仅空白 → fallback 到 property 名（与 VTX-039 buildInputOutputColumns
  // 的 `displayName ?? name` fallback 同形态）。
  displayName?: string;
}

// 节点对象的最小投影：保留 __rid/__apiName/__primaryKey + 任意 property bag。
// 与 web/src/api/types.ts WireObject 兼容但不强绑，避免单测必须造完整 wire
// 对象（同 VTX-041 WireObjectLike 设计）。
export interface PropertyLabelObjectLike {
  [property: string]: unknown;
}

export interface PropertyLabelRenderResult {
  status: PropertyLabelStatus;
  labelName: string;
  valueText: string;
  text: string;
}

export interface RenderPropertyExtendedLabelOptions {
  // 自定义值格式化：React 接线层可注入 percentage / currency / duration
  // 等专用 formatter。formatter 收 raw 值，返回展示字符串；返回空字符串
  // 视为 missing 走 placeholder。
  formatValue?: (raw: unknown) => string;
  // 自定义 missing placeholder（默认 '—'）。React 接线层做国际化时覆盖。
  missingPlaceholder?: string;
}

export function isPropertyExtendedLabel(label: { kind: string; [key: string]: unknown }): boolean {
  return label.kind === 'property';
}

export function resolvePropertyValue(obj: PropertyLabelObjectLike, property: string): unknown {
  if (typeof property !== 'string' || property.trim().length === 0) {
    throw new Error('property name is required');
  }
  // 直接读 obj[property]；underscore-prefixed meta 字段（__apiName 等）也走同
  // 路径。obj 是 wire bag 不需要 Object.prototype.hasOwnProperty.call —— vitest
  // 单测构造的对象是 plain object，hasOwn 与 in 在此 contract 上等价。
  return obj[property];
}

export function isPropertyValueMissing(v: unknown): boolean {
  if (v === undefined || v === null) return true;
  if (typeof v === 'number' && Number.isNaN(v)) return true;
  if (typeof v === 'string' && v.trim().length === 0) return true;
  return false;
}

export function formatScalarPropertyValue(v: unknown): string {
  if (isPropertyValueMissing(v)) return MISSING_VALUE_PLACEHOLDER;
  if (typeof v === 'string') return v;
  if (typeof v === 'number') return String(v);
  if (typeof v === 'boolean') return String(v);
  // Defensive: object / array / Date / function → JSON encode（Date 退化为
  // toISOString-equivalent 字符串）。React 接线层若需要更友好的 Date 展示，
  // 通过 RenderPropertyExtendedLabelOptions.formatValue 注入自己的 formatter。
  try {
    return JSON.stringify(v);
  } catch {
    return MISSING_VALUE_PLACEHOLDER;
  }
}

export function renderPropertyExtendedLabel(
  spec: PropertyExtendedLabelSpec,
  obj: PropertyLabelObjectLike,
  options: RenderPropertyExtendedLabelOptions = {},
): PropertyLabelRenderResult {
  if (spec.kind !== 'property') {
    throw new Error(`renderPropertyExtendedLabel: expected kind="property", got "${spec.kind}"`);
  }
  if (typeof spec.property !== 'string' || spec.property.trim().length === 0) {
    throw new Error('renderPropertyExtendedLabel: spec.property is required');
  }

  const labelName = (() => {
    const dn = spec.displayName;
    if (typeof dn === 'string' && dn.trim().length > 0) return dn;
    return spec.property;
  })();

  const rawValue = resolvePropertyValue(obj, spec.property);
  const placeholder = options.missingPlaceholder ?? MISSING_VALUE_PLACEHOLDER;

  let status: PropertyLabelStatus;
  let valueText: string;

  if (isPropertyValueMissing(rawValue)) {
    status = 'missing';
    valueText = placeholder;
  } else if (options.formatValue) {
    const formatted = options.formatValue(rawValue);
    if (typeof formatted !== 'string' || formatted.length === 0) {
      status = 'missing';
      valueText = placeholder;
    } else {
      status = 'present';
      valueText = formatted;
    }
  } else {
    const fallbackFormatted = formatScalarPropertyValue(rawValue);
    // formatScalarPropertyValue 对 missing 自己已返回 MISSING_VALUE_PLACEHOLDER，
    // 但 rawValue 通过了 isPropertyValueMissing 检查，所以这里不会命中 missing
    // 路径。仍保留双重防御让 future 扩展（如新增 missing 形态）不必两处同步。
    if (fallbackFormatted === MISSING_VALUE_PLACEHOLDER) {
      status = 'missing';
      valueText = placeholder;
    } else {
      status = 'present';
      valueText = fallbackFormatted;
    }
  }

  return {
    status,
    labelName,
    valueText,
    text: `${labelName}: ${valueText}`,
  };
}
