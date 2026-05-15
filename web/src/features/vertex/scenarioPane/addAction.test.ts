import { describe, expect, it } from 'vitest';

import {
  MAX_ACTIONS_MESSAGE,
  MAX_ACTIONS_PER_SCENARIO,
  ScenarioActionCapacityError,
  assertScenarioActionCapacity,
  buildAddActionRow,
  closeAddActionDialog,
  createAddActionDialogState,
  filterPublishedActions,
  getRequiredParamNames,
  getSelectedAction,
  isActionPublished,
  isFunctionBackedAction,
  isScenarioAtActionCapacity,
  openAddActionDialog,
  setAddActionError,
  setAddActionList,
  setAddActionLoading,
  setAddActionParam,
  setAddActionSelected,
  setAddActionSubmitting,
  validateAddAction,
  type AddActionActionType,
  type AddActionDialogState,
} from './addAction';

const scenarioRid = 'ri.vertex.main.scenario.s-1';

const standardAction: AddActionActionType = {
  rid: 'ri.action.main.action-type.create-flight',
  apiName: 'createFlight',
  displayName: 'Create Flight',
  status: 'ACTIVE',
  parameters: {
    origin: { required: true },
    destination: { required: true },
    note: { required: false },
  },
};

const functionAction: AddActionActionType = {
  rid: 'ri.action.main.action-type.predict-delay',
  apiName: 'predictDelay',
  displayName: 'Predict Delay',
  status: 'ENDORSED',
  kind: 'function_backed',
  functionRid: 'ri.functions.main.fn.predict-delay',
  parameters: {
    flightRid: { required: true },
  },
};

const deprecatedAction: AddActionActionType = {
  rid: 'ri.action.main.action-type.legacy',
  apiName: 'legacy',
  displayName: 'Legacy',
  status: 'DEPRECATED',
};

function stateWithActions(actions: AddActionActionType[]): AddActionDialogState {
  return setAddActionList(openAddActionDialog(scenarioRid), actions);
}

describe('VTX-038 Add Action dialog state', () => {
  it('given_no_args_when_create_then_returns_blank_state', () => {
    const s = createAddActionDialogState();
    expect(s).toEqual({
      open: false,
      scenarioRid: null,
      actions: [],
      loading: false,
      selectedActionRid: null,
      paramValues: {},
      submitting: false,
      error: null,
    });
  });

  it('given_scenarioRid_when_open_then_dialog_open_with_rid', () => {
    const s = openAddActionDialog(scenarioRid);
    expect(s.open).toBe(true);
    expect(s.scenarioRid).toBe(scenarioRid);
    expect(s.actions).toEqual([]);
    expect(s.selectedActionRid).toBeNull();
    expect(s.paramValues).toEqual({});
  });

  it('given_blank_rid_when_open_then_throws', () => {
    expect(() => openAddActionDialog('')).toThrow(/scenario rid/);
    expect(() => openAddActionDialog('   ')).toThrow(/scenario rid/);
  });

  it('given_open_dialog_when_close_then_returns_blank_state', () => {
    const closed = closeAddActionDialog();
    expect(closed).toEqual(createAddActionDialogState());
  });

  it('given_state_when_setLoading_true_then_marks_loading', () => {
    const s = openAddActionDialog(scenarioRid);
    expect(setAddActionLoading(s, true).loading).toBe(true);
  });
});

describe('VTX-038 Add Action list management', () => {
  it('given_actions_when_setList_then_actions_stored_and_loading_cleared', () => {
    let s = setAddActionLoading(openAddActionDialog(scenarioRid), true);
    s = setAddActionList(s, [standardAction, functionAction]);
    expect(s.actions).toHaveLength(2);
    expect(s.loading).toBe(false);
  });

  it('given_selection_when_setList_keeping_selection_then_selection_preserved', () => {
    let s = stateWithActions([standardAction, functionAction]);
    s = setAddActionSelected(s, standardAction.rid);
    s = setAddActionParam(s, 'origin', 'JFK');
    s = setAddActionList(s, [standardAction, deprecatedAction]);
    expect(s.selectedActionRid).toBe(standardAction.rid);
    expect(s.paramValues).toEqual({ origin: 'JFK' });
  });

  it('given_selection_when_setList_dropping_selection_then_selection_cleared', () => {
    let s = stateWithActions([standardAction, functionAction]);
    s = setAddActionSelected(s, standardAction.rid);
    s = setAddActionParam(s, 'origin', 'JFK');
    s = setAddActionList(s, [functionAction]);
    expect(s.selectedActionRid).toBeNull();
    expect(s.paramValues).toEqual({});
  });
});

