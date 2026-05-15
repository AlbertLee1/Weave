import { describe, expect, it } from 'vitest';

import {
  buildObjectBaselineOutputKey,
  buildPaneBaselineOutputKey,
} from './baselineRun';
import {
  BASELINE_COLUMN_KEY,
  BASELINE_COLUMN_LABEL,
  clearActiveIfMissing,
  createActiveScenarioState,
  getMultiScenarioColumns,
  getMultiScenarioObjectReadings,
  getMultiScenarioPaneRowReadings,
  getNodeColoringOutputs,
  getNodeColoringSource,
  getScenarioOutput,
  isBaselineActive,
  isScenarioActive,
  setActiveColumnFromHeader,
  setActiveScenario,
  type MultiScenarioColumn,
  type ScenarioOutputsByRid,
} from './multiScenarioCompare';
import { addScenario, createScenarioPaneState, setCaseStudy } from './scenarioPane';

const caseStudyRid = 'ri.vertex.main.case-study.cs-1';
const sA = 'ri.vertex.main.scenario.s-A';
const sB = 'ri.vertex.main.scenario.s-B';
const sC = 'ri.vertex.main.scenario.s-C';

function paneWith3Scenarios(): ReturnType<typeof createScenarioPaneState> {
  let pane = createScenarioPaneState();
  pane = setCaseStudy(pane, { rid: caseStudyRid, name: 'Q3 plan' });
  pane = addScenario(pane, { rid: sA, name: 'Scenario A' });
  pane = addScenario(pane, { rid: sB, name: 'Scenario B' });
  pane = addScenario(pane, { rid: sC, name: 'Scenario C' });
  return pane;
}

describe('VTX-047 ActiveScenarioState', () => {
  it('given_no_init_when_createState_then_baseline_is_active', () => {
    const s = createActiveScenarioState();
    expect(s.activeScenarioRid).toBeNull();
    expect(isBaselineActive(s)).toBe(true);
  });

  it('given_init_rid_when_createState_then_records_rid', () => {
    const s = createActiveScenarioState({ activeScenarioRid: sA });
    expect(s.activeScenarioRid).toBe(sA);
    expect(isBaselineActive(s)).toBe(false);
  });

  it('given_state_when_setActiveScenario_then_returns_new_state', () => {
    const s = createActiveScenarioState();
    const next = setActiveScenario(s, sA);
    expect(next.activeScenarioRid).toBe(sA);
    expect(s.activeScenarioRid).toBeNull();
  });

  it('given_state_when_setActiveScenario_same_value_then_returns_same_reference', () => {
    const s = createActiveScenarioState({ activeScenarioRid: sA });
    expect(setActiveScenario(s, sA)).toBe(s);
  });

  it('given_state_when_setActiveScenario_null_then_returns_baseline', () => {
    const s = createActiveScenarioState({ activeScenarioRid: sA });
    const next = setActiveScenario(s, null);
    expect(next.activeScenarioRid).toBeNull();
    expect(isBaselineActive(next)).toBe(true);
  });

  it('given_baseline_active_when_setActiveScenario_null_then_same_reference', () => {
    const s = createActiveScenarioState();
    expect(setActiveScenario(s, null)).toBe(s);
  });

  it('given_scenario_active_when_isScenarioActive_then_true_only_for_match', () => {
    const s = createActiveScenarioState({ activeScenarioRid: sA });
    expect(isScenarioActive(s, sA)).toBe(true);
    expect(isScenarioActive(s, sB)).toBe(false);
  });

  it('given_baseline_active_when_isScenarioActive_for_any_rid_then_false', () => {
    const s = createActiveScenarioState();
    expect(isScenarioActive(s, sA)).toBe(false);
  });

  it('given_baseline_active_when_clearActiveIfMissing_then_same_reference', () => {
    const s = createActiveScenarioState();
    expect(clearActiveIfMissing(s, [sA, sB])).toBe(s);
  });

  it('given_scenario_active_still_present_when_clearActiveIfMissing_then_same_reference', () => {
    const s = createActiveScenarioState({ activeScenarioRid: sA });
    expect(clearActiveIfMissing(s, [sA, sB])).toBe(s);
  });

  it('given_scenario_active_now_missing_when_clearActiveIfMissing_then_resets_to_baseline', () => {
    const s = createActiveScenarioState({ activeScenarioRid: sA });
    const next = clearActiveIfMissing(s, [sB, sC]);
    expect(next.activeScenarioRid).toBeNull();
  });

  it('given_scenario_active_empty_list_when_clearActiveIfMissing_then_resets_to_baseline', () => {
    const s = createActiveScenarioState({ activeScenarioRid: sA });
    const next = clearActiveIfMissing(s, []);
    expect(next.activeScenarioRid).toBeNull();
  });
});

