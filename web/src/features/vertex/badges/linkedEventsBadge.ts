// VTX-063 — Badges（Linked Events 计数 + 自定义图标）纯逻辑层。
//
// 调研报告 §2.1 graph schema badges 字段：节点角标（小气泡）用于显示
// 与本节点关联的某类 Event ObjectType 的数量 + 自定义图标，hover 时展开
// tooltip 列出前 N 个 event（默认 5）。本模块的输入：
//   - spec: { kind:'linkedEvents', objectType, linkType, icon?, displayName?,
//     tooltipEventLimit? } —— 角标定义。kind 用 discriminator 与 VTX-058
//     ExtendedLabel ('property'|'timeSeries'|'measure') 区分；这是节点角标
//     而非 overlay label，命名空间独立。
//   - 渲染输入: { count?, events?, loading?, error? } —— 接线层把 OSS
//     listLinkedObjects 拿到的总数 + 头 N 个事件喂进来。count===0 → 'empty'，
//     count>0 → 'present'，loading=true 或缺 count → 'loading'，error → 'error'
//     (error 优先于 count，避免显示陈旧数据)。
//
// URL 形态对齐 OSS listLinkedObjects（web/src/api/objects.ts:79-90）：
//   GET /api/v2/ontologies/{ontology}/objects/{objectType}/{primaryKey}
//       /links/{linkType}?pageSize=<N>
// pageSize 默认与 spec.tooltipEventLimit 一致（默认 5）—— 单次拉到的事件
// 就是 tooltip 要展示的事件，无需二次请求。总数需求由 ObjectPage.totalCount
// 字段（或接线层 fallback 计数策略）承担，本模块只接 count。
//
// 与 VTX-059/060/061/062 Extended Label 模块解耦：
//   - 不复用 ERROR_PLACEHOLDER 默认 icon（angle bracket vs '•' 视觉差异，
//     badge 用 '!' 单字符做 error icon 仍合适）—— re-export 以便接线层
//     按需统一 placeholder 字典。
//   - 不继承 ExtendedLabel render 三态 (present/missing/error)，本模块用
//     四态 (present/empty/loading/error)：empty (count=0) 在 badge 上下文有
//     独立语义 (React 层可选 hide 整颗 badge)，与 missing (字段缺失) 不同。

import { ERROR_PLACEHOLDER } from '../extendedLabels/timeSeriesLabel';

export { ERROR_PLACEHOLDER };

export const BADGE_TOOLTIP_MAX_EVENTS = 5;
export const BADGE_DEFAULT_ICON = '•';

export type LinkedEventsBadgeStatus = 'present' | 'empty' | 'loading' | 'error';

export interface LinkedEventsBadgeSpec {
  kind: 'linkedEvents';
  /** Event ObjectType apiName（也是 OSS path 段 + 默认显示文本）。 */
  objectType: string;
  /** OSS link traversal 名（path 段 /links/{linkType}）。 */
  linkType: string;
  /** 自定义角标图标字符（emoji / Unicode glyph / React 层映射 key）。 */
  icon?: string;
  /** 显示标签覆盖 objectType；留空 / 仅空白 → fallback 到 objectType。 */
  displayName?: string;
  /** 单 badge tooltip 显示事件上限；默认 BADGE_TOOLTIP_MAX_EVENTS=5。 */
  tooltipEventLimit?: number;
}

export interface LinkedEventsBadgeRequestContext {
  ontology: string;
  sourceObjectType: string;
  sourcePrimaryKey: string | number;
}

export interface BuildLinkedEventsBadgeRequestOptions {
  /** 覆盖默认 pageSize（默认取 spec.tooltipEventLimit ?? BADGE_TOOLTIP_MAX_EVENTS）。 */
  pageSize?: number;
}

export interface LinkedEventsBadgeRequest {
  method: 'GET';
  path: string;
}

/**
 * 节点对象的最小投影 —— 与 web/src/api/types.ts WireObject 兼容但不强绑，
 * 避免单测构造完整 wire 对象。tooltip 列表可读取 __rid / __primaryKey /
 * eventStart / 自定义 labelProperty。
 */
