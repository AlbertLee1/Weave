import { describe, it, expect } from 'vitest';
import {
  applyTimeSeriesWindowOverride,
  assertTimeSeriesWindowCellEditable,
  buildCreateTimeSeriesWindowOverrideRequest,
  buildDeleteTimeSeriesWindowOverrideRequest,
  buildTimeSeriesWindowOverrideKey,
  computeTimeSeriesWindowAggregate,
  findTimeSeriesWindowOverride,
  getTimeSeriesWindowOverride,
  isTimeSeriesWindowCellHighlighted,
  removeTimeSeriesWindowOverride,
  resolveTimeSeriesWindowCellEdit,
  setTimeSeriesWindowOverride,
  smoothSeriesByBucket,
  type TimeSeriesAggregationMethod,
  type TimeSeriesWindowOverride,
  type TimeSeriesWindowOverrideMap,
} from './timeSeriesOverride';
import { ScenarioFrozenError } from './overrideCell';
import type { TimePoint } from '../timeSeries/aggregateAtTime';

const t = (s: string) => new Date(s).getTime();
const AT = t('2026-05-15T12:00:00Z');
const WIN_5MIN = 5 * 60_000;
const WIN_1HR = 60 * 60_000;

function makeOverride(partial: Partial<TimeSeriesWindowOverride> = {}): TimeSeriesWindowOverride {
  return {
    id: 'ovr-1',
    scenarioRid: 'ri.vertex.main.scenario.s1',
    objectType: 'Airport',
    primaryKey: 'JFK',
    property: 'throughput',
    atTimestamp: AT,
    windowMs: WIN_5MIN,
    agg: 'avg',
    value: 100,
    ...partial,
  };
}

describe('VTX-042 timeSeriesOverride — cell key', () => {
  it('given_AllSixSegments_when_BuildKey_then_JoinedByDoubleColon', () => {
    const key = buildTimeSeriesWindowOverrideKey(
      'ri.vertex.main.scenario.s1',
      'Airport',
      'JFK',
      'throughput',
      AT,
      WIN_5MIN,
    );
    expect(key).toBe(`ri.vertex.main.scenario.s1::Airport::JFK::throughput::${AT}::${WIN_5MIN}`);
  });

  it('given_DifferentAtTimestamp_when_BuildKey_then_KeysDiffer', () => {
    const k1 = buildTimeSeriesWindowOverrideKey('s1', 'Airport', 'JFK', 'throughput', AT, WIN_5MIN);
    const k2 = buildTimeSeriesWindowOverrideKey('s1', 'Airport', 'JFK', 'throughput', AT + 60_000, WIN_5MIN);
    expect(k1).not.toBe(k2);
  });

  it('given_DifferentWindowMs_when_BuildKey_then_KeysDiffer', () => {
    const k1 = buildTimeSeriesWindowOverrideKey('s1', 'Airport', 'JFK', 'throughput', AT, WIN_5MIN);
    const k2 = buildTimeSeriesWindowOverrideKey('s1', 'Airport', 'JFK', 'throughput', AT, WIN_1HR);
    expect(k1).not.toBe(k2);
  });

  it('given_DifferentScenario_when_BuildKey_then_KeysDiffer', () => {
    const k1 = buildTimeSeriesWindowOverrideKey('s1', 'Airport', 'JFK', 'throughput', AT, WIN_5MIN);
    const k2 = buildTimeSeriesWindowOverrideKey('s2', 'Airport', 'JFK', 'throughput', AT, WIN_5MIN);
    expect(k1).not.toBe(k2);
  });
});