describe('VTX-047 getMultiScenarioColumns', () => {
  it('given_pane_without_case_study_when_getColumns_then_empty', () => {
    const pane = createScenarioPaneState();
    const active = createActiveScenarioState();
    expect(getMultiScenarioColumns(pane, active)).toEqual([]);
  });

  it('given_case_study_no_scenarios_when_getColumns_then_only_baseline', () => {
    let pane = createScenarioPaneState();
    pane = setCaseStudy(pane, { rid: caseStudyRid, name: 'CS' });
    const cols = getMultiScenarioColumns(pane, createActiveScenarioState());
    expect(cols).toEqual<MultiScenarioColumn[]>([
      {
        key: BASELINE_COLUMN_KEY,
        label: BASELINE_COLUMN_LABEL,
        kind: 'baseline',
        isActive: true,
      },
    ]);
  });

  it('given_case_study_three_scenarios_baseline_active_when_getColumns_then_four_columns_baseline_first', () => {
    const pane = paneWith3Scenarios();
    const cols = getMultiScenarioColumns(pane, createActiveScenarioState());
    expect(cols.map((c) => c.key)).toEqual([BASELINE_COLUMN_KEY, sA, sB, sC]);
    expect(cols.map((c) => c.label)).toEqual([
      BASELINE_COLUMN_LABEL,
      'Scenario A',
      'Scenario B',
      'Scenario C',
    ]);
    expect(cols.map((c) => c.kind)).toEqual([
      'baseline',
      'scenario',
      'scenario',
      'scenario',
    ]);
    expect(cols.map((c) => c.isActive)).toEqual([true, false, false, false]);
    expect(cols[1].scenarioRid).toBe(sA);
    expect(cols[2].scenarioRid).toBe(sB);
    expect(cols[3].scenarioRid).toBe(sC);
    expect(cols[0].scenarioRid).toBeUndefined();
  });

  it('given_scenario_active_when_getColumns_then_isActive_flips_to_scenario_column', () => {
    const pane = paneWith3Scenarios();
    const active = createActiveScenarioState({ activeScenarioRid: sB });
    const cols = getMultiScenarioColumns(pane, active);
    expect(cols.map((c) => c.isActive)).toEqual([false, false, true, false]);
  });
});

describe('VTX-047 setActiveColumnFromHeader', () => {
  it('given_baseline_column_when_setActive_then_resets_to_baseline', () => {
    const s = createActiveScenarioState({ activeScenarioRid: sA });
    expect(setActiveColumnFromHeader(s, BASELINE_COLUMN_KEY).activeScenarioRid).toBeNull();
  });

  it('given_scenario_column_when_setActive_then_records_rid', () => {
    const s = createActiveScenarioState();
    expect(setActiveColumnFromHeader(s, sB).activeScenarioRid).toBe(sB);
  });

  it('given_same_column_when_setActive_then_same_reference', () => {
    const s = createActiveScenarioState({ activeScenarioRid: sA });
    expect(setActiveColumnFromHeader(s, sA)).toBe(s);
  });

  it('given_baseline_active_when_setActive_baseline_again_then_same_reference', () => {
    const s = createActiveScenarioState();
    expect(setActiveColumnFromHeader(s, BASELINE_COLUMN_KEY)).toBe(s);
  });
});

describe('VTX-047 getScenarioOutput', () => {
  const outputs: ScenarioOutputsByRid = {
    [sA]: { 'row-1::demand': 1500 },
    [sB]: { 'row-1::demand': null },
  };

  it('given_known_rid_known_key_when_get_then_returns_value', () => {
    expect(getScenarioOutput(outputs, sA, 'row-1::demand')).toBe(1500);
  });

  it('given_unknown_rid_when_get_then_returns_null', () => {
    expect(getScenarioOutput(outputs, sC, 'row-1::demand')).toBeNull();
  });

  it('given_known_rid_unknown_key_when_get_then_returns_null', () => {
    expect(getScenarioOutput(outputs, sA, 'row-1::missing')).toBeNull();
  });

  it('given_explicit_null_value_when_get_then_preserves_null', () => {
    expect(getScenarioOutput(outputs, sB, 'row-1::demand')).toBeNull();
  });
});

