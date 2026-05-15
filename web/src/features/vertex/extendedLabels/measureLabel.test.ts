// VTX-061 — Measure / Derived Property Function Label
//
// BDD acceptance（来自 prd.json VTX-061）：
//   1. Given label {kind:measure,measureRid:ri.functions.measure.total-alerts}
//      When 渲染 Then 调 /api/functions/{rid}/invoke 传入对象 RID + 拿 scalar
//   2. Given measure function 涉及多跳 link When 调用 Then function 用 SDK
//      link traversal API（front-end 不感知 traversal — 仅传对象 RID）
//   3. Given measure 计算失败 When 渲染 Then 显示红色 ! + tooltip 错误

import { describe, expect, it } from 'vitest';

import {
  ERROR_PLACEHOLDER,
  MEASURE_LABEL_DEFAULT_INPUT_PARAM,
  MISSING_VALUE_PLACEHOLDER,
  buildMeasureLabelRequest,
  isMeasureExtendedLabel,
  renderMeasureExtendedLabel,
  type MeasureExtendedLabelSpec,
} from './measureLabel';

describe('VTX-061 extendedLabels.measureLabel — isMeasureExtendedLabel', () => {
  it('given_kind_measure_when_check_then_true', () => {
    expect(
      isMeasureExtendedLabel({ kind: 'measure', measureRid: 'ri.functions.measure.total-alerts' }),
    ).toBe(true);
  });

  it('given_kind_property_when_check_then_false', () => {
    expect(isMeasureExtendedLabel({ kind: 'property', property: 'onTimePct' })).toBe(false);
  });

  it('given_kind_timeSeries_when_check_then_false', () => {
    expect(isMeasureExtendedLabel({ kind: 'timeSeries', timeSeriesRid: 't' })).toBe(false);
  });

  it('given_kind_unknown_when_check_then_false', () => {
    expect(isMeasureExtendedLabel({ kind: 'badge' })).toBe(false);
  });
});

