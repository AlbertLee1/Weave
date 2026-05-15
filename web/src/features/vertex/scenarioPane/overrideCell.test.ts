import { describe, expect, it } from 'vitest';

import {
  assertCellEditable,
  buildCreateOverrideRequest,
  buildDeleteOverrideRequest,
  buildOverrideCellKey,
  FROZEN_SCENARIO_TOOLTIP,
  getCellTooltip,
  getOverride,
  isCellDisabled,
  isCellHighlighted,
  parseScalarInput,
  removeOverride,
  resolveCellEdit,
  ScenarioFrozenError,
  setOverride,
  type OverrideMap,
  type ScalarOverride,
} from './overrideCell';

const scenarioRid = 'ri.vertex.main.scenario.s-1';
const rowRid = 'ri.vertex.main.action.a-1';
const paramName = 'capacity';
const cellKey = buildOverrideCellKey(scenarioRid, rowRid, paramName);

function makeOverride(value: ScalarOverride['value'], id = 'ovr-1'): ScalarOverride {
  return { id, scenarioRid, rowRid, paramName, value };
}

describe('VTX-040 buildOverrideCellKey', () => {
  it('given_three_segments_when_buildKey_then_returns_double_colon_joined_string', () => {
    expect(buildOverrideCellKey(scenarioRid, rowRid, paramName)).toBe(
      `${scenarioRid}::${rowRid}::${paramName}`,
    );
  });

  it('given_two_different_scenarios_same_row_param_when_buildKey_then_keys_differ', () => {
    const k1 = buildOverrideCellKey('s1', rowRid, paramName);
    const k2 = buildOverrideCellKey('s2', rowRid, paramName);
    expect(k1).not.toBe(k2);
  });
});

describe('VTX-040 OverrideMap helpers', () => {
  it('given_empty_map_when_getOverride_then_returns_null', () => {
    expect(getOverride({}, cellKey)).toBeNull();
  });

  it('given_override_set_when_getOverride_then_returns_same_record', () => {
    const ovr = makeOverride(100);
    const map = setOverride({}, ovr);
    expect(getOverride(map, cellKey)).toEqual(ovr);
  });

  it('given_setOverride_when_called_then_does_not_mutate_original_map', () => {
    const original: OverrideMap = {};
    setOverride(original, makeOverride(100));
    expect(original).toEqual({});
  });

  it('given_existing_key_when_setOverride_then_replaces_value', () => {
    let map = setOverride({}, makeOverride(100));
    map = setOverride(map, makeOverride(200, 'ovr-2'));
    expect(getOverride(map, cellKey)?.value).toBe(200);
    expect(getOverride(map, cellKey)?.id).toBe('ovr-2');
  });

  it('given_existing_key_when_removeOverride_then_drops_entry', () => {
    let map = setOverride({}, makeOverride(100));
    map = removeOverride(map, cellKey);
    expect(getOverride(map, cellKey)).toBeNull();
    expect(Object.keys(map)).toHaveLength(0);
  });

  it('given_unknown_key_when_removeOverride_then_returns_same_reference', () => {
    const map: OverrideMap = { other: makeOverride(1, 'other') };
    const next = removeOverride(map, cellKey);
    expect(next).toBe(map);
  });

  it('given_removeOverride_when_called_then_does_not_mutate_original_map', () => {
    const original = setOverride({}, makeOverride(100));
    removeOverride(original, cellKey);
    expect(getOverride(original, cellKey)?.value).toBe(100);
  });
});

describe('VTX-040 isCellHighlighted', () => {
  it('given_no_override_when_isCellHighlighted_then_false', () => {
    expect(isCellHighlighted({}, cellKey)).toBe(false);
  });

  it('given_override_present_when_isCellHighlighted_then_true', () => {
    const map = setOverride({}, makeOverride('JFK'));
    expect(isCellHighlighted(map, cellKey)).toBe(true);
  });
});

