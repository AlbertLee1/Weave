import { describe, it, expect } from 'vitest';
import {
  createSeriesPanelState,
  openSeries,
  closeSeries,
  togglePanel,
  setPanelExpanded,
  computeSubplotLayouts,
  timestampFromX,
  xFromTimestamp,
  type SeriesPanelEntry,
} from './seriesPanel';

const t = (s: string) => new Date(s).getTime();

const ENTRY_A: SeriesPanelEntry = {
  key: 'ri.objects.airport.JFK:throughput',
  seriesRid: 'ri.timeseries.metric.JFK-throughput',
  label: 'JFK throughput',
};

const ENTRY_B: SeriesPanelEntry = {
  key: 'ri.objects.airport.LAX:throughput',
  seriesRid: 'ri.timeseries.metric.LAX-throughput',
  label: 'LAX throughput',
};

const ENTRY_C: SeriesPanelEntry = {
  key: 'ri.objects.airport.SFO:throughput',
  seriesRid: 'ri.timeseries.metric.SFO-throughput',
  label: 'SFO throughput',
};

describe('VTX-033 createSeriesPanelState', () => {
  it('given_NoInit_when_Create_then_PanelCollapsedWithEmptyEntries', () => {
    const s = createSeriesPanelState();
    expect(s.expanded).toBe(false);
    expect(s.entries).toEqual([]);
  });

  it('given_InitWithEntries_when_Create_then_EntriesCopiedAndExpandedHonored', () => {
    const s = createSeriesPanelState({ expanded: true, entries: [ENTRY_A] });
    expect(s.expanded).toBe(true);
    expect(s.entries).toEqual([ENTRY_A]);
  });

  it('given_InitEntries_when_MutateInitArray_then_StateUnaffected', () => {
    const initEntries = [ENTRY_A];
    const s = createSeriesPanelState({ entries: initEntries });
    initEntries.push(ENTRY_B);
    expect(s.entries).toEqual([ENTRY_A]);
  });
});

describe('VTX-033 openSeries', () => {
  it('given_CollapsedEmptyPanel_when_OpenSeries_then_EntryAddedAndPanelExpanded', () => {
    const s0 = createSeriesPanelState();
    const s1 = openSeries(s0, ENTRY_A);
    expect(s1.entries).toEqual([ENTRY_A]);
    expect(s1.expanded).toBe(true);
  });

  it('given_AlreadyOpenSeries_when_OpenAgain_then_NoDuplicateEntry', () => {
    const s0 = createSeriesPanelState();
    const s1 = openSeries(s0, ENTRY_A);
    const s2 = openSeries(s1, ENTRY_A);
    expect(s2.entries).toHaveLength(1);
    expect(s2.entries[0]).toEqual(ENTRY_A);
  });

  it('given_CollapsedPanelWithOpenEntries_when_OpenSameKey_then_PanelReExpands', () => {
    let s = createSeriesPanelState({ entries: [ENTRY_A] });
    s = setPanelExpanded(s, false);
    expect(s.expanded).toBe(false);
    const s2 = openSeries(s, ENTRY_A);
    expect(s2.expanded).toBe(true);
    expect(s2.entries).toEqual([ENTRY_A]);
  });

  it('given_TwoDistinctSeriesOpened_when_Inspect_then_StackedInInsertionOrder', () => {
    let s = createSeriesPanelState();
    s = openSeries(s, ENTRY_A);
    s = openSeries(s, ENTRY_B);
    expect(s.entries.map(e => e.key)).toEqual([ENTRY_A.key, ENTRY_B.key]);
  });

  it('given_State_when_OpenSeries_then_ReturnsNewStateObject', () => {
    const s0 = createSeriesPanelState();
    const s1 = openSeries(s0, ENTRY_A);
    expect(s1).not.toBe(s0);
    expect(s1.entries).not.toBe(s0.entries);
  });
});

describe('VTX-033 closeSeries', () => {
  it('given_EntryPresent_when_Close_then_EntryRemoved', () => {
    let s = createSeriesPanelState();
    s = openSeries(s, ENTRY_A);
    s = openSeries(s, ENTRY_B);
    const next = closeSeries(s, ENTRY_A.key);
    expect(next.entries.map(e => e.key)).toEqual([ENTRY_B.key]);
  });

  it('given_LastEntry_when_Close_then_PanelCollapses', () => {
    let s = createSeriesPanelState();
    s = openSeries(s, ENTRY_A);
    expect(s.expanded).toBe(true);
    const next = closeSeries(s, ENTRY_A.key);
    expect(next.entries).toEqual([]);
    expect(next.expanded).toBe(false);
  });

  it('given_NonexistentKey_when_Close_then_StateUnchanged', () => {
    const s = createSeriesPanelState({ expanded: true, entries: [ENTRY_A] });
    const next = closeSeries(s, 'no-such-key');
    expect(next.entries).toEqual([ENTRY_A]);
    expect(next.expanded).toBe(true);
  });

  it('given_MultipleEntries_when_CloseMiddle_then_OrderPreserved', () => {
    let s = createSeriesPanelState();
    s = openSeries(s, ENTRY_A);
    s = openSeries(s, ENTRY_B);
    s = openSeries(s, ENTRY_C);
    const next = closeSeries(s, ENTRY_B.key);
    expect(next.entries.map(e => e.key)).toEqual([ENTRY_A.key, ENTRY_C.key]);
    expect(next.expanded).toBe(true);
  });
});