describe('VTX-061 extendedLabels.measureLabel — buildMeasureLabelRequest (BDD #1, #2)', () => {
  const spec: MeasureExtendedLabelSpec = {
    kind: 'measure',
    measureRid: 'ri.functions.measure.total-alerts',
  };
  const ctx = {
    ontology: 'ri.ontology.main.ontology.vtx',
    objectRid: 'ri.objects.main.airport.JFK',
  };

  it('given_spec_and_object_rid_when_build_then_returns_post_to_function_execute_path', () => {
    const r = buildMeasureLabelRequest(spec, ctx);
    expect(r.method).toBe('POST');
    expect(r.path).toBe(
      '/api/v2/ontologies/ri.ontology.main.ontology.vtx/functions/ri.functions.measure.total-alerts/execute',
    );
  });

  it('given_spec_and_object_rid_when_build_then_body_carries_object_rid_in_parameters', () => {
    const r = buildMeasureLabelRequest(spec, ctx);
    expect(r.body).toEqual({
      parameters: { [MEASURE_LABEL_DEFAULT_INPUT_PARAM]: 'ri.objects.main.airport.JFK' },
    });
  });

  it('given_default_input_param_constant_then_named_object', () => {
    expect(MEASURE_LABEL_DEFAULT_INPUT_PARAM).toBe('object');
  });

  it('given_custom_inputParamName_when_build_then_body_uses_override', () => {
    const r = buildMeasureLabelRequest(spec, ctx, { inputParamName: 'airport' });
    expect(r.body).toEqual({
      parameters: { airport: 'ri.objects.main.airport.JFK' },
    });
  });

  // BDD #2 — multi-hop link traversal is opaque to the front-end client. The
  // request body must carry ONLY the object RID — no link metadata. The
  // Python runtime function uses its SDK link traversal API internally; the
  // wire payload doesn't leak that detail.
  it('given_measure_with_multi_hop_links_when_build_then_payload_carries_only_object_rid', () => {
    const r = buildMeasureLabelRequest(spec, ctx);
    expect(Object.keys(r.body.parameters)).toEqual([MEASURE_LABEL_DEFAULT_INPUT_PARAM]);
    // Front-end does not encode link traversal hints.
    expect(JSON.stringify(r.body)).not.toMatch(/link|traversal|hop/i);
  });

  it('given_special_chars_when_build_then_uri_components_escaped', () => {
    const r = buildMeasureLabelRequest(
      { kind: 'measure', measureRid: 'ri funcs/measure A' },
      { ontology: 'ri ont', objectRid: 'ri.objects/x#1' },
    );
    expect(r.path).toBe(
      '/api/v2/ontologies/ri%20ont/functions/ri%20funcs%2Fmeasure%20A/execute',
    );
    // objectRid is NOT URL-encoded — it's a JSON body value, not a path
    // segment. URL-encoding here would corrupt the RID at the function side.
    expect(r.body.parameters[MEASURE_LABEL_DEFAULT_INPUT_PARAM]).toBe('ri.objects/x#1');
  });

  it('given_blank_ontology_when_build_then_throws', () => {
    expect(() =>
      buildMeasureLabelRequest(spec, { ontology: '', objectRid: 'rid' }),
    ).toThrow(/ontology/i);
  });

  it('given_blank_objectRid_when_build_then_throws', () => {
    expect(() =>
      buildMeasureLabelRequest(spec, { ontology: 'o', objectRid: '' }),
    ).toThrow(/objectRid/i);
  });

  it('given_blank_measureRid_when_build_then_throws', () => {
    expect(() =>
      buildMeasureLabelRequest({ kind: 'measure', measureRid: '' }, ctx),
    ).toThrow(/measureRid/i);
  });

  it('given_blank_inputParamName_override_when_build_then_throws', () => {
    expect(() => buildMeasureLabelRequest(spec, ctx, { inputParamName: '   ' })).toThrow(
      /inputParamName/i,
    );
  });

  it('given_non_measure_kind_when_build_then_throws', () => {
    expect(() =>
      buildMeasureLabelRequest(
        { kind: 'property', measureRid: 'r' } as unknown as MeasureExtendedLabelSpec,
        ctx,
      ),
    ).toThrow(/measure/i);
  });
});

