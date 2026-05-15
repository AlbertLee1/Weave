import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  createTimeSelectionState,
  setRange,
  setSelectedTime,
  toggleLiveMode,
  type TimeSelectionState,
  makeDebouncedNotifier,
  createLivePoller,
} from './timeSelection';

const TS_FROM = new Date('2026-05-01T00:00:00Z').getTime();
const TS_TO = new Date('2026-05-15T00:00:00Z').getTime();
const TS_MID = new Date('2026-05-08T00:00:00Z').getTime();

describe('VTX-031 createTimeSelectionState', () => {
  it('given_FromAndTo_when_Init_then_SelectedTimeDefaultsToTo', () => {
    const s = createTimeSelectionState({ from: TS_FROM, to: TS_TO, pollingIntervalSec: 10 });
    expect(s.from).toBe(TS_FROM);
    expect(s.to).toBe(TS_TO);
    expect(s.selectedTime).toBe(TS_TO);
    expect(s.liveMode).toBe(false);
    expect(s.pollingIntervalSec).toBe(10);
  });

  it('given_FromGreaterThanTo_when_Init_then_Throws', () => {
    expect(() =>
      createTimeSelectionState({ from: TS_TO, to: TS_FROM, pollingIntervalSec: 10 }),
    ).toThrow(/from.*to/i);
  });

  it('given_PollingIntervalBelow1_when_Init_then_Throws', () => {
    expect(() =>
      createTimeSelectionState({ from: TS_FROM, to: TS_TO, pollingIntervalSec: 0 }),
    ).toThrow(/pollingIntervalSec/i);
  });
});

describe('VTX-031 setRange', () => {
  const base = (): TimeSelectionState =>
    createTimeSelectionState({ from: TS_FROM, to: TS_TO, pollingIntervalSec: 10 });

  it('given_NewRangeContainsSelected_when_SetRange_then_SelectedUnchanged', () => {
    const s = setSelectedTime(base(), TS_MID);
    const next = setRange(s, { from: TS_FROM, to: TS_TO });
    expect(next.selectedTime).toBe(TS_MID);
  });

  it('given_NewRangeBeforeSelected_when_SetRange_then_SelectedClampsToTo', () => {
    const s = setSelectedTime(base(), TS_TO);
    const earlierTo = new Date('2026-05-05T00:00:00Z').getTime();
    const next = setRange(s, { from: TS_FROM, to: earlierTo });
    expect(next.selectedTime).toBe(earlierTo);
  });

  it('given_NewRangeAfterSelected_when_SetRange_then_SelectedClampsToFrom', () => {
    const s = setSelectedTime(base(), TS_FROM);
    const laterFrom = new Date('2026-05-10T00:00:00Z').getTime();
    const next = setRange(s, { from: laterFrom, to: TS_TO });
    expect(next.selectedTime).toBe(laterFrom);
  });

  it('given_FromGreaterThanTo_when_SetRange_then_Throws', () => {
    expect(() => setRange(base(), { from: TS_TO, to: TS_FROM })).toThrow(/from.*to/i);
  });
});

describe('VTX-031 setSelectedTime', () => {
  const base = (): TimeSelectionState =>
    createTimeSelectionState({ from: TS_FROM, to: TS_TO, pollingIntervalSec: 10 });

  it('given_TimeInRange_when_Set_then_Updated', () => {
    const next = setSelectedTime(base(), TS_MID);
    expect(next.selectedTime).toBe(TS_MID);
  });

  it('given_TimeBeforeFrom_when_Set_then_ClampedToFrom', () => {
    const next = setSelectedTime(base(), TS_FROM - 1_000);
    expect(next.selectedTime).toBe(TS_FROM);
  });

  it('given_TimeAfterTo_when_Set_then_ClampedToTo', () => {
    const next = setSelectedTime(base(), TS_TO + 1_000);
    expect(next.selectedTime).toBe(TS_TO);
  });
});

describe('VTX-031 toggleLiveMode', () => {
  it('given_LiveOff_when_Toggle_then_LiveOn', () => {
    const s = createTimeSelectionState({ from: TS_FROM, to: TS_TO, pollingIntervalSec: 10 });
    expect(toggleLiveMode(s).liveMode).toBe(true);
  });

  it('given_LiveOn_when_Toggle_then_LiveOff', () => {
    const s = toggleLiveMode(
      createTimeSelectionState({ from: TS_FROM, to: TS_TO, pollingIntervalSec: 10 }),
    );
    expect(toggleLiveMode(s).liveMode).toBe(false);
  });
});

describe('VTX-031 makeDebouncedNotifier', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('given_RapidUpdates_when_DebounceWindow_then_OnlyLastFires', () => {
    const cb = vi.fn();
    const notify = makeDebouncedNotifier<number>(cb, 100);
    notify(1);
    notify(2);
    notify(3);
    expect(cb).not.toHaveBeenCalled();
    vi.advanceTimersByTime(99);
    expect(cb).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(cb).toHaveBeenCalledTimes(1);
    expect(cb).toHaveBeenCalledWith(3);
  });

  it('given_DefaultDebounce_when_NotSpecified_then_100ms', () => {
    const cb = vi.fn();
    const notify = makeDebouncedNotifier<number>(cb);
    notify(7);
    vi.advanceTimersByTime(99);
    expect(cb).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(cb).toHaveBeenCalledWith(7);
  });

  it('given_Cancel_when_BeforeFire_then_Suppressed', () => {
    const cb = vi.fn();
    const notify = makeDebouncedNotifier<number>(cb, 100);
    notify(9);
    notify.cancel();
    vi.advanceTimersByTime(200);
    expect(cb).not.toHaveBeenCalled();
  });
});

describe('VTX-031 createLivePoller', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('given_LiveModeOff_when_Start_then_NoTick', () => {
    const tick = vi.fn();
    const p = createLivePoller(tick, 5);
    p.update({ liveMode: false });
    vi.advanceTimersByTime(60_000);
    expect(tick).not.toHaveBeenCalled();
  });

  it('given_LiveModeOn_when_TimeAdvances_then_TickAtInterval', () => {
    const tick = vi.fn();
    const p = createLivePoller(tick, 5);
    p.update({ liveMode: true });
    vi.advanceTimersByTime(4_999);
    expect(tick).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(tick).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(5_000);
    expect(tick).toHaveBeenCalledTimes(2);
  });

  it('given_LiveMode_when_Stop_then_NoMoreTicks', () => {
    const tick = vi.fn();
    const p = createLivePoller(tick, 5);
    p.update({ liveMode: true });
    vi.advanceTimersByTime(5_000);
    expect(tick).toHaveBeenCalledTimes(1);
    p.stop();
    vi.advanceTimersByTime(60_000);
    expect(tick).toHaveBeenCalledTimes(1);
  });

  it('given_IntervalChange_when_Update_then_NewIntervalUsed', () => {
    const tick = vi.fn();
    const p = createLivePoller(tick, 5);
    p.update({ liveMode: true });
    vi.advanceTimersByTime(5_000);
    expect(tick).toHaveBeenCalledTimes(1);
    p.update({ liveMode: true, pollingIntervalSec: 2 });
    vi.advanceTimersByTime(1_999);
    expect(tick).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(1);
    expect(tick).toHaveBeenCalledTimes(2);
  });
});
