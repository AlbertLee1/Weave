import { describe, expect, it } from 'vitest';

import {
  applyObjectPropertyOverrides,
  assertObjectPropertyCellEditable,
  buildCreateObjectPropertyOverrideRequest,
  buildDeleteObjectPropertyOverrideRequest,
  buildObjectPropertyOverrideKey,
  getObjectPropertyOverride,
  isObjectPropertyCellHighlighted,
  removeObjectPropertyOverride,
  resolveObjectPropertyCellEdit,
  setObjectPropertyOverride,
  withScenarioId,
  type ObjectPropertyOverride,
  type ObjectPropertyOverrideMap,
  type WireObjectLike,
} from './objectPropertyOverride';
import { FROZEN_SCENARIO_TOOLTIP, ScenarioFrozenError } from './overrideCell';

const scenarioRid = 'ri.vertex.main.scenario.s-1';
const objectType = 'Airport';
const primaryKey = 'JFK';
const property = 'capacity';
const cellKey = buildObjectPropertyOverrideKey(
  scenarioRid,
  objectType,
  primaryKey,
  property,
);

function makeOverride(
  value: ObjectPropertyOverride['value'],
  id = 'ovr-1',
): ObjectPropertyOverride {
  return { id, scenarioRid, objectType, primaryKey, property, value };
}

function makeWireObject(overrides: Partial<WireObjectLike> = {}): WireObjectLike {
  return {
    __rid: 'ri.oms.main.object.airport-jfk',
    __primaryKey: 'JFK',
    __apiName: 'Airport',
    capacity: 1000,
    code: 'JFK',
    ...overrides,
  };
}

describe('VTX-041 buildObjectPropertyOverrideKey', () => {
  it('given_four_segments_when_buildKey_then_returns_double_colon_joined_string', () => {
    expect(
      buildObjectPropertyOverrideKey(scenarioRid, objectType, primaryKey, property),
    ).toBe(`${scenarioRid}::${objectType}::${primaryKey}::${property}`);
  });

  it('given_two_different_scenarios_same_object_property_when_buildKey_then_keys_differ', () => {
    const k1 = buildObjectPropertyOverrideKey('s1', objectType, primaryKey, property);
    const k2 = buildObjectPropertyOverrideKey('s2', objectType, primaryKey, property);
    expect(k1).not.toBe(k2);
  });

  it('given_two_different_objects_same_property_when_buildKey_then_keys_differ', () => {
    const k1 = buildObjectPropertyOverrideKey(scenarioRid, 'Airport', 'JFK', 'capacity');
    const k2 = buildObjectPropertyOverrideKey(scenarioRid, 'Airport', 'LAX', 'capacity');
    expect(k1).not.toBe(k2);
  });

  it('given_same_pk_different_objectType_when_buildKey_then_keys_differ', () => {
    const k1 = buildObjectPropertyOverrideKey(scenarioRid, 'Airport', '42', 'capacity');
    const k2 = buildObjectPropertyOverrideKey(scenarioRid, 'Gate', '42', 'capacity');
    expect(k1).not.toBe(k2);
  });
});

describe('VTX-041 ObjectPropertyOverrideMap helpers', () => {
  it('given_empty_map_when_getOverride_then_returns_null', () => {
    expect(getObjectPropertyOverride({}, cellKey)).toBeNull();
  });

  it('given_override_set_when_getOverride_then_returns_same_record', () => {
    const ovr = makeOverride(1500);
    const map = setObjectPropertyOverride({}, ovr);
    expect(getObjectPropertyOverride(map, cellKey)).toEqual(ovr);
  });

  it('given_setOverride_when_called_then_does_not_mutate_original_map', () => {
    const original: ObjectPropertyOverrideMap = {};
    setObjectPropertyOverride(original, makeOverride(1500));
    expect(original).toEqual({});
  });

  it('given_existing_override_when_setOverride_with_same_key_then_replaces_value', () => {
    let map = setObjectPropertyOverride({}, makeOverride(1500, 'ovr-1'));
    map = setObjectPropertyOverride(map, makeOverride(2000, 'ovr-1'));
    expect(getObjectPropertyOverride(map, cellKey)?.value).toBe(2000);
  });

  it('given_override_when_removeOverride_then_returns_map_without_key', () => {
    const map = setObjectPropertyOverride({}, makeOverride(1500));
    const next = removeObjectPropertyOverride(map, cellKey);
    expect(getObjectPropertyOverride(next, cellKey)).toBeNull();
  });

  it('given_unknown_key_when_removeOverride_then_returns_same_reference', () => {
    const map = setObjectPropertyOverride({}, makeOverride(1500));
    expect(removeObjectPropertyOverride(map, 'nonexistent')).toBe(map);
  });

  it('given_removeOverride_when_called_then_does_not_mutate_original_map', () => {
    const map = setObjectPropertyOverride({}, makeOverride(1500));
    removeObjectPropertyOverride(map, cellKey);
    expect(getObjectPropertyOverride(map, cellKey)).not.toBeNull();
  });
});