describe('VTX-042 timeSeriesOverride — map CRUD', () => {
  it('given_EmptyMap_when_GetUnknownKey_then_ReturnsNull', () => {
    const map: TimeSeriesWindowOverrideMap = {};
    expect(getTimeSeriesWindowOverride(map, 'missing')).toBeNull();
  });

  it('given_PopulatedMap_when_GetExistingKey_then_ReturnsOverride', () => {
    const ovr = makeOverride();
    const key = buildTimeSeriesWindowOverrideKey(
      ovr.scenarioRid,
      ovr.objectType,
      ovr.primaryKey,
      ovr.property,
      ovr.atTimestamp,
      ovr.windowMs,
    );
    const map: TimeSeriesWindowOverrideMap = { [key]: ovr };
    expect(getTimeSeriesWindowOverride(map, key)).toEqual(ovr);
  });

  it('given_SetOverride_when_Called_then_DoesNotMutateOriginalMap', () => {
    const original: TimeSeriesWindowOverrideMap = {};
    const next = setTimeSeriesWindowOverride(original, makeOverride());
    expect(original).toEqual({});
    expect(Object.keys(next)).toHaveLength(1);
  });

  it('given_SetOverrideTwice_when_SameKey_then_Replaces', () => {
    let map: TimeSeriesWindowOverrideMap = {};
    map = setTimeSeriesWindowOverride(map, makeOverride({ value: 100 }));
    map = setTimeSeriesWindowOverride(map, makeOverride({ value: 200 }));
    expect(Object.values(map)).toHaveLength(1);
    expect(Object.values(map)[0].value).toBe(200);
  });

  it('given_RemoveOverride_when_UnknownKey_then_ReturnsSameReference', () => {
    const map: TimeSeriesWindowOverrideMap = {};
    expect(removeTimeSeriesWindowOverride(map, 'missing')).toBe(map);
  });

  it('given_RemoveOverride_when_ExistingKey_then_DoesNotMutateOriginal', () => {
    const ovr = makeOverride();
    const key = buildTimeSeriesWindowOverrideKey(
      ovr.scenarioRid,
      ovr.objectType,
      ovr.primaryKey,
      ovr.property,
      ovr.atTimestamp,
      ovr.windowMs,
    );
    const original: TimeSeriesWindowOverrideMap = { [key]: ovr };
    const next = removeTimeSeriesWindowOverride(original, key);
    expect(Object.keys(original)).toEqual([key]);
    expect(Object.keys(next)).toEqual([]);
  });

  it('given_PopulatedMap_when_IsCellHighlighted_then_TrueIffKeyPresent', () => {
    const ovr = makeOverride();
    const key = buildTimeSeriesWindowOverrideKey(
      ovr.scenarioRid,
      ovr.objectType,
      ovr.primaryKey,
      ovr.property,
      ovr.atTimestamp,
      ovr.windowMs,
    );
    const map: TimeSeriesWindowOverrideMap = { [key]: ovr };
    expect(isTimeSeriesWindowCellHighlighted(map, key)).toBe(true);
    expect(isTimeSeriesWindowCellHighlighted(map, 'missing')).toBe(false);
  });
});

describe('VTX-042 timeSeriesOverride — findOverrideForWindow', () => {
  it('given_OverrideExists_when_FindWithSameWindow_then_ReturnsIt', () => {
    const ovr = makeOverride();
    let map: TimeSeriesWindowOverrideMap = {};
    map = setTimeSeriesWindowOverride(map, ovr);
    const found = findTimeSeriesWindowOverride(map, {
      scenarioRid: ovr.scenarioRid,
      objectType: ovr.objectType,
      primaryKey: ovr.primaryKey,
      property: ovr.property,
      atTimestamp: ovr.atTimestamp,
      windowMs: ovr.windowMs,
    });
    expect(found).toEqual(ovr);
  });

  it('given_OverrideExists_when_FindWithDifferentAtTimestamp_then_ReturnsNull', () => {
    const ovr = makeOverride();
    let map: TimeSeriesWindowOverrideMap = {};
    map = setTimeSeriesWindowOverride(map, ovr);
    const found = findTimeSeriesWindowOverride(map, {
      scenarioRid: ovr.scenarioRid,
      objectType: ovr.objectType,
      primaryKey: ovr.primaryKey,
      property: ovr.property,
      atTimestamp: ovr.atTimestamp + 60_000, // selectedTime moved
      windowMs: ovr.windowMs,
    });
    expect(found).toBeNull();
  });

  it('given_OverrideExists_when_FindWithDifferentWindow_then_ReturnsNull', () => {
    const ovr = makeOverride({ windowMs: WIN_5MIN });
    let map: TimeSeriesWindowOverrideMap = {};
    map = setTimeSeriesWindowOverride(map, ovr);
    const found = findTimeSeriesWindowOverride(map, {
      scenarioRid: ovr.scenarioRid,
      objectType: ovr.objectType,
      primaryKey: ovr.primaryKey,
      property: ovr.property,
      atTimestamp: ovr.atTimestamp,
      windowMs: WIN_1HR,
    });
    expect(found).toBeNull();
  });
});