describe('VTX-038 Add Action selection + params', () => {
  it('given_no_selection_when_setSelected_then_selects', () => {
    const s = stateWithActions([standardAction]);
    const next = setAddActionSelected(s, standardAction.rid);
    expect(next.selectedActionRid).toBe(standardAction.rid);
  });

  it('given_same_selection_when_setSelected_then_returns_same_state_ref', () => {
    let s = stateWithActions([standardAction]);
    s = setAddActionSelected(s, standardAction.rid);
    const next = setAddActionSelected(s, standardAction.rid);
    expect(next).toBe(s);
  });

  it('given_existing_selection_when_setSelected_other_then_params_reset', () => {
    let s = stateWithActions([standardAction, functionAction]);
    s = setAddActionSelected(s, standardAction.rid);
    s = setAddActionParam(s, 'origin', 'JFK');
    const next = setAddActionSelected(s, functionAction.rid);
    expect(next.selectedActionRid).toBe(functionAction.rid);
    expect(next.paramValues).toEqual({});
  });

  it('given_setSelected_null_then_clears_selection_and_params', () => {
    let s = stateWithActions([standardAction]);
    s = setAddActionSelected(s, standardAction.rid);
    s = setAddActionParam(s, 'origin', 'JFK');
    const next = setAddActionSelected(s, null);
    expect(next.selectedActionRid).toBeNull();
    expect(next.paramValues).toEqual({});
  });

  it('given_selection_when_setParam_then_value_stored', () => {
    let s = setAddActionSelected(stateWithActions([standardAction]), standardAction.rid);
    s = setAddActionParam(s, 'origin', 'JFK');
    s = setAddActionParam(s, 'destination', 'LAX');
    expect(s.paramValues).toEqual({ origin: 'JFK', destination: 'LAX' });
  });

  it('given_existing_param_when_setParam_again_then_value_replaced', () => {
    let s = setAddActionSelected(stateWithActions([standardAction]), standardAction.rid);
    s = setAddActionParam(s, 'origin', 'JFK');
    s = setAddActionParam(s, 'origin', 'BOS');
    expect(s.paramValues.origin).toBe('BOS');
  });
});

describe('VTX-038 Add Action submitting + error', () => {
  it('given_error_when_setSubmitting_true_then_error_cleared', () => {
    let s = openAddActionDialog(scenarioRid);
    s = setAddActionError(s, 'previous');
    const next = setAddActionSubmitting(s, true);
    expect(next.submitting).toBe(true);
    expect(next.error).toBeNull();
  });

  it('given_submitting_when_setSubmitting_false_preserves_error', () => {
    let s = openAddActionDialog(scenarioRid);
    s = setAddActionSubmitting(s, true);
    s = setAddActionError(s, 'server 500');
    expect(s.submitting).toBe(false);
    const next = setAddActionSubmitting(s, false);
    expect(next.submitting).toBe(false);
    expect(next.error).toBe('server 500');
  });

  it('given_submitting_when_setError_then_submitting_cleared', () => {
    let s = openAddActionDialog(scenarioRid);
    s = setAddActionSubmitting(s, true);
    const next = setAddActionError(s, 'oops');
    expect(next.error).toBe('oops');
    expect(next.submitting).toBe(false);
  });

  it('given_error_when_setError_null_then_error_cleared', () => {
    let s = openAddActionDialog(scenarioRid);
    s = setAddActionError(s, 'oops');
    const next = setAddActionError(s, null);
    expect(next.error).toBeNull();
  });
});