export interface LinkedEventsBadgeEventLike {
  __rid: string;
  __apiName?: string;
  __primaryKey: string | number;
  eventStart?: number;
  eventEnd?: number;
  [property: string]: unknown;
}

export interface LinkedEventsBadgeTooltipItem {
  rid: string;
  /** 优先用 spec/options.labelProperty 字段；否则用 __primaryKey 字符串。 */
  label: string;
  /** 事件起始时间戳（毫秒），缺失时 undefined。 */
  eventStart?: number;
}

export interface BuildLinkedEventsBadgeTooltipItemsOptions {
  limit?: number;
  /**
   * 用作 label 的事件字段名（如 'description', 'title'）；未指定或字段缺失
   * 时 fallback 到 __primaryKey。
   */
  labelProperty?: string;
}

export interface LinkedEventsBadgeRenderInput {
  /** 已知总数；undefined + 无 loading/error → 默认 'loading'。 */
  count?: number;
  /** 已拉到的事件列表（通常前 N 个，用于 tooltip）。 */
  events?: LinkedEventsBadgeEventLike[];
  /** 显式 loading 态（fetch 仍在飞）。 */
  loading?: boolean;
  /** 显式 error 态；非空字符串触发 'error' 状态，空串视为无错误。 */
  error?: string;
}

export interface RenderLinkedEventsBadgeOptions {
  labelProperty?: string;
}

export interface LinkedEventsBadgeRenderResult {
  status: LinkedEventsBadgeStatus;
  /** present/empty 时为具体数字；loading/error 时为 null。 */
  count: number | null;
  label: string;
  icon: string;
  /** "FlightDelay: 3" 单行文本，React 层可直接渲染。 */
  text: string;
  /** 最多 spec.tooltipEventLimit (默认 5) 条；empty/loading/error 时为 []。 */
  tooltipItems: LinkedEventsBadgeTooltipItem[];
  errorMessage?: string;
}

function requireNonBlank(value: string, field: string): string {
  if (typeof value !== 'string' || value.trim().length === 0) {
    throw new Error(`${field} is required`);
  }
  return value;
}

function requireNonBlankKey(value: string | number, field: string): string {
  if (typeof value === 'number') {
    if (!Number.isFinite(value)) throw new Error(`${field} is required`);
    return String(value);
  }
  if (typeof value !== 'string' || value.trim().length === 0) {
    throw new Error(`${field} is required`);
  }
  return value;
}

function requirePositiveInt(value: number, field: string): number {
  if (!Number.isFinite(value) || value <= 0 || !Number.isInteger(value)) {
    throw new Error(`${field} must be a positive integer`);
  }
  return value;
}

export function isLinkedEventsBadge(badge: { kind: string; [key: string]: unknown }): boolean {
  return badge.kind === 'linkedEvents';
}

export function buildLinkedEventsBadgeRequest(
  spec: LinkedEventsBadgeSpec,
  ctx: LinkedEventsBadgeRequestContext,
  options: BuildLinkedEventsBadgeRequestOptions = {},
): LinkedEventsBadgeRequest {
  if (spec.kind !== 'linkedEvents') {
    throw new Error(
      `buildLinkedEventsBadgeRequest: expected kind="linkedEvents", got "${spec.kind}"`,
    );
  }
  requireNonBlank(spec.objectType, 'objectType');
  requireNonBlank(spec.linkType, 'linkType');
  requireNonBlank(ctx.ontology, 'ontology');
  requireNonBlank(ctx.sourceObjectType, 'sourceObjectType');
  const primaryKey = requireNonBlankKey(ctx.sourcePrimaryKey, 'sourcePrimaryKey');

  const pageSize =
    options.pageSize !== undefined
      ? requirePositiveInt(options.pageSize, 'pageSize')
      : spec.tooltipEventLimit !== undefined
        ? requirePositiveInt(spec.tooltipEventLimit, 'tooltipEventLimit')
        : BADGE_TOOLTIP_MAX_EVENTS;

  const segments = [
    'api',
    'v2',
    'ontologies',
    encodeURIComponent(ctx.ontology),
    'objects',
    encodeURIComponent(ctx.sourceObjectType),
    encodeURIComponent(primaryKey),
    'links',
    encodeURIComponent(spec.linkType),
  ];
  const path = `/${segments.join('/')}?pageSize=${pageSize}`;

  return { method: 'GET', path };
}