describe('VTX-042 timeSeriesOverride — frozen scenario guard', () => {
  it('given_MutableScenario_when_AssertCellEditable_then_NoThrow', () => {
    expect(() =>
      assertTimeSeriesWindowCellEditable({ rid: 's1', immutable: false }),
    ).not.toThrow();
  });

  it('given_UndefinedImmutable_when_AssertCellEditable_then_NoThrow', () => {
    expect(() =>
      assertTimeSeriesWindowCellEditable({ rid: 's1' }),
    ).not.toThrow();
  });

  it('given_ImmutableScenario_when_AssertCellEditable_then_ThrowsScenarioFrozenError', () => {
    let caught: unknown = null;
    try {
      assertTimeSeriesWindowCellEditable({ rid: 's1', immutable: true });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(ScenarioFrozenError);
    if (caught instanceof ScenarioFrozenError) {
      expect(caught.scenarioRid).toBe('s1');
    }
  });
});

describe('VTX-042 timeSeriesOverride — build POST request', () => {
  it('given_AllFields_when_BuildCreate_then_PostsToScopedEndpoint', () => {
    const req = buildCreateTimeSeriesWindowOverrideRequest({
      scenarioRid: 'ri.vertex.main.scenario.s1',
      objectType: 'Airport',
      primaryKey: 'JFK',
      property: 'throughput',
      atTimestamp: AT,
      windowMs: WIN_5MIN,
      agg: 'avg',
      value: 1500,
    });
    expect(req.method).toBe('POST');
    expect(req.path).toBe(
      `/api/vertex/v1/scenarios/${encodeURIComponent('ri.vertex.main.scenario.s1')}/time-series-overrides`,
    );
    expect(req.body).toEqual({
      objectType: 'Airport',
      primaryKey: 'JFK',
      property: 'throughput',
      atTimestamp: AT,
      windowMs: WIN_5MIN,
      agg: 'avg',
      value: 1500,
    });
  });

  it('given_SmoothingProvided_when_BuildCreate_then_BodyIncludesSmoothingMs', () => {
    const req = buildCreateTimeSeriesWindowOverrideRequest({
      scenarioRid: 's1',
      objectType: 'Airport',
      primaryKey: 'JFK',
      property: 'throughput',
      atTimestamp: AT,
      windowMs: WIN_1HR,
      agg: 'avg',
      smoothingMs: WIN_5MIN,
      value: 1500,
    });
    expect(req.body).not.toBeNull();
    expect(req.body?.smoothingMs).toBe(WIN_5MIN);
  });

  it('given_SmoothingOmitted_when_BuildCreate_then_BodyDoesNotIncludeSmoothingKey', () => {
    const req = buildCreateTimeSeriesWindowOverrideRequest({
      scenarioRid: 's1',
      objectType: 'Airport',
      primaryKey: 'JFK',
      property: 'throughput',
      atTimestamp: AT,
      windowMs: WIN_5MIN,
      agg: 'avg',
      value: 1500,
    });
    expect(req.body).not.toBeNull();
    expect(req.body && 'smoothingMs' in req.body).toBe(false);
  });

  it('given_SpecialCharsInScenarioRid_when_BuildCreate_then_RidEncoded', () => {
    const req = buildCreateTimeSeriesWindowOverrideRequest({
      scenarioRid: 'ri/with spaces',
      objectType: 'Airport',
      primaryKey: 'JFK',
      property: 'throughput',
      atTimestamp: AT,
      windowMs: WIN_5MIN,
      agg: 'avg',
      value: 100,
    });
    expect(req.path).toContain(encodeURIComponent('ri/with spaces'));
  });

  it.each<[string, () => void]>([
    ['scenarioRid', () =>
      buildCreateTimeSeriesWindowOverrideRequest({
        scenarioRid: '  ',
        objectType: 'Airport',
        primaryKey: 'JFK',
        property: 'throughput',
        atTimestamp: AT,
        windowMs: WIN_5MIN,
        agg: 'avg',
        value: 1,
      })],
    ['objectType', () =>
      buildCreateTimeSeriesWindowOverrideRequest({
        scenarioRid: 's1',
        objectType: '',
        primaryKey: 'JFK',
        property: 'throughput',
        atTimestamp: AT,
        windowMs: WIN_5MIN,
        agg: 'avg',
        value: 1,
      })],
    ['primaryKey', () =>
      buildCreateTimeSeriesWindowOverrideRequest({
        scenarioRid: 's1',
        objectType: 'Airport',
        primaryKey: '',
        property: 'throughput',
        atTimestamp: AT,
        windowMs: WIN_5MIN,
        agg: 'avg',
        value: 1,
      })],
    ['property', () =>
      buildCreateTimeSeriesWindowOverrideRequest({
        scenarioRid: 's1',
        objectType: 'Airport',
        primaryKey: 'JFK',
        property: '   ',
        atTimestamp: AT,
        windowMs: WIN_5MIN,
        agg: 'avg',
        value: 1,
      })],
  ])('given_Blank_$0_when_BuildCreate_then_Throws', (_field, callIt) => {
    expect(callIt).toThrow();
  });

  it('given_WindowMsZero_when_BuildCreate_then_Throws', () => {
    expect(() =>
      buildCreateTimeSeriesWindowOverrideRequest({
        scenarioRid: 's1',
        objectType: 'Airport',
        primaryKey: 'JFK',
        property: 'throughput',
        atTimestamp: AT,
        windowMs: 0,
        agg: 'avg',
        value: 1,
      }),
    ).toThrow();
  });

  it('given_WindowMsNegative_when_BuildCreate_then_Throws', () => {
    expect(() =>
      buildCreateTimeSeriesWindowOverrideRequest({
        scenarioRid: 's1',
        objectType: 'Airport',
        primaryKey: 'JFK',
        property: 'throughput',
        atTimestamp: AT,
        windowMs: -100,
        agg: 'avg',
        value: 1,
      }),
    ).toThrow();
  });

  it('given_NaNValue_when_BuildCreate_then_Throws', () => {
    expect(() =>
      buildCreateTimeSeriesWindowOverrideRequest({
        scenarioRid: 's1',
        objectType: 'Airport',
        primaryKey: 'JFK',
        property: 'throughput',
        atTimestamp: AT,
        windowMs: WIN_5MIN,
        agg: 'avg',
        value: Number.NaN,
      }),
    ).toThrow();
  });

  it('given_InfinityValue_when_BuildCreate_then_Throws', () => {
    expect(() =>
      buildCreateTimeSeriesWindowOverrideRequest({
        scenarioRid: 's1',
        objectType: 'Airport',
        primaryKey: 'JFK',
        property: 'throughput',
        atTimestamp: AT,
        windowMs: WIN_5MIN,
        agg: 'avg',
        value: Number.POSITIVE_INFINITY,
      }),
    ).toThrow();
  });
});

describe('VTX-042 timeSeriesOverride — build DELETE request', () => {
  it('given_OverrideId_when_BuildDelete_then_HitsTopLevelEndpoint', () => {
    const req = buildDeleteTimeSeriesWindowOverrideRequest('ovr-123');
    expect(req.method).toBe('DELETE');
    expect(req.path).toBe(`/api/vertex/v1/time-series-overrides/${encodeURIComponent('ovr-123')}`);
    expect(req.body).toBeNull();
  });

  it('given_SpecialChars_when_BuildDelete_then_IdEncoded', () => {
    const req = buildDeleteTimeSeriesWindowOverrideRequest('id/with spaces');
    expect(req.path).toContain(encodeURIComponent('id/with spaces'));
  });

  it('given_BlankId_when_BuildDelete_then_Throws', () => {
    expect(() => buildDeleteTimeSeriesWindowOverrideRequest('  ')).toThrow();
  });
});

describe('VTX-042 timeSeriesOverride — smoothing', () => {
  const series: TimePoint[] = [
    { t: t('2026-05-15T12:00:00Z'), v: 10 },
    { t: t('2026-05-15T12:01:00Z'), v: 20 },
    { t: t('2026-05-15T12:02:00Z'), v: 30 },
    { t: t('2026-05-15T12:05:00Z'), v: 100 },
    { t: t('2026-05-15T12:06:00Z'), v: 200 },
  ];

  it('given_5MinBucket_when_Smooth_then_AveragesPointsPerBucket', () => {
    const smoothed = smoothSeriesByBucket(series, WIN_5MIN);
    // bucket starting at 12:00 → [10, 20, 30] avg = 20
    // bucket starting at 12:05 → [100, 200] avg = 150
    expect(smoothed).toHaveLength(2);
    expect(smoothed[0].t).toBe(t('2026-05-15T12:00:00Z'));
    expect(smoothed[0].v).toBe(20);
    expect(smoothed[1].t).toBe(t('2026-05-15T12:05:00Z'));
    expect(smoothed[1].v).toBe(150);
  });

  it('given_EmptySeries_when_Smooth_then_ReturnsEmpty', () => {
    expect(smoothSeriesByBucket([], WIN_5MIN)).toEqual([]);
  });

  it('given_ZeroBucket_when_Smooth_then_ReturnsOriginalCopy', () => {
    const out = smoothSeriesByBucket(series, 0);
    expect(out).toEqual(series);
    expect(out).not.toBe(series);
  });

  it('given_NegativeBucket_when_Smooth_then_ReturnsOriginalCopy', () => {
    const out = smoothSeriesByBucket(series, -1);
    expect(out).toEqual(series);
  });
});

describe('VTX-042 timeSeriesOverride — computeWindowAggregate', () => {
  const series: TimePoint[] = [
    { t: t('2026-05-15T12:00:00Z'), v: 10 },
    { t: t('2026-05-15T12:01:00Z'), v: 20 },
    { t: t('2026-05-15T12:02:00Z'), v: 30 },
    { t: t('2026-05-15T12:05:00Z'), v: 100 },
    { t: t('2026-05-15T12:06:00Z'), v: 200 },
  ];
  const selectedTime = t('2026-05-15T12:06:00Z');

  it('given_NoOverrideNoSmoothing_when_Compute_then_RawAggregate', () => {
    const r = computeTimeSeriesWindowAggregate(series, {
      selectedTime,
      windowMs: 10 * 60_000,
      agg: 'avg',
    }, null);
    // [10, 20, 30, 100, 200] avg = 72
    expect(r).toBe(72);
  });

  it('given_SmoothingApplied_when_NoOverride_then_AggregateOverBuckets', () => {
    const r = computeTimeSeriesWindowAggregate(series, {
      selectedTime,
      windowMs: 10 * 60_000,
      agg: 'avg',
      smoothingMs: WIN_5MIN,
    }, null);
    // smoothed buckets: 20, 150 → avg = 85
    expect(r).toBe(85);
  });

  it('given_OverridePresent_when_Compute_then_OverrideValueWins', () => {
    const r = computeTimeSeriesWindowAggregate(series, {
      selectedTime,
      windowMs: 10 * 60_000,
      agg: 'avg',
    }, makeOverride({ value: 9999 }));
    expect(r).toBe(9999);
  });

  it('given_OverridePresent_when_SmoothingAlsoConfigured_then_OverrideStillWins', () => {
    const r = computeTimeSeriesWindowAggregate(series, {
      selectedTime,
      windowMs: 10 * 60_000,
      agg: 'avg',
      smoothingMs: WIN_5MIN,
    }, makeOverride({ value: 1234 }));
    expect(r).toBe(1234);
  });

  it('given_EmptySeries_when_NoOverride_then_ReturnsNull', () => {
    const r = computeTimeSeriesWindowAggregate([], {
      selectedTime,
      windowMs: WIN_5MIN,
      agg: 'avg',
    }, null);
    expect(r).toBeNull();
  });
});

describe('VTX-042 timeSeriesOverride — applyTimeSeriesWindowOverride', () => {
  // applyTimeSeriesWindowOverride is the read overlay path: given a map and a
  // window query, return either { value: override.value } or null when no
  // override applies. Selecting a different selectedTime moves to a different
  // window key, so a 5-min override at AT does NOT bleed into AT+60s windows.
  const ovr = makeOverride({ value: 500 });

  it('given_OverrideKeyMatchesQuery_when_Apply_then_OverrideValue', () => {
    let map: TimeSeriesWindowOverrideMap = {};
    map = setTimeSeriesWindowOverride(map, ovr);
    const value = applyTimeSeriesWindowOverride(map, {
      scenarioRid: ovr.scenarioRid,
      objectType: ovr.objectType,
      primaryKey: ovr.primaryKey,
      property: ovr.property,
      atTimestamp: ovr.atTimestamp,
      windowMs: ovr.windowMs,
    });
    expect(value).toBe(500);
  });

  it('given_QueryAtDifferentTimestamp_when_Apply_then_Null', () => {
    let map: TimeSeriesWindowOverrideMap = {};
    map = setTimeSeriesWindowOverride(map, ovr);
    const value = applyTimeSeriesWindowOverride(map, {
      scenarioRid: ovr.scenarioRid,
      objectType: ovr.objectType,
      primaryKey: ovr.primaryKey,
      property: ovr.property,
      atTimestamp: ovr.atTimestamp + 60_000,
      windowMs: ovr.windowMs,
    });
    expect(value).toBeNull();
  });

  it('given_QueryAtDifferentWindow_when_Apply_then_Null', () => {
    let map: TimeSeriesWindowOverrideMap = {};
    map = setTimeSeriesWindowOverride(map, ovr);
    const value = applyTimeSeriesWindowOverride(map, {
      scenarioRid: ovr.scenarioRid,
      objectType: ovr.objectType,
      primaryKey: ovr.primaryKey,
      property: ovr.property,
      atTimestamp: ovr.atTimestamp,
      windowMs: WIN_1HR,
    });
    expect(value).toBeNull();
  });

  it('given_EmptyMap_when_Apply_then_Null', () => {
    const value = applyTimeSeriesWindowOverride({}, {
      scenarioRid: ovr.scenarioRid,
      objectType: ovr.objectType,
      primaryKey: ovr.primaryKey,
      property: ovr.property,
      atTimestamp: ovr.atTimestamp,
      windowMs: ovr.windowMs,
    });
    expect(value).toBeNull();
  });
});

describe('VTX-042 timeSeriesOverride — resolveCellEdit decisions', () => {
  const baseInput = {
    scenarioRid: 'ri.vertex.main.scenario.s1',
    objectType: 'Airport',
    primaryKey: 'JFK',
    property: 'throughput',
    atTimestamp: AT,
    windowMs: WIN_5MIN,
    agg: 'avg' as TimeSeriesAggregationMethod,
  };

  it('given_NoExistingNoInput_when_Resolve_then_Noop', () => {
    const d = resolveTimeSeriesWindowCellEdit({
      ...baseInput,
      rawInput: '   ',
      existing: null,
    });
    expect(d.kind).toBe('noop');
  });

  it('given_NoExistingNewValue_when_Resolve_then_Create', () => {
    const d = resolveTimeSeriesWindowCellEdit({
      ...baseInput,
      rawInput: '1500',
      existing: null,
    });
    expect(d.kind).toBe('create');
    if (d.kind === 'create') {
      expect(d.request.method).toBe('POST');
      expect(d.request.body?.value).toBe(1500);
      expect(d.request.body?.atTimestamp).toBe(AT);
      expect(d.request.body?.windowMs).toBe(WIN_5MIN);
    }
  });

  it('given_ExistingSameValue_when_Resolve_then_Noop', () => {
    const d = resolveTimeSeriesWindowCellEdit({
      ...baseInput,
      rawInput: '1500',
      existing: makeOverride({ value: 1500 }),
    });
    expect(d.kind).toBe('noop');
  });

  it('given_ExistingDifferentValue_when_Resolve_then_Update', () => {
    const d = resolveTimeSeriesWindowCellEdit({
      ...baseInput,
      rawInput: '2500',
      existing: makeOverride({ id: 'prev', value: 1500 }),
    });
    expect(d.kind).toBe('update');
    if (d.kind === 'update') {
      expect(d.previousId).toBe('prev');
      expect(d.request.body?.value).toBe(2500);
    }
  });

  it('given_ExistingButEmptyInput_when_Resolve_then_Delete', () => {
    const d = resolveTimeSeriesWindowCellEdit({
      ...baseInput,
      rawInput: '',
      existing: makeOverride({ id: 'prev', value: 1500 }),
    });
    expect(d.kind).toBe('delete');
    if (d.kind === 'delete') {
      expect(d.previousId).toBe('prev');
      expect(d.request.method).toBe('DELETE');
    }
  });

  it('given_InvalidNumberInput_when_Resolve_then_Invalid', () => {
    const d = resolveTimeSeriesWindowCellEdit({
      ...baseInput,
      rawInput: 'abc',
      existing: null,
    });
    expect(d.kind).toBe('invalid');
    if (d.kind === 'invalid') {
      expect(d.reason).toBe('not_a_number');
    }
  });

  it('given_InvalidInputWithExisting_when_Resolve_then_InvalidDoesNotEmitDelete', () => {
    // Critical invariant: typo in cell with existing override must NOT
    // silently delete existing — React layer should toast + roll back to
    // existing.value rather than data-loss.
    const d = resolveTimeSeriesWindowCellEdit({
      ...baseInput,
      rawInput: 'abc',
      existing: makeOverride({ id: 'prev', value: 1500 }),
    });
    expect(d.kind).toBe('invalid');
  });

  it('given_SmoothingSpecified_when_Create_then_SmoothingMsInRequestBody', () => {
    const d = resolveTimeSeriesWindowCellEdit({
      ...baseInput,
      smoothingMs: WIN_5MIN,
      rawInput: '1500',
      existing: null,
    });
    expect(d.kind).toBe('create');
    if (d.kind === 'create') {
      expect(d.request.body?.smoothingMs).toBe(WIN_5MIN);
    }
  });
});

describe('VTX-042 timeSeriesOverride — end-to-end happy paths', () => {
  it('given_OverrideAt5MinWindow_when_CursorMovesPast_then_OverlayNoLongerApplies', () => {
    // Spec: 用户多次修改 selectedTime When 移动游标 Then override 仅对配置时窗
    // 生效（应保存 atTimestamp）. Verified by key including atTimestamp.
    let map: TimeSeriesWindowOverrideMap = {};
    const ovr = makeOverride({ atTimestamp: AT, windowMs: WIN_5MIN, value: 1500 });
    map = setTimeSeriesWindowOverride(map, ovr);
    // same window → overlay applies
    const sameWindowValue = applyTimeSeriesWindowOverride(map, {
      scenarioRid: ovr.scenarioRid,
      objectType: ovr.objectType,
      primaryKey: ovr.primaryKey,
      property: ovr.property,
      atTimestamp: AT,
      windowMs: WIN_5MIN,
    });
    expect(sameWindowValue).toBe(1500);
    // cursor moved 1 min later → different window key → no overlay
    const movedCursorValue = applyTimeSeriesWindowOverride(map, {
      scenarioRid: ovr.scenarioRid,
      objectType: ovr.objectType,
      primaryKey: ovr.primaryKey,
      property: ovr.property,
      atTimestamp: AT + 60_000,
      windowMs: WIN_5MIN,
    });
    expect(movedCursorValue).toBeNull();
  });

  it('given_BlurWith1500_when_SmoothingConfigured_then_PostsScalarOverrideWithSmoothingMetadata', () => {
    const d = resolveTimeSeriesWindowCellEdit({
      scenarioRid: 'ri.vertex.main.scenario.s1',
      objectType: 'Airport',
      primaryKey: 'JFK',
      property: 'throughput',
      atTimestamp: AT,
      windowMs: WIN_1HR,
      agg: 'avg',
      smoothingMs: WIN_5MIN,
      rawInput: '1500',
      existing: null,
    });
    expect(d.kind).toBe('create');
    if (d.kind === 'create') {
      expect(d.request.body).toEqual({
        objectType: 'Airport',
        primaryKey: 'JFK',
        property: 'throughput',
        atTimestamp: AT,
        windowMs: WIN_1HR,
        agg: 'avg',
        smoothingMs: WIN_5MIN,
        value: 1500,
      });
    }
  });

  it('given_ImmutableScenario_when_AssertCellEditable_then_BlocksWithFrozenTooltipMessage', () => {
    let caught: unknown = null;
    try {
      assertTimeSeriesWindowCellEditable({ rid: 'ri.vertex.main.scenario.s1', immutable: true });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(ScenarioFrozenError);
    if (caught instanceof ScenarioFrozenError) {
      expect(caught.message.toLowerCase()).toContain('scenario is frozen');
    }
  });
});
