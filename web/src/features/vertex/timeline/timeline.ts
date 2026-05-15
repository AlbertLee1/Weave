// VTX-034 — Timeline（事件 + 时序统一视图）的纯逻辑层。
//
// 三块能力：
//   1. TimelineState reducer —— 子模式 toggle / play-pause / speed / loop
//   2. advanceSelectedTime / createPlaybackController —— play 按钮按 speed
//      自动推进 selectedTime；loop=true 到达 to 时回环至 from
//   3. layoutEventBars —— eventStart/eventEnd 对象画成水平条 bar；first-fit
//      贪心算法把重叠事件拆到多行
//
// 颜色 / 分类过滤复用 VTX-082 eventBars.ts；本文件只关心布局与状态。
// React 接线层负责把 ObjectType eventStart/eventEnd 配置读出来，把对象
// 集合喂给 layoutEventBars，并把 onAdvance 接到 TimeSelection store。

import type { AnnotatedEvent } from './eventBars';

export type { AnnotatedEvent };

export type TimelineSpeed = 0.5 | 1 | 2 | 5;

export const TIMELINE_SPEEDS: readonly TimelineSpeed[] = [0.5, 1, 2, 5];

const ALLOWED_SPEEDS = new Set<number>(TIMELINE_SPEEDS);

function assertSpeed(v: number): asserts v is TimelineSpeed {
  if (!ALLOWED_SPEEDS.has(v)) {
    throw new Error(`timeline: invalid speed ${v} (allowed: 0.5, 1, 2, 5)`);
  }
}

export interface TimelineState {
  enabled: boolean;
  playing: boolean;
  speed: TimelineSpeed;
  loop: boolean;
}

export interface TimelineInit {
  enabled?: boolean;
  playing?: boolean;
  speed?: TimelineSpeed;
  loop?: boolean;
}

export function createTimelineState(init?: TimelineInit): TimelineState {
  const speed = init?.speed ?? 1;
  assertSpeed(speed);
  const enabled = init?.enabled ?? false;
  // disabled 状态下 playing 必须为 false，避免 controller 在 UI 未启用 Timeline
  // 子模式时空转
  const playing = enabled ? (init?.playing ?? false) : false;
  return {
    enabled,
    playing,
    speed,
    loop: init?.loop ?? false,
  };
}

export function setTimelineEnabled(state: TimelineState, enabled: boolean): TimelineState {
  return {
    ...state,
    enabled,
    playing: enabled ? state.playing : false,
  };
}

export function toggleTimelineEnabled(state: TimelineState): TimelineState {
  return setTimelineEnabled(state, !state.enabled);
}

export function setTimelinePlaying(state: TimelineState, playing: boolean): TimelineState {
  if (!state.enabled) return { ...state, playing: false };
  return { ...state, playing };
}

export function toggleTimelinePlaying(state: TimelineState): TimelineState {
  return setTimelinePlaying(state, !state.playing);
}

export function setTimelineSpeed(state: TimelineState, speed: TimelineSpeed): TimelineState {
  assertSpeed(speed);
  return { ...state, speed };
}

export function setTimelineLoop(state: TimelineState, loop: boolean): TimelineState {
  return { ...state, loop };
}

export function toggleTimelineLoop(state: TimelineState): TimelineState {
  return { ...state, loop: !state.loop };
}

export interface AdvanceResult {
  selectedTime: number;
  // reachedEnd: 此次 advance 后 selectedTime 触及/越过 to —— 调用方据此在
  // loop=false 时自动 pause。
  reachedEnd: boolean;
  // looped: loop=true 且发生回环（next > to）。
  looped: boolean;
}

export interface AdvanceArgs {
  current: number;
  from: number;
  to: number;
  speed: TimelineSpeed;
  loop: boolean;
  realDtMs: number;
  // baseAdvancePerSecond: 1x speed 下每实时秒推进的虚拟毫秒数。默认 1000，
  // 表示虚拟时间和实时同步；调用方可改成 60_000 实现"1 实时秒 = 1 虚拟分钟"。
  baseAdvancePerSecond?: number;
}

export function advanceSelectedTime(args: AdvanceArgs): AdvanceResult {
  const base = args.baseAdvancePerSecond ?? 1000;
  const { current, from, to, speed, loop, realDtMs } = args;
  if (to <= from) {
    // 退化窗口：clamp 到 from，标 reachedEnd 让 UI 自动 pause。
    return { selectedTime: from, reachedEnd: true, looped: false };
  }
  const delta = (realDtMs * speed * base) / 1000;
  const next = current + delta;
  if (next < to) {
    return { selectedTime: next, reachedEnd: false, looped: false };
  }
  if (!loop) {
    return { selectedTime: to, reachedEnd: true, looped: false };
  }
  const span = to - from;
  const wrap = (next - from) % span;
  return { selectedTime: from + wrap, reachedEnd: true, looped: true };
}

