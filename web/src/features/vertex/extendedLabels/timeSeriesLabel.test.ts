// VTX-060 — TimeSeries Extended Label 渲染（窗口聚合 + smoothing + selectedTime debounce）。
//
// BDD acceptance（来自 prd.json VTX-060）：
//   1. Given label timeSeries kind + 当前窗口 When 渲染 Then 调
//      /timeseries/throughput?from=...&to=...&agg=AVG + 显示 scalar
//   2. Given smoothing=5min 配置 When 计算 Then 用 5 min bucket 滑窗
//   3. Given selectedTime 移动 When debounce 100ms 后 Then 全部
//      timeSeries label 重算

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  MISSING_VALUE_PLACEHOLDER,
  buildTimeSeriesLabelRequest,
  computeTimeSeriesLabelScalar,
  createTimeSeriesLabelDebouncer,
  isTimeSeriesExtendedLabel,
  renderTimeSeriesExtendedLabel,
  TIMESERIES_LABEL_DEBOUNCE_MS,
  type TimeSeriesExtendedLabelSpec,
  type TimeSeriesLabelWindow,
} from './timeSeriesLabel';

describe('VTX-060 extendedLabels.timeSeriesLabel — isTimeSeriesExtendedLabel', () => {
  it('given_kind_timeSeries_when_check_then_true', () => {
    expect(isTimeSeriesExtendedLabel({ kind: 'timeSeries', timeSeriesRid: 'throughput' })).toBe(true);
  });

  it('given_kind_property_when_check_then_false', () => {
    expect(isTimeSeriesExtendedLabel({ kind: 'property', property: 'onTimePct' })).toBe(false);
  });

  it('given_kind_measure_when_check_then_false', () => {
    expect(isTimeSeriesExtendedLabel({ kind: 'measure', measureRid: 'rid' })).toBe(false);
  });

  it('given_kind_unknown_when_check_then_false', () => {
    expect(isTimeSeriesExtendedLabel({ kind: 'badge' })).toBe(false);
  });
});

describe('VTX-060 extendedLabels.timeSeriesLabel — buildTimeSeriesLabelRequest (BDD #1)', () => {
  const spec: TimeSeriesExtendedLabelSpec = {
    kind: 'timeSeries',
    timeSeriesRid: 'throughput',
  };
  const ctx = {
    ontology: 'ri.ontology.main.ontology.vtx',
    objectType: 'Airport',
    primaryKey: 'JFK',
  };
  const window: TimeSeriesLabelWindow = {
    from: 1_700_000_000_000,
    to: 1_700_000_300_000,
    agg: 'avg',
  };

  it('given_spec_and_window_when_build_then_returns_get_with_oss_timeseries_path', () => {
    const r = buildTimeSeriesLabelRequest(spec, ctx, window);
    expect(r.method).toBe('GET');
    expect(r.path).toContain('/api/v2/ontologies/ri.ontology.main.ontology.vtx/objects/Airport/JFK/timeseries/throughput');
    expect(r.path).toContain('from=');
    expect(r.path).toContain('to=');
    expect(r.path).toContain('agg=AVG');
  });

  it('given_agg_avg_when_build_then_query_uppercases_to_AVG', () => {
    const r = buildTimeSeriesLabelRequest(spec, ctx, { ...window, agg: 'avg' });
    expect(r.path).toContain('agg=AVG');
  });

  it('given_agg_last_when_build_then_query_uppercases_to_LAST', () => {
    const r = buildTimeSeriesLabelRequest(spec, ctx, { ...window, agg: 'last' });
    expect(r.path).toContain('agg=LAST');
  });

  it('given_smoothingMs_5min_when_build_then_query_includes_bucket_300000ms', () => {
    const r = buildTimeSeriesLabelRequest(spec, ctx, { ...window, smoothingMs: 5 * 60 * 1000 });
    expect(r.path).toContain('bucket=300000ms');
  });

  it('given_smoothingMs_undefined_when_build_then_query_omits_bucket', () => {
    const r = buildTimeSeriesLabelRequest(spec, ctx, window);
    expect(r.path).not.toContain('bucket=');
  });

  it('given_smoothingMs_zero_when_build_then_query_omits_bucket', () => {
    const r = buildTimeSeriesLabelRequest(spec, ctx, { ...window, smoothingMs: 0 });
    expect(r.path).not.toContain('bucket=');
  });

  it('given_special_chars_when_build_then_uri_components_escaped', () => {
    const r = buildTimeSeriesLabelRequest(
      { kind: 'timeSeries', timeSeriesRid: 'a b/c' },
      { ontology: 'ri ont', objectType: 'A/B', primaryKey: 'JFK#1' },
      window,
    );
    expect(r.path).toContain('/objects/A%2FB/JFK%231/timeseries/a%20b%2Fc');
    expect(r.path).toContain('/ontologies/ri%20ont/');
  });

  it('given_from_to_when_build_then_uses_millisecond_numbers', () => {
    const r = buildTimeSeriesLabelRequest(spec, ctx, window);
    expect(r.path).toContain('from=1700000000000');
    expect(r.path).toContain('to=1700000300000');
  });

  it('given_blank_ontology_when_build_then_throws', () => {
    expect(() =>
      buildTimeSeriesLabelRequest(spec, { ontology: '', objectType: 'A', primaryKey: 'k' }, window),
    ).toThrow(/ontology/i);
  });

  it('given_blank_objectType_when_build_then_throws', () => {
    expect(() =>
      buildTimeSeriesLabelRequest(spec, { ontology: 'o', objectType: '', primaryKey: 'k' }, window),
    ).toThrow(/objectType/i);
  });

  it('given_blank_primaryKey_when_build_then_throws', () => {
    expect(() =>
      buildTimeSeriesLabelRequest(spec, { ontology: 'o', objectType: 't', primaryKey: '' }, window),
    ).toThrow(/primaryKey/i);
  });

  it('given_blank_timeSeriesRid_when_build_then_throws', () => {
    expect(() =>
      buildTimeSeriesLabelRequest({ kind: 'timeSeries', timeSeriesRid: '' }, ctx, window),
    ).toThrow(/timeSeriesRid/i);
  });

  it('given_window_with_to_before_from_when_build_then_throws', () => {
    expect(() =>
      buildTimeSeriesLabelRequest(spec, ctx, { from: 100, to: 50, agg: 'avg' }),
    ).toThrow(/from/i);
  });

  it('given_non_timeSeries_spec_when_build_then_throws', () => {
    expect(() =>
      buildTimeSeriesLabelRequest(
        { kind: 'property', timeSeriesRid: 't' } as unknown as TimeSeriesExtendedLabelSpec,
        ctx,
        window,
      ),
    ).toThrow(/timeSeries/i);
  });
});

