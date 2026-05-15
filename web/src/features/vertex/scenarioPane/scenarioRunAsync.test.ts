import { describe, expect, it } from 'vitest';

import type { ScenarioRef } from './scenarioPane';
import {
  applyAcceptedScenarioRunJob,
  applyScenarioRunSseEvent,
  buildAsyncRunScenarioRequest,
  buildCancelScenarioRunRequest,
  buildScenarioRunStreamUrl,
  createScenarioRunJobMap,
  createScenarioRunJobState,
  getModelProgressPct,
  getOverallProgressPct,
  getScenarioRunJobStatus,
  getScenarioRunJobStatusIcon,
  getScenarioRunJobStatusTooltip,
  isScenarioRunJobActive,
  parseAcceptedScenarioRunResponse,
  parseScenarioRunSseEvent,
  setScenarioRunJobState,
  shouldRunAsync,
} from './scenarioRunAsync';
import type {
  ScenarioRunJobState,
  ScenarioRunSseEvent,
} from './scenarioRunAsync';

const scenarioRid = 'ri.vertex.main.scenario.s-1';
const otherRid = 'ri.vertex.main.scenario.s-2';
const jobId = 'job-abc-123';

function mutableScenario(rid = scenarioRid): ScenarioRef {
  return { rid, name: 'Scenario A', immutable: false };
}

describe('VTX-044 shouldRunAsync', () => {
  it('given_two_models_when_decide_then_true', () => {
    expect(shouldRunAsync({ modelCount: 2, forceAsync: false })).toBe(true);
  });

  it('given_three_models_when_decide_then_true', () => {
    expect(shouldRunAsync({ modelCount: 3, forceAsync: false })).toBe(true);
  });

  it('given_one_model_and_not_forced_when_decide_then_false', () => {
    expect(shouldRunAsync({ modelCount: 1, forceAsync: false })).toBe(false);
  });

  it('given_zero_models_when_decide_then_false', () => {
    expect(shouldRunAsync({ modelCount: 0, forceAsync: false })).toBe(false);
  });

  it('given_forceAsync_true_with_one_model_when_decide_then_true', () => {
    expect(shouldRunAsync({ modelCount: 1, forceAsync: true })).toBe(true);
  });

  it('given_forceAsync_true_with_zero_models_when_decide_then_true', () => {
    expect(shouldRunAsync({ modelCount: 0, forceAsync: true })).toBe(true);
  });

  it('given_forceAsync_omitted_when_decide_then_treated_as_false', () => {
    expect(shouldRunAsync({ modelCount: 1 })).toBe(false);
  });
});

describe('VTX-044 async run request builder', () => {
  it('given_scenarioRid_when_buildRequest_then_returns_POST_with_encoded_path', () => {
    const req = buildAsyncRunScenarioRequest({ scenarioRid });
    expect(req.method).toBe('POST');
    expect(req.path).toBe(
      `/api/vertex/v1/scenarios/${encodeURIComponent(scenarioRid)}/run`,
    );
    expect(req.body).toEqual({});
  });

  it('given_forceAsync_true_when_buildRequest_then_body_contains_flag', () => {
    const req = buildAsyncRunScenarioRequest({ scenarioRid, forceAsync: true });
    expect(req.body).toEqual({ forceAsync: true });
  });

  it('given_forceAsync_false_when_buildRequest_then_body_omits_flag', () => {
    const req = buildAsyncRunScenarioRequest({ scenarioRid, forceAsync: false });
    expect(req.body).toEqual({});
  });

  it('given_scenarioRid_with_special_chars_when_buildRequest_then_path_encoded', () => {
    const ridWithSpecial = 'ri.vertex.main.scenario.s/with space';
    const req = buildAsyncRunScenarioRequest({ scenarioRid: ridWithSpecial });
    expect(req.path).toBe(
      `/api/vertex/v1/scenarios/${encodeURIComponent(ridWithSpecial)}/run`,
    );
  });

  it('given_blank_scenarioRid_when_buildRequest_then_throws', () => {
    expect(() => buildAsyncRunScenarioRequest({ scenarioRid: '   ' })).toThrow(
      /scenarioRid/,
    );
  });
});

