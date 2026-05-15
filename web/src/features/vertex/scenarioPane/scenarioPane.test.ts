import { describe, expect, it } from 'vitest';

import {
  addActionRow,
  addInputOutputColumn,
  addModelRow,
  addScenario,
  createScenarioPaneState,
  getColumnHeaders,
  getRowGroups,
  getToolbarButtons,
  isEmpty,
  removeInputOutputColumn,
  removeRow,
  removeScenario,
  setCaseStudy,
  setPaneExpanded,
  togglePane,
  type ScenarioPaneRow,
} from './scenarioPane';

const cs1 = { rid: 'ri.vertex.main.case-study.cs-1', name: 'Hub Capacity' };
const cs2 = { rid: 'ri.vertex.main.case-study.cs-2', name: 'Demand Shock' };
const scenarioA = { rid: 'ri.vertex.main.scenario.s-A', name: 'Scenario A' };
const scenarioB = { rid: 'ri.vertex.main.scenario.s-B', name: 'Scenario B' };
const scenarioImm = {
  rid: 'ri.vertex.main.scenario.s-frozen',
  name: 'Frozen',
  immutable: true,
};

describe('VTX-036 ScenarioPane state factory', () => {
  it('given_no_init_when_create_then_returns_collapsed_empty_pane', () => {
    const state = createScenarioPaneState();
    expect(state.expanded).toBe(false);
    expect(state.caseStudy).toBeNull();
    expect(state.scenarios).toEqual([]);
    expect(state.rows).toEqual([]);
    expect(state.inputOutputColumns).toEqual([]);
  });

  it('given_init_payload_when_create_then_copies_collections_to_prevent_external_mutation', () => {
    const seedScenarios = [scenarioA];
    const seedRows: ScenarioPaneRow[] = [
      { kind: 'model', rid: 'r1', label: 'M1', modelRid: 'ri.fn.model.m1' },
    ];
    const seedCols = [{ key: 'p1', label: 'capacity' }];

    const state = createScenarioPaneState({
      expanded: true,
      caseStudy: cs1,
      scenarios: seedScenarios,
      rows: seedRows,
      inputOutputColumns: seedCols,
    });

    seedScenarios.push(scenarioB);
    seedRows.push({ kind: 'model', rid: 'r2', label: 'M2', modelRid: 'm2' });
    seedCols.push({ key: 'p2', label: 'demand' });

    expect(state.scenarios).toHaveLength(1);
    expect(state.rows).toHaveLength(1);
    expect(state.inputOutputColumns).toHaveLength(1);
  });
});

describe('VTX-036 ScenarioPane expand/collapse', () => {
  it('given_collapsed_pane_when_toggle_then_expands', () => {
    const next = togglePane(createScenarioPaneState());
    expect(next.expanded).toBe(true);
  });

  it('given_expanded_pane_when_toggle_then_collapses', () => {
    const next = togglePane(createScenarioPaneState({ expanded: true }));
    expect(next.expanded).toBe(false);
  });

  it('given_pane_when_setPaneExpanded_then_returns_state_with_expanded_value', () => {
    const a = setPaneExpanded(createScenarioPaneState(), true);
    const b = setPaneExpanded(a, false);
    expect(a.expanded).toBe(true);
    expect(b.expanded).toBe(false);
  });
});

describe('VTX-036 ScenarioPane case study lifecycle', () => {
  it('given_empty_pane_when_setCaseStudy_then_assigns_case_study', () => {
    const next = setCaseStudy(createScenarioPaneState(), cs1);
    expect(next.caseStudy).toEqual(cs1);
    expect(next.scenarios).toEqual([]);
    expect(next.rows).toEqual([]);
  });

  it('given_pane_with_scenarios_and_rows_when_switch_case_study_then_resets_dependent_state', () => {
    let state = setCaseStudy(createScenarioPaneState(), cs1);
    state = addScenario(state, scenarioA);
    state = addActionRow(state, {
      kind: 'action',
      rid: 'a1',
      label: 'AddCapacity',
      actionTypeId: 'add-capacity',
    });
    state = addInputOutputColumn(state, { key: 'c1', label: 'capacity' });

    const next = setCaseStudy(state, cs2);

    expect(next.caseStudy).toEqual(cs2);
    expect(next.scenarios).toEqual([]);
    expect(next.rows).toEqual([]);
    expect(next.inputOutputColumns).toEqual([]);
  });

  it('given_pane_with_case_study_when_setCaseStudy_null_then_clears_everything', () => {
    let state = setCaseStudy(createScenarioPaneState(), cs1);
    state = addScenario(state, scenarioA);
    const next = setCaseStudy(state, null);
    expect(next.caseStudy).toBeNull();
    expect(next.scenarios).toEqual([]);
  });

  it('given_same_case_study_when_setCaseStudy_then_preserves_dependent_state', () => {
    let state = setCaseStudy(createScenarioPaneState(), cs1);
    state = addScenario(state, scenarioA);
    const next = setCaseStudy(state, { ...cs1 });
    expect(next.scenarios).toEqual([scenarioA]);
  });
});