describe('VTX-038 published + function-backed predicates', () => {
  it('given_ACTIVE_status_when_isActionPublished_then_true', () => {
    expect(isActionPublished({ ...standardAction, status: 'ACTIVE' })).toBe(true);
  });

  it('given_ENDORSED_status_when_isActionPublished_then_true', () => {
    expect(isActionPublished({ ...standardAction, status: 'ENDORSED' })).toBe(true);
  });

  it('given_EXPERIMENTAL_status_when_isActionPublished_then_false', () => {
    expect(isActionPublished({ ...standardAction, status: 'EXPERIMENTAL' })).toBe(false);
  });

  it('given_DEPRECATED_status_when_isActionPublished_then_false', () => {
    expect(isActionPublished({ ...standardAction, status: 'DEPRECATED' })).toBe(false);
  });

  it('given_function_backed_when_isFunctionBackedAction_then_true', () => {
    expect(isFunctionBackedAction(functionAction)).toBe(true);
  });

  it('given_undefined_kind_when_isFunctionBackedAction_then_false', () => {
    expect(isFunctionBackedAction(standardAction)).toBe(false);
  });

  it('given_kind_standard_when_isFunctionBackedAction_then_false', () => {
    expect(isFunctionBackedAction({ ...standardAction, kind: 'standard' })).toBe(false);
  });

  it('given_mixed_actions_when_filterPublished_then_keeps_active_and_endorsed_including_function_backed', () => {
    const out = filterPublishedActions([
      standardAction,
      functionAction,
      deprecatedAction,
      { ...standardAction, rid: 'experimental-rid', status: 'EXPERIMENTAL' },
    ]);
    const rids = out.map(a => a.rid);
    expect(rids).toEqual([standardAction.rid, functionAction.rid]);
  });
});

describe('VTX-038 getSelectedAction + getRequiredParamNames', () => {
  it('given_no_selection_then_getSelectedAction_returns_null', () => {
    expect(getSelectedAction(stateWithActions([standardAction]))).toBeNull();
  });

  it('given_unknown_selection_then_getSelectedAction_returns_null', () => {
    let s = stateWithActions([standardAction]);
    s = { ...s, selectedActionRid: 'ri.action.unknown' };
    expect(getSelectedAction(s)).toBeNull();
  });

  it('given_valid_selection_then_getSelectedAction_returns_action', () => {
    let s = stateWithActions([standardAction, functionAction]);
    s = setAddActionSelected(s, functionAction.rid);
    expect(getSelectedAction(s)).toEqual(functionAction);
  });

  it('given_action_without_params_then_getRequiredParamNames_returns_empty', () => {
    expect(getRequiredParamNames(deprecatedAction)).toEqual([]);
  });

  it('given_action_with_required_and_optional_then_returns_only_required', () => {
    const required = getRequiredParamNames(standardAction).sort();
    expect(required).toEqual(['destination', 'origin']);
  });

  it('given_action_with_only_optional_then_returns_empty', () => {
    expect(
      getRequiredParamNames({ ...standardAction, parameters: { x: { required: false } } }),
    ).toEqual([]);
  });
});

describe('VTX-038 validateAddAction', () => {
  it('given_no_selection_then_valid_false_no_selection', () => {
    expect(validateAddAction(stateWithActions([standardAction]))).toEqual({
      valid: false,
      reason: 'no_selection',
    });
  });

  it('given_selection_not_in_list_then_valid_false_unknown_selection', () => {
    let s = stateWithActions([standardAction]);
    s = { ...s, selectedActionRid: 'ri.action.unknown' };
    expect(validateAddAction(s)).toEqual({
      valid: false,
      reason: 'unknown_selection',
    });
  });

  it('given_required_param_blank_then_valid_false_missing_required_param', () => {
    let s = setAddActionSelected(stateWithActions([standardAction]), standardAction.rid);
    s = setAddActionParam(s, 'origin', 'JFK');
    const res = validateAddAction(s);
    expect(res).toEqual({
      valid: false,
      reason: 'missing_required_param',
      param: 'destination',
    });
  });

  it('given_required_param_only_whitespace_then_missing_required_param', () => {
    let s = setAddActionSelected(stateWithActions([standardAction]), standardAction.rid);
    s = setAddActionParam(s, 'origin', 'JFK');
    s = setAddActionParam(s, 'destination', '   ');
    expect(validateAddAction(s)).toEqual({
      valid: false,
      reason: 'missing_required_param',
      param: 'destination',
    });
  });

  it('given_all_required_filled_then_valid_true', () => {
    let s = setAddActionSelected(stateWithActions([standardAction]), standardAction.rid);
    s = setAddActionParam(s, 'origin', 'JFK');
    s = setAddActionParam(s, 'destination', 'LAX');
    expect(validateAddAction(s)).toEqual({ valid: true });
  });
});

