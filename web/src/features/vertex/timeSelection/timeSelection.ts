// VTX-031 — TopBar Time Selection Bar 状态模型 + 副作用工具
//
// 这里只暴露与 UI 无关的纯函数/工厂：
//   • createTimeSelectionState / setRange / setSelectedTime / toggleLiveMode
//     用于驱动 TopBar 的 from-to range slider + selectedTime 游标
//   • makeDebouncedNotifier — 拖动游标时 100ms 防抖再触发下游重算
//   • createLivePoller — Live mode 开启后按 pollingIntervalSec 周期刷新
// React 层和 Zustand 适配各自负责把这些工具串起来，本文件不依赖 React，
// 方便 vitest 在 happy-dom 之外纯逻辑驱动。

export interface TimeSelectionState {
  from: number;
  to: number;
  selectedTime: number;
  liveMode: boolean;
  pollingIntervalSec: number;
}

export interface TimeSelectionInit {
  from: number;
  to: number;
  pollingIntervalSec: number;
  selectedTime?: number;
  liveMode?: boolean;
}

function ensureRange(from: number, to: number): void {
  if (!(from <= to)) {
    throw new Error(`timeSelection: from must be ≤ to (got from=${from} to=${to})`);
  }
}

function clamp(v: number, lo: number, hi: number): number {
  if (v < lo) return lo;
  if (v > hi) return hi;
  return v;
}

export function createTimeSelectionState(init: TimeSelectionInit): TimeSelectionState {
  ensureRange(init.from, init.to);
  if (!Number.isFinite(init.pollingIntervalSec) || init.pollingIntervalSec < 1) {
    throw new Error('timeSelection: pollingIntervalSec must be ≥ 1');
  }
  const selectedTime = init.selectedTime ?? init.to;
  return {
    from: init.from,
    to: init.to,
    selectedTime: clamp(selectedTime, init.from, init.to),
    liveMode: init.liveMode ?? false,
    pollingIntervalSec: init.pollingIntervalSec,
  };
}

export interface RangeUpdate {
  from: number;
  to: number;
}

export function setRange(state: TimeSelectionState, range: RangeUpdate): TimeSelectionState {
  ensureRange(range.from, range.to);
  return {
    ...state,
    from: range.from,
    to: range.to,
    selectedTime: clamp(state.selectedTime, range.from, range.to),
  };
}

export function setSelectedTime(state: TimeSelectionState, t: number): TimeSelectionState {
  return { ...state, selectedTime: clamp(t, state.from, state.to) };
}

export function toggleLiveMode(state: TimeSelectionState): TimeSelectionState {
  return { ...state, liveMode: !state.liveMode };
}

export interface DebouncedNotifier<T> {
  (value: T): void;
  cancel: () => void;
}

export function makeDebouncedNotifier<T>(
  callback: (value: T) => void,
  delayMs = 100,
): DebouncedNotifier<T> {
  let handle: ReturnType<typeof setTimeout> | null = null;
  let pending: { value: T } | null = null;

  const notify = ((value: T) => {
    pending = { value };
    if (handle !== null) clearTimeout(handle);
    handle = setTimeout(() => {
      handle = null;
      const p = pending;
      pending = null;
      if (p) callback(p.value);
    }, delayMs);
  }) as DebouncedNotifier<T>;

  notify.cancel = () => {
    if (handle !== null) {
      clearTimeout(handle);
      handle = null;
    }
    pending = null;
  };

  return notify;
}

export interface LivePollerUpdate {
  liveMode: boolean;
  pollingIntervalSec?: number;
}

export interface LivePoller {
  update(next: LivePollerUpdate): void;
  stop(): void;
}

// createLivePoller schedules `tick` every pollingIntervalSec while liveMode is
// on. Switching liveMode off — or calling stop() — clears the timer. Changing
// the interval restarts the timer so the new cadence takes effect on the next
// tick.
export function createLivePoller(tick: () => void, initialIntervalSec: number): LivePoller {
  if (!Number.isFinite(initialIntervalSec) || initialIntervalSec < 1) {
    throw new Error('createLivePoller: pollingIntervalSec must be ≥ 1');
  }
  let intervalSec = initialIntervalSec;
  let live = false;
  let handle: ReturnType<typeof setInterval> | null = null;

  const start = () => {
    stop();
    handle = setInterval(() => tick(), intervalSec * 1_000);
  };

  function stop() {
    if (handle !== null) {
      clearInterval(handle);
      handle = null;
    }
  }

  return {
    update(next) {
      const nextLive = next.liveMode;
      const nextInterval = next.pollingIntervalSec ?? intervalSec;
      const intervalChanged = nextInterval !== intervalSec;
      intervalSec = nextInterval;

      if (!nextLive) {
        live = false;
        stop();
        return;
      }
      if (!live || intervalChanged) {
        live = true;
        start();
      }
    },
    stop() {
      live = false;
      stop();
    },
  };
}