describe('VTX-041 isObjectPropertyCellHighlighted', () => {
  it('given_no_override_when_check_then_false', () => {
    expect(isObjectPropertyCellHighlighted({}, cellKey)).toBe(false);
  });

  it('given_override_present_when_check_then_true', () => {
    const map = setObjectPropertyOverride({}, makeOverride(1500));
    expect(isObjectPropertyCellHighlighted(map, cellKey)).toBe(true);
  });
});

describe('VTX-041 assertObjectPropertyCellEditable', () => {
  it('given_mutable_scenario_when_assert_then_does_not_throw', () => {
    expect(() =>
      assertObjectPropertyCellEditable({ rid: scenarioRid, immutable: false }),
    ).not.toThrow();
  });

  it('given_undefined_immutable_when_assert_then_does_not_throw', () => {
    expect(() =>
      assertObjectPropertyCellEditable({ rid: scenarioRid }),
    ).not.toThrow();
  });

  it('given_immutable_scenario_when_assert_then_throws_ScenarioFrozenError', () => {
    expect(() =>
      assertObjectPropertyCellEditable({ rid: scenarioRid, immutable: true }),
    ).toThrow(ScenarioFrozenError);
  });

  it('given_immutable_scenario_when_assert_throws_then_message_is_frozen_tooltip', () => {
    try {
      assertObjectPropertyCellEditable({ rid: scenarioRid, immutable: true });
      throw new Error('did not throw');
    } catch (err) {
      expect(err).toBeInstanceOf(ScenarioFrozenError);
      expect((err as Error).message).toBe(FROZEN_SCENARIO_TOOLTIP);
      expect((err as ScenarioFrozenError).scenarioRid).toBe(scenarioRid);
    }
  });
});

describe('VTX-041 buildCreateObjectPropertyOverrideRequest', () => {
  it('given_valid_input_when_build_then_returns_POST_request_to_scenario_endpoint', () => {
    const req = buildCreateObjectPropertyOverrideRequest({
      scenarioRid,
      objectType,
      primaryKey,
      property,
      value: 1500,
    });
    expect(req.method).toBe('POST');
    expect(req.path).toBe(
      `/api/vertex/v1/scenarios/${encodeURIComponent(scenarioRid)}/object-property-overrides`,
    );
    expect(req.body).toEqual({
      objectType,
      primaryKey,
      property,
      value: 1500,
    });
  });

  it('given_special_chars_in_scenarioRid_when_build_then_path_segment_is_url_encoded', () => {
    const rid = 'ri.vertex/main:scenario#1';
    const req = buildCreateObjectPropertyOverrideRequest({
      scenarioRid: rid,
      objectType,
      primaryKey,
      property,
      value: 1500,
    });
    expect(req.path).toBe(
      `/api/vertex/v1/scenarios/${encodeURIComponent(rid)}/object-property-overrides`,
    );
  });

  it.each([
    ['scenarioRid', { scenarioRid: '   ', objectType, primaryKey, property, value: 1 }],
    ['objectType', { scenarioRid, objectType: '', primaryKey, property, value: 1 }],
    ['primaryKey', { scenarioRid, objectType, primaryKey: '', property, value: 1 }],
    ['property', { scenarioRid, objectType, primaryKey, property: '', value: 1 }],
  ])('given_blank_%s_when_build_then_throws_required_error', (field, input) => {
    expect(() => buildCreateObjectPropertyOverrideRequest(input)).toThrow(
      `${field} is required`,
    );
  });

  it('given_string_value_when_build_then_value_threads_through', () => {
    const req = buildCreateObjectPropertyOverrideRequest({
      scenarioRid,
      objectType,
      primaryKey,
      property,
      value: 'closed',
    });
    expect(req.body?.value).toBe('closed');
  });

  it('given_boolean_value_when_build_then_value_threads_through', () => {
    const req = buildCreateObjectPropertyOverrideRequest({
      scenarioRid,
      objectType,
      primaryKey,
      property: 'active',
      value: false,
    });
    expect(req.body?.value).toBe(false);
  });
});