describe('VTX-036 ScenarioPane scenarios', () => {
  it('given_pane_without_case_study_when_addScenario_then_throws', () => {
    expect(() => addScenario(createScenarioPaneState(), scenarioA)).toThrow(
      /case study/i,
    );
  });

  it('given_case_study_when_addScenario_then_appends_to_columns', () => {
    const start = setCaseStudy(createScenarioPaneState(), cs1);
    const a = addScenario(start, scenarioA);
    const b = addScenario(a, scenarioB);
    expect(b.scenarios).toEqual([scenarioA, scenarioB]);
  });

  it('given_existing_scenario_when_addScenario_again_then_idempotent', () => {
    let state = setCaseStudy(createScenarioPaneState(), cs1);
    state = addScenario(state, scenarioA);
    const dup = addScenario(state, scenarioA);
    expect(dup.scenarios).toEqual([scenarioA]);
  });

  it('given_two_scenarios_when_removeScenario_middle_then_remaining_keep_order', () => {
    let state = setCaseStudy(createScenarioPaneState(), cs1);
    state = addScenario(state, scenarioA);
    state = addScenario(state, scenarioB);
    const next = removeScenario(state, scenarioA.rid);
    expect(next.scenarios).toEqual([scenarioB]);
  });

  it('given_unknown_rid_when_removeScenario_then_returns_same_state', () => {
    let state = setCaseStudy(createScenarioPaneState(), cs1);
    state = addScenario(state, scenarioA);
    const next = removeScenario(state, 'ri.does.not.exist');
    expect(next).toBe(state);
  });
});

describe('VTX-036 ScenarioPane rows (models / actions)', () => {
  const seed = () => setCaseStudy(createScenarioPaneState(), cs1);
  const model1 = {
    kind: 'model' as const,
    rid: 'r-m1',
    label: 'DemandModel',
    modelRid: 'ri.fn.model.demand',
  };
  const action1 = {
    kind: 'action' as const,
    rid: 'r-a1',
    label: 'AddCapacity',
    actionTypeId: 'add-capacity',
  };

  it('given_pane_when_addModelRow_then_appends_to_rows', () => {
    const next = addModelRow(seed(), model1);
    expect(next.rows).toEqual([model1]);
  });

  it('given_pane_when_addActionRow_then_appends_to_rows', () => {
    const next = addActionRow(seed(), action1);
    expect(next.rows).toEqual([action1]);
  });

  it('given_duplicate_rid_when_addModelRow_then_idempotent', () => {
    const a = addModelRow(seed(), model1);
    const b = addModelRow(a, { ...model1, label: 'Different' });
    expect(b.rows).toEqual([model1]);
  });

  it('given_mixed_rows_when_removeRow_then_drops_matching_rid', () => {
    let state = addModelRow(seed(), model1);
    state = addActionRow(state, action1);
    const next = removeRow(state, model1.rid);
    expect(next.rows).toEqual([action1]);
  });

  it('given_unknown_rid_when_removeRow_then_returns_same_state', () => {
    const state = addModelRow(seed(), model1);
    const next = removeRow(state, 'r-nope');
    expect(next).toBe(state);
  });
});

describe('VTX-036 ScenarioPane input/output columns', () => {
  const seed = () => setCaseStudy(createScenarioPaneState(), cs1);

  it('given_pane_when_addInputOutputColumn_then_appends', () => {
    const next = addInputOutputColumn(seed(), { key: 'capacity', label: 'Capacity' });
    expect(next.inputOutputColumns).toEqual([
      { key: 'capacity', label: 'Capacity' },
    ]);
  });

  it('given_duplicate_key_when_addInputOutputColumn_then_idempotent', () => {
    const a = addInputOutputColumn(seed(), { key: 'capacity', label: 'Capacity' });
    const b = addInputOutputColumn(a, { key: 'capacity', label: 'Override' });
    expect(b.inputOutputColumns).toEqual([{ key: 'capacity', label: 'Capacity' }]);
  });

  it('given_column_when_removeInputOutputColumn_then_drops_by_key', () => {
    let state = addInputOutputColumn(seed(), { key: 'capacity', label: 'Capacity' });
    state = addInputOutputColumn(state, { key: 'demand', label: 'Demand' });
    const next = removeInputOutputColumn(state, 'capacity');
    expect(next.inputOutputColumns).toEqual([{ key: 'demand', label: 'Demand' }]);
  });

  it('given_unknown_key_when_removeInputOutputColumn_then_returns_same_state', () => {
    const state = addInputOutputColumn(seed(), { key: 'capacity', label: 'Capacity' });
    const next = removeInputOutputColumn(state, 'nope');
    expect(next).toBe(state);
  });
});