describe('VTX-060 extendedLabels.timeSeriesLabel — computeTimeSeriesLabelScalar (BDD #2 smoothing)', () => {
  it('given_series_inside_window_when_compute_avg_then_returns_average', () => {
    const t0 = 1_700_000_000_000;
    const series = [
      { t: t0, v: 100 },
      { t: t0 + 60_000, v: 200 },
    ];
    const r = computeTimeSeriesLabelScalar(series, {
      selectedTime: t0 + 60_000,
      windowMs: 120_000,
      agg: 'avg',
    });
    expect(r).toBe(150);
  });

  it('given_empty_series_when_compute_then_returns_null', () => {
    expect(
      computeTimeSeriesLabelScalar([], { selectedTime: 0, windowMs: 60_000, agg: 'avg' }),
    ).toBeNull();
  });

  it('given_series_outside_window_when_compute_then_returns_null', () => {
    const series = [{ t: 0, v: 100 }];
    expect(
      computeTimeSeriesLabelScalar(series, {
        selectedTime: 10_000_000,
        windowMs: 1_000,
        agg: 'avg',
      }),
    ).toBeNull();
  });

  // BDD #2 — smoothing=5min
  it('given_smoothingMs_5min_when_compute_then_uses_bucketed_average', () => {
    // 5-min bucket = 300_000 ms. Two points in bucket A (avg=150), two in bucket B (avg=300).
    const bucket = 5 * 60 * 1000;
    const a = 1_700_000_000_000;
    const b = a + bucket;
    const series = [
      { t: a, v: 100 },
      { t: a + 60_000, v: 200 },
      { t: b, v: 250 },
      { t: b + 60_000, v: 350 },
    ];
    // Window covers both buckets. avg of bucketed (150, 300) = 225.
    const r = computeTimeSeriesLabelScalar(series, {
      selectedTime: b + 60_000,
      windowMs: 2 * bucket,
      agg: 'avg',
      smoothingMs: bucket,
    });
    expect(r).toBe(225);
  });

  it('given_smoothingMs_zero_when_compute_then_uses_raw_points', () => {
    // Without smoothing, avg of all raw points (100,200,250,350) = 225 too —
    // use sum to distinguish: smoothing would collapse to bucketed sum 450,
    // raw would sum 900.
    const a = 0;
    const b = 5 * 60 * 1000;
    const series = [
      { t: a, v: 100 },
      { t: a + 60_000, v: 200 },
      { t: b, v: 250 },
      { t: b + 60_000, v: 350 },
    ];
    const r = computeTimeSeriesLabelScalar(series, {
      selectedTime: b + 60_000,
      windowMs: 2 * 5 * 60 * 1000,
      agg: 'sum',
      smoothingMs: 0,
    });
    expect(r).toBe(900);
  });

  it('given_smoothingMs_undefined_when_compute_then_uses_raw_points', () => {
    const series = [
      { t: 0, v: 100 },
      { t: 60_000, v: 200 },
    ];
    const r = computeTimeSeriesLabelScalar(series, {
      selectedTime: 60_000,
      windowMs: 60_000,
      agg: 'sum',
    });
    expect(r).toBe(300);
  });

  it('given_agg_max_when_compute_then_returns_max', () => {
    const series = [
      { t: 0, v: 100 },
      { t: 60_000, v: 200 },
      { t: 120_000, v: 50 },
    ];
    expect(
      computeTimeSeriesLabelScalar(series, {
        selectedTime: 120_000,
        windowMs: 120_000,
        agg: 'max',
      }),
    ).toBe(200);
  });

  it('given_agg_count_when_compute_then_returns_point_count', () => {
    const series = [
      { t: 0, v: 100 },
      { t: 60_000, v: 200 },
    ];
    expect(
      computeTimeSeriesLabelScalar(series, {
        selectedTime: 60_000,
        windowMs: 60_000,
        agg: 'count',
      }),
    ).toBe(2);
  });
});