describe('VTX-061 extendedLabels.measureLabel — renderMeasureExtendedLabel', () => {
  const spec: MeasureExtendedLabelSpec = {
    kind: 'measure',
    measureRid: 'ri.functions.measure.total-alerts',
  };

  it('given_scalar_present_when_render_then_shows_rid_colon_scalar', () => {
    const r = renderMeasureExtendedLabel(spec, 1500);
    expect(r.text).toBe('ri.functions.measure.total-alerts: 1500');
    expect(r.labelName).toBe('ri.functions.measure.total-alerts');
    expect(r.valueText).toBe('1500');
    expect(r.status).toBe('present');
  });

  it('given_scalar_null_when_render_then_shows_em_dash_and_status_missing', () => {
    const r = renderMeasureExtendedLabel(spec, null);
    expect(r.text).toBe('ri.functions.measure.total-alerts: —');
    expect(r.status).toBe('missing');
    expect(r.valueText).toBe(MISSING_VALUE_PLACEHOLDER);
  });

  it('given_NaN_scalar_when_render_then_status_missing', () => {
    const r = renderMeasureExtendedLabel(spec, Number.NaN);
    expect(r.status).toBe('missing');
  });

  it('given_infinity_scalar_when_render_then_status_missing', () => {
    expect(renderMeasureExtendedLabel(spec, Number.POSITIVE_INFINITY).status).toBe('missing');
    expect(renderMeasureExtendedLabel(spec, Number.NEGATIVE_INFINITY).status).toBe('missing');
  });

  it('given_zero_scalar_when_render_then_status_present', () => {
    const r = renderMeasureExtendedLabel(spec, 0);
    expect(r.status).toBe('present');
    expect(r.valueText).toBe('0');
  });

  it('given_negative_scalar_when_render_then_status_present', () => {
    const r = renderMeasureExtendedLabel(spec, -42.5);
    expect(r.status).toBe('present');
    expect(r.valueText).toBe('-42.5');
  });

  it('given_displayName_when_render_then_uses_displayName_as_label', () => {
    const r = renderMeasureExtendedLabel({ ...spec, displayName: 'Total Alerts' }, 1500);
    expect(r.labelName).toBe('Total Alerts');
    expect(r.text).toBe('Total Alerts: 1500');
  });

  it('given_blank_displayName_when_render_then_falls_back_to_measureRid', () => {
    const r = renderMeasureExtendedLabel({ ...spec, displayName: '   ' }, 1500);
    expect(r.labelName).toBe('ri.functions.measure.total-alerts');
  });

  it('given_formatValue_when_render_then_uses_formatter_output', () => {
    const r = renderMeasureExtendedLabel(spec, 1500, {
      formatValue: (n) => `${(n / 1000).toFixed(1)}k`,
    });
    expect(r.valueText).toBe('1.5k');
    expect(r.status).toBe('present');
  });

  it('given_formatValue_returns_empty_when_render_then_status_missing', () => {
    const r = renderMeasureExtendedLabel(spec, 1500, { formatValue: () => '' });
    expect(r.status).toBe('missing');
    expect(r.valueText).toBe(MISSING_VALUE_PLACEHOLDER);
  });

  it('given_custom_missingPlaceholder_when_render_then_used_for_null_scalar', () => {
    const r = renderMeasureExtendedLabel(spec, null, { missingPlaceholder: 'N/A' });
    expect(r.text).toBe('ri.functions.measure.total-alerts: N/A');
    expect(r.status).toBe('missing');
  });

  // BDD #3 — calculation failure shows red ! + tooltip error
  it('given_error_option_when_render_then_status_error_value_is_bang_and_errorMessage_propagated', () => {
    const r = renderMeasureExtendedLabel(spec, null, { error: 'function timeout' });
    expect(r.status).toBe('error');
    expect(r.valueText).toBe(ERROR_PLACEHOLDER);
    expect(r.valueText).toBe('!');
    expect(r.text).toBe('ri.functions.measure.total-alerts: !');
    expect(r.errorMessage).toBe('function timeout');
  });

  it('given_error_option_overrides_present_scalar_when_render_then_status_error_not_present', () => {
    // Same precedence rule as VTX-060: error > missing > present. A stale
    // scalar with a current fetch error must show error, not "success".
    const r = renderMeasureExtendedLabel(spec, 1500, { error: 'fetch failed' });
    expect(r.status).toBe('error');
    expect(r.valueText).toBe('!');
  });

  it('given_custom_errorPlaceholder_when_render_then_uses_override', () => {
    const r = renderMeasureExtendedLabel(spec, null, {
      error: 'boom',
      errorPlaceholder: 'ERR',
    });
    expect(r.valueText).toBe('ERR');
    expect(r.text).toBe('ri.functions.measure.total-alerts: ERR');
  });

  it('given_empty_string_error_when_render_then_status_not_error', () => {
    // Empty error string is treated as "no error" — caller doesn't have a
    // failure to surface. Falls back to scalar rendering.
    const r = renderMeasureExtendedLabel(spec, 1500, { error: '' });
    expect(r.status).toBe('present');
    expect(r.valueText).toBe('1500');
  });

  it('given_non_measure_kind_when_render_then_throws', () => {
    expect(() =>
      renderMeasureExtendedLabel(
        { kind: 'property' as 'measure', measureRid: 'r' },
        1,
      ),
    ).toThrow(/measure/i);
  });

  it('given_blank_measureRid_when_render_then_throws', () => {
    expect(() =>
      renderMeasureExtendedLabel({ kind: 'measure', measureRid: '' }, 1),
    ).toThrow(/measureRid/i);
  });
});
