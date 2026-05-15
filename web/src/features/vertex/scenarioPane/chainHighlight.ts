// VTX-053 — Scenario Pane 链路传递高亮的纯逻辑层。
//
// 调研报告 §3.3 末段。当 Scenario 中 M1 已跑 + M2 的某个 input 是从 M1
// 的 output 派生时，M2 的 input 单元格背景色变蓝（impacted by chained
// model），hover 时 tooltip 显示 "Value from M1.output (not Ontology
// current state)"。React 接线层在 Pane 的 input/output 列 cell renderer
// 调用本模块派生 className / title，反向定位则用 `${rowRid}::${paramName}`
// 组合 key（与 VTX-039 buildInputOutputColumnKey 一致）。
//
// 本模块只持有 ChainEdge 静态拓扑（哪个 cell 被哪条 upstream output 喂养）；
// 不读 Scenario Run 状态、不发请求；与 VTX-052 modelmesh 计划器解耦：
// 后端 mesh 派生 edge → 前端配置 ModelChainSpec → 本模块按 property name
// 匹配生成 ChainEdge[]。

export interface ChainEdge {
  /** 上游 model row 的 rid（Scenario Pane row id）。 */
  upstreamRowRid: string;
  /** 上游 model rid（Function registry / Live model）。 */
  upstreamModelRid: string;
  /** 上游 model 的展示名，用于 tooltip。 */
  upstreamModelLabel: string;
  /** 上游 model 写出的 property name（mesh edge label）。 */
  upstreamProperty: string;
  /** 下游 model row rid。 */
  downstreamRowRid: string;
  /** 下游 model 的 input 参数名。 */
  downstreamParamName: string;
}

export type ChainHighlightMap = Record<string, ChainEdge>;

export interface ModelChainSpec {
  rowRid: string;
  modelRid: string;
  modelLabel: string;
  /** 该 model 产出的 property name 列表。 */
  outputProperties: string[];
  /**
   * 该 model 的输入绑定：每条 input 参数对应一个 source property。当
   * sourceProperty 与 *其他* model 的 outputProperties 命中时即派生一条
   * ChainEdge。
   */
  inputBindings: Array<{
    paramName: string;
    sourceProperty?: string;
  }>;
}

// 与 spec "M2 的 input 单元格背景色变蓝" 对齐。挑 bg-blue-100 是因为
// VTX-040 overrideCell 的黄色边框（border-yellow-400）和 baseline-compare
// 的红/绿（VTX-045 + VTX-047）都已经占了语义槽位；蓝色专属 "chained
// model" 渠道，不会撞色。React 层若要加 hover 浓度可在外层叠加。
export const CHAIN_HIGHLIGHT_BG_CLASS = 'bg-blue-100';

export const CHAIN_TOOLTIP_PREFIX = 'Value from ';
export const CHAIN_TOOLTIP_SUFFIX = ' (not Ontology current state)';

function requireNonBlank(value: string, field: string): string {
  if (typeof value !== 'string' || value.trim().length === 0) {
    throw new Error(`${field} is required`);
  }
  return value.trim();
}

// buildChainCellKey 与 VTX-039 buildInputOutputColumnKey 用同一组合键
// 形态（`${rowRid}::${paramName}`），让 React 接线层在 Pane cell renderer
// 中直接复用同一个 key 既查 source binding（VTX-039）又查 chain edge
// （本模块），避免维护两套映射。
export function buildChainCellKey(rowRid: string, paramName: string): string {
  const r = requireNonBlank(rowRid, 'row rid');
  const p = requireNonBlank(paramName, 'param name');
  return `${r}::${p}`;
}

export function createChainHighlightMap(edges?: ChainEdge[]): ChainHighlightMap {
  const map: ChainHighlightMap = {};
  if (!edges) return map;
  for (const edge of edges) {
    const key = buildChainCellKey(edge.downstreamRowRid, edge.downstreamParamName);
    map[key] = edge;
  }
  return map;
}

export function addChainEdge(
  map: ChainHighlightMap,
  edge: ChainEdge,
): ChainHighlightMap {
  const key = buildChainCellKey(edge.downstreamRowRid, edge.downstreamParamName);
  return { ...map, [key]: edge };
}

