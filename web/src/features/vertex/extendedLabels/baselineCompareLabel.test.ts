// VTX-062 — baseline vs simulated 对比 Extended Label
//
// BDD acceptance（来自 prd.json VTX-062）：
//   1. Given Scenario 已 Run + Baseline 已 Run
//      When Extended Label 渲染
//      Then 自动显示 "1500 (baseline 1000)"，差值 "+50%" 上色（绿/红）
//   2. Given 用户切换 active scenario 列
//      When 触发
//      Then label 切换对比基线（simulated 值从新的 scenario outputs 取）

import { describe, expect, it } from 'vitest';

import {
  applyBaselineRunSuccess,
  createBaselineRunMap,
} from '../scenarioPane/baselineRun';
import {
  createActiveScenarioState,
  setActiveScenario,
  type ScenarioOutputsByRid,
} from '../scenarioPane/multiScenarioCompare';
import {
  ERROR_PLACEHOLDER,
  MISSING_VALUE_PLACEHOLDER,
  renderBaselineCompareExtendedLabel,
  selectBaselineCompareValues,
  type BaselineCompareLabelInput,
  type BaselineCompareLabelSpec,
} from './baselineCompareLabel';

const CASE_RID = 'ri.case.main.case-study.airports';
const SCENARIO_A = 'ri.case.main.scenario.scn-A';
const SCENARIO_B = 'ri.case.main.scenario.scn-B';
const OBJ_TYPE = 'Route';
const PK = 'JFK-LAX';
const PROPERTY = 'totalAlerts';
// Object key (3-segment) shape comes from buildObjectBaselineOutputKey in
// VTX-045; we hard-code the string in the per-test expectations only when
// asserting the lookup behaviour, otherwise we rely on the helper.
const KEY = `${OBJ_TYPE}::${PK}::${PROPERTY}`;

function buildOutputs(value: number | null) {
  return value === null ? {} : { [KEY]: value };
}

function buildScenarioMap(rid: string, value: number | null): ScenarioOutputsByRid {
  return { [rid]: buildOutputs(value) };
}

describe('VTX-062 extendedLabels.baselineCompareLabel — selectBaselineCompareValues', () => {
  const baseInput = (
    activeScenarioRid: string | null,
    baselineValue: number | null,
    scenarioOutputsByRid: ScenarioOutputsByRid = {},
  ): BaselineCompareLabelInput => {
    const baselineMap = applyBaselineRunSuccess(
      createBaselineRunMap(),
      CASE_RID,
      120,
      buildOutputs(baselineValue),
    );
    return {
      activeState: createActiveScenarioState({ activeScenarioRid }),
      baselineOutputs: baselineMap[CASE_RID]?.outputs ?? {},
      scenarioOutputsByRid,
      context: { objectType: OBJ_TYPE, primaryKey: PK, property: PROPERTY },
    };
  };

  it('given_active_baseline_when_select_then_simulated_null_baseline_value', () => {
    const v = selectBaselineCompareValues(baseInput(null, 1000));
    expect(v).toEqual({ simulated: null, baseline: 1000, activeScenarioRid: null });
  });

  it('given_active_scenario_with_value_when_select_then_returns_both', () => {
    const v = selectBaselineCompareValues(
      baseInput(SCENARIO_A, 1000, buildScenarioMap(SCENARIO_A, 1500)),
    );
    expect(v).toEqual({
      simulated: 1500,
      baseline: 1000,
      activeScenarioRid: SCENARIO_A,
    });
  });

  it('given_active_scenario_missing_outputs_when_select_then_simulated_null', () => {
    const v = selectBaselineCompareValues(baseInput(SCENARIO_A, 1000, {}));
    expect(v).toEqual({
      simulated: null,
      baseline: 1000,
      activeScenarioRid: SCENARIO_A,
    });
  });

  it('given_active_scenario_outputs_missing_key_when_select_then_simulated_null', () => {
    const v = selectBaselineCompareValues(
      baseInput(SCENARIO_A, 1000, { [SCENARIO_A]: { 'Other::pk::p': 99 } }),
    );
    expect(v.simulated).toBeNull();
    expect(v.baseline).toBe(1000);
  });

  it('given_baseline_missing_when_select_then_baseline_null', () => {
    const v = selectBaselineCompareValues(
      baseInput(SCENARIO_A, null, buildScenarioMap(SCENARIO_A, 1500)),
    );
    expect(v).toEqual({
      simulated: 1500,
      baseline: null,
      activeScenarioRid: SCENARIO_A,
    });
  });

  it('given_active_switches_scenarios_when_select_then_simulated_follows', () => {
    // BDD #2 — switching active scenario column flips simulated source.
    const scenarioOutputsByRid: ScenarioOutputsByRid = {
      [SCENARIO_A]: buildOutputs(1500),
      [SCENARIO_B]: buildOutputs(800),
    };
    const inputA: BaselineCompareLabelInput = {
      activeState: createActiveScenarioState({ activeScenarioRid: SCENARIO_A }),
      baselineOutputs: buildOutputs(1000),
      scenarioOutputsByRid,
      context: { objectType: OBJ_TYPE, primaryKey: PK, property: PROPERTY },
    };
    const inputB: BaselineCompareLabelInput = {
      ...inputA,
      activeState: setActiveScenario(inputA.activeState, SCENARIO_B),
    };
    expect(selectBaselineCompareValues(inputA).simulated).toBe(1500);
    expect(selectBaselineCompareValues(inputB).simulated).toBe(800);
  });

  it('given_string_baseline_value_when_select_then_passes_through', () => {
    const v = selectBaselineCompareValues(
      baseInput(SCENARIO_A, null, {
        [SCENARIO_A]: { [KEY]: 'down' },
      }),
    );
    expect(v.simulated).toBe('down');
    expect(v.baseline).toBeNull();
  });
});

