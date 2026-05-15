import { describe, expect, it } from 'vitest';

import {
  BASELINE_RUN_FAILED_MESSAGE,
  applyBaselineOutputs,
  applyBaselineRunError,
  applyBaselineRunStart,
  applyBaselineRunSuccess,
  buildBaselineRunRequest,
  buildObjectBaselineOutputKey,
  buildPaneBaselineOutputKey,
  computeBaselineCompare,
  createBaselineRunMap,
  createBaselineRunState,
  createScenarioBaselineOptions,
  formatBaselineCompareLabel,
  getBaselineColorHint,
  getBaselineOutput,
  getBaselineRunStatus,
  isBaselineRunning,
  parseBaselineRunErrorResponse,
  parseBaselineRunSuccessResponse,
  setBaselineRunState,
  setRunBaseline,
  shouldDispatchBaseline,
  shouldRunBaseline,
} from './baselineRun';

const caseStudyRid = 'ri.vertex.main.case-study.cs-1';
const otherCaseStudyRid = 'ri.vertex.main.case-study.cs-2';

describe('VTX-045 ScenarioBaselineOptions', () => {
  it('given_no_init_when_createOptions_then_runBaseline_false_by_default', () => {
    const opts = createScenarioBaselineOptions();
    expect(opts.runBaseline).toBe(false);
  });

  it('given_init_runBaseline_true_when_createOptions_then_uses_initial_value', () => {
    const opts = createScenarioBaselineOptions({ runBaseline: true });
    expect(opts.runBaseline).toBe(true);
  });

  it('given_options_when_setRunBaseline_true_then_returns_enabled_copy', () => {
    const opts = createScenarioBaselineOptions();
    const next = setRunBaseline(opts, true);
    expect(next.runBaseline).toBe(true);
    expect(opts.runBaseline).toBe(false); // immutable
  });

  it('given_options_when_setRunBaseline_same_value_then_returns_same_reference', () => {
    const opts = createScenarioBaselineOptions({ runBaseline: true });
    const next = setRunBaseline(opts, true);
    expect(next).toBe(opts);
  });

  it('given_runBaseline_true_when_shouldRunBaseline_then_true', () => {
    expect(shouldRunBaseline({ runBaseline: true })).toBe(true);
    expect(shouldRunBaseline({ runBaseline: false })).toBe(false);
  });
});