describe('VTX-040 frozen scenario guards', () => {
  it('given_mutable_scenario_when_isCellDisabled_then_false', () => {
    expect(isCellDisabled({ rid: scenarioRid, immutable: false })).toBe(false);
  });

  it('given_undefined_immutable_when_isCellDisabled_then_false', () => {
    expect(isCellDisabled({ rid: scenarioRid })).toBe(false);
  });

  it('given_immutable_scenario_when_isCellDisabled_then_true', () => {
    expect(isCellDisabled({ rid: scenarioRid, immutable: true })).toBe(true);
  });

  it('given_mutable_scenario_when_getCellTooltip_then_null', () => {
    expect(getCellTooltip({ rid: scenarioRid, immutable: false })).toBeNull();
  });

  it('given_immutable_scenario_when_getCellTooltip_then_frozen_message', () => {
    expect(getCellTooltip({ rid: scenarioRid, immutable: true })).toBe(
      FROZEN_SCENARIO_TOOLTIP,
    );
  });

  it('given_FROZEN_SCENARIO_TOOLTIP_then_matches_spec_string', () => {
    // Spec wording: "scenario is frozen". We use sentence-cased + final period
    // for tooltip render; both spec spirit and capitalization stable.
    expect(FROZEN_SCENARIO_TOOLTIP.toLowerCase()).toContain('scenario is frozen');
  });

  it('given_mutable_scenario_when_assertCellEditable_then_no_throw', () => {
    expect(() => assertCellEditable({ rid: scenarioRid, immutable: false })).not.toThrow();
  });

  it('given_immutable_scenario_when_assertCellEditable_then_throws_ScenarioFrozenError', () => {
    let caught: unknown;
    try {
      assertCellEditable({ rid: scenarioRid, immutable: true });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(ScenarioFrozenError);
    expect((caught as ScenarioFrozenError).scenarioRid).toBe(scenarioRid);
    expect((caught as ScenarioFrozenError).message).toBe(FROZEN_SCENARIO_TOOLTIP);
    expect((caught as ScenarioFrozenError).name).toBe('ScenarioFrozenError');
  });
});

describe('VTX-040 parseScalarInput', () => {
  it('given_blank_string_when_parse_then_ok_empty', () => {
    expect(parseScalarInput('', 'number')).toEqual({ ok: true, empty: true });
  });

  it('given_whitespace_only_when_parse_then_ok_empty', () => {
    expect(parseScalarInput('   ', 'string')).toEqual({ ok: true, empty: true });
  });

  it('given_string_input_when_parse_string_then_trimmed_value', () => {
    expect(parseScalarInput('  JFK  ', 'string')).toEqual({
      ok: true,
      empty: false,
      value: 'JFK',
    });
  });

  it('given_numeric_text_when_parse_number_then_value_is_number', () => {
    expect(parseScalarInput('1500', 'number')).toEqual({
      ok: true,
      empty: false,
      value: 1500,
    });
  });

  it('given_decimal_when_parse_number_then_value_is_float', () => {
    expect(parseScalarInput('3.14', 'number')).toEqual({
      ok: true,
      empty: false,
      value: 3.14,
    });
  });

  it('given_negative_when_parse_number_then_value_negative', () => {
    expect(parseScalarInput('-42', 'number')).toEqual({
      ok: true,
      empty: false,
      value: -42,
    });
  });

  it('given_non_numeric_when_parse_number_then_invalid_not_a_number', () => {
    expect(parseScalarInput('abc', 'number')).toEqual({
      ok: false,
      reason: 'not_a_number',
    });
  });

  it('given_infinity_when_parse_number_then_invalid_not_a_number', () => {
    expect(parseScalarInput('Infinity', 'number')).toEqual({
      ok: false,
      reason: 'not_a_number',
    });
  });

  it('given_NaN_text_when_parse_number_then_invalid_not_a_number', () => {
    expect(parseScalarInput('NaN', 'number')).toEqual({
      ok: false,
      reason: 'not_a_number',
    });
  });

  it('given_true_when_parse_boolean_then_value_true', () => {
    expect(parseScalarInput('true', 'boolean')).toEqual({
      ok: true,
      empty: false,
      value: true,
    });
  });

  it('given_False_when_parse_boolean_then_value_false_case_insensitive', () => {
    expect(parseScalarInput('False', 'boolean')).toEqual({
      ok: true,
      empty: false,
      value: false,
    });
  });

  it('given_unknown_text_when_parse_boolean_then_invalid_not_a_boolean', () => {
    expect(parseScalarInput('yes', 'boolean')).toEqual({
      ok: false,
      reason: 'not_a_boolean',
    });
  });

  it('given_1_when_parse_boolean_then_invalid_not_a_boolean', () => {
    // 0/1 are intentionally not accepted to keep boolean parsing unambiguous
    expect(parseScalarInput('1', 'boolean')).toEqual({
      ok: false,
      reason: 'not_a_boolean',
    });
  });
});

describe('VTX-040 buildCreateOverrideRequest', () => {
  it('given_valid_input_when_build_then_returns_POST_request', () => {
    const req = buildCreateOverrideRequest({
      scenarioRid,
      rowRid,
      paramName,
      value: 1500,
    });
    expect(req).toEqual({
      method: 'POST',
      path: `/api/vertex/v1/scenarios/${encodeURIComponent(scenarioRid)}/overrides`,
      body: { rowRid, paramName, value: 1500 },
    });
  });

  it('given_scenarioRid_with_special_chars_when_build_then_path_segment_encoded', () => {
    const req = buildCreateOverrideRequest({
      scenarioRid: 'ri.vertex.main.scenario.s 1/2',
      rowRid,
      paramName,
      value: 'JFK',
    });
    expect(req.path).toBe(
      `/api/vertex/v1/scenarios/${encodeURIComponent('ri.vertex.main.scenario.s 1/2')}/overrides`,
    );
  });

  it('given_string_value_when_build_then_body_value_is_string', () => {
    const req = buildCreateOverrideRequest({
      scenarioRid,
      rowRid,
      paramName,
      value: 'JFK',
    });
    expect(req.body?.value).toBe('JFK');
  });

  it('given_boolean_value_when_build_then_body_value_is_boolean', () => {
    const req = buildCreateOverrideRequest({
      scenarioRid,
      rowRid,
      paramName,
      value: true,
    });
    expect(req.body?.value).toBe(true);
  });

  it('given_blank_scenarioRid_when_build_then_throws_required', () => {
    expect(() =>
      buildCreateOverrideRequest({ scenarioRid: '', rowRid, paramName, value: 1 }),
    ).toThrow('scenarioRid is required');
  });

  it('given_blank_rowRid_when_build_then_throws_required', () => {
    expect(() =>
      buildCreateOverrideRequest({ scenarioRid, rowRid: '   ', paramName, value: 1 }),
    ).toThrow('rowRid is required');
  });

  it('given_blank_paramName_when_build_then_throws_required', () => {
    expect(() =>
      buildCreateOverrideRequest({ scenarioRid, rowRid, paramName: '', value: 1 }),
    ).toThrow('paramName is required');
  });
});

describe('VTX-040 buildDeleteOverrideRequest', () => {
  it('given_valid_id_when_build_then_returns_DELETE_request', () => {
    const req = buildDeleteOverrideRequest('ovr-1');
    expect(req).toEqual({
      method: 'DELETE',
      path: `/api/vertex/v1/overrides/${encodeURIComponent('ovr-1')}`,
      body: null,
    });
  });

  it('given_id_with_special_chars_when_build_then_path_segment_encoded', () => {
    const req = buildDeleteOverrideRequest('ri.vertex.main.override.ovr/1');
    expect(req.path).toBe(
      `/api/vertex/v1/overrides/${encodeURIComponent('ri.vertex.main.override.ovr/1')}`,
    );
  });

  it('given_blank_id_when_build_then_throws_required', () => {
    expect(() => buildDeleteOverrideRequest('')).toThrow('overrideId is required');
  });
});

describe('VTX-040 resolveCellEdit', () => {
  const baseInput = {
    scenarioRid,
    rowRid,
    paramName,
    valueType: 'number' as const,
  };

  it('given_no_existing_and_empty_input_when_resolve_then_noop', () => {
    const d = resolveCellEdit({ ...baseInput, rawInput: '', existing: null });
    expect(d).toEqual({ kind: 'noop' });
  });

  it('given_existing_and_empty_input_when_resolve_then_delete_with_DELETE_request', () => {
    const existing = makeOverride(100, 'ovr-9');
    const d = resolveCellEdit({ ...baseInput, rawInput: '', existing });
    expect(d.kind).toBe('delete');
    if (d.kind !== 'delete') throw new Error('unreachable');
    expect(d.previousId).toBe('ovr-9');
    expect(d.request.method).toBe('DELETE');
    expect(d.request.path).toBe(
      `/api/vertex/v1/overrides/${encodeURIComponent('ovr-9')}`,
    );
  });

  it('given_no_existing_and_valid_input_when_resolve_then_create_with_POST_request', () => {
    const d = resolveCellEdit({
      ...baseInput,
      rawInput: '1500',
      existing: null,
    });
    expect(d.kind).toBe('create');
    if (d.kind !== 'create') throw new Error('unreachable');
    expect(d.request.method).toBe('POST');
    expect(d.request.body).toEqual({ rowRid, paramName, value: 1500 });
  });

  it('given_existing_and_same_value_when_resolve_then_noop', () => {
    const d = resolveCellEdit({
      ...baseInput,
      rawInput: '100',
      existing: makeOverride(100),
    });
    expect(d).toEqual({ kind: 'noop' });
  });

  it('given_existing_and_changed_value_when_resolve_then_update_with_POST_request', () => {
    const existing = makeOverride(100, 'ovr-7');
    const d = resolveCellEdit({
      ...baseInput,
      rawInput: '200',
      existing,
    });
    expect(d.kind).toBe('update');
    if (d.kind !== 'update') throw new Error('unreachable');
    expect(d.previousId).toBe('ovr-7');
    expect(d.request.method).toBe('POST');
    expect(d.request.body).toEqual({ rowRid, paramName, value: 200 });
  });

  it('given_invalid_input_when_resolve_then_invalid_with_reason', () => {
    const d = resolveCellEdit({
      ...baseInput,
      rawInput: 'abc',
      existing: null,
    });
    expect(d).toEqual({ kind: 'invalid', reason: 'not_a_number' });
  });

  it('given_invalid_input_with_existing_when_resolve_then_invalid_does_not_emit_delete', () => {
    const existing = makeOverride(100);
    const d = resolveCellEdit({
      ...baseInput,
      rawInput: 'abc',
      existing,
    });
    // Invalid input must NOT silently drop the existing override; React layer
    // should toast and keep the cell value.
    expect(d.kind).toBe('invalid');
  });

  it('given_string_value_type_when_resolve_then_value_is_trimmed_string', () => {
    const d = resolveCellEdit({
      scenarioRid,
      rowRid,
      paramName,
      valueType: 'string',
      rawInput: '  JFK  ',
      existing: null,
    });
    expect(d.kind).toBe('create');
    if (d.kind !== 'create') throw new Error('unreachable');
    expect(d.request.body).toEqual({ rowRid, paramName, value: 'JFK' });
  });

  it('given_boolean_value_type_and_existing_same_when_resolve_then_noop', () => {
    const d = resolveCellEdit({
      scenarioRid,
      rowRid,
      paramName,
      valueType: 'boolean',
      rawInput: 'TRUE',
      existing: makeOverride(true),
    });
    expect(d).toEqual({ kind: 'noop' });
  });

  it('given_boolean_invalid_when_resolve_then_invalid_not_a_boolean', () => {
    const d = resolveCellEdit({
      scenarioRid,
      rowRid,
      paramName,
      valueType: 'boolean',
      rawInput: 'yes',
      existing: null,
    });
    expect(d).toEqual({ kind: 'invalid', reason: 'not_a_boolean' });
  });
});

describe('VTX-040 end-to-end happy paths', () => {
  // BDD acceptance: click → input → blur → POST + 高亮
  it('given_blank_cell_when_user_inputs_and_blurs_then_create_request_emitted_and_highlight_appears', () => {
    let map: OverrideMap = {};
    const decision = resolveCellEdit({
      scenarioRid,
      rowRid,
      paramName,
      valueType: 'number',
      rawInput: '1500',
      existing: null,
    });
    expect(decision.kind).toBe('create');
    // Server returns override; React layer commits it to the map.
    map = setOverride(map, makeOverride(1500, 'ovr-new'));
    expect(isCellHighlighted(map, cellKey)).toBe(true);
  });

  // BDD acceptance: 清空单元格 → DELETE + 高亮消失
  it('given_existing_override_when_user_clears_cell_then_delete_request_emitted_and_highlight_removed', () => {
    let map: OverrideMap = setOverride({}, makeOverride(1500, 'ovr-1'));
    expect(isCellHighlighted(map, cellKey)).toBe(true);
    const decision = resolveCellEdit({
      scenarioRid,
      rowRid,
      paramName,
      valueType: 'number',
      rawInput: '',
      existing: getOverride(map, cellKey),
    });
    expect(decision.kind).toBe('delete');
    if (decision.kind !== 'delete') throw new Error('unreachable');
    expect(decision.request.method).toBe('DELETE');
    // Server confirms; React layer drops it from the map.
    map = removeOverride(map, cellKey);
    expect(isCellHighlighted(map, cellKey)).toBe(false);
  });

  // BDD acceptance: scenario.immutable=true → 单元格禁用 + tooltip
  it('given_immutable_scenario_when_check_disabled_and_tooltip_then_frozen', () => {
    const frozen = { rid: scenarioRid, immutable: true };
    expect(isCellDisabled(frozen)).toBe(true);
    expect(getCellTooltip(frozen)).toBe(FROZEN_SCENARIO_TOOLTIP);
    expect(() => assertCellEditable(frozen)).toThrow(ScenarioFrozenError);
  });
});
