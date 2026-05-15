import { describe, expect, it } from 'vitest';

import {
  MAX_MODELS_MESSAGE,
  MAX_MODELS_PER_SCENARIO,
  ScenarioModelCapacityError,
  assertScenarioModelCapacity,
  buildAddModelRow,
  closeAddModelDialog,
  createAddModelDialogState,
  filterLiveModels,
  getAvailableConfigVersions,
  getAvailableModelVersions,
  getSelectedModel,
  isLiveModel,
  isScenarioAtModelCapacity,
  openAddModelDialog,
  setAddModelConfigVersion,
  setAddModelError,
  setAddModelList,
  setAddModelLoading,
  setAddModelSelected,
  setAddModelSubmitting,
  setAddModelVersion,
  validateAddModel,
  type AddModelDialogState,
  type AddModelLiveModel,
} from './addModel';

const scenarioRid = 'ri.vertex.main.scenario.s-1';

const demandModel: AddModelLiveModel = {
  rid: 'ri.vertex.main.model-deployment.demand',
  name: 'DemandModel',
  kind: 'live',
  modelVersions: ['v1.0', 'v1.1', 'v2.0'],
  configVersions: ['cfg-baseline', 'cfg-aggressive'],
};

const supplyModel: AddModelLiveModel = {
  rid: 'ri.vertex.main.model-deployment.supply',
  name: 'SupplyModel',
  kind: 'live',
  modelVersions: ['v0.9'],
  configVersions: ['cfg-default'],
};

const standardModel: AddModelLiveModel = {
  rid: 'ri.vertex.main.model-deployment.legacy',
  name: 'LegacyHeuristic',
  kind: 'standard',
  modelVersions: ['v1.0'],
  configVersions: ['cfg-default'],
};

const noVersionModel: AddModelLiveModel = {
  rid: 'ri.vertex.main.model-deployment.empty',
  name: 'EmptyModel',
  kind: 'live',
  modelVersions: [],
  configVersions: [],
};

describe('VTX-054 Add Model dialog state', () => {
  it('given_no_args_when_create_then_returns_blank_state', () => {
    const s = createAddModelDialogState();
    expect(s).toEqual({
      open: false,
      scenarioRid: null,
      models: [],
      loading: false,
      selectedModelRid: null,
      selectedModelVersion: null,
      selectedConfigVersion: null,
      submitting: false,
      error: null,
    });
  });

  it('given_scenarioRid_when_open_then_dialog_open_with_rid', () => {
    const s = openAddModelDialog(scenarioRid);
    expect(s.open).toBe(true);
    expect(s.scenarioRid).toBe(scenarioRid);
    expect(s.models).toEqual([]);
    expect(s.selectedModelRid).toBeNull();
    expect(s.selectedModelVersion).toBeNull();
    expect(s.selectedConfigVersion).toBeNull();
  });

  it('given_blank_rid_when_open_then_throws', () => {
    expect(() => openAddModelDialog('')).toThrow(/scenario rid/);
    expect(() => openAddModelDialog('   ')).toThrow(/scenario rid/);
  });

  it('given_open_dialog_when_close_then_returns_blank_state', () => {
    const s = openAddModelDialog(scenarioRid);
    expect(closeAddModelDialog()).toEqual(createAddModelDialogState());
    expect(s.open).toBe(true);
  });

  it('given_state_when_setLoading_true_then_marks_loading', () => {
    const s = setAddModelLoading(createAddModelDialogState(), true);
    expect(s.loading).toBe(true);
  });
});