describe('VTX-044 stream URL builder', () => {
  it('given_scenarioRid_and_jobId_when_buildStreamUrl_then_returns_sse_path', () => {
    const url = buildScenarioRunStreamUrl({ scenarioRid, jobId });
    expect(url).toBe(
      `/api/vertex/v1/scenarios/${encodeURIComponent(scenarioRid)}/run/${encodeURIComponent(jobId)}/stream`,
    );
  });

  it('given_jobId_with_slash_when_buildStreamUrl_then_path_encodes_jobId', () => {
    const slashJob = 'job/with/slashes';
    const url = buildScenarioRunStreamUrl({ scenarioRid, jobId: slashJob });
    expect(url).toContain(encodeURIComponent(slashJob));
  });

  it('given_blank_jobId_when_buildStreamUrl_then_throws', () => {
    expect(() =>
      buildScenarioRunStreamUrl({ scenarioRid, jobId: '   ' }),
    ).toThrow(/jobId/);
  });

  it('given_blank_scenarioRid_when_buildStreamUrl_then_throws', () => {
    expect(() =>
      buildScenarioRunStreamUrl({ scenarioRid: '', jobId }),
    ).toThrow(/scenarioRid/);
  });
});

describe('VTX-044 cancel request builder', () => {
  it('given_scenarioRid_and_jobId_when_buildCancel_then_returns_POST_cancel_path', () => {
    const req = buildCancelScenarioRunRequest({ scenarioRid, jobId });
    expect(req.method).toBe('POST');
    expect(req.path).toBe(
      `/api/vertex/v1/scenarios/${encodeURIComponent(scenarioRid)}/run/${encodeURIComponent(jobId)}/cancel`,
    );
    expect(req.body).toEqual({});
  });

  it('given_blank_jobId_when_buildCancel_then_throws', () => {
    expect(() =>
      buildCancelScenarioRunRequest({ scenarioRid, jobId: '' }),
    ).toThrow(/jobId/);
  });

  it('given_blank_scenarioRid_when_buildCancel_then_throws', () => {
    expect(() =>
      buildCancelScenarioRunRequest({ scenarioRid: '   ', jobId }),
    ).toThrow(/scenarioRid/);
  });

  it('given_jobId_with_special_chars_when_buildCancel_then_path_encoded', () => {
    const specialJob = 'job with space';
    const req = buildCancelScenarioRunRequest({ scenarioRid, jobId: specialJob });
    expect(req.path).toContain(encodeURIComponent(specialJob));
  });
});

describe('VTX-044 accepted response parser', () => {
  it('given_202_response_with_jobId_when_parse_then_returns_jobId', () => {
    const parsed = parseAcceptedScenarioRunResponse({
      status: 'accepted',
      jobId,
    });
    expect(parsed).toEqual({ jobId });
  });

  it('given_response_missing_jobId_when_parse_then_throws', () => {
    expect(() =>
      parseAcceptedScenarioRunResponse({
        status: 'accepted',
      } as unknown as Parameters<typeof parseAcceptedScenarioRunResponse>[0]),
    ).toThrow(/jobId/);
  });

  it('given_response_with_blank_jobId_when_parse_then_throws', () => {
    expect(() =>
      parseAcceptedScenarioRunResponse({
        status: 'accepted',
        jobId: '   ',
      }),
    ).toThrow(/jobId/);
  });

  it('given_response_with_non_string_jobId_when_parse_then_throws', () => {
    expect(() =>
      parseAcceptedScenarioRunResponse({
        status: 'accepted',
        jobId: 123 as unknown as string,
      }),
    ).toThrow(/jobId/);
  });
});

