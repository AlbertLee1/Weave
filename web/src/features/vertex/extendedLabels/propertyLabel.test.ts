// VTX-059 — Property Extended Label 渲染（节点 DOM overlay 显示属性 key/value）。
//
// BDD acceptance（来自 prd.json VTX-059）：
//   1. Given label {kind:property, property:onTimePct} When 节点 DOM overlay
//      渲染 Then 显示 `onTimePct: 92%`
//   2. Given 属性值缺失 When 渲染 Then 显示 `onTimePct: —`

import { describe, expect, it } from 'vitest';

import {
  MISSING_VALUE_PLACEHOLDER,
  formatScalarPropertyValue,
  isPropertyExtendedLabel,
  isPropertyValueMissing,
  renderPropertyExtendedLabel,
  resolvePropertyValue,
  type PropertyExtendedLabelSpec,
} from './propertyLabel';

describe('VTX-059 extendedLabels.propertyLabel — isPropertyExtendedLabel', () => {
  it('given_kind_property_when_check_then_true', () => {
    expect(isPropertyExtendedLabel({ kind: 'property', property: 'onTimePct' })).toBe(true);
  });

  it('given_kind_timeSeries_when_check_then_false', () => {
    expect(isPropertyExtendedLabel({ kind: 'timeSeries', timeSeriesRid: 'rid' })).toBe(false);
  });

  it('given_kind_measure_when_check_then_false', () => {
    expect(isPropertyExtendedLabel({ kind: 'measure', measureRid: 'rid' })).toBe(false);
  });

  it('given_kind_unknown_when_check_then_false', () => {
    expect(isPropertyExtendedLabel({ kind: 'badge' })).toBe(false);
  });
});

describe('VTX-059 extendedLabels.propertyLabel — resolvePropertyValue', () => {
  it('given_property_present_when_resolve_then_returns_raw_value', () => {
    const obj = { __rid: 'r', __apiName: 'Airport', __primaryKey: 'JFK', onTimePct: '92%' };
    expect(resolvePropertyValue(obj, 'onTimePct')).toBe('92%');
  });

  it('given_property_missing_when_resolve_then_returns_undefined', () => {
    const obj = { __rid: 'r', __apiName: 'Airport', __primaryKey: 'JFK' };
    expect(resolvePropertyValue(obj, 'onTimePct')).toBeUndefined();
  });

  it('given_property_explicit_null_when_resolve_then_returns_null', () => {
    const obj = { __rid: 'r', __apiName: 'Airport', __primaryKey: 'JFK', onTimePct: null };
    expect(resolvePropertyValue(obj, 'onTimePct')).toBeNull();
  });

  it('given_property_with_meta_prefix_when_resolve_then_returns_meta_value', () => {
    const obj = { __rid: 'r', __apiName: 'Airport', __primaryKey: 'JFK', onTimePct: 92 };
    expect(resolvePropertyValue(obj, '__apiName')).toBe('Airport');
    expect(resolvePropertyValue(obj, '__primaryKey')).toBe('JFK');
  });

  it('given_blank_property_when_resolve_then_throws', () => {
    expect(() => resolvePropertyValue({ __rid: 'r', __apiName: 'A', __primaryKey: 'k' }, '')).toThrow(
      /property/i,
    );
  });
});

describe('VTX-059 extendedLabels.propertyLabel — isPropertyValueMissing', () => {
  it('given_undefined_when_check_then_true', () => {
    expect(isPropertyValueMissing(undefined)).toBe(true);
  });

  it('given_null_when_check_then_true', () => {
    expect(isPropertyValueMissing(null)).toBe(true);
  });

  it('given_empty_string_when_check_then_true', () => {
    expect(isPropertyValueMissing('')).toBe(true);
  });

  it('given_whitespace_string_when_check_then_true', () => {
    expect(isPropertyValueMissing('   ')).toBe(true);
  });

  it('given_zero_number_when_check_then_false', () => {
    expect(isPropertyValueMissing(0)).toBe(false);
  });

  it('given_false_boolean_when_check_then_false', () => {
    expect(isPropertyValueMissing(false)).toBe(false);
  });

  it('given_string_value_when_check_then_false', () => {
    expect(isPropertyValueMissing('92%')).toBe(false);
  });

  it('given_NaN_when_check_then_true', () => {
    expect(isPropertyValueMissing(Number.NaN)).toBe(true);
  });
});

describe('VTX-059 extendedLabels.propertyLabel — formatScalarPropertyValue', () => {
  it('given_string_when_format_then_returns_as_is', () => {
    expect(formatScalarPropertyValue('92%')).toBe('92%');
  });

  it('given_integer_when_format_then_returns_string_without_decimals', () => {
    expect(formatScalarPropertyValue(92)).toBe('92');
  });

  it('given_decimal_number_when_format_then_returns_string_preserving_precision', () => {
    expect(formatScalarPropertyValue(92.5)).toBe('92.5');
  });

  it('given_boolean_true_when_format_then_returns_true_string', () => {
    expect(formatScalarPropertyValue(true)).toBe('true');
  });

  it('given_boolean_false_when_format_then_returns_false_string', () => {
    expect(formatScalarPropertyValue(false)).toBe('false');
  });

  it('given_NaN_when_format_then_returns_missing_placeholder', () => {
    expect(formatScalarPropertyValue(Number.NaN)).toBe(MISSING_VALUE_PLACEHOLDER);
  });

  it('given_object_when_format_then_returns_json_string', () => {
    expect(formatScalarPropertyValue({ a: 1 })).toBe('{"a":1}');
  });

  it('given_array_when_format_then_returns_json_string', () => {
    expect(formatScalarPropertyValue([1, 2])).toBe('[1,2]');
  });

  it('given_null_when_format_then_returns_missing_placeholder', () => {
    expect(formatScalarPropertyValue(null)).toBe(MISSING_VALUE_PLACEHOLDER);
  });

  it('given_undefined_when_format_then_returns_missing_placeholder', () => {
    expect(formatScalarPropertyValue(undefined)).toBe(MISSING_VALUE_PLACEHOLDER);
  });
});