export function removeChainEdge(
  map: ChainHighlightMap,
  downstreamRowRid: string,
  downstreamParamName: string,
): ChainHighlightMap {
  const key = buildChainCellKey(downstreamRowRid, downstreamParamName);
  if (!(key in map)) return map;
  const next = { ...map };
  delete next[key];
  return next;
}

// clearChainHighlights 在 Scenario 重新运行或 Pane 切换 Case Study 时调用，
// 一次性丢弃所有 edge。每次都返回新引用以触发下游 React memo 失效（避免
// 老 tooltip 残留在 DOM）。不读 prior map —— 语义上"清空 = 永远空"。
export function clearChainHighlights(): ChainHighlightMap {
  return {};
}

export function isCellChainImpacted(
  map: ChainHighlightMap,
  rowRid: string,
  paramName: string,
): boolean {
  const key = buildChainCellKey(rowRid, paramName);
  return key in map;
}

export function getCellChainEdge(
  map: ChainHighlightMap,
  rowRid: string,
  paramName: string,
): ChainEdge | null {
  const key = buildChainCellKey(rowRid, paramName);
  return map[key] ?? null;
}

export function buildChainTooltip(edge: ChainEdge): string {
  return `${CHAIN_TOOLTIP_PREFIX}${edge.upstreamModelLabel}.${edge.upstreamProperty}${CHAIN_TOOLTIP_SUFFIX}`;
}

export function getCellChainTooltip(
  map: ChainHighlightMap,
  rowRid: string,
  paramName: string,
): string | null {
  const edge = getCellChainEdge(map, rowRid, paramName);
  return edge === null ? null : buildChainTooltip(edge);
}

export function getCellChainClassNames(
  map: ChainHighlightMap,
  rowRid: string,
  paramName: string,
): string {
  return isCellChainImpacted(map, rowRid, paramName) ? CHAIN_HIGHLIGHT_BG_CLASS : '';
}

// buildChainEdgesFromModelSpecs 按 property name 匹配 producer → consumer：
//   - 同一 model 的 output ∩ input 视为自环，丢弃（与 VTX-052 modelmesh
//     TopologicalLayers 把 self-loop 当 cycle 拒的语义一致 —— Pane 高亮
//     不应该把 model 自己的中间值标蓝）。
//   - 多个上游 producer 命中同一 consumer input 时，按 specs 顺序取第一个，
//     让结果稳定可测。
//   - 缺失或空白的 sourceProperty 直接跳过（标量 override / time series /
//     measure 等非 chain 源不参与）。
export function buildChainEdgesFromModelSpecs(
  specs: ModelChainSpec[],
): ChainEdge[] {
  // 第一遍：建 property → producer 索引（取 specs 顺序中的第一个）。
  const producers = new Map<
    string,
    { rowRid: string; modelRid: string; modelLabel: string }
  >();
  for (const spec of specs) {
    requireNonBlank(spec.rowRid, 'spec.rowRid');
    requireNonBlank(spec.modelLabel, 'spec.modelLabel');
    for (const property of spec.outputProperties) {
      const prop = typeof property === 'string' ? property.trim() : '';
      if (prop.length === 0) continue;
      if (!producers.has(prop)) {
        producers.set(prop, {
          rowRid: spec.rowRid,
          modelRid: spec.modelRid,
          modelLabel: spec.modelLabel,
        });
      }
    }
  }

  // 第二遍：扫 consumer inputBindings，按 sourceProperty 命中即生成 edge。
  const edges: ChainEdge[] = [];
  for (const spec of specs) {
    for (const binding of spec.inputBindings) {
      const src =
        typeof binding.sourceProperty === 'string'
          ? binding.sourceProperty.trim()
          : '';
      if (src.length === 0) continue;
      const producer = producers.get(src);
      if (producer === undefined) continue;
      if (producer.rowRid === spec.rowRid) continue; // skip self-loop
      edges.push({
        upstreamRowRid: producer.rowRid,
        upstreamModelRid: producer.modelRid,
        upstreamModelLabel: producer.modelLabel,
        upstreamProperty: src,
        downstreamRowRid: spec.rowRid,
        downstreamParamName: binding.paramName,
      });
    }
  }
  return edges;
}