describe('VTX-044 SSE event parser', () => {
  it('given_progress_payload_when_parse_then_returns_progress_event', () => {
    const ev = parseScenarioRunSseEvent(
      JSON.stringify({ type: 'progress', model: 'M1', pct: 42 }),
    );
    expect(ev).toEqual({ type: 'progress', model: 'M1', pct: 42 });
  });

  it('given_progress_with_pct_above_100_when_parse_then_clamps_to_100', () => {
    const ev = parseScenarioRunSseEvent(
      JSON.stringify({ type: 'progress', model: 'M1', pct: 200 }),
    );
    expect(ev).toEqual({ type: 'progress', model: 'M1', pct: 100 });
  });

  it('given_progress_with_negative_pct_when_parse_then_clamps_to_zero', () => {
    const ev = parseScenarioRunSseEvent(
      JSON.stringify({ type: 'progress', model: 'M1', pct: -5 }),
    );
    expect(ev).toEqual({ type: 'progress', model: 'M1', pct: 0 });
  });

  it('given_progress_missing_model_when_parse_then_throws', () => {
    expect(() =>
      parseScenarioRunSseEvent(
        JSON.stringify({ type: 'progress', pct: 50 }),
      ),
    ).toThrow(/model/);
  });

  it('given_progress_with_non_finite_pct_when_parse_then_throws', () => {
    expect(() =>
      parseScenarioRunSseEvent(
        JSON.stringify({ type: 'progress', model: 'M1', pct: 'half' }),
      ),
    ).toThrow(/pct/);
  });

  it('given_result_payload_with_object_output_when_parse_then_returns_result_event', () => {
    const ev = parseScenarioRunSseEvent(
      JSON.stringify({ type: 'result', model: 'M1', output: { score: 0.83 } }),
    );
    expect(ev).toEqual({ type: 'result', model: 'M1', output: { score: 0.83 } });
  });

  it('given_result_with_null_output_when_parse_then_keeps_null', () => {
    const ev = parseScenarioRunSseEvent(
      JSON.stringify({ type: 'result', model: 'M1', output: null }),
    );
    expect(ev).toEqual({ type: 'result', model: 'M1', output: null });
  });

  it('given_result_missing_model_when_parse_then_throws', () => {
    expect(() =>
      parseScenarioRunSseEvent(
        JSON.stringify({ type: 'result', output: 1 }),
      ),
    ).toThrow(/model/);
  });

  it('given_done_payload_when_parse_then_returns_done_event', () => {
    const ev = parseScenarioRunSseEvent(
      JSON.stringify({ type: 'done', durationMs: 4250 }),
    );
    expect(ev).toEqual({ type: 'done', durationMs: 4250 });
  });

  it('given_done_with_non_finite_duration_when_parse_then_throws', () => {
    expect(() =>
      parseScenarioRunSseEvent(JSON.stringify({ type: 'done', durationMs: 'fast' })),
    ).toThrow(/durationMs/);
  });

  it('given_done_with_negative_duration_when_parse_then_throws', () => {
    expect(() =>
      parseScenarioRunSseEvent(JSON.stringify({ type: 'done', durationMs: -10 })),
    ).toThrow(/durationMs/);
  });

  it('given_error_payload_with_model_when_parse_then_returns_error_event', () => {
    const ev = parseScenarioRunSseEvent(
      JSON.stringify({ type: 'error', model: 'M2', message: 'predict failed' }),
    );
    expect(ev).toEqual({ type: 'error', model: 'M2', message: 'predict failed' });
  });

  it('given_error_payload_without_model_when_parse_then_omits_model', () => {
    const ev = parseScenarioRunSseEvent(
      JSON.stringify({ type: 'error', message: 'workflow timeout' }),
    );
    expect(ev).toEqual({ type: 'error', message: 'workflow timeout' });
  });

  it('given_error_payload_with_blank_message_when_parse_then_falls_back_to_generic', () => {
    const ev = parseScenarioRunSseEvent(
      JSON.stringify({ type: 'error', message: '   ' }),
    );
    expect(ev).toEqual({ type: 'error', message: 'Scenario run failed' });
  });

  it('given_error_payload_missing_message_when_parse_then_falls_back_to_generic', () => {
    const ev = parseScenarioRunSseEvent(
      JSON.stringify({ type: 'error' }),
    );
    expect(ev).toEqual({ type: 'error', message: 'Scenario run failed' });
  });

  it('given_cancelled_payload_when_parse_then_returns_cancelled_event', () => {
    const ev = parseScenarioRunSseEvent(JSON.stringify({ type: 'cancelled' }));
    expect(ev).toEqual({ type: 'cancelled' });
  });

  it('given_unknown_event_type_when_parse_then_throws', () => {
    expect(() =>
      parseScenarioRunSseEvent(JSON.stringify({ type: 'mystery' })),
    ).toThrow(/type/);
  });

  it('given_malformed_json_when_parse_then_throws', () => {
    expect(() => parseScenarioRunSseEvent('not-json')).toThrow();
  });

  it('given_empty_string_when_parse_then_throws', () => {
    expect(() => parseScenarioRunSseEvent('')).toThrow();
  });
});