describe('VTX-054 Add Model list management', () => {
  it('given_models_when_setList_then_models_stored_and_loading_cleared', () => {
    const seed = setAddModelLoading(openAddModelDialog(scenarioRid), true);
    const next = setAddModelList(seed, [demandModel, supplyModel]);
    expect(next.models).toEqual([demandModel, supplyModel]);
    expect(next.loading).toBe(false);
  });

  it('given_selection_when_setList_keeping_selection_and_versions_then_preserved', () => {
    let s = setAddModelList(openAddModelDialog(scenarioRid), [demandModel, supplyModel]);
    s = setAddModelSelected(s, demandModel.rid);
    s = setAddModelVersion(s, 'v1.1');
    s = setAddModelConfigVersion(s, 'cfg-baseline');
    const next = setAddModelList(s, [demandModel, supplyModel]);
    expect(next.selectedModelRid).toBe(demandModel.rid);
    expect(next.selectedModelVersion).toBe('v1.1');
    expect(next.selectedConfigVersion).toBe('cfg-baseline');
  });

  it('given_selection_when_setList_dropping_selection_then_selection_and_versions_cleared', () => {
    let s = setAddModelList(openAddModelDialog(scenarioRid), [demandModel]);
    s = setAddModelSelected(s, demandModel.rid);
    s = setAddModelVersion(s, 'v1.1');
    s = setAddModelConfigVersion(s, 'cfg-baseline');
    const next = setAddModelList(s, [supplyModel]);
    expect(next.selectedModelRid).toBeNull();
    expect(next.selectedModelVersion).toBeNull();
    expect(next.selectedConfigVersion).toBeNull();
  });

  it('given_selection_when_setList_with_model_missing_old_versions_then_versions_cleared_but_selection_kept', () => {
    let s = setAddModelList(openAddModelDialog(scenarioRid), [demandModel]);
    s = setAddModelSelected(s, demandModel.rid);
    s = setAddModelVersion(s, 'v1.1');
    s = setAddModelConfigVersion(s, 'cfg-baseline');
    const updated: AddModelLiveModel = {
      ...demandModel,
      modelVersions: ['v3.0'],
      configVersions: ['cfg-strict'],
    };
    const next = setAddModelList(s, [updated]);
    expect(next.selectedModelRid).toBe(demandModel.rid);
    expect(next.selectedModelVersion).toBeNull();
    expect(next.selectedConfigVersion).toBeNull();
  });
});

describe('VTX-054 Add Model selection + versions', () => {
  const baseSeed = () =>
    setAddModelList(openAddModelDialog(scenarioRid), [demandModel, supplyModel]);

  it('given_no_selection_when_setSelected_then_selects_and_clears_versions', () => {
    let s = baseSeed();
    s = setAddModelVersion(s, 'leftover'); // simulate stale state shouldn't happen but defensively reset
    const next = setAddModelSelected(s, demandModel.rid);
    expect(next.selectedModelRid).toBe(demandModel.rid);
    expect(next.selectedModelVersion).toBeNull();
    expect(next.selectedConfigVersion).toBeNull();
  });

  it('given_same_selection_when_setSelected_then_returns_same_state_ref', () => {
    let s = baseSeed();
    s = setAddModelSelected(s, demandModel.rid);
    const next = setAddModelSelected(s, demandModel.rid);
    expect(next).toBe(s);
  });

  it('given_existing_selection_when_setSelected_other_then_versions_reset', () => {
    let s = baseSeed();
    s = setAddModelSelected(s, demandModel.rid);
    s = setAddModelVersion(s, 'v1.1');
    s = setAddModelConfigVersion(s, 'cfg-baseline');
    const next = setAddModelSelected(s, supplyModel.rid);
    expect(next.selectedModelRid).toBe(supplyModel.rid);
    expect(next.selectedModelVersion).toBeNull();
    expect(next.selectedConfigVersion).toBeNull();
  });

  it('given_setSelected_null_then_clears_selection_and_versions', () => {
    let s = baseSeed();
    s = setAddModelSelected(s, demandModel.rid);
    s = setAddModelVersion(s, 'v1.1');
    const next = setAddModelSelected(s, null);
    expect(next.selectedModelRid).toBeNull();
    expect(next.selectedModelVersion).toBeNull();
    expect(next.selectedConfigVersion).toBeNull();
  });

  it('given_selection_when_setVersion_then_value_stored', () => {
    let s = baseSeed();
    s = setAddModelSelected(s, demandModel.rid);
    s = setAddModelVersion(s, 'v2.0');
    expect(s.selectedModelVersion).toBe('v2.0');
  });

  it('given_selection_when_setConfigVersion_then_value_stored', () => {
    let s = baseSeed();
    s = setAddModelSelected(s, demandModel.rid);
    s = setAddModelConfigVersion(s, 'cfg-aggressive');
    expect(s.selectedConfigVersion).toBe('cfg-aggressive');
  });

  it('given_existing_versions_when_setVersion_again_then_value_replaced', () => {
    let s = baseSeed();
    s = setAddModelSelected(s, demandModel.rid);
    s = setAddModelVersion(s, 'v1.0');
    s = setAddModelVersion(s, 'v2.0');
    expect(s.selectedModelVersion).toBe('v2.0');
  });
});

describe('VTX-054 Add Model submitting + error', () => {
  it('given_error_when_setSubmitting_true_then_error_cleared', () => {
    let s = setAddModelError(createAddModelDialogState(), 'Old');
    expect(s.error).toBe('Old');
    s = setAddModelSubmitting(s, true);
    expect(s.submitting).toBe(true);
    expect(s.error).toBeNull();
  });

  it('given_submitting_when_setSubmitting_false_preserves_error', () => {
    let s = setAddModelSubmitting(createAddModelDialogState(), true);
    s = setAddModelError(s, 'Boom');
    s = setAddModelSubmitting(s, false);
    expect(s.submitting).toBe(false);
    expect(s.error).toBe('Boom');
  });

  it('given_submitting_when_setError_then_submitting_cleared', () => {
    let s = setAddModelSubmitting(createAddModelDialogState(), true);
    s = setAddModelError(s, 'Boom');
    expect(s.submitting).toBe(false);
    expect(s.error).toBe('Boom');
  });

  it('given_error_when_setError_null_then_error_cleared', () => {
    let s = setAddModelError(createAddModelDialogState(), 'Old');
    s = setAddModelError(s, null);
    expect(s.error).toBeNull();
  });
});

