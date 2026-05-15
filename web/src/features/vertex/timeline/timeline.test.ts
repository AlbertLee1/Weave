import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  createTimelineState,
  setTimelineEnabled,
  toggleTimelineEnabled,
  setTimelinePlaying,
  toggleTimelinePlaying,
  setTimelineSpeed,
  setTimelineLoop,
  toggleTimelineLoop,
  TIMELINE_SPEEDS,
  advanceSelectedTime,
  createPlaybackController,
  layoutEventBars,
  type TimelineSpeed,
  type AnnotatedEvent,
} from './timeline';

const T_FROM = new Date('2026-05-01T00:00:00Z').getTime();
const T_TO = new Date('2026-05-15T00:00:00Z').getTime();
const T_MID = new Date('2026-05-08T00:00:00Z').getTime();
const SPAN = T_TO - T_FROM; // 14 days in ms

describe('VTX-034 createTimelineState', () => {
  it('given_NoInit_when_Create_then_DisabledNotPlayingDefaultSpeed1NoLoop', () => {
    const s = createTimelineState();
    expect(s.enabled).toBe(false);
    expect(s.playing).toBe(false);
    expect(s.speed).toBe(1);
    expect(s.loop).toBe(false);
  });

  it('given_InitOverrides_when_Create_then_OverridesHonored', () => {
    const s = createTimelineState({ enabled: true, playing: true, speed: 2, loop: true });
    expect(s.enabled).toBe(true);
    expect(s.playing).toBe(true);
    expect(s.speed).toBe(2);
    expect(s.loop).toBe(true);
  });

  it('given_DisabledInitButPlaying_when_Create_then_PlayingForcedOff', () => {
    const s = createTimelineState({ enabled: false, playing: true });
    expect(s.playing).toBe(false);
  });

  it('given_InvalidInitSpeed_when_Create_then_Throws', () => {
    expect(() => createTimelineState({ speed: 3 as unknown as TimelineSpeed })).toThrow(/speed/i);
  });
});

describe('VTX-034 setTimelineEnabled / toggleTimelineEnabled', () => {
  it('given_DisablingWhilePlaying_when_SetFalse_then_PlayingForcedOff', () => {
    const s = createTimelineState({ enabled: true, playing: true });
    const next = setTimelineEnabled(s, false);
    expect(next.enabled).toBe(false);
    expect(next.playing).toBe(false);
  });

  it('given_Enabled_when_Toggle_then_DisabledAndPlayingOff', () => {
    const s = createTimelineState({ enabled: true, playing: true });
    const next = toggleTimelineEnabled(s);
    expect(next.enabled).toBe(false);
    expect(next.playing).toBe(false);
  });

  it('given_Disabled_when_Toggle_then_EnabledAndPlayingPreserved', () => {
    const s = createTimelineState({ enabled: false });
    const next = toggleTimelineEnabled(s);
    expect(next.enabled).toBe(true);
    expect(next.playing).toBe(false);
  });
});

describe('VTX-034 setTimelinePlaying / toggleTimelinePlaying', () => {
  it('given_EnabledNotPlaying_when_SetTrue_then_PlayingTrue', () => {
    const s = createTimelineState({ enabled: true });
    expect(setTimelinePlaying(s, true).playing).toBe(true);
  });

  it('given_DisabledTimeline_when_SetPlayingTrue_then_PlayingStillFalse', () => {
    const s = createTimelineState({ enabled: false });
    expect(setTimelinePlaying(s, true).playing).toBe(false);
  });

  it('given_EnabledPlaying_when_Toggle_then_PlayingFalse', () => {
    const s = createTimelineState({ enabled: true, playing: true });
    expect(toggleTimelinePlaying(s).playing).toBe(false);
  });

  it('given_DisabledTimeline_when_TogglePlaying_then_StillFalse', () => {
    const s = createTimelineState({ enabled: false });
    expect(toggleTimelinePlaying(s).playing).toBe(false);
  });
});