describe('VTX-036 ScenarioPane toolbar buttons', () => {
  it('given_empty_pane_when_getToolbarButtons_then_returns_addCaseStudy_only', () => {
    expect(getToolbarButtons(createScenarioPaneState())).toEqual(['addCaseStudy']);
  });

  it('given_case_study_no_scenarios_when_getToolbarButtons_then_returns_full_toolbar', () => {
    const state = setCaseStudy(createScenarioPaneState(), cs1);
    expect(getToolbarButtons(state)).toEqual([
      'addScenario',
      'addAction',
      'addInputOrOutput',
      'run',
    ]);
  });

  it('given_case_study_with_two_scenarios_when_getToolbarButtons_then_still_full_toolbar', () => {
    let state = setCaseStudy(createScenarioPaneState(), cs1);
    state = addScenario(state, scenarioA);
    state = addScenario(state, scenarioB);
    expect(getToolbarButtons(state)).toEqual([
      'addScenario',
      'addAction',
      'addInputOrOutput',
      'run',
    ]);
  });
});

describe('VTX-036 ScenarioPane column headers', () => {
  it('given_no_case_study_when_getColumnHeaders_then_empty', () => {
    expect(getColumnHeaders(createScenarioPaneState())).toEqual([]);
  });

  it('given_case_study_no_scenarios_when_getColumnHeaders_then_baseline_only', () => {
    const state = setCaseStudy(createScenarioPaneState(), cs1);
    expect(getColumnHeaders(state)).toEqual(['Baseline']);
  });

  it('given_case_study_two_scenarios_when_getColumnHeaders_then_baseline_then_scenario_names', () => {
    let state = setCaseStudy(createScenarioPaneState(), cs1);
    state = addScenario(state, scenarioA);
    state = addScenario(state, scenarioB);
    expect(getColumnHeaders(state)).toEqual(['Baseline', 'Scenario A', 'Scenario B']);
  });

  it('given_immutable_scenario_when_getColumnHeaders_then_label_unchanged_for_layout', () => {
    let state = setCaseStudy(createScenarioPaneState(), cs1);
    state = addScenario(state, scenarioImm);
    expect(getColumnHeaders(state)).toEqual(['Baseline', 'Frozen']);
  });
});

describe('VTX-036 ScenarioPane row groups', () => {
  it('given_no_rows_when_getRowGroups_then_empty_groups', () => {
    expect(getRowGroups(createScenarioPaneState())).toEqual({
      models: [],
      actions: [],
    });
  });

  it('given_mixed_rows_when_getRowGroups_then_splits_by_kind_preserving_insertion_order', () => {
    let state = setCaseStudy(createScenarioPaneState(), cs1);
    state = addModelRow(state, {
      kind: 'model',
      rid: 'r-m1',
      label: 'DemandModel',
      modelRid: 'ri.fn.model.demand',
    });
    state = addActionRow(state, {
      kind: 'action',
      rid: 'r-a1',
      label: 'AddCapacity',
      actionTypeId: 'add-capacity',
    });
    state = addModelRow(state, {
      kind: 'model',
      rid: 'r-m2',
      label: 'RouteModel',
      modelRid: 'ri.fn.model.route',
    });

    const groups = getRowGroups(state);
    expect(groups.models.map(r => r.rid)).toEqual(['r-m1', 'r-m2']);
    expect(groups.actions.map(r => r.rid)).toEqual(['r-a1']);
  });
});

describe('VTX-036 ScenarioPane isEmpty', () => {
  it('given_no_case_study_when_isEmpty_then_true', () => {
    expect(isEmpty(createScenarioPaneState())).toBe(true);
  });

  it('given_case_study_when_isEmpty_then_false', () => {
    const state = setCaseStudy(createScenarioPaneState(), cs1);
    expect(isEmpty(state)).toBe(false);
  });
});