describe('VTX-041 buildDeleteObjectPropertyOverrideRequest', () => {
  it('given_valid_id_when_build_then_returns_DELETE_request_with_null_body', () => {
    const req = buildDeleteObjectPropertyOverrideRequest('ovr-42');
    expect(req.method).toBe('DELETE');
    expect(req.path).toBe('/api/vertex/v1/object-property-overrides/ovr-42');
    expect(req.body).toBeNull();
  });

  it('given_special_chars_in_id_when_build_then_path_segment_is_url_encoded', () => {
    const req = buildDeleteObjectPropertyOverrideRequest('ovr/42:foo');
    expect(req.path).toBe(
      `/api/vertex/v1/object-property-overrides/${encodeURIComponent('ovr/42:foo')}`,
    );
  });

  it('given_blank_id_when_build_then_throws_required_error', () => {
    expect(() => buildDeleteObjectPropertyOverrideRequest('  ')).toThrow(
      'overrideId is required',
    );
  });
});

describe('VTX-041 withScenarioId', () => {
  it('given_null_scenarioRid_when_inject_then_returns_path_unchanged', () => {
    expect(withScenarioId('/api/v2/ontologies/foundry/objects/Airport/JFK', null)).toBe(
      '/api/v2/ontologies/foundry/objects/Airport/JFK',
    );
  });

  it('given_undefined_scenarioRid_when_inject_then_returns_path_unchanged', () => {
    expect(
      withScenarioId('/api/v2/ontologies/foundry/objects/Airport/JFK', undefined),
    ).toBe('/api/v2/ontologies/foundry/objects/Airport/JFK');
  });

  it('given_blank_scenarioRid_when_inject_then_returns_path_unchanged', () => {
    expect(
      withScenarioId('/api/v2/ontologies/foundry/objects/Airport/JFK', '   '),
    ).toBe('/api/v2/ontologies/foundry/objects/Airport/JFK');
  });

  it('given_path_without_query_when_inject_then_appends_question_mark_scenarioId', () => {
    expect(
      withScenarioId('/api/v2/ontologies/foundry/objects/Airport/JFK', scenarioRid),
    ).toBe(
      `/api/v2/ontologies/foundry/objects/Airport/JFK?scenarioId=${encodeURIComponent(scenarioRid)}`,
    );
  });

  it('given_path_with_existing_query_when_inject_then_appends_ampersand_scenarioId', () => {
    expect(
      withScenarioId(
        '/api/v2/ontologies/foundry/objects/Airport?pageSize=50',
        scenarioRid,
      ),
    ).toBe(
      `/api/v2/ontologies/foundry/objects/Airport?pageSize=50&scenarioId=${encodeURIComponent(scenarioRid)}`,
    );
  });

  it('given_special_chars_in_scenarioRid_when_inject_then_value_is_url_encoded', () => {
    const rid = 'ri.vertex/main:scenario#1';
    expect(
      withScenarioId('/api/v2/ontologies/foundry/objects/Airport/JFK', rid),
    ).toBe(
      `/api/v2/ontologies/foundry/objects/Airport/JFK?scenarioId=${encodeURIComponent(rid)}`,
    );
  });
});