describe('VTX-034 setTimelineSpeed', () => {
  it('given_AllowedSpeeds_when_Set_then_Accepted', () => {
    const s = createTimelineState();
    for (const v of TIMELINE_SPEEDS) {
      expect(setTimelineSpeed(s, v).speed).toBe(v);
    }
  });

  it('given_BcdAcceptanceSpeeds_when_Set_then_AllPresent', () => {
    // Acceptance: speed 可调 0.5x/1x/2x/5x
    expect(TIMELINE_SPEEDS).toEqual([0.5, 1, 2, 5]);
  });

  it('given_InvalidSpeed_when_Set_then_Throws', () => {
    const s = createTimelineState();
    expect(() => setTimelineSpeed(s, 3 as unknown as TimelineSpeed)).toThrow(/speed/i);
    expect(() => setTimelineSpeed(s, 0 as unknown as TimelineSpeed)).toThrow(/speed/i);
  });
});

describe('VTX-034 setTimelineLoop / toggleTimelineLoop', () => {
  it('given_LoopOff_when_Toggle_then_LoopOn', () => {
    const s = createTimelineState();
    expect(toggleTimelineLoop(s).loop).toBe(true);
  });

  it('given_LoopOn_when_SetFalse_then_LoopOff', () => {
    const s = createTimelineState({ loop: true });
    expect(setTimelineLoop(s, false).loop).toBe(false);
  });
});

describe('VTX-034 advanceSelectedTime', () => {
  it('given_NormalAdvance_when_NotReachedEnd_then_AdvancesByRealDtTimesSpeed', () => {
    const baseAdvancePerSecond = 1000; // 1x -> 每实时秒推进 1 虚拟秒
    const r = advanceSelectedTime({
      current: T_FROM,
      from: T_FROM,
      to: T_TO,
      speed: 1,
      loop: false,
      realDtMs: 500,
      baseAdvancePerSecond,
    });
    expect(r.selectedTime).toBe(T_FROM + 500); // 0.5s real * 1x * 1000 = 500ms 虚拟
    expect(r.reachedEnd).toBe(false);
    expect(r.looped).toBe(false);
  });

  it('given_Speed2x_when_Advance_then_VirtualTimeMovesTwice', () => {
    const r = advanceSelectedTime({
      current: T_FROM,
      from: T_FROM,
      to: T_TO,
      speed: 2,
      loop: false,
      realDtMs: 100,
      baseAdvancePerSecond: 1000,
    });
    expect(r.selectedTime).toBe(T_FROM + 200);
  });

  it('given_Speed0_5x_when_Advance_then_VirtualTimeHalved', () => {
    const r = advanceSelectedTime({
      current: T_FROM,
      from: T_FROM,
      to: T_TO,
      speed: 0.5,
      loop: false,
      realDtMs: 1000,
      baseAdvancePerSecond: 1000,
    });
    expect(r.selectedTime).toBe(T_FROM + 500);
  });

  it('given_AdvancePastTo_when_LoopOff_then_ClampToToAndReachedEnd', () => {
    const r = advanceSelectedTime({
      current: T_TO - 100,
      from: T_FROM,
      to: T_TO,
      speed: 1,
      loop: false,
      realDtMs: 500,
      baseAdvancePerSecond: 1000,
    });
    expect(r.selectedTime).toBe(T_TO);
    expect(r.reachedEnd).toBe(true);
    expect(r.looped).toBe(false);
  });

  it('given_AdvancePastTo_when_LoopOn_then_WrapToFromPlusRemainder', () => {
    const r = advanceSelectedTime({
      current: T_TO - 100,
      from: T_FROM,
      to: T_TO,
      speed: 1,
      loop: true,
      realDtMs: 500,
      baseAdvancePerSecond: 1000,
    });
    // 越过 to 400ms,wrap 至 from + 400
    expect(r.selectedTime).toBe(T_FROM + 400);
    expect(r.reachedEnd).toBe(true);
    expect(r.looped).toBe(true);
  });

  it('given_AdvanceMultipleSpans_when_LoopOn_then_WrapsViaMod', () => {
    const r = advanceSelectedTime({
      current: T_FROM,
      from: T_FROM,
      to: T_TO,
      speed: 1,
      loop: true,
      realDtMs: SPAN + 1000, // 真实毫秒数 = 1 span + 1s
      baseAdvancePerSecond: 1000,
    });
    expect(r.selectedTime).toBe(T_FROM + 1000);
    expect(r.looped).toBe(true);
  });

  it('given_DegenerateRange_when_Advance_then_StaysAtFromAndReachedEnd', () => {
    const r = advanceSelectedTime({
      current: T_FROM,
      from: T_FROM,
      to: T_FROM, // span = 0
      speed: 1,
      loop: true,
      realDtMs: 1000,
      baseAdvancePerSecond: 1000,
    });
    expect(r.selectedTime).toBe(T_FROM);
    expect(r.reachedEnd).toBe(true);
    expect(r.looped).toBe(false);
  });

  it('given_DtZero_when_Advance_then_NoMovementNoReachedEnd', () => {
    const r = advanceSelectedTime({
      current: T_MID,
      from: T_FROM,
      to: T_TO,
      speed: 1,
      loop: false,
      realDtMs: 0,
      baseAdvancePerSecond: 1000,
    });
    expect(r.selectedTime).toBe(T_MID);
    expect(r.reachedEnd).toBe(false);
  });

  it('given_DefaultBaseAdvancePerSecond_when_Speed1xRealOneSecond_then_AdvancesOneVirtualSecond', () => {
    const r = advanceSelectedTime({
      current: T_FROM,
      from: T_FROM,
      to: T_TO,
      speed: 1,
      loop: false,
      realDtMs: 1000,
    });
    expect(r.selectedTime).toBe(T_FROM + 1000);
  });
});