describe('VTX-045 BaselineRunMap state machine', () => {
  it('given_fresh_map_when_createBaselineRunMap_then_empty', () => {
    expect(createBaselineRunMap()).toEqual({});
  });

  it('given_unknown_caseStudyRid_when_getStatus_then_idle', () => {
    expect(getBaselineRunStatus(createBaselineRunMap(), caseStudyRid)).toBe('idle');
  });

  it('given_createState_when_invoked_then_returns_idle_blank_state', () => {
    const s = createBaselineRunState();
    expect(s.status).toBe('idle');
    expect(s.startedAt).toBeNull();
    expect(s.durationMs).toBeNull();
    expect(s.outputs).toEqual({});
    expect(s.error).toBeNull();
  });

  it('given_idle_map_when_applyStart_then_marks_running_and_clears_outputs', () => {
    const map = applyBaselineRunSuccess(
      createBaselineRunMap(),
      caseStudyRid,
      1000,
      { 'r1::p1': 100 },
    );
    const next = applyBaselineRunStart(map, caseStudyRid, 1700000000000);
    expect(getBaselineRunStatus(next, caseStudyRid)).toBe('running');
    expect(next[caseStudyRid].startedAt).toBe(1700000000000);
    expect(next[caseStudyRid].durationMs).toBeNull();
    expect(next[caseStudyRid].outputs).toEqual({});
    expect(next[caseStudyRid].error).toBeNull();
  });

  it('given_previously_failed_run_when_applyStart_then_clears_error', () => {
    let map = createBaselineRunMap();
    map = applyBaselineRunError(map, caseStudyRid, 'boom');
    const next = applyBaselineRunStart(map, caseStudyRid, 1);
    expect(next[caseStudyRid].error).toBeNull();
  });

  it('given_running_state_when_applySuccess_then_records_duration_outputs_clears_error', () => {
    let map = createBaselineRunMap();
    map = applyBaselineRunStart(map, caseStudyRid, 100);
    const outputs = { 'r1::p1': 1000, 'Airport::JFK::capacity': 200 };
    const next = applyBaselineRunSuccess(map, caseStudyRid, 1850, outputs);
    expect(getBaselineRunStatus(next, caseStudyRid)).toBe('success');
    expect(next[caseStudyRid].durationMs).toBe(1850);
    expect(next[caseStudyRid].outputs).toEqual(outputs);
    expect(next[caseStudyRid].error).toBeNull();
  });

  it('given_applySuccess_when_outputs_passed_then_does_not_share_reference', () => {
    const outputs = { 'r1::p1': 1 };
    const next = applyBaselineRunSuccess(createBaselineRunMap(), caseStudyRid, 100, outputs);
    expect(next[caseStudyRid].outputs).not.toBe(outputs);
    expect(next[caseStudyRid].outputs).toEqual(outputs);
    // mutate input afterwards
    (outputs as Record<string, number>)['r1::p1'] = 999;
    expect(next[caseStudyRid].outputs['r1::p1']).toBe(1);
  });

  it('given_prior_success_when_applyError_then_preserves_prior_outputs', () => {
    let map = createBaselineRunMap();
    map = applyBaselineRunSuccess(map, caseStudyRid, 100, { 'r1::p1': 42 });
    const next = applyBaselineRunError(map, caseStudyRid, 'rerun failed');
    expect(next[caseStudyRid].status).toBe('error');
    expect(next[caseStudyRid].error).toBe('rerun failed');
    expect(next[caseStudyRid].outputs).toEqual({ 'r1::p1': 42 });
  });

  it('given_state_for_one_case_study_when_mutate_other_then_first_state_unchanged_by_reference', () => {
    let map = createBaselineRunMap();
    map = applyBaselineRunStart(map, caseStudyRid, 100);
    const prior = map[caseStudyRid];
    const next = applyBaselineRunStart(map, otherCaseStudyRid, 200);
    expect(next[caseStudyRid]).toBe(prior);
    expect(next[otherCaseStudyRid].status).toBe('running');
  });

  it('given_map_when_setBaselineRunState_then_replaces_only_target_key', () => {
    let map = createBaselineRunMap();
    map = applyBaselineRunStart(map, caseStudyRid, 100);
    map = applyBaselineRunStart(map, otherCaseStudyRid, 200);
    const next = setBaselineRunState(map, caseStudyRid, {
      status: 'success',
      durationMs: 999,
      outputs: {},
      error: null,
      startedAt: 100,
    });
    expect(next[caseStudyRid].status).toBe('success');
    expect(next[otherCaseStudyRid].status).toBe('running');
  });

  it('given_running_state_when_isBaselineRunning_then_true', () => {
    let map = createBaselineRunMap();
    map = applyBaselineRunStart(map, caseStudyRid, 0);
    expect(isBaselineRunning(map, caseStudyRid)).toBe(true);
    expect(isBaselineRunning(map, otherCaseStudyRid)).toBe(false);
  });
});

describe('VTX-045 Baseline run request builder', () => {
  it('given_case_study_rid_when_buildRequest_then_returns_POST_with_encoded_path', () => {
    const req = buildBaselineRunRequest({ caseStudyRid });
    expect(req.method).toBe('POST');
    expect(req.path).toBe(
      `/api/vertex/v1/case-studies/${encodeURIComponent(caseStudyRid)}/baseline/run`,
    );
    expect(req.body).toEqual({});
  });

  it('given_caseStudyRid_with_special_chars_when_buildRequest_then_path_is_url_encoded', () => {
    const ridWithSpecial = 'ri.vertex.main.case-study.with/slash space';
    const req = buildBaselineRunRequest({ caseStudyRid: ridWithSpecial });
    expect(req.path).toBe(
      `/api/vertex/v1/case-studies/${encodeURIComponent(ridWithSpecial)}/baseline/run`,
    );
  });

  it('given_blank_caseStudyRid_when_buildRequest_then_throws', () => {
    expect(() => buildBaselineRunRequest({ caseStudyRid: '   ' })).toThrow(
      /caseStudyRid/,
    );
  });

  it('given_empty_caseStudyRid_when_buildRequest_then_throws', () => {
    expect(() => buildBaselineRunRequest({ caseStudyRid: '' })).toThrow(/caseStudyRid/);
  });
});

