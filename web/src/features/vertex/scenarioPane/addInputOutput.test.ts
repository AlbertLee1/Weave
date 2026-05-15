import { describe, expect, it } from 'vitest';

import {
  buildInputOutputColumnKey,
  buildInputOutputColumns,
  closeAddInputOutputDialog,
  createAddInputOutputDialogState,
  getCheckedParamNames,
  isAllChecked,
  isAllUnchecked,
  openAddInputOutputDialog,
  setAddInputOutputAllChecked,
  setAddInputOutputError,
  setAddInputOutputParamSource,
  setAddInputOutputSubmitting,
  toggleAddInputOutputParam,
  validateAddInputOutput,
  type AddInputOutputParameter,
  type AddInputOutputRowRef,
  type ParameterSource,
} from './addInputOutput';

const scenarioRid = 'ri.vertex.main.scenario.s-1';

const tsSource: ParameterSource = {
  kind: 'time_series',
  rid: 'ri.timeseries.main.ts.throughput',
  label: 'Throughput (ts)',
};

const propSource: ParameterSource = {
  kind: 'object_property',
  rid: 'Airport.capacity',
  label: 'Airport.capacity',
};

const measureSource: ParameterSource = {
  kind: 'measure',
  rid: 'ri.functions.main.measure.total-alerts',
  label: 'totalAlerts',
};

const standardRow: AddInputOutputRowRef = {
  rowRid: 'row-1',
  kind: 'action',
  label: 'Create Flight',
  parameters: [
    {
      name: 'origin',
      direction: 'input',
      required: true,
      candidateSources: [propSource],
    },
    {
      name: 'demand',
      direction: 'input',
      required: false,
      candidateSources: [tsSource, propSource],
    },
    {
      name: 'predictedDelay',
      direction: 'output',
      candidateSources: [measureSource],
    },
  ],
};

const functionBackedRow: AddInputOutputRowRef = {
  rowRid: 'row-2',
  kind: 'action',
  label: 'Predict Delay',
  parameters: [
    {
      name: 'flightCapacity',
      direction: 'input',
      required: true,
      candidateSources: [propSource],
      autoBound: propSource,
    },
    {
      name: 'recentThroughput',
      direction: 'input',
      required: true,
      candidateSources: [tsSource],
      autoBound: tsSource,
    },
  ],
};

function openWith(row: AddInputOutputRowRef) {
  return openAddInputOutputDialog(scenarioRid, row);
}

describe('VTX-039 Add Input or Output dialog state', () => {
  it('given_no_args_when_create_then_returns_blank_state', () => {
    const s = createAddInputOutputDialogState();
    expect(s).toEqual({
      open: false,
      scenarioRid: null,
      row: null,
      selections: {},
      submitting: false,
      error: null,
    });
  });

  it('given_scenarioRid_and_row_when_open_then_dialog_open_with_unchecked_selections', () => {
    const s = openWith(standardRow);
    expect(s.open).toBe(true);
    expect(s.scenarioRid).toBe(scenarioRid);
    expect(s.row).toEqual(standardRow);
    expect(Object.keys(s.selections).sort()).toEqual(
      ['demand', 'origin', 'predictedDelay'],
    );
    expect(s.selections.origin).toEqual({ checked: false, source: null });
    expect(s.selections.demand).toEqual({ checked: false, source: null });
    expect(s.selections.predictedDelay).toEqual({ checked: false, source: null });
  });

  it('given_function_backed_row_when_open_then_autoBound_sources_preset_but_unchecked', () => {
    const s = openWith(functionBackedRow);
    expect(s.selections.flightCapacity).toEqual({
      checked: false,
      source: propSource,
    });
    expect(s.selections.recentThroughput).toEqual({
      checked: false,
      source: tsSource,
    });
  });

  it('given_blank_scenarioRid_when_open_then_throws', () => {
    expect(() => openAddInputOutputDialog('', standardRow)).toThrow(
      /scenario rid/,
    );
    expect(() => openAddInputOutputDialog('   ', standardRow)).toThrow(
      /scenario rid/,
    );
  });

  it('given_open_dialog_when_close_then_returns_blank_state', () => {
    expect(closeAddInputOutputDialog()).toEqual(createAddInputOutputDialogState());
  });

  it('given_state_when_setSubmitting_true_then_clears_error', () => {
    let s = openWith(standardRow);
    s = setAddInputOutputError(s, 'boom');
    s = setAddInputOutputSubmitting(s, true);
    expect(s.submitting).toBe(true);
    expect(s.error).toBeNull();
  });

  it('given_state_with_error_when_setSubmitting_false_then_preserves_error', () => {
    let s = openWith(standardRow);
    s = setAddInputOutputError(s, 'oops');
    s = setAddInputOutputSubmitting(s, false);
    expect(s.submitting).toBe(false);
    expect(s.error).toBe('oops');
  });

  it('given_submitting_state_when_setError_then_clears_submitting', () => {
    let s = openWith(standardRow);
    s = setAddInputOutputSubmitting(s, true);
    s = setAddInputOutputError(s, 'failed');
    expect(s.submitting).toBe(false);
    expect(s.error).toBe('failed');
  });

  it('given_error_when_setError_null_then_just_clears', () => {
    let s = openWith(standardRow);
    s = setAddInputOutputError(s, 'failed');
    s = setAddInputOutputError(s, null);
    expect(s.error).toBeNull();
  });
});