describe('VTX-034 createPlaybackController', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('given_NotStarted_when_TimeAdvances_then_NoTick', () => {
    const onAdvance = vi.fn();
    createPlaybackController({
      getState: () => ({
        selectedTime: T_FROM,
        from: T_FROM,
        to: T_TO,
        speed: 1,
        loop: false,
      }),
      onAdvance,
    });
    vi.advanceTimersByTime(5_000);
    expect(onAdvance).not.toHaveBeenCalled();
  });

  it('given_Started_when_OneTick_then_AdvancesByTickIntervalAtSpeed', () => {
    const onAdvance = vi.fn();
    let selectedTime = T_FROM;
    const pc = createPlaybackController({
      getState: () => ({
        selectedTime,
        from: T_FROM,
        to: T_TO,
        speed: 1,
        loop: false,
      }),
      onAdvance: (r) => {
        selectedTime = r.selectedTime;
        onAdvance(r);
      },
      tickIntervalMs: 100,
      baseAdvancePerSecond: 1000,
    });
    pc.start();
    vi.advanceTimersByTime(100);
    expect(onAdvance).toHaveBeenCalledTimes(1);
    expect(onAdvance.mock.calls[0][0].selectedTime).toBe(T_FROM + 100);
  });

  it('given_Started_when_StoppedBeforeTick_then_NoCall', () => {
    const onAdvance = vi.fn();
    const pc = createPlaybackController({
      getState: () => ({
        selectedTime: T_FROM,
        from: T_FROM,
        to: T_TO,
        speed: 1,
        loop: false,
      }),
      onAdvance,
      tickIntervalMs: 100,
    });
    pc.start();
    pc.stop();
    vi.advanceTimersByTime(1_000);
    expect(onAdvance).not.toHaveBeenCalled();
    expect(pc.isRunning()).toBe(false);
  });

  it('given_Running_when_IsRunning_then_True', () => {
    const pc = createPlaybackController({
      getState: () => ({ selectedTime: T_FROM, from: T_FROM, to: T_TO, speed: 1, loop: false }),
      onAdvance: () => {},
      tickIntervalMs: 100,
    });
    expect(pc.isRunning()).toBe(false);
    pc.start();
    expect(pc.isRunning()).toBe(true);
    pc.stop();
    expect(pc.isRunning()).toBe(false);
  });

  it('given_DoubleStart_when_Tick_then_OnlyOneIntervalActive', () => {
    const onAdvance = vi.fn();
    let selectedTime = T_FROM;
    const pc = createPlaybackController({
      getState: () => ({
        selectedTime,
        from: T_FROM,
        to: T_TO,
        speed: 1,
        loop: false,
      }),
      onAdvance: (r) => {
        selectedTime = r.selectedTime;
        onAdvance(r);
      },
      tickIntervalMs: 100,
    });
    pc.start();
    pc.start(); // 不应该叠加
    vi.advanceTimersByTime(100);
    expect(onAdvance).toHaveBeenCalledTimes(1);
  });

  it('given_LoopOnAndReachedEnd_when_Tick_then_OnAdvanceReceivesLooped', () => {
    const onAdvance = vi.fn();
    let selectedTime = T_TO - 50;
    const pc = createPlaybackController({
      getState: () => ({
        selectedTime,
        from: T_FROM,
        to: T_TO,
        speed: 1,
        loop: true,
      }),
      onAdvance: (r) => {
        selectedTime = r.selectedTime;
        onAdvance(r);
      },
      tickIntervalMs: 100,
      baseAdvancePerSecond: 1000,
    });
    pc.start();
    vi.advanceTimersByTime(100);
    expect(onAdvance).toHaveBeenCalledTimes(1);
    const r = onAdvance.mock.calls[0][0];
    expect(r.reachedEnd).toBe(true);
    expect(r.looped).toBe(true);
    expect(r.selectedTime).toBe(T_FROM + 50);
  });

  it('given_StateChangedBetweenTicks_when_Tick_then_UsesLatestState', () => {
    const onAdvance = vi.fn();
    let selectedTime = T_FROM;
    let speed: TimelineSpeed = 1;
    const pc = createPlaybackController({
      getState: () => ({
        selectedTime,
        from: T_FROM,
        to: T_TO,
        speed,
        loop: false,
      }),
      onAdvance: (r) => {
        selectedTime = r.selectedTime;
        onAdvance(r);
      },
      tickIntervalMs: 100,
      baseAdvancePerSecond: 1000,
    });
    pc.start();
    vi.advanceTimersByTime(100);
    expect(onAdvance.mock.calls[0][0].selectedTime).toBe(T_FROM + 100);
    speed = 5;
    vi.advanceTimersByTime(100);
    expect(onAdvance.mock.calls[1][0].selectedTime).toBe(T_FROM + 100 + 500);
  });
});

