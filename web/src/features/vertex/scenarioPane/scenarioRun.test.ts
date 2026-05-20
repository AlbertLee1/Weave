import { describe, expect, it } from 'vitest';

import type { ScenarioRef } from './scenarioPane';
import {
  RUN_FROZEN_TOOLTIP_PREFIX,
  ScenarioRunNotEditableError,
  applyScenarioRunError,
  applyScenarioRunStart,
  applyScenarioRunSuccess,
  assertScenarioRunnable,
  buildGetRunScenarioRequest,
  buildRunScenarioRequest,
  createScenarioRunMap,
  createScenarioRunState,
  getScenarioRunStatus,
  getScenarioRunStatusIcon,
  getScenarioRunStatusTooltip,
  hasRunnablePayload,
  immutableScenario,
  isScenarioRunning,
  parseScenarioRunAcceptedResponse,
  parseScenarioRunErrorResponse,
  resolveRunButtonState,
  setScenarioRunState,
  validateScenarioRunRequest,
} from './scenarioRun';

const scenarioRid = 'ri.vertex.main.scenario.s-1';
const otherRid = 'ri.vertex.main.scenario.s-2';
const runRid = 'ri.vertex.main.scenario-run.r-1';

function mutableScenario(rid = scenarioRid): ScenarioRef {
  return { rid, name: 'Scenario A', immutable: false };
}

function frozenScenario(rid = scenarioRid): ScenarioRef {
  return { rid, name: 'Scenario A (run)', immutable: true };
}

describe('VTX-043 Scenario run state machine', () => {
  it('given_no_runs_yet_when_createMap_then_returns_empty_map', () => {
    const map = createScenarioRunMap();
    expect(map).toEqual({});
  });

  it('given_no_state_for_scenario_when_getStatus_then_returns_idle', () => {
    const map = createScenarioRunMap();
    expect(getScenarioRunStatus(map, scenarioRid)).toBe('idle');
  });

  it('given_idle_when_createScenarioRunState_then_returns_idle_state', () => {
    const s = createScenarioRunState();
    expect(s.status).toBe('idle');
    expect(s.durationMs).toBeNull();
    expect(s.error).toBeNull();
    expect(s.startedAt).toBeNull();
  });

  it('given_idle_state_when_applyStart_then_marks_running_and_records_startedAt', () => {
    const map = createScenarioRunMap();
    const next = applyScenarioRunStart(map, scenarioRid, 1700000000000);
    expect(getScenarioRunStatus(next, scenarioRid)).toBe('running');
    expect(next[scenarioRid].startedAt).toBe(1700000000000);
    expect(next[scenarioRid].durationMs).toBeNull();
    expect(next[scenarioRid].error).toBeNull();
  });

  it('given_previously_failed_run_when_applyStart_then_clears_error', () => {
    let map = createScenarioRunMap();
    map = applyScenarioRunError(map, scenarioRid, 'boom');
    const next = applyScenarioRunStart(map, scenarioRid, 100);
    expect(getScenarioRunStatus(next, scenarioRid)).toBe('running');
    expect(next[scenarioRid].error).toBeNull();
  });

  it('given_running_state_when_applySuccess_then_records_duration_and_marks_success', () => {
    let map = createScenarioRunMap();
    map = applyScenarioRunStart(map, scenarioRid, 1000);
    const next = applyScenarioRunSuccess(map, scenarioRid, 4250);
    expect(getScenarioRunStatus(next, scenarioRid)).toBe('success');
    expect(next[scenarioRid].durationMs).toBe(4250);
    expect(next[scenarioRid].error).toBeNull();
  });

  it('given_running_state_when_applyError_then_records_message_and_marks_error', () => {
    let map = createScenarioRunMap();
    map = applyScenarioRunStart(map, scenarioRid, 1000);
    const next = applyScenarioRunError(map, scenarioRid, 'sklearn predict failed');
    expect(getScenarioRunStatus(next, scenarioRid)).toBe('error');
    expect(next[scenarioRid].error).toBe('sklearn predict failed');
    expect(next[scenarioRid].durationMs).toBeNull();
  });

  it('given_state_for_one_scenario_when_mutate_other_then_first_state_unchanged_by_reference', () => {
    let map = createScenarioRunMap();
    map = applyScenarioRunStart(map, scenarioRid, 100);
    const prior = map[scenarioRid];
    const next = applyScenarioRunStart(map, otherRid, 200);
    expect(next[scenarioRid]).toBe(prior);
    expect(next[otherRid].status).toBe('running');
  });

  it('given_map_when_setScenarioRunState_then_replaces_only_target_key', () => {
    let map = createScenarioRunMap();
    map = applyScenarioRunStart(map, scenarioRid, 100);
    map = applyScenarioRunStart(map, otherRid, 200);
    const next = setScenarioRunState(map, scenarioRid, {
      status: 'success',
      durationMs: 999,
      error: null,
      startedAt: 100,
    });
    expect(next[scenarioRid].status).toBe('success');
    expect(next[otherRid].status).toBe('running');
  });

  it('given_running_state_when_isScenarioRunning_then_true', () => {
    let map = createScenarioRunMap();
    map = applyScenarioRunStart(map, scenarioRid, 0);
    expect(isScenarioRunning(map, scenarioRid)).toBe(true);
    expect(isScenarioRunning(map, otherRid)).toBe(false);
  });
});