describe('VTX-054 live model predicates + filter', () => {
  it('given_kind_live_when_isLiveModel_then_true', () => {
    expect(isLiveModel(demandModel)).toBe(true);
  });

  it('given_kind_standard_when_isLiveModel_then_false', () => {
    expect(isLiveModel(standardModel)).toBe(false);
  });

  it('given_undefined_kind_when_isLiveModel_then_false', () => {
    const m: AddModelLiveModel = { ...demandModel, kind: undefined };
    expect(isLiveModel(m)).toBe(false);
  });

  it('given_mixed_models_when_filterLiveModels_then_only_live_kept', () => {
    const out = filterLiveModels([demandModel, standardModel, supplyModel]);
    expect(out).toEqual([demandModel, supplyModel]);
  });
});

describe('VTX-054 getSelectedModel + version helpers', () => {
  const baseSeed = () =>
    setAddModelList(openAddModelDialog(scenarioRid), [demandModel, supplyModel]);

  it('given_no_selection_then_getSelectedModel_returns_null', () => {
    expect(getSelectedModel(baseSeed())).toBeNull();
  });

  it('given_unknown_selection_then_getSelectedModel_returns_null', () => {
    const s: AddModelDialogState = { ...baseSeed(), selectedModelRid: 'ri.unknown' };
    expect(getSelectedModel(s)).toBeNull();
  });

  it('given_valid_selection_then_getSelectedModel_returns_model', () => {
    const s = setAddModelSelected(baseSeed(), demandModel.rid);
    expect(getSelectedModel(s)).toEqual(demandModel);
  });

  it('given_no_selection_then_getAvailableModelVersions_returns_empty', () => {
    expect(getAvailableModelVersions(baseSeed())).toEqual([]);
  });

  it('given_valid_selection_then_getAvailableModelVersions_returns_model_versions', () => {
    const s = setAddModelSelected(baseSeed(), demandModel.rid);
    expect(getAvailableModelVersions(s)).toEqual(['v1.0', 'v1.1', 'v2.0']);
  });

  it('given_no_selection_then_getAvailableConfigVersions_returns_empty', () => {
    expect(getAvailableConfigVersions(baseSeed())).toEqual([]);
  });

  it('given_valid_selection_then_getAvailableConfigVersions_returns_config_versions', () => {
    const s = setAddModelSelected(baseSeed(), demandModel.rid);
    expect(getAvailableConfigVersions(s)).toEqual(['cfg-baseline', 'cfg-aggressive']);
  });
});

describe('VTX-054 validateAddModel', () => {
  const baseSeed = () =>
    setAddModelList(openAddModelDialog(scenarioRid), [demandModel, supplyModel, noVersionModel]);

  it('given_no_selection_then_validation_no_selection', () => {
    expect(validateAddModel(baseSeed())).toEqual({ valid: false, reason: 'no_selection' });
  });

  it('given_unknown_selection_then_validation_unknown_selection', () => {
    const s: AddModelDialogState = { ...baseSeed(), selectedModelRid: 'ri.ghost' };
    expect(validateAddModel(s)).toEqual({ valid: false, reason: 'unknown_selection' });
  });

  it('given_model_with_no_versions_then_validation_no_versions_available', () => {
    const s = setAddModelSelected(baseSeed(), noVersionModel.rid);
    expect(validateAddModel(s)).toEqual({ valid: false, reason: 'no_versions_available' });
  });

  it('given_selection_without_modelVersion_then_missing_model_version', () => {
    const s = setAddModelSelected(baseSeed(), demandModel.rid);
    expect(validateAddModel(s)).toEqual({ valid: false, reason: 'missing_model_version' });
  });

  it('given_modelVersion_without_configVersion_then_missing_config_version', () => {
    let s = setAddModelSelected(baseSeed(), demandModel.rid);
    s = setAddModelVersion(s, 'v1.1');
    expect(validateAddModel(s)).toEqual({ valid: false, reason: 'missing_config_version' });
  });

  it('given_modelVersion_not_in_available_list_then_missing_model_version', () => {
    let s = setAddModelSelected(baseSeed(), demandModel.rid);
    s = setAddModelVersion(s, 'v9.9');
    s = setAddModelConfigVersion(s, 'cfg-baseline');
    expect(validateAddModel(s)).toEqual({ valid: false, reason: 'missing_model_version' });
  });

  it('given_configVersion_not_in_available_list_then_missing_config_version', () => {
    let s = setAddModelSelected(baseSeed(), demandModel.rid);
    s = setAddModelVersion(s, 'v1.1');
    s = setAddModelConfigVersion(s, 'cfg-ghost');
    expect(validateAddModel(s)).toEqual({ valid: false, reason: 'missing_config_version' });
  });

  it('given_complete_selection_then_validation_valid', () => {
    let s = setAddModelSelected(baseSeed(), demandModel.rid);
    s = setAddModelVersion(s, 'v1.1');
    s = setAddModelConfigVersion(s, 'cfg-baseline');
    expect(validateAddModel(s)).toEqual({ valid: true });
  });
});