describe('VTX-045 Baseline run response parsing', () => {
  it('given_success_response_with_outputs_when_parse_then_returns_durationMs_and_outputs', () => {
    const parsed = parseBaselineRunSuccessResponse({
      status: 'success',
      durationMs: 2100,
      outputs: { 'r1::predicted_delay': 1000, 'Airport::JFK::capacity': 220 },
    });
    expect(parsed).toEqual({
      durationMs: 2100,
      outputs: { 'r1::predicted_delay': 1000, 'Airport::JFK::capacity': 220 },
    });
  });

  it('given_success_response_without_outputs_when_parse_then_returns_empty_outputs', () => {
    const parsed = parseBaselineRunSuccessResponse({
      status: 'success',
      durationMs: 500,
    });
    expect(parsed.outputs).toEqual({});
  });

  it('given_success_response_missing_durationMs_when_parse_then_throws', () => {
    expect(() =>
      parseBaselineRunSuccessResponse({
        status: 'success',
      } as unknown as Parameters<typeof parseBaselineRunSuccessResponse>[0]),
    ).toThrow(/durationMs/);
  });

  it('given_negative_durationMs_when_parse_then_throws', () => {
    expect(() =>
      parseBaselineRunSuccessResponse({
        status: 'success',
        durationMs: -100,
      }),
    ).toThrow(/durationMs/);
  });

  it('given_non_finite_durationMs_when_parse_then_throws', () => {
    expect(() =>
      parseBaselineRunSuccessResponse({
        status: 'success',
        durationMs: Number.POSITIVE_INFINITY,
      }),
    ).toThrow(/durationMs/);
    expect(() =>
      parseBaselineRunSuccessResponse({
        status: 'success',
        durationMs: Number.NaN,
      }),
    ).toThrow(/durationMs/);
  });

  it('given_outputs_with_non_scalar_value_when_parse_then_throws', () => {
    expect(() =>
      parseBaselineRunSuccessResponse({
        status: 'success',
        durationMs: 100,
        // @ts-expect-error: deliberately invalid output value for test
        outputs: { 'r1::predicted_delay': { nested: 1 } },
      }),
    ).toThrow(/output/);
  });

  it('given_error_response_with_message_when_parseError_then_returns_message', () => {
    const parsed = parseBaselineRunErrorResponse({
      status: 'error',
      message: 'sklearn fork unavailable',
    });
    expect(parsed).toEqual({ message: 'sklearn fork unavailable' });
  });

  it('given_error_response_without_message_when_parseError_then_falls_back_to_generic', () => {
    const parsed = parseBaselineRunErrorResponse({
      status: 'error',
    } as unknown as Parameters<typeof parseBaselineRunErrorResponse>[0]);
    expect(parsed.message).toBe(BASELINE_RUN_FAILED_MESSAGE);
  });

  it('given_error_response_with_blank_message_when_parseError_then_falls_back_to_generic', () => {
    const parsed = parseBaselineRunErrorResponse({ status: 'error', message: '   ' });
    expect(parsed.message).toBe(BASELINE_RUN_FAILED_MESSAGE);
  });
});