describe('VTX-047 getMultiScenarioPaneRowReadings', () => {
  const rowRid = 'row-1';
  const param = 'demand';
  const key = buildPaneBaselineOutputKey(rowRid, param);

  it('given_no_case_study_when_get_then_empty', () => {
    const pane = createScenarioPaneState();
    const readings = getMultiScenarioPaneRowReadings({
      paneState: pane,
      activeState: createActiveScenarioState(),
      baselineOutputs: { [key]: 1000 },
      scenarioOutputsByRid: {},
      rowRid,
      paramName: param,
    });
    expect(readings).toEqual([]);
  });

  it('given_four_columns_all_numeric_when_get_then_four_readings_with_compare', () => {
    const pane = paneWith3Scenarios();
    const readings = getMultiScenarioPaneRowReadings({
      paneState: pane,
      activeState: createActiveScenarioState(),
      baselineOutputs: { [key]: 1000 },
      scenarioOutputsByRid: {
        [sA]: { [key]: 1500 },
        [sB]: { [key]: 800 },
        [sC]: { [key]: 1000 },
      },
      rowRid,
      paramName: param,
    });

    expect(readings).toHaveLength(4);

    // Baseline column itself: value is the baseline value, no compare.
    expect(readings[0].kind).toBe('baseline');
    expect(readings[0].columnKey).toBe(BASELINE_COLUMN_KEY);
    expect(readings[0].value).toBe(1000);
    expect(readings[0].compare).toBeNull();
    expect(readings[0].colorHint).toBeNull();

    // Scenario A (positive)
    expect(readings[1].kind).toBe('scenario');
    expect(readings[1].scenarioRid).toBe(sA);
    expect(readings[1].value).toBe(1500);
    expect(readings[1].compare?.delta).toBe(500);
    expect(readings[1].compare?.deltaPct).toBeCloseTo(50, 6);
    expect(readings[1].colorHint).toBe('positive');

    // Scenario B (negative)
    expect(readings[2].compare?.delta).toBe(-200);
    expect(readings[2].colorHint).toBe('negative');

    // Scenario C (neutral)
    expect(readings[3].compare?.delta).toBe(0);
    expect(readings[3].colorHint).toBe('neutral');
  });

  it('given_baseline_value_missing_when_get_then_scenario_readings_have_null_compare', () => {
    const pane = paneWith3Scenarios();
    const readings = getMultiScenarioPaneRowReadings({
      paneState: pane,
      activeState: createActiveScenarioState(),
      baselineOutputs: {},
      scenarioOutputsByRid: { [sA]: { [key]: 1500 } },
      rowRid,
      paramName: param,
    });
    expect(readings[0].value).toBeNull();
    expect(readings[1].value).toBe(1500);
    expect(readings[1].compare).toBeNull();
    expect(readings[1].colorHint).toBeNull();
  });

  it('given_scenario_value_missing_when_get_then_reading_value_null_no_compare', () => {
    const pane = paneWith3Scenarios();
    const readings = getMultiScenarioPaneRowReadings({
      paneState: pane,
      activeState: createActiveScenarioState(),
      baselineOutputs: { [key]: 1000 },
      scenarioOutputsByRid: { [sA]: { [key]: 1500 } },
      rowRid,
      paramName: param,
    });
    expect(readings[1].value).toBe(1500);
    expect(readings[2].value).toBeNull();
    expect(readings[2].compare).toBeNull();
    expect(readings[2].colorHint).toBeNull();
  });

  it('given_non_numeric_baseline_when_get_then_no_compare_for_scenarios', () => {
    const pane = paneWith3Scenarios();
    const readings = getMultiScenarioPaneRowReadings({
      paneState: pane,
      activeState: createActiveScenarioState(),
      baselineOutputs: { [key]: 'baseline-string' },
      scenarioOutputsByRid: { [sA]: { [key]: 1500 } },
      rowRid,
      paramName: param,
    });
    expect(readings[0].value).toBe('baseline-string');
    expect(readings[1].value).toBe(1500);
    expect(readings[1].compare).toBeNull();
  });

  it('given_non_numeric_scenario_value_when_get_then_no_compare', () => {
    const pane = paneWith3Scenarios();
    const readings = getMultiScenarioPaneRowReadings({
      paneState: pane,
      activeState: createActiveScenarioState(),
      baselineOutputs: { [key]: 1000 },
      scenarioOutputsByRid: { [sA]: { [key]: true } },
      rowRid,
      paramName: param,
    });
    expect(readings[1].value).toBe(true);
    expect(readings[1].compare).toBeNull();
    expect(readings[1].colorHint).toBeNull();
  });

  it('given_active_scenario_when_get_then_column_isActive_flag_propagates', () => {
    const pane = paneWith3Scenarios();
    const active = createActiveScenarioState({ activeScenarioRid: sA });
    const readings = getMultiScenarioPaneRowReadings({
      paneState: pane,
      activeState: active,
      baselineOutputs: { [key]: 1000 },
      scenarioOutputsByRid: { [sA]: { [key]: 1500 } },
      rowRid,
      paramName: param,
    });
    expect(readings.map((r) => r.columnKey)).toEqual([
      BASELINE_COLUMN_KEY,
      sA,
      sB,
      sC,
    ]);
  });
});