describe('VTX-041 applyObjectPropertyOverrides', () => {
  it('given_no_overrides_when_apply_then_returns_object_with_same_property_values', () => {
    const obj = makeWireObject();
    const result = applyObjectPropertyOverrides(obj, []);
    expect(result.capacity).toBe(1000);
    expect(result.code).toBe('JFK');
  });

  it('given_no_overrides_when_apply_then_returns_a_new_object_reference', () => {
    const obj = makeWireObject();
    const result = applyObjectPropertyOverrides(obj, []);
    expect(result).not.toBe(obj);
    expect(result).toEqual(obj);
  });

  it('given_one_matching_override_when_apply_then_property_value_is_replaced', () => {
    const obj = makeWireObject({ capacity: 1000 });
    const result = applyObjectPropertyOverrides(obj, [makeOverride(1500)]);
    expect(result.capacity).toBe(1500);
  });

  it('given_matching_override_when_apply_then_base_object_is_not_mutated', () => {
    const obj = makeWireObject({ capacity: 1000 });
    applyObjectPropertyOverrides(obj, [makeOverride(1500)]);
    expect(obj.capacity).toBe(1000);
  });

  it('given_override_for_different_objectType_when_apply_then_property_unchanged', () => {
    const obj = makeWireObject({ __apiName: 'Gate', capacity: 50 });
    const result = applyObjectPropertyOverrides(obj, [makeOverride(1500)]);
    expect(result.capacity).toBe(50);
  });

  it('given_override_for_different_primaryKey_when_apply_then_property_unchanged', () => {
    const obj = makeWireObject({ __primaryKey: 'LAX', capacity: 800 });
    const result = applyObjectPropertyOverrides(obj, [makeOverride(1500)]);
    expect(result.capacity).toBe(800);
  });

  it('given_numeric_primaryKey_matches_string_override_when_apply_then_property_replaced', () => {
    const obj: WireObjectLike = {
      __rid: 'ri.x',
      __primaryKey: 42,
      __apiName: 'Order',
      total: 100,
    };
    const result = applyObjectPropertyOverrides(obj, [
      {
        id: 'o',
        scenarioRid,
        objectType: 'Order',
        primaryKey: '42',
        property: 'total',
        value: 500,
      },
    ]);
    expect(result.total).toBe(500);
  });

  it('given_multiple_overrides_for_same_object_when_apply_then_all_properties_replaced', () => {
    const obj = makeWireObject({ capacity: 1000, code: 'JFK', active: true });
    const result = applyObjectPropertyOverrides(obj, [
      makeOverride(2000, 'o1'),
      {
        id: 'o2',
        scenarioRid,
        objectType,
        primaryKey,
        property: 'active',
        value: false,
      },
    ]);
    expect(result.capacity).toBe(2000);
    expect(result.active).toBe(false);
    expect(result.code).toBe('JFK');
  });

  it('given_override_for_unknown_property_when_apply_then_property_is_added', () => {
    const obj = makeWireObject();
    const result = applyObjectPropertyOverrides(obj, [
      {
        id: 'o',
        scenarioRid,
        objectType,
        primaryKey,
        property: 'newProperty',
        value: 'hello',
      },
    ]);
    expect(result.newProperty).toBe('hello');
  });

  it('given_overrides_apply_when_called_then_meta_fields_preserved', () => {
    const obj = makeWireObject();
    const result = applyObjectPropertyOverrides(obj, [makeOverride(1500)]);
    expect(result.__rid).toBe(obj.__rid);
    expect(result.__primaryKey).toBe(obj.__primaryKey);
    expect(result.__apiName).toBe(obj.__apiName);
  });

  it('given_mixed_overrides_when_apply_then_only_matching_ones_take_effect', () => {
    const obj = makeWireObject({ capacity: 1000 });
    const result = applyObjectPropertyOverrides(obj, [
      makeOverride(1500, 'matching'),
      {
        id: 'other',
        scenarioRid,
        objectType: 'Gate',
        primaryKey: 'A1',
        property: 'capacity',
        value: 9999,
      },
    ]);
    expect(result.capacity).toBe(1500);
  });
});