describe('VTX-062 extendedLabels.baselineCompareLabel — renderBaselineCompareExtendedLabel (BDD #1)', () => {
  const spec: BaselineCompareLabelSpec = { labelName: 'totalAlerts' };

  it('given_simulated_1500_baseline_1000_when_render_then_present_and_+50pct_positive', () => {
    const r = renderBaselineCompareExtendedLabel(spec, {
      simulated: 1500,
      baseline: 1000,
      activeScenarioRid: SCENARIO_A,
    });
    expect(r.status).toBe('present');
    expect(r.labelName).toBe('totalAlerts');
    expect(r.simulatedText).toBe('1500');
    expect(r.baselineText).toBe('baseline 1000');
    expect(r.deltaText).toBe('+50.0%');
    expect(r.colorHint).toBe('positive');
    expect(r.text).toBe('totalAlerts: 1500 (baseline 1000, +50.0%)');
  });

  it('given_simulated_500_baseline_1000_when_render_then_negative_color', () => {
    const r = renderBaselineCompareExtendedLabel(spec, {
      simulated: 500,
      baseline: 1000,
      activeScenarioRid: SCENARIO_A,
    });
    expect(r.status).toBe('present');
    expect(r.deltaText).toBe('-50.0%');
    expect(r.colorHint).toBe('negative');
    expect(r.text).toBe('totalAlerts: 500 (baseline 1000, -50.0%)');
  });

  it('given_simulated_equals_baseline_when_render_then_neutral_color', () => {
    const r = renderBaselineCompareExtendedLabel(spec, {
      simulated: 1000,
      baseline: 1000,
      activeScenarioRid: SCENARIO_A,
    });
    expect(r.status).toBe('present');
    expect(r.colorHint).toBe('neutral');
    // formatBaselineCompareLabel only signs strictly-positive deltas — 0.0
    // stays unsigned (与 VTX-045 formatDeltaPct 一致)。
    expect(r.deltaText).toBe('0.0%');
  });

  it('given_baseline_zero_when_render_then_falls_back_to_signed_delta', () => {
    const r = renderBaselineCompareExtendedLabel(spec, {
      simulated: 5,
      baseline: 0,
      activeScenarioRid: SCENARIO_A,
    });
    expect(r.status).toBe('present');
    expect(r.deltaText).toBe('+5');
    expect(r.colorHint).toBe('positive');
  });

  it('given_displayName_when_render_then_used_as_label_name', () => {
    const r = renderBaselineCompareExtendedLabel(
      { labelName: 'totalAlerts', displayName: 'Total Alerts' },
      { simulated: 1500, baseline: 1000, activeScenarioRid: SCENARIO_A },
    );
    expect(r.labelName).toBe('Total Alerts');
    expect(r.text).toBe('Total Alerts: 1500 (baseline 1000, +50.0%)');
  });

  it('given_blank_displayName_when_render_then_falls_back_to_labelName', () => {
    const r = renderBaselineCompareExtendedLabel(
      { labelName: 'totalAlerts', displayName: '   ' },
      { simulated: 1500, baseline: 1000, activeScenarioRid: SCENARIO_A },
    );
    expect(r.labelName).toBe('totalAlerts');
  });

  it('given_decimals_2_when_render_then_simulated_and_baseline_have_two_decimals', () => {
    const r = renderBaselineCompareExtendedLabel(
      spec,
      { simulated: 1234.567, baseline: 1000, activeScenarioRid: SCENARIO_A },
      { decimals: 2 },
    );
    expect(r.simulatedText).toBe('1234.57');
    expect(r.baselineText).toBe('baseline 1000.00');
    // Pct is always 1-decimal (consistent with formatBaselineCompareLabel).
    expect(r.deltaText).toBe('+23.5%');
  });

  it('given_hideDelta_when_render_then_text_omits_delta_segment', () => {
    const r = renderBaselineCompareExtendedLabel(
      spec,
      { simulated: 1500, baseline: 1000, activeScenarioRid: SCENARIO_A },
      { hideDelta: true },
    );
    expect(r.deltaText).toBe('');
    expect(r.text).toBe('totalAlerts: 1500 (baseline 1000)');
  });
});