describe('VTX-045 Output access', () => {
  it('given_row_and_param_when_buildPaneBaselineOutputKey_then_joins_with_double_colon', () => {
    expect(buildPaneBaselineOutputKey('row-1', 'predicted_delay')).toBe(
      'row-1::predicted_delay',
    );
  });

  it('given_blank_inputs_when_buildPaneBaselineOutputKey_then_throws', () => {
    expect(() => buildPaneBaselineOutputKey('', 'p')).toThrow(/rowRid/);
    expect(() => buildPaneBaselineOutputKey('r', '   ')).toThrow(/paramName/);
  });

  it('given_object_triple_when_buildObjectBaselineOutputKey_then_joins_three_segments', () => {
    expect(buildObjectBaselineOutputKey('Airport', 'JFK', 'capacity')).toBe(
      'Airport::JFK::capacity',
    );
  });

  it('given_blank_inputs_when_buildObjectBaselineOutputKey_then_throws', () => {
    expect(() => buildObjectBaselineOutputKey('', 'k', 'p')).toThrow(/objectType/);
    expect(() => buildObjectBaselineOutputKey('o', '', 'p')).toThrow(/primaryKey/);
    expect(() => buildObjectBaselineOutputKey('o', 'k', '   ')).toThrow(/property/);
  });

  it('given_outputs_populated_when_getBaselineOutput_then_returns_value', () => {
    const map = applyBaselineRunSuccess(
      createBaselineRunMap(),
      caseStudyRid,
      1000,
      { 'row-1::predicted_delay': 1500 },
    );
    expect(getBaselineOutput(map, caseStudyRid, 'row-1::predicted_delay')).toBe(1500);
  });

  it('given_missing_key_when_getBaselineOutput_then_returns_null', () => {
    const map = applyBaselineRunSuccess(
      createBaselineRunMap(),
      caseStudyRid,
      1000,
      { 'row-1::predicted_delay': 1500 },
    );
    expect(getBaselineOutput(map, caseStudyRid, 'row-X::missing')).toBeNull();
  });

  it('given_no_state_for_case_study_when_getBaselineOutput_then_returns_null', () => {
    expect(
      getBaselineOutput(createBaselineRunMap(), caseStudyRid, 'row-1::predicted_delay'),
    ).toBeNull();
  });

  it('given_prior_outputs_when_applyBaselineOutputs_then_merges_with_existing', () => {
    let map = applyBaselineRunSuccess(
      createBaselineRunMap(),
      caseStudyRid,
      1000,
      { 'row-1::p1': 100 },
    );
    map = applyBaselineOutputs(map, caseStudyRid, { 'row-2::p1': 200 });
    expect(map[caseStudyRid].outputs).toEqual({ 'row-1::p1': 100, 'row-2::p1': 200 });
  });

  it('given_applyBaselineOutputs_with_no_prior_state_when_invoked_then_creates_state', () => {
    const map = applyBaselineOutputs(createBaselineRunMap(), caseStudyRid, { k: 1 });
    expect(map[caseStudyRid].outputs).toEqual({ k: 1 });
    expect(map[caseStudyRid].status).toBe('idle');
  });
});

describe('VTX-045 Dispatch decision', () => {
  it('given_runBaseline_disabled_when_shouldDispatch_then_false', () => {
    expect(
      shouldDispatchBaseline({
        options: { runBaseline: false },
        currentStatus: 'idle',
      }),
    ).toBe(false);
  });

  it('given_runBaseline_enabled_and_idle_when_shouldDispatch_then_true', () => {
    expect(
      shouldDispatchBaseline({
        options: { runBaseline: true },
        currentStatus: 'idle',
      }),
    ).toBe(true);
  });

  it('given_runBaseline_enabled_and_running_when_shouldDispatch_then_false', () => {
    expect(
      shouldDispatchBaseline({
        options: { runBaseline: true },
        currentStatus: 'running',
      }),
    ).toBe(false);
  });

  it('given_runBaseline_enabled_and_success_when_shouldDispatch_then_false_by_default', () => {
    expect(
      shouldDispatchBaseline({
        options: { runBaseline: true },
        currentStatus: 'success',
      }),
    ).toBe(false);
  });

  it('given_runBaseline_enabled_and_error_when_shouldDispatch_then_true_for_retry', () => {
    expect(
      shouldDispatchBaseline({
        options: { runBaseline: true },
        currentStatus: 'error',
      }),
    ).toBe(true);
  });

  it('given_forceRerun_when_shouldDispatch_then_true_even_with_success_status', () => {
    expect(
      shouldDispatchBaseline({
        options: { runBaseline: true },
        currentStatus: 'success',
        forceRerun: true,
      }),
    ).toBe(true);
  });

  it('given_forceRerun_when_options_disabled_then_still_false', () => {
    expect(
      shouldDispatchBaseline({
        options: { runBaseline: false },
        currentStatus: 'idle',
        forceRerun: true,
      }),
    ).toBe(false);
  });
});