describe('VTX-034 layoutEventBars', () => {
  const ev = (rid: string, objectType: string, start: number, end?: number): AnnotatedEvent => ({
    rid,
    objectType,
    start,
    end,
    color: '#000000',
  });

  it('given_NoEvents_when_Layout_then_Empty', () => {
    const out = layoutEventBars([], { from: T_FROM, to: T_TO }, 1000);
    expect(out).toEqual([]);
  });

  it('given_OneEventSpanningHalfWindow_when_Layout_then_HalfWidth', () => {
    const half = T_FROM + SPAN / 2;
    const out = layoutEventBars(
      [ev('e1', 'FlightDelay', T_FROM, half)],
      { from: T_FROM, to: T_TO },
      1000,
    );
    expect(out).toHaveLength(1);
    expect(out[0].x).toBeCloseTo(0, 5);
    expect(out[0].width).toBeCloseTo(500, 5);
    expect(out[0].row).toBe(0);
  });

  it('given_NonOverlappingEvents_when_Layout_then_AllInRowZero', () => {
    const out = layoutEventBars(
      [
        ev('e1', 'FlightDelay', T_FROM, T_FROM + SPAN * 0.1),
        ev('e2', 'FlightDelay', T_FROM + SPAN * 0.5, T_FROM + SPAN * 0.6),
      ],
      { from: T_FROM, to: T_TO },
      1000,
    );
    expect(out.every((b) => b.row === 0)).toBe(true);
  });

  it('given_OverlappingEvents_when_Layout_then_DifferentRowsAssigned', () => {
    const out = layoutEventBars(
      [
        ev('e1', 'FlightDelay', T_FROM, T_FROM + SPAN * 0.5),
        ev('e2', 'Weather', T_FROM + SPAN * 0.2, T_FROM + SPAN * 0.7),
        ev('e3', 'Maintenance', T_FROM + SPAN * 0.3, T_FROM + SPAN * 0.4),
      ],
      { from: T_FROM, to: T_TO },
      1000,
    );
    const rows = out.map((b) => b.row);
    expect(new Set(rows).size).toBeGreaterThan(1);
  });

  it('given_BackToBackEvents_when_Layout_then_BothFitRowZero', () => {
    const mid = T_FROM + SPAN / 2;
    const out = layoutEventBars(
      [ev('e1', 'X', T_FROM, mid), ev('e2', 'Y', mid, T_TO)],
      { from: T_FROM, to: T_TO },
      1000,
    );
    expect(out.find((b) => b.rid === 'e1')!.row).toBe(0);
    expect(out.find((b) => b.rid === 'e2')!.row).toBe(0);
  });

  it('given_EventWithoutEnd_when_Layout_then_RendersAtMinBarWidth', () => {
    const out = layoutEventBars(
      [ev('e1', 'X', T_MID)],
      { from: T_FROM, to: T_TO },
      1000,
      { minBarWidthPx: 4 },
    );
    expect(out).toHaveLength(1);
    expect(out[0].width).toBe(4);
    expect(out[0].end).toBe(T_MID); // raw end 缺失时回填为 start
  });

  it('given_EventOutsideWindow_when_Layout_then_Excluded', () => {
    const before = T_FROM - SPAN; // 完全在 from 之前
    const after = T_TO + SPAN; // 完全在 to 之后
    const out = layoutEventBars(
      [
        ev('e_before', 'X', before, before + 100),
        ev('e_after', 'X', after, after + 100),
        ev('e_in', 'X', T_MID, T_MID + 100),
      ],
      { from: T_FROM, to: T_TO },
      1000,
    );
    expect(out.map((b) => b.rid)).toEqual(['e_in']);
  });

  it('given_EventOverflowingLeft_when_Layout_then_ClampedToZero', () => {
    const out = layoutEventBars(
      [ev('e1', 'X', T_FROM - SPAN * 0.1, T_FROM + SPAN * 0.1)],
      { from: T_FROM, to: T_TO },
      1000,
    );
    expect(out).toHaveLength(1);
    expect(out[0].x).toBe(0);
    expect(out[0].width).toBeCloseTo(100, 5);
  });

  it('given_EventOverflowingRight_when_Layout_then_ClampedToPixelWidth', () => {
    const out = layoutEventBars(
      [ev('e1', 'X', T_FROM + SPAN * 0.9, T_TO + SPAN * 0.5)],
      { from: T_FROM, to: T_TO },
      1000,
    );
    expect(out).toHaveLength(1);
    expect(out[0].x).toBeCloseTo(900, 5);
    expect(out[0].width).toBeCloseTo(100, 5);
  });

  it('given_PixelWidthZero_when_Layout_then_Empty', () => {
    const out = layoutEventBars(
      [ev('e1', 'X', T_FROM, T_TO)],
      { from: T_FROM, to: T_TO },
      0,
    );
    expect(out).toEqual([]);
  });

  it('given_DegenerateWindow_when_Layout_then_Empty', () => {
    const out = layoutEventBars(
      [ev('e1', 'X', T_FROM, T_TO)],
      { from: T_TO, to: T_FROM },
      1000,
    );
    expect(out).toEqual([]);
  });

  it('given_MaxRowsExceeded_when_Layout_then_OverflowingEventsDropped', () => {
    // 3 个互相重叠的事件，maxRows=2 → 第三个被丢弃
    const out = layoutEventBars(
      [
        ev('e1', 'X', T_FROM, T_FROM + SPAN * 0.9),
        ev('e2', 'X', T_FROM, T_FROM + SPAN * 0.9),
        ev('e3', 'X', T_FROM, T_FROM + SPAN * 0.9),
      ],
      { from: T_FROM, to: T_TO },
      1000,
      { maxRows: 2 },
    );
    expect(out).toHaveLength(2);
    expect(out.map((b) => b.row).sort()).toEqual([0, 1]);
  });

  it('given_AnnotatedColor_when_Layout_then_PassedThrough', () => {
    const out = layoutEventBars(
      [{ rid: 'e1', objectType: 'X', start: T_FROM, end: T_MID, color: '#FF0000' }],
      { from: T_FROM, to: T_TO },
      1000,
    );
    expect(out[0].color).toBe('#FF0000');
  });
});