export interface PlaybackState {
  selectedTime: number;
  from: number;
  to: number;
  speed: TimelineSpeed;
  loop: boolean;
}

export interface PlaybackController {
  start(): void;
  stop(): void;
  isRunning(): boolean;
}

export interface PlaybackControllerArgs {
  getState: () => PlaybackState;
  onAdvance: (result: AdvanceResult) => void;
  tickIntervalMs?: number;
  baseAdvancePerSecond?: number;
}

// createPlaybackController 按固定 tickIntervalMs 调用 onAdvance，把虚拟
// selectedTime 按 speed 推进。设计上 controller 不存 speed/loop/selectedTime —
// 每个 tick 都向 getState 拉一次，让调用方（Zustand store）持有真理。
// loop=false reachedEnd 后 controller 不自动 stop —— 让调用方决定（通常
// 在 onAdvance 里看 reachedEnd 并 dispatch setTimelinePlaying(false)）。
export function createPlaybackController(args: PlaybackControllerArgs): PlaybackController {
  const tickIntervalMs = args.tickIntervalMs ?? 100;
  const base = args.baseAdvancePerSecond ?? 1000;
  let handle: ReturnType<typeof setInterval> | null = null;

  const tick = () => {
    const s = args.getState();
    const r = advanceSelectedTime({
      current: s.selectedTime,
      from: s.from,
      to: s.to,
      speed: s.speed,
      loop: s.loop,
      realDtMs: tickIntervalMs,
      baseAdvancePerSecond: base,
    });
    args.onAdvance(r);
  };

  return {
    start() {
      if (handle !== null) return;
      handle = setInterval(tick, tickIntervalMs);
    },
    stop() {
      if (handle !== null) {
        clearInterval(handle);
        handle = null;
      }
    },
    isRunning() {
      return handle !== null;
    },
  };
}

export interface EventBarWindow {
  from: number;
  to: number;
}

export interface EventBarLayoutOptions {
  // 瞬时事件（无 end）或极窄事件的最小像素宽度，避免 0 px 渲染丢失。
  minBarWidthPx?: number;
  // first-fit 行数上限；超过则丢弃事件（UI 层可显示 "+N more"）。
  maxRows?: number;
}

export interface EventBarLayout {
  rid: string;
  objectType: string;
  color: string;
  x: number;
  width: number;
  row: number;
  start: number;
  end: number;
}

// layoutEventBars 把事件集合按 [from, to] 时间窗映射到 [0, pixelWidth]，并
// 用 first-fit 贪心避免视觉重叠：
//   • 按 start 升序排序（稳定）
//   • 对每个事件，找第一个 lastRowEnd[row] ≤ event.start 的行；找不到则新开
//   • 越界事件（end < from 或 start > to）剔除；横跨边界的事件 clamp 到窗口内
//   • 瞬时事件（end 缺失 / end === start）按 minBarWidthPx 兜底
export function layoutEventBars(
  events: AnnotatedEvent[],
  window: EventBarWindow,
  pixelWidth: number,
  opts?: EventBarLayoutOptions,
): EventBarLayout[] {
  const { from, to } = window;
  if (pixelWidth <= 0 || to <= from) return [];
  const minBarWidthPx = opts?.minBarWidthPx ?? 2;
  const maxRows = opts?.maxRows ?? Infinity;
  const span = to - from;
  const pxPerMs = pixelWidth / span;

  const inWindow = events.filter((e) => {
    const end = e.end ?? e.start;
    return end >= from && e.start <= to;
  });
  inWindow.sort((a, b) => a.start - b.start);

  const rowEnds: number[] = [];
  const out: EventBarLayout[] = [];
  for (const e of inWindow) {
    const rawEnd = e.end ?? e.start;
    let row = -1;
    for (let i = 0; i < rowEnds.length; i++) {
      if (rowEnds[i] <= e.start) {
        row = i;
        break;
      }
    }
    if (row === -1) {
      if (rowEnds.length >= maxRows) continue;
      row = rowEnds.length;
      rowEnds.push(rawEnd);
    } else {
      rowEnds[row] = rawEnd;
    }
    const clampedStart = e.start < from ? from : e.start;
    const clampedEnd = rawEnd > to ? to : rawEnd;
    const x = (clampedStart - from) * pxPerMs;
    const naturalWidth = (clampedEnd - clampedStart) * pxPerMs;
    const width = naturalWidth < minBarWidthPx ? minBarWidthPx : naturalWidth;
    out.push({
      rid: e.rid,
      objectType: e.objectType,
      color: e.color,
      x,
      width,
      row,
      start: e.start,
      end: rawEnd,
    });
  }
  return out;
}