describe('VTX-047 getMultiScenarioObjectReadings', () => {
  const objectType = 'Airport';
  const primaryKey = 'JFK';
  const property = 'capacity';
  const key = buildObjectBaselineOutputKey(objectType, primaryKey, property);

  it('given_no_case_study_when_get_then_empty', () => {
    const pane = createScenarioPaneState();
    const readings = getMultiScenarioObjectReadings({
      paneState: pane,
      activeState: createActiveScenarioState(),
      baselineOutputs: { [key]: 100 },
      scenarioOutputsByRid: {},
      objectType,
      primaryKey,
      property,
    });
    expect(readings).toEqual([]);
  });

  it('given_three_scenarios_when_get_then_four_readings_with_compare', () => {
    const pane = paneWith3Scenarios();
    const readings = getMultiScenarioObjectReadings({
      paneState: pane,
      activeState: createActiveScenarioState(),
      baselineOutputs: { [key]: 1000 },
      scenarioOutputsByRid: {
        [sA]: { [key]: 1500 },
        [sB]: { [key]: 1000 },
        [sC]: { [key]: 900 },
      },
      objectType,
      primaryKey,
      property,
    });
    expect(readings).toHaveLength(4);
    expect(readings[0].value).toBe(1000);
    expect(readings[0].compare).toBeNull();
    expect(readings[1].value).toBe(1500);
    expect(readings[1].compare?.deltaPct).toBeCloseTo(50, 6);
    expect(readings[1].colorHint).toBe('positive');
    expect(readings[2].colorHint).toBe('neutral');
    expect(readings[3].colorHint).toBe('negative');
  });

  it('given_baseline_zero_when_get_then_scenario_deltaPct_null_but_colorHint_present', () => {
    const pane = paneWith3Scenarios();
    const readings = getMultiScenarioObjectReadings({
      paneState: pane,
      activeState: createActiveScenarioState(),
      baselineOutputs: { [key]: 0 },
      scenarioOutputsByRid: { [sA]: { [key]: 500 } },
      objectType,
      primaryKey,
      property,
    });
    expect(readings[1].compare?.delta).toBe(500);
    expect(readings[1].compare?.deltaPct).toBeNull();
    expect(readings[1].colorHint).toBe('positive');
  });

  it('given_baseline_null_when_get_then_no_compare_even_if_scenario_finite', () => {
    const pane = paneWith3Scenarios();
    const readings = getMultiScenarioObjectReadings({
      paneState: pane,
      activeState: createActiveScenarioState(),
      baselineOutputs: { [key]: null },
      scenarioOutputsByRid: { [sA]: { [key]: 100 } },
      objectType,
      primaryKey,
      property,
    });
    expect(readings[0].value).toBeNull();
    expect(readings[1].value).toBe(100);
    expect(readings[1].compare).toBeNull();
  });
});