describe('VTX-039 toggleAddInputOutputParam', () => {
  it('given_open_dialog_when_toggleParam_then_flips_checked', () => {
    let s = openWith(standardRow);
    s = toggleAddInputOutputParam(s, 'origin');
    expect(s.selections.origin.checked).toBe(true);
    s = toggleAddInputOutputParam(s, 'origin');
    expect(s.selections.origin.checked).toBe(false);
  });

  it('given_function_backed_param_when_toggle_then_source_remains_autoBound', () => {
    let s = openWith(functionBackedRow);
    s = toggleAddInputOutputParam(s, 'flightCapacity');
    expect(s.selections.flightCapacity).toEqual({
      checked: true,
      source: propSource,
    });
  });

  it('given_unknown_param_when_toggle_then_returns_same_state', () => {
    const s = openWith(standardRow);
    const next = toggleAddInputOutputParam(s, 'missing');
    expect(next).toBe(s);
  });

  it('given_dialog_not_open_when_toggle_then_returns_same_state', () => {
    const s = createAddInputOutputDialogState();
    const next = toggleAddInputOutputParam(s, 'origin');
    expect(next).toBe(s);
  });
});

describe('VTX-039 setAddInputOutputParamSource', () => {
  it('given_param_when_setSource_then_source_stored', () => {
    let s = openWith(standardRow);
    s = setAddInputOutputParamSource(s, 'demand', tsSource);
    expect(s.selections.demand.source).toEqual(tsSource);
    expect(s.selections.demand.checked).toBe(false);
  });

  it('given_param_when_setSource_then_other_params_untouched', () => {
    let s = openWith(standardRow);
    s = setAddInputOutputParamSource(s, 'demand', tsSource);
    expect(s.selections.origin.source).toBeNull();
  });

  it('given_unknown_param_when_setSource_then_returns_same_state', () => {
    const s = openWith(standardRow);
    const next = setAddInputOutputParamSource(s, 'missing', tsSource);
    expect(next).toBe(s);
  });

  it('given_dialog_not_open_when_setSource_then_returns_same_state', () => {
    const s = createAddInputOutputDialogState();
    const next = setAddInputOutputParamSource(s, 'origin', tsSource);
    expect(next).toBe(s);
  });

  it('given_param_with_source_when_setSource_null_then_clears', () => {
    let s = openWith(standardRow);
    s = setAddInputOutputParamSource(s, 'demand', tsSource);
    s = setAddInputOutputParamSource(s, 'demand', null);
    expect(s.selections.demand.source).toBeNull();
  });
});

describe('VTX-039 setAddInputOutputAllChecked', () => {
  it('given_standard_row_when_setAllChecked_true_then_all_params_checked', () => {
    let s = openWith(standardRow);
    s = setAddInputOutputAllChecked(s, true);
    for (const name of Object.keys(s.selections)) {
      expect(s.selections[name].checked).toBe(true);
    }
  });

  it('given_function_backed_row_when_setAllChecked_true_then_autoBound_sources_present', () => {
    let s = openWith(functionBackedRow);
    s = setAddInputOutputAllChecked(s, true);
    expect(s.selections.flightCapacity).toEqual({
      checked: true,
      source: propSource,
    });
    expect(s.selections.recentThroughput).toEqual({
      checked: true,
      source: tsSource,
    });
  });

  it('given_all_checked_when_setAllChecked_false_then_all_unchecked', () => {
    let s = openWith(standardRow);
    s = setAddInputOutputAllChecked(s, true);
    s = setAddInputOutputAllChecked(s, false);
    for (const name of Object.keys(s.selections)) {
      expect(s.selections[name].checked).toBe(false);
    }
  });

  it('given_setAllChecked_false_when_param_has_source_then_source_preserved', () => {
    let s = openWith(standardRow);
    s = setAddInputOutputParamSource(s, 'demand', tsSource);
    s = setAddInputOutputAllChecked(s, false);
    expect(s.selections.demand.source).toEqual(tsSource);
    expect(s.selections.demand.checked).toBe(false);
  });

  it('given_dialog_not_open_when_setAllChecked_then_returns_same_state', () => {
    const s = createAddInputOutputDialogState();
    const next = setAddInputOutputAllChecked(s, true);
    expect(next).toBe(s);
  });
});