describe('VTX-033 togglePanel / setPanelExpanded', () => {
  it('given_CollapsedPanel_when_Toggle_then_Expanded', () => {
    const s = createSeriesPanelState();
    expect(togglePanel(s).expanded).toBe(true);
  });

  it('given_ExpandedPanel_when_Toggle_then_Collapsed', () => {
    const s = createSeriesPanelState({ expanded: true });
    expect(togglePanel(s).expanded).toBe(false);
  });

  it('given_AnyState_when_SetExpandedExplicit_then_FieldUpdatesAndOthersPreserved', () => {
    const s = createSeriesPanelState({ expanded: true, entries: [ENTRY_A] });
    const next = setPanelExpanded(s, false);
    expect(next.expanded).toBe(false);
    expect(next.entries).toEqual([ENTRY_A]);
  });
});

describe('VTX-033 computeSubplotLayouts', () => {
  it('given_NoEntries_when_Compute_then_EmptyLayout', () => {
    expect(computeSubplotLayouts([], 200)).toEqual([]);
  });

  it('given_OneEntryNoGap_when_Compute_then_FullHeightAtTopZero', () => {
    const layouts = computeSubplotLayouts([ENTRY_A], 200);
    expect(layouts).toEqual([{ key: ENTRY_A.key, top: 0, height: 200 }]);
  });

  it('given_ThreeEntriesNoGap_when_Compute_then_EqualHeightsStacked', () => {
    const layouts = computeSubplotLayouts([ENTRY_A, ENTRY_B, ENTRY_C], 300);
    expect(layouts).toEqual([
      { key: ENTRY_A.key, top: 0, height: 100 },
      { key: ENTRY_B.key, top: 100, height: 100 },
      { key: ENTRY_C.key, top: 200, height: 100 },
    ]);
  });

  it('given_TwoEntriesWithGap_when_Compute_then_GapSubtractedFromTotal', () => {
    const layouts = computeSubplotLayouts([ENTRY_A, ENTRY_B], 220, 20);
    expect(layouts).toEqual([
      { key: ENTRY_A.key, top: 0, height: 100 },
      { key: ENTRY_B.key, top: 120, height: 100 },
    ]);
  });

  it('given_GapLargerThanAvailable_when_Compute_then_HeightClampedToZero', () => {
    const layouts = computeSubplotLayouts([ENTRY_A, ENTRY_B], 10, 50);
    for (const l of layouts) {
      expect(l.height).toBeGreaterThanOrEqual(0);
    }
  });

  it('given_NegativeTotalHeight_when_Compute_then_HeightsClampedToZero', () => {
    const layouts = computeSubplotLayouts([ENTRY_A, ENTRY_B], -50);
    for (const l of layouts) {
      expect(l.height).toBe(0);
    }
  });
});

describe('VTX-033 timestampFromX', () => {
  const FROM = t('2026-05-01T00:00:00Z');
  const TO = t('2026-05-01T04:00:00Z');

  it('given_XAtZero_when_Convert_then_ReturnsFrom', () => {
    expect(timestampFromX(0, 400, FROM, TO)).toBe(FROM);
  });

  it('given_XAtWidth_when_Convert_then_ReturnsTo', () => {
    expect(timestampFromX(400, 400, FROM, TO)).toBe(TO);
  });

  it('given_XAtMiddle_when_Convert_then_ReturnsMidpointTimestamp', () => {
    const expected = FROM + (TO - FROM) / 2;
    expect(timestampFromX(200, 400, FROM, TO)).toBe(expected);
  });

  it('given_XBelowZero_when_Convert_then_ClampedToFrom', () => {
    expect(timestampFromX(-100, 400, FROM, TO)).toBe(FROM);
  });

  it('given_XAboveWidth_when_Convert_then_ClampedToTo', () => {
    expect(timestampFromX(9_999, 400, FROM, TO)).toBe(TO);
  });

  it('given_ZeroWidth_when_Convert_then_ReturnsFrom', () => {
    expect(timestampFromX(50, 0, FROM, TO)).toBe(FROM);
  });

  it('given_FromEqualsTo_when_Convert_then_ReturnsFrom', () => {
    expect(timestampFromX(123, 400, FROM, FROM)).toBe(FROM);
  });
});

describe('VTX-033 xFromTimestamp', () => {
  const FROM = t('2026-05-01T00:00:00Z');
  const TO = t('2026-05-01T04:00:00Z');

  it('given_TimestampEqualsFrom_when_Convert_then_ReturnsZero', () => {
    expect(xFromTimestamp(FROM, 400, FROM, TO)).toBe(0);
  });

  it('given_TimestampEqualsTo_when_Convert_then_ReturnsWidth', () => {
    expect(xFromTimestamp(TO, 400, FROM, TO)).toBe(400);
  });

  it('given_TimestampAtMidpoint_when_Convert_then_ReturnsHalfWidth', () => {
    const mid = FROM + (TO - FROM) / 2;
    expect(xFromTimestamp(mid, 400, FROM, TO)).toBe(200);
  });

  it('given_TimestampBeforeFrom_when_Convert_then_ClampedToZero', () => {
    expect(xFromTimestamp(FROM - 10_000, 400, FROM, TO)).toBe(0);
  });

  it('given_TimestampAfterTo_when_Convert_then_ClampedToWidth', () => {
    expect(xFromTimestamp(TO + 10_000, 400, FROM, TO)).toBe(400);
  });

  it('given_FromEqualsTo_when_Convert_then_ReturnsZero', () => {
    expect(xFromTimestamp(FROM, 400, FROM, FROM)).toBe(0);
  });

  it('given_XThenBack_when_RoundTrip_then_TimestampStable', () => {
    const original = FROM + (TO - FROM) * 0.37;
    const x = xFromTimestamp(original, 400, FROM, TO);
    expect(timestampFromX(x, 400, FROM, TO)).toBe(original);
  });
});