describe('VTX-062 extendedLabels.baselineCompareLabel — render: baseline-only / missing / error', () => {
  const spec: BaselineCompareLabelSpec = { labelName: 'totalAlerts' };

  it('given_active_null_when_render_then_status_baseline_only', () => {
    const r = renderBaselineCompareExtendedLabel(spec, {
      simulated: null,
      baseline: 1000,
      activeScenarioRid: null,
    });
    expect(r.status).toBe('baseline-only');
    expect(r.simulatedText).toBe('1000');
    expect(r.baselineText).toBe('baseline 1000');
    expect(r.deltaText).toBe('');
    expect(r.colorHint).toBeNull();
    expect(r.text).toBe('totalAlerts: 1000');
  });

  it('given_active_null_and_baseline_missing_when_render_then_status_missing', () => {
    const r = renderBaselineCompareExtendedLabel(spec, {
      simulated: null,
      baseline: null,
      activeScenarioRid: null,
    });
    expect(r.status).toBe('missing');
    expect(r.simulatedText).toBe(MISSING_VALUE_PLACEHOLDER);
    expect(r.text).toBe(`totalAlerts: ${MISSING_VALUE_PLACEHOLDER}`);
    expect(r.colorHint).toBeNull();
  });

  it('given_active_scenario_baseline_missing_when_render_then_status_missing_baseline', () => {
    const r = renderBaselineCompareExtendedLabel(spec, {
      simulated: 1500,
      baseline: null,
      activeScenarioRid: SCENARIO_A,
    });
    expect(r.status).toBe('missing-baseline');
    expect(r.simulatedText).toBe('1500');
    expect(r.baselineText).toBe(`baseline ${MISSING_VALUE_PLACEHOLDER}`);
    expect(r.deltaText).toBe('');
    expect(r.colorHint).toBeNull();
    expect(r.text).toBe(`totalAlerts: 1500 (baseline ${MISSING_VALUE_PLACEHOLDER})`);
  });

  it('given_active_scenario_simulated_missing_when_render_then_status_missing_simulated', () => {
    const r = renderBaselineCompareExtendedLabel(spec, {
      simulated: null,
      baseline: 1000,
      activeScenarioRid: SCENARIO_A,
    });
    expect(r.status).toBe('missing-simulated');
    expect(r.simulatedText).toBe(MISSING_VALUE_PLACEHOLDER);
    expect(r.baselineText).toBe('baseline 1000');
    expect(r.deltaText).toBe('');
    expect(r.colorHint).toBeNull();
    expect(r.text).toBe(`totalAlerts: ${MISSING_VALUE_PLACEHOLDER} (baseline 1000)`);
  });

  it('given_string_simulated_when_render_then_status_missing_simulated', () => {
    // 非有限数（包括 string / boolean）走 missing 路径 — compare 只在两侧
    // 都是 finite number 时才有意义；否则按 type 缺失处理。
    const r = renderBaselineCompareExtendedLabel(spec, {
      simulated: 'down',
      baseline: 1000,
      activeScenarioRid: SCENARIO_A,
    });
    expect(r.status).toBe('missing-simulated');
    expect(r.simulatedText).toBe(MISSING_VALUE_PLACEHOLDER);
  });

  it('given_NaN_simulated_when_render_then_status_missing_simulated', () => {
    const r = renderBaselineCompareExtendedLabel(spec, {
      simulated: Number.NaN,
      baseline: 1000,
      activeScenarioRid: SCENARIO_A,
    });
    expect(r.status).toBe('missing-simulated');
  });

  it('given_explicit_error_when_render_then_status_error_and_uses_error_placeholder', () => {
    const r = renderBaselineCompareExtendedLabel(
      spec,
      { simulated: 1500, baseline: 1000, activeScenarioRid: SCENARIO_A },
      { error: 'Scenario run failed: timeout' },
    );
    expect(r.status).toBe('error');
    expect(r.simulatedText).toBe(ERROR_PLACEHOLDER);
    expect(r.baselineText).toBe('');
    expect(r.deltaText).toBe('');
    expect(r.colorHint).toBeNull();
    expect(r.errorMessage).toBe('Scenario run failed: timeout');
    expect(r.text).toBe(`totalAlerts: ${ERROR_PLACEHOLDER}`);
  });

  it('given_empty_string_error_when_render_then_treated_as_no_error', () => {
    const r = renderBaselineCompareExtendedLabel(
      spec,
      { simulated: 1500, baseline: 1000, activeScenarioRid: SCENARIO_A },
      { error: '' },
    );
    expect(r.status).toBe('present');
    expect(r.errorMessage).toBeUndefined();
  });

  it('given_custom_missingPlaceholder_when_render_then_used_in_segments', () => {
    const r = renderBaselineCompareExtendedLabel(
      spec,
      { simulated: null, baseline: 1000, activeScenarioRid: SCENARIO_A },
      { missingPlaceholder: 'n/a' },
    );
    expect(r.simulatedText).toBe('n/a');
    expect(r.text).toBe('totalAlerts: n/a (baseline 1000)');
  });

  it('given_custom_errorPlaceholder_when_render_then_used_in_text', () => {
    const r = renderBaselineCompareExtendedLabel(
      spec,
      { simulated: 1500, baseline: 1000, activeScenarioRid: SCENARIO_A },
      { error: 'boom', errorPlaceholder: '✗' },
    );
    expect(r.simulatedText).toBe('✗');
    expect(r.text).toBe('totalAlerts: ✗');
  });

  it('given_blank_labelName_when_render_then_throws', () => {
    expect(() =>
      renderBaselineCompareExtendedLabel(
        { labelName: '   ' },
        { simulated: 1500, baseline: 1000, activeScenarioRid: SCENARIO_A },
      ),
    ).toThrow(/labelName/i);
  });
});