describe('VTX-039 derived helpers', () => {
  it('given_2_params_checked_when_getCheckedParamNames_then_returns_in_param_order', () => {
    let s = openWith(standardRow);
    s = toggleAddInputOutputParam(s, 'predictedDelay');
    s = toggleAddInputOutputParam(s, 'origin');
    expect(getCheckedParamNames(s)).toEqual(['origin', 'predictedDelay']);
  });

  it('given_no_open_when_getCheckedParamNames_then_returns_empty', () => {
    const s = createAddInputOutputDialogState();
    expect(getCheckedParamNames(s)).toEqual([]);
  });

  it('given_no_params_checked_when_isAllChecked_then_false', () => {
    const s = openWith(standardRow);
    expect(isAllChecked(s)).toBe(false);
  });

  it('given_all_params_checked_when_isAllChecked_then_true', () => {
    let s = openWith(standardRow);
    s = setAddInputOutputAllChecked(s, true);
    expect(isAllChecked(s)).toBe(true);
  });

  it('given_no_open_when_isAllChecked_then_false', () => {
    expect(isAllChecked(createAddInputOutputDialogState())).toBe(false);
  });

  it('given_no_open_when_isAllUnchecked_then_true', () => {
    expect(isAllUnchecked(createAddInputOutputDialogState())).toBe(true);
  });

  it('given_one_param_checked_when_isAllUnchecked_then_false', () => {
    let s = openWith(standardRow);
    s = toggleAddInputOutputParam(s, 'origin');
    expect(isAllUnchecked(s)).toBe(false);
  });
});

describe('VTX-039 validateAddInputOutput', () => {
  it('given_dialog_not_open_when_validate_then_not_open', () => {
    expect(validateAddInputOutput(createAddInputOutputDialogState())).toEqual({
      valid: false,
      reason: 'not_open',
    });
  });

  it('given_open_no_checks_when_validate_then_no_params_checked', () => {
    const s = openWith(standardRow);
    expect(validateAddInputOutput(s)).toEqual({
      valid: false,
      reason: 'no_params_checked',
    });
  });

  it('given_checked_with_no_source_when_validate_then_unbound_param', () => {
    let s = openWith(standardRow);
    s = toggleAddInputOutputParam(s, 'origin');
    expect(validateAddInputOutput(s)).toEqual({
      valid: false,
      reason: 'unbound_param',
      param: 'origin',
    });
  });

  it('given_checked_with_source_when_validate_then_valid', () => {
    let s = openWith(standardRow);
    s = setAddInputOutputParamSource(s, 'origin', propSource);
    s = toggleAddInputOutputParam(s, 'origin');
    expect(validateAddInputOutput(s)).toEqual({ valid: true });
  });

  it('given_3_function_backed_params_when_setAllChecked_then_validate_passes', () => {
    let s = openWith(functionBackedRow);
    s = setAddInputOutputAllChecked(s, true);
    expect(validateAddInputOutput(s)).toEqual({ valid: true });
  });

  it('given_two_checked_one_unbound_when_validate_then_unbound_param_first', () => {
    let s = openWith(standardRow);
    s = setAddInputOutputParamSource(s, 'origin', propSource);
    s = toggleAddInputOutputParam(s, 'origin');
    s = toggleAddInputOutputParam(s, 'demand');
    const v = validateAddInputOutput(s);
    expect(v).toEqual({
      valid: false,
      reason: 'unbound_param',
      param: 'demand',
    });
  });
});