describe('VTX-060 extendedLabels.timeSeriesLabel — renderTimeSeriesExtendedLabel', () => {
  const spec: TimeSeriesExtendedLabelSpec = {
    kind: 'timeSeries',
    timeSeriesRid: 'throughput',
  };

  it('given_scalar_present_when_render_then_shows_rid_colon_scalar', () => {
    const r = renderTimeSeriesExtendedLabel(spec, 1500);
    expect(r.text).toBe('throughput: 1500');
    expect(r.labelName).toBe('throughput');
    expect(r.valueText).toBe('1500');
    expect(r.status).toBe('present');
  });

  it('given_scalar_null_when_render_then_shows_em_dash', () => {
    const r = renderTimeSeriesExtendedLabel(spec, null);
    expect(r.text).toBe('throughput: —');
    expect(r.status).toBe('missing');
    expect(r.valueText).toBe(MISSING_VALUE_PLACEHOLDER);
  });

  it('given_NaN_scalar_when_render_then_status_missing', () => {
    const r = renderTimeSeriesExtendedLabel(spec, Number.NaN);
    expect(r.status).toBe('missing');
    expect(r.text).toBe('throughput: —');
  });

  it('given_infinity_scalar_when_render_then_status_missing', () => {
    const r = renderTimeSeriesExtendedLabel(spec, Number.POSITIVE_INFINITY);
    expect(r.status).toBe('missing');
  });

  it('given_zero_scalar_when_render_then_status_present', () => {
    const r = renderTimeSeriesExtendedLabel(spec, 0);
    expect(r.status).toBe('present');
    expect(r.text).toBe('throughput: 0');
  });

  it('given_negative_scalar_when_render_then_status_present', () => {
    const r = renderTimeSeriesExtendedLabel(spec, -42.5);
    expect(r.status).toBe('present');
    expect(r.text).toBe('throughput: -42.5');
  });

  it('given_displayName_when_render_then_uses_displayName_as_label', () => {
    const r = renderTimeSeriesExtendedLabel(
      { ...spec, displayName: 'Throughput (pax/hr)' },
      1500,
    );
    expect(r.text).toBe('Throughput (pax/hr): 1500');
    expect(r.labelName).toBe('Throughput (pax/hr)');
  });

  it('given_blank_displayName_when_render_then_falls_back_to_rid', () => {
    const r = renderTimeSeriesExtendedLabel({ ...spec, displayName: '   ' }, 1500);
    expect(r.labelName).toBe('throughput');
  });

  it('given_formatValue_option_when_render_then_uses_formatter_output', () => {
    const r = renderTimeSeriesExtendedLabel(spec, 1500, {
      formatValue: (n) => `${(n / 1000).toFixed(1)}k`,
    });
    expect(r.text).toBe('throughput: 1.5k');
    expect(r.status).toBe('present');
  });

  it('given_formatValue_returns_empty_when_render_then_status_missing', () => {
    const r = renderTimeSeriesExtendedLabel(spec, 1500, {
      formatValue: () => '',
    });
    expect(r.status).toBe('missing');
    expect(r.valueText).toBe(MISSING_VALUE_PLACEHOLDER);
  });

  it('given_custom_missingPlaceholder_when_render_then_used_for_null_scalar', () => {
    const r = renderTimeSeriesExtendedLabel(spec, null, { missingPlaceholder: 'N/A' });
    expect(r.text).toBe('throughput: N/A');
    expect(r.status).toBe('missing');
  });

  it('given_error_option_when_render_then_status_error_and_value_uses_error_placeholder', () => {
    const r = renderTimeSeriesExtendedLabel(spec, null, { error: 'series not found' });
    expect(r.status).toBe('error');
    expect(r.valueText).toBe('!');
    expect(r.text).toBe('throughput: !');
    expect(r.errorMessage).toBe('series not found');
  });

  it('given_error_option_overrides_present_scalar_when_render_then_status_error', () => {
    const r = renderTimeSeriesExtendedLabel(spec, 1500, { error: 'fetch failed' });
    expect(r.status).toBe('error');
    expect(r.valueText).toBe('!');
  });

  it('given_custom_errorPlaceholder_when_render_then_uses_override', () => {
    const r = renderTimeSeriesExtendedLabel(spec, null, {
      error: 'boom',
      errorPlaceholder: 'ERR',
    });
    expect(r.valueText).toBe('ERR');
    expect(r.text).toBe('throughput: ERR');
  });

  it('given_non_timeSeries_kind_when_render_then_throws', () => {
    expect(() =>
      renderTimeSeriesExtendedLabel(
        { kind: 'property' as 'timeSeries', timeSeriesRid: 't' },
        1,
      ),
    ).toThrow(/timeSeries/i);
  });

  it('given_blank_timeSeriesRid_when_render_then_throws', () => {
    expect(() =>
      renderTimeSeriesExtendedLabel({ kind: 'timeSeries', timeSeriesRid: '' }, 1),
    ).toThrow(/timeSeriesRid/i);
  });
});