describe('VTX-041 resolveObjectPropertyCellEdit', () => {
  const baseInput = {
    scenarioRid,
    objectType,
    primaryKey,
    property,
    valueType: 'number' as const,
  };

  it('given_empty_input_and_no_existing_when_resolve_then_noop', () => {
    const decision = resolveObjectPropertyCellEdit({
      ...baseInput,
      rawInput: '',
      existing: null,
    });
    expect(decision).toEqual({ kind: 'noop' });
  });

  it('given_empty_input_and_existing_when_resolve_then_delete_with_previous_id', () => {
    const existing = makeOverride(1500, 'ovr-42');
    const decision = resolveObjectPropertyCellEdit({
      ...baseInput,
      rawInput: '   ',
      existing,
    });
    expect(decision.kind).toBe('delete');
    if (decision.kind === 'delete') {
      expect(decision.previousId).toBe('ovr-42');
      expect(decision.request.method).toBe('DELETE');
      expect(decision.request.path).toBe('/api/vertex/v1/object-property-overrides/ovr-42');
    }
  });

  it('given_value_input_and_no_existing_when_resolve_then_create_POST_request', () => {
    const decision = resolveObjectPropertyCellEdit({
      ...baseInput,
      rawInput: '1500',
      existing: null,
    });
    expect(decision.kind).toBe('create');
    if (decision.kind === 'create') {
      expect(decision.request.method).toBe('POST');
      expect(decision.request.body).toEqual({
        objectType,
        primaryKey,
        property,
        value: 1500,
      });
    }
  });

  it('given_unchanged_value_and_existing_when_resolve_then_noop', () => {
    const existing = makeOverride(1500);
    const decision = resolveObjectPropertyCellEdit({
      ...baseInput,
      rawInput: '1500',
      existing,
    });
    expect(decision).toEqual({ kind: 'noop' });
  });

  it('given_changed_value_and_existing_when_resolve_then_update_POST_with_previous_id', () => {
    const existing = makeOverride(1500, 'ovr-7');
    const decision = resolveObjectPropertyCellEdit({
      ...baseInput,
      rawInput: '2000',
      existing,
    });
    expect(decision.kind).toBe('update');
    if (decision.kind === 'update') {
      expect(decision.previousId).toBe('ovr-7');
      expect(decision.request.method).toBe('POST');
      expect(decision.request.body?.value).toBe(2000);
    }
  });

  it('given_invalid_input_with_existing_when_resolve_then_invalid_does_not_emit_delete', () => {
    const existing = makeOverride(1500);
    const decision = resolveObjectPropertyCellEdit({
      ...baseInput,
      rawInput: 'abc',
      existing,
    });
    expect(decision.kind).toBe('invalid');
    if (decision.kind === 'invalid') {
      expect(decision.reason).toBe('not_a_number');
    }
  });

  it('given_invalid_input_no_existing_when_resolve_then_invalid', () => {
    const decision = resolveObjectPropertyCellEdit({
      ...baseInput,
      rawInput: 'xyz',
      existing: null,
    });
    expect(decision.kind).toBe('invalid');
  });

  it('given_string_valueType_when_resolve_then_text_value_threads_through', () => {
    const decision = resolveObjectPropertyCellEdit({
      ...baseInput,
      valueType: 'string',
      rawInput: '  closed  ',
      existing: null,
    });
    expect(decision.kind).toBe('create');
    if (decision.kind === 'create') {
      expect(decision.request.body?.value).toBe('closed');
    }
  });

  it('given_boolean_valueType_when_resolve_then_boolean_value_threads_through', () => {
    const decision = resolveObjectPropertyCellEdit({
      ...baseInput,
      valueType: 'boolean',
      rawInput: 'true',
      existing: null,
    });
    expect(decision.kind).toBe('create');
    if (decision.kind === 'create') {
      expect(decision.request.body?.value).toBe(true);
    }
  });
});

describe('VTX-041 end-to-end flows', () => {
  it('given_input_capacity_when_user_types_and_blurs_then_POST_request_built_and_fork_only', () => {
    const decision = resolveObjectPropertyCellEdit({
      scenarioRid,
      objectType: 'Airport',
      primaryKey: 'JFK',
      property: 'capacity',
      rawInput: '1500',
      valueType: 'number',
      existing: null,
    });
    expect(decision.kind).toBe('create');
    if (decision.kind === 'create') {
      expect(decision.request.path).toBe(
        `/api/vertex/v1/scenarios/${encodeURIComponent(scenarioRid)}/object-property-overrides`,
      );
      expect(decision.request.body).toEqual({
        objectType: 'Airport',
        primaryKey: 'JFK',
        property: 'capacity',
        value: 1500,
      });
    }
  });

  it('given_GET_objects_query_when_inject_scenarioId_then_url_threads_fork_overlay', () => {
    const baseUrl = '/api/v2/ontologies/foundry/objects/Airport/JFK';
    const overlayUrl = withScenarioId(baseUrl, scenarioRid);
    expect(overlayUrl).toContain(`scenarioId=${encodeURIComponent(scenarioRid)}`);
  });

  it('given_fork_query_when_client_applies_overrides_then_capacity_shows_new_value', () => {
    const base = makeWireObject({ capacity: 1000 });
    const overrides = [makeOverride(1500)];
    const result = applyObjectPropertyOverrides(base, overrides);
    expect(result.capacity).toBe(1500);
    expect(base.capacity).toBe(1000);
  });

  it('given_immutable_scenario_when_user_attempts_cell_edit_then_assertCellEditable_throws', () => {
    const immutable = { rid: scenarioRid, immutable: true };
    expect(() => assertObjectPropertyCellEditable(immutable)).toThrow(
      ScenarioFrozenError,
    );
    expect(() => assertObjectPropertyCellEditable(immutable)).toThrow(
      FROZEN_SCENARIO_TOOLTIP,
    );
  });
});