describe('VTX-043 Scenario run request builder', () => {
  it('given_scenario_rid_when_buildRequest_then_returns_POST_with_encoded_path', () => {
    const req = buildRunScenarioRequest({ scenarioRid });
    expect(req.method).toBe('POST');
    expect(req.path).toBe(
      `/api/vertex/v1/scenarios/${encodeURIComponent(scenarioRid)}/runs`,
    );
    expect(req.body).toEqual({});
  });

  it('given_scenarioRid_with_special_chars_when_buildRequest_then_path_is_url_encoded', () => {
    const ridWithSpecial = 'ri.vertex.main.scenario.s/with space';
    const req = buildRunScenarioRequest({ scenarioRid: ridWithSpecial });
    expect(req.path).toBe(
      `/api/vertex/v1/scenarios/${encodeURIComponent(ridWithSpecial)}/runs`,
    );
  });

  it('given_blank_scenario_rid_when_buildRequest_then_throws', () => {
    expect(() => buildRunScenarioRequest({ scenarioRid: '   ' })).toThrow(
      /scenarioRid/,
    );
  });

  it('given_empty_scenario_rid_when_buildRequest_then_throws', () => {
    expect(() => buildRunScenarioRequest({ scenarioRid: '' })).toThrow(
      /scenarioRid/,
    );
  });
});