describe('VTX-038 scenario action capacity', () => {
  it('given_capacity_constant_equals_50', () => {
    expect(MAX_ACTIONS_PER_SCENARIO).toBe(50);
  });

  it('given_message_constant_matches_spec', () => {
    expect(MAX_ACTIONS_MESSAGE).toBe('Max 50 actions per scenario');
  });

  it('given_49_actions_when_isAtCapacity_then_false', () => {
    expect(isScenarioAtActionCapacity(49)).toBe(false);
  });

  it('given_50_actions_when_isAtCapacity_then_true', () => {
    expect(isScenarioAtActionCapacity(50)).toBe(true);
  });

  it('given_under_capacity_when_assert_then_no_throw', () => {
    expect(() => assertScenarioActionCapacity(0)).not.toThrow();
    expect(() => assertScenarioActionCapacity(49)).not.toThrow();
  });

  it('given_at_capacity_when_assert_then_throws_ScenarioActionCapacityError_with_message_and_limit', () => {
    try {
      assertScenarioActionCapacity(50);
      throw new Error('expected throw');
    } catch (err) {
      expect(err).toBeInstanceOf(ScenarioActionCapacityError);
      expect((err as ScenarioActionCapacityError).message).toBe(MAX_ACTIONS_MESSAGE);
      expect((err as ScenarioActionCapacityError).limit).toBe(MAX_ACTIONS_PER_SCENARIO);
      expect((err as ScenarioActionCapacityError).name).toBe('ScenarioActionCapacityError');
    }
  });

  it('given_above_capacity_when_assert_then_throws', () => {
    expect(() => assertScenarioActionCapacity(51)).toThrow(MAX_ACTIONS_MESSAGE);
  });
});

describe('VTX-038 buildAddActionRow', () => {
  it('given_valid_selection_when_build_then_returns_action_row', () => {
    let s = setAddActionSelected(stateWithActions([standardAction]), standardAction.rid);
    s = setAddActionParam(s, 'origin', 'JFK');
    s = setAddActionParam(s, 'destination', 'LAX');
    const row = buildAddActionRow({ state: s, rid: 'row-1' });
    expect(row).toEqual({
      kind: 'action',
      rid: 'row-1',
      label: standardAction.displayName,
      actionTypeId: standardAction.rid,
    });
  });

  it('given_no_selection_when_build_then_throws', () => {
    const s = stateWithActions([standardAction]);
    expect(() => buildAddActionRow({ state: s, rid: 'row-1' })).toThrow(/selected action/);
  });

  it('given_unknown_selection_when_build_then_throws', () => {
    let s = stateWithActions([standardAction]);
    s = { ...s, selectedActionRid: 'ri.action.unknown' };
    expect(() => buildAddActionRow({ state: s, rid: 'row-1' })).toThrow(/selected action/);
  });

  it('given_blank_rid_when_build_then_throws', () => {
    let s = setAddActionSelected(stateWithActions([standardAction]), standardAction.rid);
    s = setAddActionParam(s, 'origin', 'JFK');
    s = setAddActionParam(s, 'destination', 'LAX');
    expect(() => buildAddActionRow({ state: s, rid: '' })).toThrow(/row rid/);
    expect(() => buildAddActionRow({ state: s, rid: '   ' })).toThrow(/row rid/);
  });
});