describe('VTX-044 Scenario run job state machine', () => {
  it('given_no_jobs_when_createMap_then_empty', () => {
    expect(createScenarioRunJobMap()).toEqual({});
  });

  it('given_unknown_rid_when_getStatus_then_idle', () => {
    expect(getScenarioRunJobStatus(createScenarioRunJobMap(), scenarioRid)).toBe(
      'idle',
    );
  });

  it('given_jobId_when_createJobState_then_pending_with_jobId', () => {
    const s = createScenarioRunJobState({ jobId, startedAt: 1700000000000 });
    expect(s.jobId).toBe(jobId);
    expect(s.status).toBe('pending');
    expect(s.startedAt).toBe(1700000000000);
    expect(s.durationMs).toBeNull();
    expect(s.error).toBeNull();
    expect(s.progressByModel).toEqual({});
    expect(s.resultsByModel).toEqual({});
  });

  it('given_202_response_when_applyAccepted_then_marks_pending', () => {
    const map = applyAcceptedScenarioRunJob(
      createScenarioRunJobMap(),
      scenarioRid,
      jobId,
      1700000000000,
    );
    expect(getScenarioRunJobStatus(map, scenarioRid)).toBe('pending');
    expect(map[scenarioRid].jobId).toBe(jobId);
  });

  it('given_prior_error_when_applyAccepted_then_clears_error_and_duration', () => {
    let map = setScenarioRunJobState(
      createScenarioRunJobMap(),
      scenarioRid,
      {
        jobId: 'prev-job',
        status: 'error',
        startedAt: 1,
        durationMs: null,
        error: 'boom',
        progressByModel: { M1: 50 },
        resultsByModel: {},
      },
    );
    map = applyAcceptedScenarioRunJob(map, scenarioRid, jobId, 100);
    expect(map[scenarioRid].error).toBeNull();
    expect(map[scenarioRid].progressByModel).toEqual({});
    expect(map[scenarioRid].resultsByModel).toEqual({});
    expect(map[scenarioRid].jobId).toBe(jobId);
  });

  it('given_pending_when_progress_event_then_running_and_pct_recorded', () => {
    let map = applyAcceptedScenarioRunJob(
      createScenarioRunJobMap(),
      scenarioRid,
      jobId,
      0,
    );
    map = applyScenarioRunSseEvent(map, scenarioRid, {
      type: 'progress',
      model: 'M1',
      pct: 30,
    });
    expect(getScenarioRunJobStatus(map, scenarioRid)).toBe('running');
    expect(map[scenarioRid].progressByModel).toEqual({ M1: 30 });
  });

  it('given_two_progress_events_when_apply_then_each_model_tracked', () => {
    let map = applyAcceptedScenarioRunJob(
      createScenarioRunJobMap(),
      scenarioRid,
      jobId,
      0,
    );
    map = applyScenarioRunSseEvent(map, scenarioRid, {
      type: 'progress',
      model: 'M1',
      pct: 50,
    });
    map = applyScenarioRunSseEvent(map, scenarioRid, {
      type: 'progress',
      model: 'M2',
      pct: 25,
    });
    expect(map[scenarioRid].progressByModel).toEqual({ M1: 50, M2: 25 });
  });

  it('given_result_event_when_apply_then_output_recorded_and_status_running', () => {
    let map = applyAcceptedScenarioRunJob(
      createScenarioRunJobMap(),
      scenarioRid,
      jobId,
      0,
    );
    map = applyScenarioRunSseEvent(map, scenarioRid, {
      type: 'result',
      model: 'M1',
      output: { score: 0.9 },
    });
    expect(getScenarioRunJobStatus(map, scenarioRid)).toBe('running');
    expect(map[scenarioRid].resultsByModel).toEqual({ M1: { score: 0.9 } });
  });

  it('given_running_when_done_event_then_success_and_duration_recorded', () => {
    let map = applyAcceptedScenarioRunJob(
      createScenarioRunJobMap(),
      scenarioRid,
      jobId,
      0,
    );
    map = applyScenarioRunSseEvent(map, scenarioRid, {
      type: 'done',
      durationMs: 4250,
    });
    expect(getScenarioRunJobStatus(map, scenarioRid)).toBe('success');
    expect(map[scenarioRid].durationMs).toBe(4250);
    expect(map[scenarioRid].error).toBeNull();
  });

  it('given_running_when_error_event_with_model_then_error_status_and_prefixed_message', () => {
    let map = applyAcceptedScenarioRunJob(
      createScenarioRunJobMap(),
      scenarioRid,
      jobId,
      0,
    );
    map = applyScenarioRunSseEvent(map, scenarioRid, {
      type: 'error',
      model: 'M2',
      message: 'predict failed',
    });
    expect(getScenarioRunJobStatus(map, scenarioRid)).toBe('error');
    expect(map[scenarioRid].error).toBe('M2: predict failed');
  });

  it('given_error_event_without_model_when_apply_then_message_unprefixed', () => {
    let map = applyAcceptedScenarioRunJob(
      createScenarioRunJobMap(),
      scenarioRid,
      jobId,
      0,
    );
    map = applyScenarioRunSseEvent(map, scenarioRid, {
      type: 'error',
      message: 'workflow timeout',
    });
    expect(map[scenarioRid].error).toBe('workflow timeout');
  });

  it('given_running_when_cancelled_event_then_cancelled_status', () => {
    let map = applyAcceptedScenarioRunJob(
      createScenarioRunJobMap(),
      scenarioRid,
      jobId,
      0,
    );
    map = applyScenarioRunSseEvent(map, scenarioRid, {
      type: 'progress',
      model: 'M1',
      pct: 60,
    });
    map = applyScenarioRunSseEvent(map, scenarioRid, { type: 'cancelled' });
    expect(getScenarioRunJobStatus(map, scenarioRid)).toBe('cancelled');
  });

  it('given_no_state_for_rid_when_apply_event_then_noop_same_map_reference', () => {
    const map = createScenarioRunJobMap();
    const next = applyScenarioRunSseEvent(map, scenarioRid, {
      type: 'progress',
      model: 'M1',
      pct: 10,
    });
    expect(next).toBe(map);
  });

  it('given_pending_for_one_scenario_when_apply_event_for_other_then_first_unchanged_by_reference', () => {
    let map = applyAcceptedScenarioRunJob(
      createScenarioRunJobMap(),
      scenarioRid,
      jobId,
      0,
    );
    map = applyAcceptedScenarioRunJob(map, otherRid, 'job-2', 0);
    const priorOther = map[otherRid];
    map = applyScenarioRunSseEvent(map, scenarioRid, {
      type: 'progress',
      model: 'M1',
      pct: 80,
    });
    expect(map[otherRid]).toBe(priorOther);
  });

  it('given_pending_when_isActive_then_true', () => {
    const map = applyAcceptedScenarioRunJob(
      createScenarioRunJobMap(),
      scenarioRid,
      jobId,
      0,
    );
    expect(isScenarioRunJobActive(map, scenarioRid)).toBe(true);
  });

  it('given_running_when_isActive_then_true', () => {
    let map = applyAcceptedScenarioRunJob(
      createScenarioRunJobMap(),
      scenarioRid,
      jobId,
      0,
    );
    map = applyScenarioRunSseEvent(map, scenarioRid, {
      type: 'progress',
      model: 'M1',
      pct: 10,
    });
    expect(isScenarioRunJobActive(map, scenarioRid)).toBe(true);
  });

  it('given_success_when_isActive_then_false', () => {
    let map = applyAcceptedScenarioRunJob(
      createScenarioRunJobMap(),
      scenarioRid,
      jobId,
      0,
    );
    map = applyScenarioRunSseEvent(map, scenarioRid, {
      type: 'done',
      durationMs: 100,
    });
    expect(isScenarioRunJobActive(map, scenarioRid)).toBe(false);
  });

  it('given_cancelled_when_isActive_then_false', () => {
    let map = applyAcceptedScenarioRunJob(
      createScenarioRunJobMap(),
      scenarioRid,
      jobId,
      0,
    );
    map = applyScenarioRunSseEvent(map, scenarioRid, { type: 'cancelled' });
    expect(isScenarioRunJobActive(map, scenarioRid)).toBe(false);
  });

  it('given_done_event_for_unknown_rid_when_apply_then_no_state_created', () => {
    const map = applyScenarioRunSseEvent(
      createScenarioRunJobMap(),
      scenarioRid,
      { type: 'done', durationMs: 1 },
    );
    expect(map[scenarioRid]).toBeUndefined();
  });
});