describe('VTX-054 model capacity', () => {
  it('given_below_capacity_then_isAtCapacity_false', () => {
    expect(isScenarioAtModelCapacity(MAX_MODELS_PER_SCENARIO - 1)).toBe(false);
  });

  it('given_at_capacity_then_isAtCapacity_true', () => {
    expect(isScenarioAtModelCapacity(MAX_MODELS_PER_SCENARIO)).toBe(true);
  });

  it('given_above_capacity_then_isAtCapacity_true', () => {
    expect(isScenarioAtModelCapacity(MAX_MODELS_PER_SCENARIO + 1)).toBe(true);
  });

  it('given_at_capacity_when_assertCapacity_then_throws', () => {
    expect(() => assertScenarioModelCapacity(MAX_MODELS_PER_SCENARIO)).toThrowError(
      ScenarioModelCapacityError,
    );
    try {
      assertScenarioModelCapacity(MAX_MODELS_PER_SCENARIO);
    } catch (err) {
      expect(err).toBeInstanceOf(ScenarioModelCapacityError);
      expect((err as ScenarioModelCapacityError).message).toBe(MAX_MODELS_MESSAGE);
      expect((err as ScenarioModelCapacityError).limit).toBe(MAX_MODELS_PER_SCENARIO);
    }
  });

  it('given_below_capacity_when_assertCapacity_then_no_throw', () => {
    expect(() => assertScenarioModelCapacity(0)).not.toThrow();
    expect(() => assertScenarioModelCapacity(MAX_MODELS_PER_SCENARIO - 1)).not.toThrow();
  });
});

describe('VTX-054 buildAddModelRow', () => {
  const completeState = (): AddModelDialogState => {
    let s = setAddModelList(openAddModelDialog(scenarioRid), [demandModel]);
    s = setAddModelSelected(s, demandModel.rid);
    s = setAddModelVersion(s, 'v1.1');
    s = setAddModelConfigVersion(s, 'cfg-baseline');
    return s;
  };

  it('given_complete_state_when_build_then_returns_model_row_with_versions', () => {
    const row = buildAddModelRow({ state: completeState(), rid: 'r-row-1' });
    expect(row).toEqual({
      kind: 'model',
      rid: 'r-row-1',
      label: 'DemandModel',
      modelRid: demandModel.rid,
      modelVersion: 'v1.1',
      configVersion: 'cfg-baseline',
    });
  });

  it('given_blank_row_rid_when_build_then_throws', () => {
    expect(() => buildAddModelRow({ state: completeState(), rid: '' })).toThrow(/row rid/);
    expect(() => buildAddModelRow({ state: completeState(), rid: '   ' })).toThrow(/row rid/);
  });

  it('given_no_selection_when_build_then_throws', () => {
    const s = setAddModelList(openAddModelDialog(scenarioRid), [demandModel]);
    expect(() => buildAddModelRow({ state: s, rid: 'r-1' })).toThrow(
      /selected model/,
    );
  });

  it('given_missing_model_version_when_build_then_throws', () => {
    let s = setAddModelList(openAddModelDialog(scenarioRid), [demandModel]);
    s = setAddModelSelected(s, demandModel.rid);
    s = setAddModelConfigVersion(s, 'cfg-baseline');
    expect(() => buildAddModelRow({ state: s, rid: 'r-1' })).toThrow(/model version/);
  });

  it('given_missing_config_version_when_build_then_throws', () => {
    let s = setAddModelList(openAddModelDialog(scenarioRid), [demandModel]);
    s = setAddModelSelected(s, demandModel.rid);
    s = setAddModelVersion(s, 'v1.1');
    expect(() => buildAddModelRow({ state: s, rid: 'r-1' })).toThrow(/config version/);
  });
});