describe('VTX-039 column builders', () => {
  it('given_rowRid_and_paramName_when_buildColumnKey_then_returns_composite_key', () => {
    expect(buildInputOutputColumnKey('row-1', 'origin')).toBe('row-1::origin');
  });

  it('given_invalid_state_when_buildColumns_then_throws', () => {
    const s = openWith(standardRow);
    expect(() => buildInputOutputColumns(s)).toThrow();
  });

  it('given_no_open_when_buildColumns_then_throws', () => {
    expect(() => buildInputOutputColumns(createAddInputOutputDialogState())).toThrow();
  });

  it('given_valid_state_with_3_checked_when_buildColumns_then_returns_3_columns_in_param_order', () => {
    let s = openWith(standardRow);
    s = setAddInputOutputParamSource(s, 'origin', propSource);
    s = setAddInputOutputParamSource(s, 'demand', tsSource);
    s = setAddInputOutputParamSource(s, 'predictedDelay', measureSource);
    s = setAddInputOutputAllChecked(s, true);
    const cols = buildInputOutputColumns(s);
    expect(cols).toEqual([
      { key: 'row-1::origin', label: 'origin' },
      { key: 'row-1::demand', label: 'demand' },
      { key: 'row-1::predictedDelay', label: 'predictedDelay' },
    ]);
  });

  it('given_param_with_displayName_when_buildColumns_then_label_uses_displayName', () => {
    const rowWithDisplay: AddInputOutputRowRef = {
      ...standardRow,
      parameters: [
        {
          name: 'origin',
          displayName: 'Origin Airport',
          direction: 'input',
          required: true,
          candidateSources: [propSource],
        },
      ],
    };
    let s = openAddInputOutputDialog(scenarioRid, rowWithDisplay);
    s = setAddInputOutputParamSource(s, 'origin', propSource);
    s = toggleAddInputOutputParam(s, 'origin');
    expect(buildInputOutputColumns(s)).toEqual([
      { key: 'row-1::origin', label: 'Origin Airport' },
    ]);
  });

  it('given_only_subset_checked_when_buildColumns_then_returns_only_checked', () => {
    let s = openWith(standardRow);
    s = setAddInputOutputParamSource(s, 'demand', tsSource);
    s = toggleAddInputOutputParam(s, 'demand');
    const cols = buildInputOutputColumns(s);
    expect(cols).toEqual([{ key: 'row-1::demand', label: 'demand' }]);
  });
});

describe('VTX-039 supports parameter source kinds', () => {
  // Spec lists "time series / object property / measure" as candidate source
  // kinds. Verify each kind round-trips through the dialog state without loss.
  it.each<ParameterSource>([tsSource, propSource, measureSource])(
    'given_source_kind_$kind_when_set_then_round_trips',
    source => {
      let s = openWith(standardRow);
      s = setAddInputOutputParamSource(s, 'demand', source);
      s = toggleAddInputOutputParam(s, 'demand');
      const cols = buildInputOutputColumns(s);
      expect(cols).toEqual([{ key: 'row-1::demand', label: 'demand' }]);
      expect(s.selections.demand.source).toEqual(source);
    },
  );
});

describe('VTX-039 Function-backed Action auto-bind', () => {
  // BDD: "Given Function-backed Action When 加 input Then 自动关联到对象属性 / 时序点"
  it('given_function_backed_when_open_then_each_param_has_autoBound_source', () => {
    const s = openWith(functionBackedRow);
    for (const param of functionBackedRow.parameters) {
      expect(s.selections[param.name].source).toEqual(param.autoBound);
    }
  });

  it('given_function_backed_when_setAllChecked_then_buildColumns_valid_without_manual_binding', () => {
    let s = openWith(functionBackedRow);
    s = setAddInputOutputAllChecked(s, true);
    const cols = buildInputOutputColumns(s);
    expect(cols).toEqual([
      { key: 'row-2::flightCapacity', label: 'flightCapacity' },
      { key: 'row-2::recentThroughput', label: 'recentThroughput' },
    ]);
  });
});

describe('VTX-039 BDD acceptance: 3 params + Add all + Apply', () => {
  // "Given 用户勾选 3 个参数 + Add all parameters When Apply Then 在 Pane 表中加 3 列"
  it('given_3_params_when_setAllChecked_and_buildColumns_then_3_columns', () => {
    let s = openWith(standardRow);
    // user binds the necessary sources first
    s = setAddInputOutputParamSource(s, 'origin', propSource);
    s = setAddInputOutputParamSource(s, 'demand', tsSource);
    s = setAddInputOutputParamSource(s, 'predictedDelay', measureSource);
    // user clicks "Add all parameters"
    s = setAddInputOutputAllChecked(s, true);
    expect(getCheckedParamNames(s)).toHaveLength(3);
    expect(validateAddInputOutput(s)).toEqual({ valid: true });
    // Apply -> Pane adds 3 columns
    expect(buildInputOutputColumns(s)).toHaveLength(3);
  });
});

// Ensure exported parameter type is usable by React layer for parameter list
// rendering (BDD: "列出 parameters + 候选源").
describe('VTX-039 parameter list shape', () => {
  it('exposes_parameter_list_with_candidate_sources_for_rendering', () => {
    const params: AddInputOutputParameter[] = standardRow.parameters;
    expect(params[0].name).toBe('origin');
    expect(params[1].candidateSources.length).toBeGreaterThan(0);
    expect(params[2].direction).toBe('output');
  });
});