describe('VTX-044 progress derived helpers', () => {
  it('given_state_with_two_models_when_getOverall_then_returns_average_pct', () => {
    const state: ScenarioRunJobState = {
      jobId,
      status: 'running',
      startedAt: 0,
      durationMs: null,
      error: null,
      progressByModel: { M1: 40, M2: 60 },
      resultsByModel: {},
    };
    expect(getOverallProgressPct(state)).toBe(50);
  });

  it('given_state_with_no_models_yet_when_getOverall_then_returns_zero', () => {
    const state = createScenarioRunJobState({ jobId, startedAt: 0 });
    expect(getOverallProgressPct(state)).toBe(0);
  });

  it('given_success_state_when_getOverall_then_returns_100', () => {
    const state: ScenarioRunJobState = {
      jobId,
      status: 'success',
      startedAt: 0,
      durationMs: 100,
      error: null,
      progressByModel: { M1: 40 },
      resultsByModel: {},
    };
    expect(getOverallProgressPct(state)).toBe(100);
  });

  it('given_known_model_when_getModelProgress_then_returns_pct', () => {
    const state: ScenarioRunJobState = {
      jobId,
      status: 'running',
      startedAt: 0,
      durationMs: null,
      error: null,
      progressByModel: { M1: 73 },
      resultsByModel: {},
    };
    expect(getModelProgressPct(state, 'M1')).toBe(73);
  });

  it('given_unknown_model_when_getModelProgress_then_returns_null', () => {
    const state = createScenarioRunJobState({ jobId, startedAt: 0 });
    expect(getModelProgressPct(state, 'M1')).toBeNull();
  });
});