describe('VTX-043 Scenario run preflight validation', () => {
  it('given_scenario_has_one_action_when_hasRunnablePayload_then_true', () => {
    expect(hasRunnablePayload({ actionCount: 1, overrideCount: 0 })).toBe(true);
  });

  it('given_scenario_has_one_override_when_hasRunnablePayload_then_true', () => {
    expect(hasRunnablePayload({ actionCount: 0, overrideCount: 1 })).toBe(true);
  });

  it('given_scenario_has_neither_when_hasRunnablePayload_then_false', () => {
    expect(hasRunnablePayload({ actionCount: 0, overrideCount: 0 })).toBe(false);
  });

  it('given_mutable_scenario_with_actions_when_validate_then_valid', () => {
    const result = validateScenarioRunRequest({
      scenario: mutableScenario(),
      actionCount: 1,
      overrideCount: 0,
    });
    expect(result).toEqual({ valid: true });
  });

  it('given_immutable_scenario_when_validate_then_invalid_frozen', () => {
    const result = validateScenarioRunRequest({
      scenario: frozenScenario(),
      actionCount: 1,
      overrideCount: 0,
    });
    expect(result).toEqual({ valid: false, reason: 'frozen' });
  });

  it('given_empty_scenario_when_validate_then_invalid_empty_payload', () => {
    const result = validateScenarioRunRequest({
      scenario: mutableScenario(),
      actionCount: 0,
      overrideCount: 0,
    });
    expect(result).toEqual({ valid: false, reason: 'empty_payload' });
  });

  it('given_mutable_scenario_when_assertRunnable_then_no_throw', () => {
    expect(() =>
      assertScenarioRunnable({
        scenario: mutableScenario(),
        actionCount: 1,
        overrideCount: 0,
      }),
    ).not.toThrow();
  });

  it('given_immutable_scenario_when_assertRunnable_then_throws_ScenarioRunNotEditableError', () => {
    let caught: unknown = null;
    try {
      assertScenarioRunnable({
        scenario: frozenScenario(),
        actionCount: 1,
        overrideCount: 0,
      });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(ScenarioRunNotEditableError);
    expect((caught as ScenarioRunNotEditableError).scenarioRid).toBe(scenarioRid);
    expect((caught as ScenarioRunNotEditableError).reason).toBe('frozen');
    expect((caught as ScenarioRunNotEditableError).message).toContain(
      RUN_FROZEN_TOOLTIP_PREFIX,
    );
  });

  it('given_empty_scenario_when_assertRunnable_then_throws_with_reason_empty_payload', () => {
    let caught: unknown = null;
    try {
      assertScenarioRunnable({
        scenario: mutableScenario(),
        actionCount: 0,
        overrideCount: 0,
      });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(ScenarioRunNotEditableError);
    expect((caught as ScenarioRunNotEditableError).reason).toBe('empty_payload');
  });
});

describe('VTX-043 Run button derived state', () => {
  it('given_runnable_scenario_when_resolveRunButtonState_then_enabled', () => {
    const result = resolveRunButtonState({
      scenario: mutableScenario(),
      runStatus: 'idle',
      actionCount: 2,
      overrideCount: 0,
    });
    expect(result).toEqual({ enabled: true });
  });

  it('given_running_state_when_resolveRunButtonState_then_disabled_with_reason_running', () => {
    const result = resolveRunButtonState({
      scenario: mutableScenario(),
      runStatus: 'running',
      actionCount: 1,
      overrideCount: 0,
    });
    expect(result).toEqual({ enabled: false, reason: 'running' });
  });

  it('given_frozen_scenario_when_resolveRunButtonState_then_disabled_with_reason_frozen', () => {
    const result = resolveRunButtonState({
      scenario: frozenScenario(),
      runStatus: 'idle',
      actionCount: 1,
      overrideCount: 0,
    });
    expect(result).toEqual({ enabled: false, reason: 'frozen' });
  });

  it('given_empty_scenario_when_resolveRunButtonState_then_disabled_with_reason_empty_payload', () => {
    const result = resolveRunButtonState({
      scenario: mutableScenario(),
      runStatus: 'idle',
      actionCount: 0,
      overrideCount: 0,
    });
    expect(result).toEqual({ enabled: false, reason: 'empty_payload' });
  });

  it('given_success_status_when_resolveRunButtonState_then_disabled_with_reason_frozen_after_run', () => {
    // 一旦 Run 成功，scenario.immutable=true，下次再点 Run 应被 frozen 拦截。
    // 此 case 模拟 React 层把 immutable 翻为 true 后的 Run 按钮状态。
    const result = resolveRunButtonState({
      scenario: frozenScenario(),
      runStatus: 'success',
      actionCount: 1,
      overrideCount: 0,
    });
    expect(result).toEqual({ enabled: false, reason: 'frozen' });
  });

  it('given_error_status_after_failure_when_scenario_still_mutable_then_button_re_enabled', () => {
    // function 抛错不会冻结 scenario —— 用户改完 Override 后应可重试。
    const result = resolveRunButtonState({
      scenario: mutableScenario(),
      runStatus: 'error',
      actionCount: 1,
      overrideCount: 0,
    });
    expect(result).toEqual({ enabled: true });
  });
});

describe('VTX-043 Run status icon / tooltip', () => {
  it('given_idle_when_getStatusIcon_then_dash', () => {
    expect(getScenarioRunStatusIcon('idle')).toBe('—');
  });

  it('given_running_when_getStatusIcon_then_spinner', () => {
    expect(getScenarioRunStatusIcon('running')).toBe('spinner');
  });

  it('given_success_when_getStatusIcon_then_green_check', () => {
    expect(getScenarioRunStatusIcon('success')).toBe('✓');
  });

  it('given_error_when_getStatusIcon_then_red_cross', () => {
    expect(getScenarioRunStatusIcon('error')).toBe('×');
  });

  it('given_success_state_with_duration_when_getTooltip_then_shows_duration', () => {
    const map = setScenarioRunState(createScenarioRunMap(), scenarioRid, {
      status: 'success',
      durationMs: 2345,
      error: null,
      startedAt: 100,
    });
    expect(getScenarioRunStatusTooltip(map, scenarioRid)).toBe('Run completed in 2.3 s');
  });

  it('given_error_state_with_message_when_getTooltip_then_shows_message', () => {
    const map = setScenarioRunState(createScenarioRunMap(), scenarioRid, {
      status: 'error',
      durationMs: null,
      error: 'TypeError: predict() missing arg',
      startedAt: 100,
    });
    expect(getScenarioRunStatusTooltip(map, scenarioRid)).toBe(
      'TypeError: predict() missing arg',
    );
  });

  it('given_running_state_when_getTooltip_then_shows_running_label', () => {
    const map = setScenarioRunState(createScenarioRunMap(), scenarioRid, {
      status: 'running',
      durationMs: null,
      error: null,
      startedAt: 100,
    });
    expect(getScenarioRunStatusTooltip(map, scenarioRid)).toBe('Running…');
  });

  it('given_idle_when_getTooltip_then_returns_null', () => {
    const map = createScenarioRunMap();
    expect(getScenarioRunStatusTooltip(map, scenarioRid)).toBeNull();
  });

  it('given_sub_second_duration_when_getTooltip_then_shows_ms', () => {
    const map = setScenarioRunState(createScenarioRunMap(), scenarioRid, {
      status: 'success',
      durationMs: 450,
      error: null,
      startedAt: 100,
    });
    expect(getScenarioRunStatusTooltip(map, scenarioRid)).toBe('Run completed in 450 ms');
  });

  it('given_success_state_without_duration_when_getTooltip_then_returns_completed_no_duration', () => {
    const map = setScenarioRunState(createScenarioRunMap(), scenarioRid, {
      status: 'success',
      durationMs: null,
      error: null,
      startedAt: 100,
    });
    expect(getScenarioRunStatusTooltip(map, scenarioRid)).toBe('Run completed');
  });
});

describe('VTX-043 Run response parsing', () => {
  it('given_accepted_start_response_when_parse_then_returns_runRid_and_status', () => {
    const parsed = parseScenarioRunAcceptedResponse({
      status: 'pending',
      runRid,
    });
    expect(parsed).toEqual({
      status: 'pending',
      runRid,
    });
  });

  it('given_running_start_response_when_parse_then_returns_running_status', () => {
    const parsed = parseScenarioRunAcceptedResponse({
      status: 'running',
      runRid,
    });
    expect(parsed.status).toBe('running');
  });

  it('given_accepted_response_missing_runRid_when_parse_then_throws', () => {
    expect(() =>
      parseScenarioRunAcceptedResponse({
        status: 'pending',
      } as unknown as Parameters<typeof parseScenarioRunAcceptedResponse>[0]),
    ).toThrow(/runRid/);
  });

  it('given_accepted_response_with_blank_runRid_when_parse_then_throws', () => {
    expect(() =>
      parseScenarioRunAcceptedResponse({
        status: 'pending',
        runRid: '   ',
      }),
    ).toThrow(/runRid/);
  });

  it('given_accepted_response_with_terminal_status_when_parse_then_throws', () => {
    expect(() =>
      parseScenarioRunAcceptedResponse({
        status: 'success',
        runRid,
      } as unknown as Parameters<typeof parseScenarioRunAcceptedResponse>[0]),
    ).toThrow(/status/);
  });

  it('given_error_response_with_message_when_parseError_then_returns_message', () => {
    const parsed = parseScenarioRunErrorResponse({
      status: 'error',
      message: 'TypeError: predict() missing 1 required positional argument',
    });
    expect(parsed).toEqual({
      message: 'TypeError: predict() missing 1 required positional argument',
    });
  });

  it('given_error_response_without_message_when_parseError_then_falls_back_to_generic', () => {
    const parsed = parseScenarioRunErrorResponse({
      status: 'error',
    } as unknown as Parameters<typeof parseScenarioRunErrorResponse>[0]);
    expect(parsed.message).toBe('Scenario run failed');
  });

  it('given_error_response_with_blank_message_when_parseError_then_falls_back_to_generic', () => {
    const parsed = parseScenarioRunErrorResponse({
      status: 'error',
      message: '   ',
    });
    expect(parsed.message).toBe('Scenario run failed');
  });
});

describe('VTX-043 immutable transition helper', () => {
  it('given_mutable_scenario_when_immutableScenario_then_returns_frozen_copy', () => {
    const frozen = immutableScenario({ rid: scenarioRid, name: 'A', immutable: false });
    expect(frozen).toEqual({ rid: scenarioRid, name: 'A', immutable: true });
  });

  it('given_already_immutable_scenario_when_immutableScenario_then_returns_equal_copy', () => {
    const frozen = immutableScenario({ rid: scenarioRid, name: 'A', immutable: true });
    expect(frozen.immutable).toBe(true);
  });

  it('given_scenario_without_immutable_field_when_immutableScenario_then_sets_true', () => {
    const frozen = immutableScenario({ rid: scenarioRid, name: 'A' });
    expect(frozen.immutable).toBe(true);
  });
});

describe('VTX-043 end-to-end sync run flow', () => {
  // 模拟已挂载合同：用户点 Run → POST /runs 返回 202 {runRid,status} →
  // GET /runs/{runRid} polling 返回终态；以及 error 路径。
  it('given_scenario_with_action_and_override_when_run_accepted_then_poll_route_is_used_for_terminal_state', () => {
    let scenario: ScenarioRef = mutableScenario();
    let runMap = createScenarioRunMap();
    const t0 = 1700000000000;

    // 1. 用户点 Run：preflight 通过
    assertScenarioRunnable({ scenario, actionCount: 1, overrideCount: 1 });

    // 2. 构造请求
    const req = buildRunScenarioRequest({ scenarioRid: scenario.rid });
    expect(req.method).toBe('POST');
    expect(req.path).toBe(
      `/api/vertex/v1/scenarios/${encodeURIComponent(scenario.rid)}/runs`,
    );

    // 3. UI 标记 running
    runMap = applyScenarioRunStart(runMap, scenario.rid, t0);
    expect(getScenarioRunStatus(runMap, scenario.rid)).toBe('running');

    // 4. 后端返回 202 Accepted + parse
    const accepted = parseScenarioRunAcceptedResponse({
      status: 'pending',
      runRid,
    });
    expect(accepted).toEqual({ status: 'pending', runRid });

    // 5. 终态来自 mounted GET /runs/{runRid} polling，而不是 POST 响应。
    const getReq = buildGetRunScenarioRequest({
      scenarioRid: scenario.rid,
      runRid: accepted.runRid,
    });
    expect(getReq).toEqual({
      method: 'GET',
      path: `/api/vertex/v1/scenarios/${encodeURIComponent(scenario.rid)}/runs/${encodeURIComponent(runRid)}`,
    });

    // 6. poll helper 返回 terminal record 后，UI helper 才可标 success + freeze.
    runMap = applyScenarioRunSuccess(runMap, scenario.rid, 1850);
    scenario = immutableScenario(scenario);
    expect(getScenarioRunStatus(runMap, scenario.rid)).toBe('success');
    expect(scenario.immutable).toBe(true);

    expect(getScenarioRunStatusTooltip(runMap, scenario.rid)).toBe(
      'Run completed in 1.9 s',
    );
  });

  it('given_function_throws_when_run_then_error_state_with_message_and_scenario_remains_mutable', () => {
    const scenario: ScenarioRef = mutableScenario();
    let runMap = createScenarioRunMap();

    assertScenarioRunnable({ scenario, actionCount: 1, overrideCount: 0 });
    runMap = applyScenarioRunStart(runMap, scenario.rid, 100);

    const error = parseScenarioRunErrorResponse({
      status: 'error',
      message: 'sklearn.predict() raised KeyError: capacity',
    });
    runMap = applyScenarioRunError(runMap, scenario.rid, error.message);

    expect(getScenarioRunStatus(runMap, scenario.rid)).toBe('error');
    expect(scenario.immutable).toBeFalsy();
    expect(getScenarioRunStatusTooltip(runMap, scenario.rid)).toBe(
      'sklearn.predict() raised KeyError: capacity',
    );
    // user 改完 override 还能重试
    expect(
      resolveRunButtonState({
        scenario,
        runStatus: 'error',
        actionCount: 1,
        overrideCount: 0,
      }),
    ).toEqual({ enabled: true });
  });
});
