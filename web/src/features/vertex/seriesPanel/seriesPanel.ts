// VTX-033 — Series Panel（底部时间线面板）的纯逻辑层。
//
// 状态模型驱动 sidebar "Open in series view" → 底部展开堆叠 subplot 的交互；
// 布局 helper 把面板高度按 N 个 subplot + gap 切分；游标换算把面板上的拖
// 拽 x 同步成顶部 TimeSelection 的 selectedTime（双向）。
// React 接线层负责把 uPlot 实例、cursor 拖拽、关闭按钮串起来。

export interface SeriesPanelEntry {
  key: string;
  seriesRid: string;
  label: string;
  property?: string;
}

export interface SeriesPanelState {
  expanded: boolean;
  entries: SeriesPanelEntry[];
}

export interface SeriesPanelInit {
  expanded?: boolean;
  entries?: SeriesPanelEntry[];
}

export function createSeriesPanelState(init?: SeriesPanelInit): SeriesPanelState {
  return {
    expanded: init?.expanded ?? false,
    entries: init?.entries ? [...init.entries] : [],
  };
}

export function openSeries(
  state: SeriesPanelState,
  entry: SeriesPanelEntry,
): SeriesPanelState {
  const exists = state.entries.some(e => e.key === entry.key);
  const entries = exists ? [...state.entries] : [...state.entries, entry];
  return { expanded: true, entries };
}

export function closeSeries(state: SeriesPanelState, key: string): SeriesPanelState {
  const idx = state.entries.findIndex(e => e.key === key);
  if (idx < 0) return state;
  const entries = state.entries.filter(e => e.key !== key);
  return {
    entries,
    expanded: entries.length === 0 ? false : state.expanded,
  };
}

export function togglePanel(state: SeriesPanelState): SeriesPanelState {
  return { ...state, expanded: !state.expanded };
}

export function setPanelExpanded(
  state: SeriesPanelState,
  expanded: boolean,
): SeriesPanelState {
  return { ...state, expanded };
}

export interface SubplotLayout {
  key: string;
  top: number;
  height: number;
}

// computeSubplotLayouts splits `totalHeight` across N stacked subplots,
// reserving `gap` pixels between adjacent subplots. Heights are clamped to a
// minimum of 0 so an undersized panel (or negative totalHeight) still produces
// well-formed records that the React layer can render as collapsed rows.
export function computeSubplotLayouts(
  entries: SeriesPanelEntry[],
  totalHeight: number,
  gap = 0,
): SubplotLayout[] {
  const n = entries.length;
  if (n === 0) return [];
  const totalGap = gap * (n - 1);
  const available = Math.max(0, totalHeight - totalGap);
  const each = available / n;
  const out: SubplotLayout[] = new Array(n);
  for (let i = 0; i < n; i++) {
    out[i] = {
      key: entries[i].key,
      top: i * (each + gap),
      height: each,
    };
  }
  return out;
}

function clamp(v: number, lo: number, hi: number): number {
  if (v < lo) return lo;
  if (v > hi) return hi;
  return v;
}

// timestampFromX maps a horizontal pixel offset inside the Series Panel onto a
// timestamp in the [from, to] window — used when the user drags the synced
// cursor and we need to update the top selectedTime.
export function timestampFromX(
  x: number,
  width: number,
  from: number,
  to: number,
): number {
  if (width <= 0 || to <= from) return from;
  const clamped = clamp(x, 0, width);
  return from + (clamped / width) * (to - from);
}

// xFromTimestamp is the inverse — given the current selectedTime, where does
// the cursor sit inside the panel? Clamped to [0, width].
export function xFromTimestamp(
  timestamp: number,
  width: number,
  from: number,
  to: number,
): number {
  if (width <= 0 || to <= from) return 0;
  const ratio = clamp((timestamp - from) / (to - from), 0, 1);
  return ratio * width;
}