describe('VTX-047 NodeColoring', () => {
  it('given_baseline_active_when_getSource_then_baseline_kind', () => {
    const source = getNodeColoringSource(createActiveScenarioState());
    expect(source).toEqual({ kind: 'baseline' });
  });

  it('given_scenario_active_when_getSource_then_scenario_kind_with_rid', () => {
    const source = getNodeColoringSource(
      createActiveScenarioState({ activeScenarioRid: sA }),
    );
    expect(source).toEqual({ kind: 'scenario', scenarioRid: sA });
  });

  it('given_baseline_active_when_getOutputs_then_returns_baseline_outputs', () => {
    const baseline = { 'Airport::JFK::capacity': 100 };
    const outputs = getNodeColoringOutputs({
      activeState: createActiveScenarioState(),
      baselineOutputs: baseline,
      scenarioOutputsByRid: { [sA]: { 'Airport::JFK::capacity': 200 } },
    });
    expect(outputs).toBe(baseline);
  });

  it('given_scenario_active_when_getOutputs_then_returns_scenario_outputs', () => {
    const scenarioA = { 'Airport::JFK::capacity': 200 };
    const outputs = getNodeColoringOutputs({
      activeState: createActiveScenarioState({ activeScenarioRid: sA }),
      baselineOutputs: { 'Airport::JFK::capacity': 100 },
      scenarioOutputsByRid: { [sA]: scenarioA },
    });
    expect(outputs).toBe(scenarioA);
  });

  it('given_scenario_active_no_outputs_known_when_getOutputs_then_empty', () => {
    const outputs = getNodeColoringOutputs({
      activeState: createActiveScenarioState({ activeScenarioRid: sC }),
      baselineOutputs: { 'Airport::JFK::capacity': 100 },
      scenarioOutputsByRid: {},
    });
    expect(outputs).toEqual({});
  });
});

describe('VTX-047 end-to-end happy paths', () => {
  it('given_4_columns_user_clicks_scenario_header_then_active_flips_and_coloring_outputs_switch', () => {
    const pane = paneWith3Scenarios();
    let active = createActiveScenarioState();

    let cols = getMultiScenarioColumns(pane, active);
    expect(cols[0].isActive).toBe(true);
    expect(cols.slice(1).every((c) => !c.isActive)).toBe(true);

    // User clicks Scenario A column header.
    active = setActiveColumnFromHeader(active, sA);
    cols = getMultiScenarioColumns(pane, active);
    expect(cols.find((c) => c.key === sA)?.isActive).toBe(true);
    expect(cols.find((c) => c.key === BASELINE_COLUMN_KEY)?.isActive).toBe(false);

    // Node coloring outputs follow the active scenario.
    const scenarioA = { 'Airport::JFK::capacity': 200 };
    const baseline = { 'Airport::JFK::capacity': 100 };
    const outputs = getNodeColoringOutputs({
      activeState: active,
      baselineOutputs: baseline,
      scenarioOutputsByRid: { [sA]: scenarioA },
    });
    expect(outputs).toBe(scenarioA);

    // User clicks the Baseline column header — resets.
    active = setActiveColumnFromHeader(active, BASELINE_COLUMN_KEY);
    expect(isBaselineActive(active)).toBe(true);
    expect(
      getNodeColoringOutputs({
        activeState: active,
        baselineOutputs: baseline,
        scenarioOutputsByRid: { [sA]: scenarioA },
      }),
    ).toBe(baseline);
  });

  it('given_active_scenario_removed_when_clearIfMissing_then_resets_to_baseline_and_columns_have_no_active_scenario', () => {
    const pane = paneWith3Scenarios();
    let active = createActiveScenarioState({ activeScenarioRid: sB });
    // Simulate VTX-036 removeScenario removing sB; React 接线层调用 clearActiveIfMissing
    // 同步主视图，避免悬挂引用。
    const remainingRids = [sA, sC];
    active = clearActiveIfMissing(active, remainingRids);
    expect(active.activeScenarioRid).toBeNull();
    // Note: 这里没把 sB 从 pane.scenarios 里删掉，仅断言派生 helper 不再标记
    // 任何 scenario 列为 active —— 实际 React 接线层会先 dispatch removeScenario
    // 再 dispatch clearActiveIfMissing。
    const cols = getMultiScenarioColumns(pane, active);
    expect(cols.find((c) => c.key === BASELINE_COLUMN_KEY)?.isActive).toBe(true);
    expect(cols.filter((c) => c.kind === 'scenario').every((c) => !c.isActive)).toBe(true);
  });
});