describe('VTX-045 Baseline vs simulated compare', () => {
  it('given_simulated_above_baseline_when_compute_then_positive_color_and_delta', () => {
    const r = computeBaselineCompare({ simulated: 1500, baseline: 1000 });
    expect(r.simulated).toBe(1500);
    expect(r.baseline).toBe(1000);
    expect(r.delta).toBe(500);
    expect(r.deltaPct).toBeCloseTo(50);
    expect(r.colorHint).toBe('positive');
  });

  it('given_simulated_below_baseline_when_compute_then_negative_color_and_delta', () => {
    const r = computeBaselineCompare({ simulated: 800, baseline: 1000 });
    expect(r.delta).toBe(-200);
    expect(r.deltaPct).toBeCloseTo(-20);
    expect(r.colorHint).toBe('negative');
  });

  it('given_simulated_equal_baseline_when_compute_then_neutral_color_and_zero_delta', () => {
    const r = computeBaselineCompare({ simulated: 1000, baseline: 1000 });
    expect(r.delta).toBe(0);
    expect(r.deltaPct).toBe(0);
    expect(r.colorHint).toBe('neutral');
  });

  it('given_baseline_zero_when_compute_then_deltaPct_null_to_avoid_div_zero', () => {
    const r = computeBaselineCompare({ simulated: 500, baseline: 0 });
    expect(r.delta).toBe(500);
    expect(r.deltaPct).toBeNull();
    expect(r.colorHint).toBe('positive');
  });

  it('given_non_finite_simulated_when_compute_then_throws', () => {
    expect(() => computeBaselineCompare({ simulated: Number.NaN, baseline: 1 })).toThrow(
      /finite/,
    );
    expect(() =>
      computeBaselineCompare({ simulated: Number.POSITIVE_INFINITY, baseline: 1 }),
    ).toThrow(/finite/);
  });

  it('given_non_finite_baseline_when_compute_then_throws', () => {
    expect(() => computeBaselineCompare({ simulated: 1, baseline: Number.NaN })).toThrow(
      /finite/,
    );
  });

  it('given_compare_result_when_getBaselineColorHint_then_returns_color', () => {
    const r = computeBaselineCompare({ simulated: 1500, baseline: 1000 });
    expect(getBaselineColorHint(r)).toBe('positive');
  });
});

describe('VTX-045 Format baseline compare label', () => {
  it('given_positive_delta_when_format_then_shows_baseline_paren_and_signed_pct', () => {
    const r = computeBaselineCompare({ simulated: 1500, baseline: 1000 });
    const f = formatBaselineCompareLabel(r);
    expect(f.simulated).toBe('1500');
    expect(f.baseline).toBe('baseline 1000');
    expect(f.delta).toBe('+50.0%');
    expect(f.colorHint).toBe('positive');
  });

  it('given_negative_delta_when_format_then_shows_negative_pct', () => {
    const r = computeBaselineCompare({ simulated: 800, baseline: 1000 });
    const f = formatBaselineCompareLabel(r);
    expect(f.delta).toBe('-20.0%');
    expect(f.colorHint).toBe('negative');
  });

  it('given_zero_delta_when_format_then_shows_zero_pct_neutral', () => {
    const r = computeBaselineCompare({ simulated: 1000, baseline: 1000 });
    const f = formatBaselineCompareLabel(r);
    expect(f.delta).toBe('0.0%');
    expect(f.colorHint).toBe('neutral');
  });

  it('given_baseline_zero_when_format_then_shows_absolute_signed_delta', () => {
    const r = computeBaselineCompare({ simulated: 500, baseline: 0 });
    const f = formatBaselineCompareLabel(r);
    // deltaPct is null — fall back to absolute delta
    expect(f.delta).toBe('+500');
    expect(f.colorHint).toBe('positive');
  });

  it('given_baseline_zero_and_simulated_zero_when_format_then_shows_zero_delta', () => {
    const r = computeBaselineCompare({ simulated: 0, baseline: 0 });
    const f = formatBaselineCompareLabel(r);
    expect(f.delta).toBe('0');
    expect(f.colorHint).toBe('neutral');
  });

  it('given_hideDelta_option_when_format_then_returns_empty_delta_string', () => {
    const r = computeBaselineCompare({ simulated: 1500, baseline: 1000 });
    const f = formatBaselineCompareLabel(r, { hideDelta: true });
    expect(f.delta).toBe('');
  });

  it('given_decimals_option_when_format_then_uses_specified_decimals', () => {
    const r = computeBaselineCompare({ simulated: 1500.42, baseline: 1000.17 });
    const f = formatBaselineCompareLabel(r, { decimals: 2 });
    expect(f.simulated).toBe('1500.42');
    expect(f.baseline).toBe('baseline 1000.17');
  });

  it('given_fractional_simulated_with_default_decimals_when_format_then_rounds_to_integer', () => {
    const r = computeBaselineCompare({ simulated: 1500.6, baseline: 1000 });
    const f = formatBaselineCompareLabel(r);
    expect(f.simulated).toBe('1501');
  });
});