describe('VTX-044 status icon / tooltip', () => {
  it('given_pending_when_getIcon_then_spinner', () => {
    expect(getScenarioRunJobStatusIcon('pending')).toBe('spinner');
  });

  it('given_running_when_getIcon_then_spinner', () => {
    expect(getScenarioRunJobStatusIcon('running')).toBe('spinner');
  });

  it('given_success_when_getIcon_then_check', () => {
    expect(getScenarioRunJobStatusIcon('success')).toBe('✓');
  });

  it('given_error_when_getIcon_then_cross', () => {
    expect(getScenarioRunJobStatusIcon('error')).toBe('×');
  });

  it('given_cancelled_when_getIcon_then_circled_slash', () => {
    expect(getScenarioRunJobStatusIcon('cancelled')).toBe('⊘');
  });

  it('given_idle_when_getIcon_then_dash', () => {
    expect(getScenarioRunJobStatusIcon('idle')).toBe('—');
  });

  it('given_pending_state_when_getTooltip_then_queued', () => {
    let map = applyAcceptedScenarioRunJob(
      createScenarioRunJobMap(),
      scenarioRid,
      jobId,
      0,
    );
    expect(getScenarioRunJobStatusTooltip(map, scenarioRid)).toBe('Queued…');
    map = applyScenarioRunSseEvent(map, scenarioRid, {
      type: 'progress',
      model: 'M1',
      pct: 0,
    });
    expect(getScenarioRunJobStatusTooltip(map, scenarioRid)).toContain(
      'Running',
    );
  });

  it('given_running_state_with_progress_when_getTooltip_then_shows_overall_pct', () => {
    let map = applyAcceptedScenarioRunJob(
      createScenarioRunJobMap(),
      scenarioRid,
      jobId,
      0,
    );
    map = applyScenarioRunSseEvent(map, scenarioRid, {
      type: 'progress',
      model: 'M1',
      pct: 40,
    });
    map = applyScenarioRunSseEvent(map, scenarioRid, {
      type: 'progress',
      model: 'M2',
      pct: 60,
    });
    expect(getScenarioRunJobStatusTooltip(map, scenarioRid)).toBe(
      'Running… 50%',
    );
  });

  it('given_success_state_when_getTooltip_then_shows_duration', () => {
    let map = applyAcceptedScenarioRunJob(
      createScenarioRunJobMap(),
      scenarioRid,
      jobId,
      0,
    );
    map = applyScenarioRunSseEvent(map, scenarioRid, {
      type: 'done',
      durationMs: 4250,
    });
    expect(getScenarioRunJobStatusTooltip(map, scenarioRid)).toBe(
      'Run completed in 4.3 s',
    );
  });

  it('given_error_state_when_getTooltip_then_shows_message', () => {
    let map = applyAcceptedScenarioRunJob(
      createScenarioRunJobMap(),
      scenarioRid,
      jobId,
      0,
    );
    map = applyScenarioRunSseEvent(map, scenarioRid, {
      type: 'error',
      model: 'M1',
      message: 'predict failed',
    });
    expect(getScenarioRunJobStatusTooltip(map, scenarioRid)).toBe(
      'M1: predict failed',
    );
  });

  it('given_cancelled_state_when_getTooltip_then_cancelled', () => {
    let map = applyAcceptedScenarioRunJob(
      createScenarioRunJobMap(),
      scenarioRid,
      jobId,
      0,
    );
    map = applyScenarioRunSseEvent(map, scenarioRid, { type: 'cancelled' });
    expect(getScenarioRunJobStatusTooltip(map, scenarioRid)).toBe('Cancelled');
  });

  it('given_idle_when_getTooltip_then_null', () => {
    const map = createScenarioRunJobMap();
    expect(getScenarioRunJobStatusTooltip(map, scenarioRid)).toBeNull();
  });
});