export function buildLinkedEventsBadgeTooltipItems(
  events: LinkedEventsBadgeEventLike[],
  options: BuildLinkedEventsBadgeTooltipItemsOptions = {},
): LinkedEventsBadgeTooltipItem[] {
  if (!Array.isArray(events) || events.length === 0) return [];
  const limit = options.limit !== undefined ? options.limit : BADGE_TOOLTIP_MAX_EVENTS;
  if (!Number.isFinite(limit) || limit <= 0) return [];

  const capped = events.slice(0, Math.floor(limit));
  return capped.map((e) => {
    const rawLabel =
      options.labelProperty !== undefined ? e[options.labelProperty] : undefined;
    const label =
      typeof rawLabel === 'string' && rawLabel.trim().length > 0
        ? rawLabel
        : String(e.__primaryKey);
    const item: LinkedEventsBadgeTooltipItem = {
      rid: e.__rid,
      label,
    };
    if (typeof e.eventStart === 'number' && Number.isFinite(e.eventStart)) {
      item.eventStart = e.eventStart;
    }
    return item;
  });
}

function resolveBadgeLabel(spec: LinkedEventsBadgeSpec): string {
  const dn = spec.displayName;
  if (typeof dn === 'string' && dn.trim().length > 0) return dn;
  return spec.objectType;
}

function resolveBadgeIcon(spec: LinkedEventsBadgeSpec): string {
  if (typeof spec.icon === 'string' && spec.icon.length > 0) return spec.icon;
  return BADGE_DEFAULT_ICON;
}

export function renderLinkedEventsBadge(
  spec: LinkedEventsBadgeSpec,
  input: LinkedEventsBadgeRenderInput,
  options: RenderLinkedEventsBadgeOptions = {},
): LinkedEventsBadgeRenderResult {
  if (spec.kind !== 'linkedEvents') {
    throw new Error(
      `renderLinkedEventsBadge: expected kind="linkedEvents", got "${spec.kind}"`,
    );
  }
  requireNonBlank(spec.objectType, 'objectType');
  requireNonBlank(spec.linkType, 'linkType');

  const label = resolveBadgeLabel(spec);
  const icon = resolveBadgeIcon(spec);

  // Error 优先：陈旧 count 也不显示，避免给用户成功假象（与 VTX-060/061 同语义）。
  // 空字符串视为无错误回退。
  if (typeof input.error === 'string' && input.error.length > 0) {
    return {
      status: 'error',
      count: null,
      label,
      icon: ERROR_PLACEHOLDER,
      text: `${label}: ${ERROR_PLACEHOLDER}`,
      tooltipItems: [],
      errorMessage: input.error,
    };
  }

  if (input.loading === true || input.count === undefined) {
    return {
      status: 'loading',
      count: null,
      label,
      icon,
      text: `${label}: …`,
      tooltipItems: [],
    };
  }

  const count = input.count;
  if (typeof count !== 'number' || !Number.isFinite(count) || count < 0) {
    throw new Error(`count must be a non-negative finite number (got ${String(count)})`);
  }

  if (count === 0) {
    return {
      status: 'empty',
      count: 0,
      label,
      icon,
      text: `${label}: 0`,
      tooltipItems: [],
    };
  }

  const tooltipLimit =
    spec.tooltipEventLimit !== undefined
      ? spec.tooltipEventLimit
      : BADGE_TOOLTIP_MAX_EVENTS;
  const tooltipItems = input.events
    ? buildLinkedEventsBadgeTooltipItems(input.events, {
        limit: tooltipLimit,
        labelProperty: options.labelProperty,
      })
    : [];

  return {
    status: 'present',
    count,
    label,
    icon,
    text: `${label}: ${count}`,
    tooltipItems,
  };
}