describe('VTX-060 extendedLabels.timeSeriesLabel — createTimeSeriesLabelDebouncer (BDD #3)', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('given_default_then_debounce_constant_is_100ms', () => {
    expect(TIMESERIES_LABEL_DEBOUNCE_MS).toBe(100);
  });

  it('given_selectedTime_change_when_within_debounce_then_callback_not_yet_called', () => {
    const cb = vi.fn();
    const notify = createTimeSeriesLabelDebouncer(cb);
    notify(1000);
    vi.advanceTimersByTime(50);
    expect(cb).not.toHaveBeenCalled();
  });

  it('given_selectedTime_change_when_debounce_elapses_then_callback_called_once_with_latest_value', () => {
    const cb = vi.fn();
    const notify = createTimeSeriesLabelDebouncer(cb);
    notify(1000);
    notify(2000);
    notify(3000);
    vi.advanceTimersByTime(TIMESERIES_LABEL_DEBOUNCE_MS);
    expect(cb).toHaveBeenCalledTimes(1);
    expect(cb).toHaveBeenCalledWith(3000);
  });

  it('given_cancel_called_before_fire_then_callback_never_invoked', () => {
    const cb = vi.fn();
    const notify = createTimeSeriesLabelDebouncer(cb);
    notify(1000);
    notify.cancel();
    vi.advanceTimersByTime(1_000);
    expect(cb).not.toHaveBeenCalled();
  });

  it('given_custom_delay_when_debounced_then_uses_provided_ms', () => {
    const cb = vi.fn();
    const notify = createTimeSeriesLabelDebouncer(cb, 250);
    notify(42);
    vi.advanceTimersByTime(200);
    expect(cb).not.toHaveBeenCalled();
    vi.advanceTimersByTime(50);
    expect(cb).toHaveBeenCalledTimes(1);
    expect(cb).toHaveBeenCalledWith(42);
  });
});