describe('VTX-044 end-to-end flows', () => {
  it('given_two_models_when_run_async_then_full_lifecycle_pending_running_success', () => {
    const scenario = mutableScenario();
    expect(shouldRunAsync({ modelCount: 2 })).toBe(true);

    const req = buildAsyncRunScenarioRequest({
      scenarioRid: scenario.rid,
      forceAsync: false,
    });
    expect(req.path).toContain(scenario.rid);

    const accepted = parseAcceptedScenarioRunResponse({
      status: 'accepted',
      jobId,
    });
    expect(accepted.jobId).toBe(jobId);

    let map = applyAcceptedScenarioRunJob(
      createScenarioRunJobMap(),
      scenario.rid,
      accepted.jobId,
      1700000000000,
    );
    expect(getScenarioRunJobStatus(map, scenario.rid)).toBe('pending');

    const events: ScenarioRunSseEvent[] = [
      { type: 'progress', model: 'M1', pct: 50 },
      { type: 'progress', model: 'M2', pct: 0 },
      { type: 'result', model: 'M1', output: { ok: true } },
      { type: 'progress', model: 'M2', pct: 100 },
      { type: 'result', model: 'M2', output: { score: 0.83 } },
      { type: 'done', durationMs: 4200 },
    ];
    for (const ev of events) {
      map = applyScenarioRunSseEvent(map, scenario.rid, ev);
    }
    expect(getScenarioRunJobStatus(map, scenario.rid)).toBe('success');
    expect(map[scenario.rid].durationMs).toBe(4200);
    expect(map[scenario.rid].resultsByModel).toEqual({
      M1: { ok: true },
      M2: { score: 0.83 },
    });
  });

  it('given_running_job_when_cancel_then_cancelled_state_and_cleanup', () => {
    let map = applyAcceptedScenarioRunJob(
      createScenarioRunJobMap(),
      scenarioRid,
      jobId,
      0,
    );
    map = applyScenarioRunSseEvent(map, scenarioRid, {
      type: 'progress',
      model: 'M1',
      pct: 30,
    });

    const cancel = buildCancelScenarioRunRequest({ scenarioRid, jobId });
    expect(cancel.path).toBe(
      `/api/vertex/v1/scenarios/${encodeURIComponent(scenarioRid)}/run/${encodeURIComponent(jobId)}/cancel`,
    );

    // SSE 推 cancelled 事件后状态机翻 cancelled
    map = applyScenarioRunSseEvent(map, scenarioRid, { type: 'cancelled' });
    expect(getScenarioRunJobStatus(map, scenarioRid)).toBe('cancelled');
    expect(isScenarioRunJobActive(map, scenarioRid)).toBe(false);
  });

  it('given_function_error_in_one_model_when_apply_then_error_message_includes_model_and_is_recoverable', () => {
    let map = applyAcceptedScenarioRunJob(
      createScenarioRunJobMap(),
      scenarioRid,
      jobId,
      0,
    );
    map = applyScenarioRunSseEvent(map, scenarioRid, {
      type: 'progress',
      model: 'M1',
      pct: 80,
    });
    map = applyScenarioRunSseEvent(map, scenarioRid, {
      type: 'error',
      model: 'M2',
      message: 'predict() missing arg',
    });
    expect(getScenarioRunJobStatus(map, scenarioRid)).toBe('error');
    expect(map[scenarioRid].error).toContain('M2');
    expect(map[scenarioRid].error).toContain('predict() missing arg');
    // 仍保留 M1 的 progress（用户可看到失败前的进度）
    expect(map[scenarioRid].progressByModel.M1).toBe(80);
  });
});