describe('VTX-059 extendedLabels.propertyLabel — renderPropertyExtendedLabel (BDD acceptance)', () => {
  const airport = {
    __rid: 'ri.oss.main.object.airport-jfk',
    __apiName: 'Airport',
    __primaryKey: 'JFK',
    onTimePct: '92%',
  };

  // BDD #1
  it('given_property_present_when_render_then_shows_name_colon_value', () => {
    const spec: PropertyExtendedLabelSpec = { kind: 'property', property: 'onTimePct' };
    const result = renderPropertyExtendedLabel(spec, airport);
    expect(result.text).toBe('onTimePct: 92%');
    expect(result.labelName).toBe('onTimePct');
    expect(result.valueText).toBe('92%');
    expect(result.status).toBe('present');
  });

  // BDD #2
  it('given_property_missing_when_render_then_shows_em_dash_placeholder', () => {
    const spec: PropertyExtendedLabelSpec = { kind: 'property', property: 'onTimePct' };
    const result = renderPropertyExtendedLabel(spec, {
      __rid: 'r',
      __apiName: 'Airport',
      __primaryKey: 'JFK',
    });
    expect(result.text).toBe('onTimePct: —');
    expect(result.labelName).toBe('onTimePct');
    expect(result.valueText).toBe(MISSING_VALUE_PLACEHOLDER);
    expect(result.status).toBe('missing');
  });

  it('given_displayName_present_when_render_then_uses_displayName_as_label', () => {
    const spec: PropertyExtendedLabelSpec = {
      kind: 'property',
      property: 'onTimePct',
      displayName: 'On-Time %',
    };
    const result = renderPropertyExtendedLabel(spec, airport);
    expect(result.text).toBe('On-Time %: 92%');
    expect(result.labelName).toBe('On-Time %');
  });

  it('given_displayName_blank_when_render_then_falls_back_to_property', () => {
    const spec: PropertyExtendedLabelSpec = {
      kind: 'property',
      property: 'onTimePct',
      displayName: '   ',
    };
    const result = renderPropertyExtendedLabel(spec, airport);
    expect(result.labelName).toBe('onTimePct');
  });

  it('given_explicit_null_value_when_render_then_status_missing', () => {
    const spec: PropertyExtendedLabelSpec = { kind: 'property', property: 'onTimePct' };
    const result = renderPropertyExtendedLabel(spec, {
      __rid: 'r',
      __apiName: 'Airport',
      __primaryKey: 'JFK',
      onTimePct: null,
    });
    expect(result.status).toBe('missing');
    expect(result.text).toBe('onTimePct: —');
  });

  it('given_zero_value_when_render_then_status_present_and_shows_zero', () => {
    const spec: PropertyExtendedLabelSpec = { kind: 'property', property: 'onTimePct' };
    const result = renderPropertyExtendedLabel(spec, {
      __rid: 'r',
      __apiName: 'Airport',
      __primaryKey: 'JFK',
      onTimePct: 0,
    });
    expect(result.status).toBe('present');
    expect(result.text).toBe('onTimePct: 0');
  });

  it('given_boolean_false_value_when_render_then_status_present_and_shows_false', () => {
    const spec: PropertyExtendedLabelSpec = { kind: 'property', property: 'active' };
    const result = renderPropertyExtendedLabel(spec, {
      __rid: 'r',
      __apiName: 'Airport',
      __primaryKey: 'JFK',
      active: false,
    });
    expect(result.status).toBe('present');
    expect(result.text).toBe('active: false');
  });

  it('given_custom_formatter_when_render_then_uses_formatter_output', () => {
    const spec: PropertyExtendedLabelSpec = { kind: 'property', property: 'onTimePct' };
    const result = renderPropertyExtendedLabel(
      spec,
      { __rid: 'r', __apiName: 'Airport', __primaryKey: 'JFK', onTimePct: 0.92 },
      { formatValue: (raw) => `${Math.round(Number(raw) * 100)}%` },
    );
    expect(result.text).toBe('onTimePct: 92%');
    expect(result.valueText).toBe('92%');
    expect(result.status).toBe('present');
  });

  it('given_custom_missing_placeholder_when_render_then_uses_override', () => {
    const spec: PropertyExtendedLabelSpec = { kind: 'property', property: 'onTimePct' };
    const result = renderPropertyExtendedLabel(
      spec,
      { __rid: 'r', __apiName: 'Airport', __primaryKey: 'JFK' },
      { missingPlaceholder: 'N/A' },
    );
    expect(result.text).toBe('onTimePct: N/A');
    expect(result.valueText).toBe('N/A');
    expect(result.status).toBe('missing');
  });

  it('given_blank_property_in_spec_when_render_then_throws', () => {
    const spec: PropertyExtendedLabelSpec = { kind: 'property', property: '' };
    expect(() => renderPropertyExtendedLabel(spec, airport)).toThrow(/property/i);
  });

  it('given_non_property_kind_in_spec_when_render_then_throws', () => {
    // Defensive: caller should pre-filter with isPropertyExtendedLabel.
    expect(() =>
      renderPropertyExtendedLabel(
        { kind: 'timeSeries' as 'property', property: 'x' },
        airport,
      ),
    ).toThrow(/property/i);
  });
});