describe('VTX-045 end-to-end baseline auto-run flow', () => {
  // 模拟 spec BDD #1/#2/#3：用户在 Scenario options 勾 Run baseline → 点
  // Run Scenario → 前端并行 dispatch baseline run → 后端跑无 override/无
  // action 副本 → SuccessResponse → applyBaselineRunSuccess → Pane Baseline
  // 列读 getBaselineOutput → Extended Label 用 computeBaselineCompare +
  // formatBaselineCompareLabel 显示 simulated/baseline/delta。
  it('given_options_enabled_when_dispatch_baseline_then_request_built_and_outputs_propagate', () => {
    let runMap = createBaselineRunMap();
    const opts = setRunBaseline(createScenarioBaselineOptions(), true);
    const status = getBaselineRunStatus(runMap, caseStudyRid);

    // 1. 决策：options 启用 + idle → dispatch
    expect(shouldDispatchBaseline({ options: opts, currentStatus: status })).toBe(true);

    // 2. 构造请求
    const req = buildBaselineRunRequest({ caseStudyRid });
    expect(req.method).toBe('POST');
    expect(req.path).toBe(
      `/api/vertex/v1/case-studies/${encodeURIComponent(caseStudyRid)}/baseline/run`,
    );

    // 3. UI 标记 running
    runMap = applyBaselineRunStart(runMap, caseStudyRid, 1700000000000);
    expect(getBaselineRunStatus(runMap, caseStudyRid)).toBe('running');

    // 4. 后端 200 OK + parse + applySuccess
    const success = parseBaselineRunSuccessResponse({
      status: 'success',
      durationMs: 1500,
      outputs: {
        'row-action-1::predicted_delay': 1000,
        'Airport::JFK::capacity': 200,
      },
    });
    runMap = applyBaselineRunSuccess(runMap, caseStudyRid, success.durationMs, success.outputs);
    expect(getBaselineRunStatus(runMap, caseStudyRid)).toBe('success');

    // 5. Pane Baseline 列：cell renderer 调 getBaselineOutput
    expect(
      getBaselineOutput(
        runMap,
        caseStudyRid,
        buildPaneBaselineOutputKey('row-action-1', 'predicted_delay'),
      ),
    ).toBe(1000);

    // 6. Extended Label：simulated 1500 vs baseline 1000 → +50.0% 绿
    const baselineCap = getBaselineOutput(
      runMap,
      caseStudyRid,
      buildObjectBaselineOutputKey('Airport', 'JFK', 'capacity'),
    );
    expect(baselineCap).toBe(200);
    const compare = computeBaselineCompare({ simulated: 300, baseline: baselineCap as number });
    const formatted = formatBaselineCompareLabel(compare);
    expect(formatted.simulated).toBe('300');
    expect(formatted.baseline).toBe('baseline 200');
    expect(formatted.delta).toBe('+50.0%');
    expect(formatted.colorHint).toBe('positive');

    // 7. 已 success → 后续 Run Scenario 不重复 dispatch baseline
    expect(
      shouldDispatchBaseline({
        options: opts,
        currentStatus: getBaselineRunStatus(runMap, caseStudyRid),
      }),
    ).toBe(false);
  });

  it('given_baseline_fails_when_apply_error_then_preserves_outputs_and_allows_retry', () => {
    let runMap = createBaselineRunMap();
    runMap = applyBaselineRunSuccess(runMap, caseStudyRid, 1000, { 'r1::p1': 99 });
    runMap = applyBaselineRunError(runMap, caseStudyRid, 'connection refused');
    expect(runMap[caseStudyRid].status).toBe('error');
    // Cached outputs survive so UI can still show stale baseline column value
    expect(getBaselineOutput(runMap, caseStudyRid, 'r1::p1')).toBe(99);
    // Error status → next Run can retry
    expect(
      shouldDispatchBaseline({
        options: { runBaseline: true },
        currentStatus: 'error',
      }),
    ).toBe(true);
  });
});