describe('VTX-062 extendedLabels.baselineCompareLabel — full pipeline (selectValues → render)', () => {
  const spec: BaselineCompareLabelSpec = { labelName: 'totalAlerts' };
  const ctx = { objectType: OBJ_TYPE, primaryKey: PK, property: PROPERTY };

  it('given_full_input_when_select_then_render_then_matches_BDD1_text', () => {
    const input: BaselineCompareLabelInput = {
      activeState: createActiveScenarioState({ activeScenarioRid: SCENARIO_A }),
      baselineOutputs: buildOutputs(1000),
      scenarioOutputsByRid: buildScenarioMap(SCENARIO_A, 1500),
      context: ctx,
    };
    const values = selectBaselineCompareValues(input);
    const r = renderBaselineCompareExtendedLabel(spec, values);
    expect(r.text).toBe('totalAlerts: 1500 (baseline 1000, +50.0%)');
    expect(r.colorHint).toBe('positive');
  });

  it('given_active_switches_when_pipeline_then_text_changes', () => {
    // BDD #2 — main view switches scenarios; full render text reflects new
    // simulated / compare basis.
    const baselineOutputs = buildOutputs(1000);
    const scenarioOutputsByRid: ScenarioOutputsByRid = {
      [SCENARIO_A]: buildOutputs(1500),
      [SCENARIO_B]: buildOutputs(500),
    };
    const inputA: BaselineCompareLabelInput = {
      activeState: createActiveScenarioState({ activeScenarioRid: SCENARIO_A }),
      baselineOutputs,
      scenarioOutputsByRid,
      context: ctx,
    };
    const inputB: BaselineCompareLabelInput = {
      ...inputA,
      activeState: setActiveScenario(inputA.activeState, SCENARIO_B),
    };
    const rA = renderBaselineCompareExtendedLabel(spec, selectBaselineCompareValues(inputA));
    const rB = renderBaselineCompareExtendedLabel(spec, selectBaselineCompareValues(inputB));
    expect(rA.text).toBe('totalAlerts: 1500 (baseline 1000, +50.0%)');
    expect(rA.colorHint).toBe('positive');
    expect(rB.text).toBe('totalAlerts: 500 (baseline 1000, -50.0%)');
    expect(rB.colorHint).toBe('negative');
  });
});
