// VTX-063 — Badges（Linked Events 计数 + 自定义图标）
//
// BDD acceptance（来自 prd.json VTX-063）：
//   1. Given badge {kind:'linkedEvents', objectType:'FlightDelay'}
//      When 渲染 Then 节点角标显示 FlightDelay 数量 + 图标
//   2. Given linkedEvents 计数 > 0
//      When 用户 hover Then tooltip 列前 5 个 event

import { describe, expect, it } from 'vitest';

import {
  BADGE_DEFAULT_ICON,
  BADGE_TOOLTIP_MAX_EVENTS,
  ERROR_PLACEHOLDER,
  buildLinkedEventsBadgeRequest,
  buildLinkedEventsBadgeTooltipItems,
  isLinkedEventsBadge,
  renderLinkedEventsBadge,
  type LinkedEventsBadgeSpec,
  type LinkedEventsBadgeEventLike,
} from './linkedEventsBadge';

describe('VTX-063 badges.linkedEventsBadge — isLinkedEventsBadge', () => {
  it('given_kind_linkedEvents_when_check_then_true', () => {
    expect(
      isLinkedEventsBadge({
        kind: 'linkedEvents',
        objectType: 'FlightDelay',
        linkType: 'delays',
      }),
    ).toBe(true);
  });

  it('given_kind_property_when_check_then_false', () => {
    expect(isLinkedEventsBadge({ kind: 'property' })).toBe(false);
  });

  it('given_kind_timeSeries_when_check_then_false', () => {
    expect(isLinkedEventsBadge({ kind: 'timeSeries' })).toBe(false);
  });

  it('given_kind_measure_when_check_then_false', () => {
    expect(isLinkedEventsBadge({ kind: 'measure' })).toBe(false);
  });

  it('given_kind_unknown_when_check_then_false', () => {
    expect(isLinkedEventsBadge({ kind: 'histogram' })).toBe(false);
  });
});

describe('VTX-063 badges.linkedEventsBadge — buildLinkedEventsBadgeRequest', () => {
  const spec: LinkedEventsBadgeSpec = {
    kind: 'linkedEvents',
    objectType: 'FlightDelay',
    linkType: 'delays',
  };
  const ctx = {
    ontology: 'ri.ontology.main.ontology.vtx',
    sourceObjectType: 'Airport',
    sourcePrimaryKey: 'JFK',
  };

  it('given_spec_and_context_when_build_then_get_with_oss_links_path', () => {
    const r = buildLinkedEventsBadgeRequest(spec, ctx);
    expect(r.method).toBe('GET');
    expect(r.path).toBe(
      '/api/v2/ontologies/ri.ontology.main.ontology.vtx/objects/Airport/JFK/links/delays' +
        `?pageSize=${BADGE_TOOLTIP_MAX_EVENTS}`,
    );
  });

  it('given_default_tooltip_limit_constant_then_equals_5', () => {
    expect(BADGE_TOOLTIP_MAX_EVENTS).toBe(5);
  });

  it('given_custom_page_size_when_build_then_path_uses_override', () => {
    const r = buildLinkedEventsBadgeRequest(spec, ctx, { pageSize: 10 });
    expect(r.path).toBe(
      '/api/v2/ontologies/ri.ontology.main.ontology.vtx/objects/Airport/JFK/links/delays?pageSize=10',
    );
  });

  it('given_spec_tooltipEventLimit_when_build_then_page_size_matches_limit', () => {
    const limitedSpec: LinkedEventsBadgeSpec = {
      ...spec,
      tooltipEventLimit: 3,
    };
    const r = buildLinkedEventsBadgeRequest(limitedSpec, ctx);
    expect(r.path).toContain('?pageSize=3');
  });

  it('given_special_chars_when_build_then_uri_components_escaped', () => {
    const r = buildLinkedEventsBadgeRequest(
      { kind: 'linkedEvents', objectType: 'Flight Delay', linkType: 'delays/all' },
      {
        ontology: 'ri ont',
        sourceObjectType: 'Air port',
        sourcePrimaryKey: 'JFK 1',
      },
    );
    expect(r.path).toBe(
      '/api/v2/ontologies/ri%20ont/objects/Air%20port/JFK%201/links/delays%2Fall' +
        `?pageSize=${BADGE_TOOLTIP_MAX_EVENTS}`,
    );
  });

  it('given_numeric_primary_key_when_build_then_path_segment_uses_string_value', () => {
    const r = buildLinkedEventsBadgeRequest(spec, {
      ...ctx,
      sourcePrimaryKey: 42,
    });
    expect(r.path).toContain('/objects/Airport/42/links/delays');
  });

  it('given_non_linkedEvents_kind_when_build_then_throws', () => {
    expect(() =>
      buildLinkedEventsBadgeRequest(
        { kind: 'property' as never, objectType: 'X', linkType: 'l' },
        ctx,
      ),
    ).toThrow(/linkedEvents/);
  });

  it('given_blank_objectType_when_build_then_throws', () => {
    expect(() =>
      buildLinkedEventsBadgeRequest(
        { kind: 'linkedEvents', objectType: '   ', linkType: 'delays' },
        ctx,
      ),
    ).toThrow(/objectType/);
  });

  it('given_blank_linkType_when_build_then_throws', () => {
    expect(() =>
      buildLinkedEventsBadgeRequest(
        { kind: 'linkedEvents', objectType: 'FlightDelay', linkType: '' },
        ctx,
      ),
    ).toThrow(/linkType/);
  });

  it('given_blank_ontology_when_build_then_throws', () => {
    expect(() =>
      buildLinkedEventsBadgeRequest(spec, { ...ctx, ontology: '' }),
    ).toThrow(/ontology/);
  });

  it('given_blank_source_objectType_when_build_then_throws', () => {
    expect(() =>
      buildLinkedEventsBadgeRequest(spec, { ...ctx, sourceObjectType: '' }),
    ).toThrow(/sourceObjectType/);
  });

  it('given_blank_source_primary_key_when_build_then_throws', () => {
    expect(() =>
      buildLinkedEventsBadgeRequest(spec, { ...ctx, sourcePrimaryKey: '   ' }),
    ).toThrow(/sourcePrimaryKey/);
  });

  it('given_zero_page_size_when_build_then_throws', () => {
    expect(() =>
      buildLinkedEventsBadgeRequest(spec, ctx, { pageSize: 0 }),
    ).toThrow(/pageSize/);
  });

  it('given_negative_page_size_when_build_then_throws', () => {
    expect(() =>
      buildLinkedEventsBadgeRequest(spec, ctx, { pageSize: -3 }),
    ).toThrow(/pageSize/);
  });
});

describe('VTX-063 badges.linkedEventsBadge — buildLinkedEventsBadgeTooltipItems (BDD #2)', () => {
  const makeEvent = (i: number, overrides: Partial<LinkedEventsBadgeEventLike> = {}) => ({
    __rid: `ri.objects.main.flight-delay.${i}`,
    __apiName: 'FlightDelay',
    __primaryKey: `FD-${i}`,
    eventStart: 1_700_000_000_000 + i * 1_000,
    ...overrides,
  });

  it('given_no_events_when_build_then_empty_array', () => {
    expect(buildLinkedEventsBadgeTooltipItems([])).toEqual([]);
  });

  it('given_three_events_when_build_then_returns_all_three', () => {
    const items = buildLinkedEventsBadgeTooltipItems([
      makeEvent(1),
      makeEvent(2),
      makeEvent(3),
    ]);
    expect(items.length).toBe(3);
    expect(items[0].rid).toBe('ri.objects.main.flight-delay.1');
    expect(items[1].rid).toBe('ri.objects.main.flight-delay.2');
    expect(items[2].rid).toBe('ri.objects.main.flight-delay.3');
  });

  it('given_seven_events_when_build_then_truncated_to_default_5', () => {
    const items = buildLinkedEventsBadgeTooltipItems([
      makeEvent(1),
      makeEvent(2),
      makeEvent(3),
      makeEvent(4),
      makeEvent(5),
      makeEvent(6),
      makeEvent(7),
    ]);
    expect(items.length).toBe(BADGE_TOOLTIP_MAX_EVENTS);
    expect(items.length).toBe(5);
    expect(items[items.length - 1].rid).toBe('ri.objects.main.flight-delay.5');
  });

  it('given_custom_limit_when_build_then_respects_limit', () => {
    const items = buildLinkedEventsBadgeTooltipItems(
      [makeEvent(1), makeEvent(2), makeEvent(3), makeEvent(4)],
      { limit: 2 },
    );
    expect(items.length).toBe(2);
    expect(items[0].rid).toBe('ri.objects.main.flight-delay.1');
    expect(items[1].rid).toBe('ri.objects.main.flight-delay.2');
  });

  it('given_event_with_eventStart_when_build_then_item_carries_start_timestamp', () => {
    const items = buildLinkedEventsBadgeTooltipItems([makeEvent(1)]);
    expect(items[0].eventStart).toBe(1_700_000_000_000 + 1_000);
  });

  it('given_event_without_eventStart_when_build_then_start_undefined', () => {
    const items = buildLinkedEventsBadgeTooltipItems([
      {
        __rid: 'ri.objects.main.flight-delay.NA',
        __apiName: 'FlightDelay',
        __primaryKey: 'FD-NA',
      },
    ]);
    expect(items[0].eventStart).toBeUndefined();
  });

  it('given_event_with_explicit_label_property_when_build_then_uses_label', () => {
    const items = buildLinkedEventsBadgeTooltipItems(
      [makeEvent(1, { description: 'JFK runway 04R closure' })],
      { labelProperty: 'description' },
    );
    expect(items[0].label).toBe('JFK runway 04R closure');
  });

  it('given_event_without_labelProperty_when_build_then_label_falls_back_to_primary_key', () => {
    const items = buildLinkedEventsBadgeTooltipItems([makeEvent(1)]);
    expect(items[0].label).toBe('FD-1');
  });

  it('given_numeric_primary_key_when_build_then_label_string_coerced', () => {
    const items = buildLinkedEventsBadgeTooltipItems([
      {
        __rid: 'ri.objects.main.flight-delay.42',
        __apiName: 'FlightDelay',
        __primaryKey: 42,
      },
    ]);
    expect(items[0].label).toBe('42');
  });

  it('given_zero_limit_when_build_then_empty', () => {
    expect(
      buildLinkedEventsBadgeTooltipItems([makeEvent(1), makeEvent(2)], { limit: 0 }),
    ).toEqual([]);
  });
});

describe('VTX-063 badges.linkedEventsBadge — renderLinkedEventsBadge (BDD #1)', () => {
  const spec: LinkedEventsBadgeSpec = {
    kind: 'linkedEvents',
    objectType: 'FlightDelay',
    linkType: 'delays',
  };

  it('given_count_3_when_render_then_status_present_with_count_and_label_FlightDelay', () => {
    const r = renderLinkedEventsBadge(spec, { count: 3 });
    expect(r.status).toBe('present');
    expect(r.count).toBe(3);
    expect(r.label).toBe('FlightDelay');
    expect(r.text).toBe('FlightDelay: 3');
  });

  it('given_count_0_when_render_then_status_empty', () => {
    const r = renderLinkedEventsBadge(spec, { count: 0 });
    expect(r.status).toBe('empty');
    expect(r.count).toBe(0);
    expect(r.tooltipItems).toEqual([]);
  });

  it('given_default_icon_constant_then_dot', () => {
    expect(BADGE_DEFAULT_ICON).toBe('•');
  });

  it('given_no_custom_icon_when_render_then_uses_default_icon', () => {
    const r = renderLinkedEventsBadge(spec, { count: 1 });
    expect(r.icon).toBe(BADGE_DEFAULT_ICON);
  });

  it('given_custom_icon_when_render_then_uses_spec_icon', () => {
    const r = renderLinkedEventsBadge({ ...spec, icon: '✈' }, { count: 1 });
    expect(r.icon).toBe('✈');
  });

  it('given_displayName_when_render_then_label_overrides_objectType', () => {
    const r = renderLinkedEventsBadge(
      { ...spec, displayName: 'Flight Delays' },
      { count: 1 },
    );
    expect(r.label).toBe('Flight Delays');
    expect(r.text).toBe('Flight Delays: 1');
  });

  it('given_blank_displayName_when_render_then_label_falls_back_to_objectType', () => {
    const r = renderLinkedEventsBadge({ ...spec, displayName: '  ' }, { count: 1 });
    expect(r.label).toBe('FlightDelay');
  });

  it('given_count_present_when_render_then_tooltipItems_lists_events', () => {
    const events: LinkedEventsBadgeEventLike[] = [
      {
        __rid: 'ri.objects.main.flight-delay.1',
        __apiName: 'FlightDelay',
        __primaryKey: 'FD-1',
        eventStart: 1_700_000_000_000,
      },
      {
        __rid: 'ri.objects.main.flight-delay.2',
        __apiName: 'FlightDelay',
        __primaryKey: 'FD-2',
        eventStart: 1_700_001_000_000,
      },
    ];
    const r = renderLinkedEventsBadge(spec, { count: 2, events });
    expect(r.tooltipItems.length).toBe(2);
    expect(r.tooltipItems[0].rid).toBe('ri.objects.main.flight-delay.1');
    expect(r.tooltipItems[1].rid).toBe('ri.objects.main.flight-delay.2');
  });

  it('given_more_than_5_events_when_render_then_tooltipItems_truncated_to_5', () => {
    const events: LinkedEventsBadgeEventLike[] = Array.from({ length: 7 }, (_, i) => ({
      __rid: `ri.objects.main.flight-delay.${i + 1}`,
      __apiName: 'FlightDelay',
      __primaryKey: `FD-${i + 1}`,
    }));
    const r = renderLinkedEventsBadge(spec, { count: 7, events });
    expect(r.tooltipItems.length).toBe(BADGE_TOOLTIP_MAX_EVENTS);
  });

  it('given_spec_tooltipEventLimit_3_when_render_then_items_truncated_to_3', () => {
    const events: LinkedEventsBadgeEventLike[] = Array.from({ length: 7 }, (_, i) => ({
      __rid: `ri.objects.main.flight-delay.${i + 1}`,
      __apiName: 'FlightDelay',
      __primaryKey: `FD-${i + 1}`,
    }));
    const r = renderLinkedEventsBadge(
      { ...spec, tooltipEventLimit: 3 },
      { count: 7, events },
    );
    expect(r.tooltipItems.length).toBe(3);
  });

  it('given_loading_input_when_render_then_status_loading_and_count_null', () => {
    const r = renderLinkedEventsBadge(spec, { loading: true });
    expect(r.status).toBe('loading');
    expect(r.count).toBeNull();
    expect(r.tooltipItems).toEqual([]);
  });

  it('given_error_input_when_render_then_status_error_with_error_message', () => {
    const r = renderLinkedEventsBadge(spec, { error: 'fetch failed' });
    expect(r.status).toBe('error');
    expect(r.count).toBeNull();
    expect(r.icon).toBe(ERROR_PLACEHOLDER);
    expect(r.text).toBe(`FlightDelay: ${ERROR_PLACEHOLDER}`);
    expect(r.errorMessage).toBe('fetch failed');
    expect(r.tooltipItems).toEqual([]);
  });

  it('given_blank_error_when_render_then_error_treated_as_none', () => {
    const r = renderLinkedEventsBadge(spec, { count: 2, error: '' });
    expect(r.status).toBe('present');
    expect(r.count).toBe(2);
  });

  it('given_error_overrides_count_when_render_then_status_error', () => {
    // 即使上游有陈旧 count，error 态优先，避免显示过期数据让用户误以为成功
    const r = renderLinkedEventsBadge(spec, { count: 5, error: 'timeout' });
    expect(r.status).toBe('error');
    expect(r.count).toBeNull();
  });

  it('given_count_negative_when_render_then_throws', () => {
    expect(() => renderLinkedEventsBadge(spec, { count: -1 })).toThrow(/count/);
  });

  it('given_count_not_finite_when_render_then_throws', () => {
    expect(() => renderLinkedEventsBadge(spec, { count: Number.NaN })).toThrow(/count/);
    expect(() => renderLinkedEventsBadge(spec, { count: Infinity })).toThrow(/count/);
  });

  it('given_no_count_no_loading_no_error_when_render_then_status_loading', () => {
    // 接线层未提供任何输入时 → 默认 loading（fetch 尚未启动 / undefined fetch state）
    const r = renderLinkedEventsBadge(spec, {});
    expect(r.status).toBe('loading');
    expect(r.count).toBeNull();
  });

  it('given_non_linkedEvents_kind_when_render_then_throws', () => {
    expect(() =>
      renderLinkedEventsBadge(
        { kind: 'property' as never, objectType: 'X', linkType: 'l' },
        { count: 0 },
      ),
    ).toThrow(/linkedEvents/);
  });

  it('given_blank_objectType_when_render_then_throws', () => {
    expect(() =>
      renderLinkedEventsBadge(
        { kind: 'linkedEvents', objectType: '   ', linkType: 'l' },
        { count: 0 },
      ),
    ).toThrow(/objectType/);
  });

  it('given_blank_linkType_when_render_then_throws', () => {
    expect(() =>
      renderLinkedEventsBadge(
        { kind: 'linkedEvents', objectType: 'X', linkType: '' },
        { count: 0 },
      ),
    ).toThrow(/linkType/);
  });

  it('given_custom_label_property_when_render_then_tooltip_uses_property', () => {
    const events: LinkedEventsBadgeEventLike[] = [
      {
        __rid: 'ri.objects.main.flight-delay.1',
        __apiName: 'FlightDelay',
        __primaryKey: 'FD-1',
        description: 'Snow storm — JFK',
      },
    ];
    const r = renderLinkedEventsBadge(spec, { count: 1, events }, { labelProperty: 'description' });
    expect(r.tooltipItems[0].label).toBe('Snow storm — JFK');
  });
});

describe('VTX-063 badges.linkedEventsBadge — full pipeline (BDD #1 + #2)', () => {
  // BDD #1 端到端：badge {kind:linkedEvents, objectType:FlightDelay} 渲染显示
  //   "FlightDelay: 3" + icon + tooltipItems。
  // BDD #2 端到端：count > 0 时 tooltipItems 列前 5 个 event。
  it('given_badge_spec_with_count_and_5_events_when_render_then_complete_badge_state', () => {
    const spec: LinkedEventsBadgeSpec = {
      kind: 'linkedEvents',
      objectType: 'FlightDelay',
      linkType: 'delays',
      icon: '✈',
    };
    const events: LinkedEventsBadgeEventLike[] = Array.from({ length: 7 }, (_, i) => ({
      __rid: `ri.objects.main.flight-delay.${i + 1}`,
      __apiName: 'FlightDelay',
      __primaryKey: `FD-${i + 1}`,
      eventStart: 1_700_000_000_000 + i * 60_000,
    }));
    const r = renderLinkedEventsBadge(spec, { count: 12, events });
    expect(r.status).toBe('present');
    expect(r.count).toBe(12);
    expect(r.label).toBe('FlightDelay');
    expect(r.icon).toBe('✈');
    expect(r.text).toBe('FlightDelay: 12');
    expect(r.tooltipItems.length).toBe(BADGE_TOOLTIP_MAX_EVENTS);
    expect(r.tooltipItems[0].rid).toBe('ri.objects.main.flight-delay.1');
    expect(r.tooltipItems[4].rid).toBe('ri.objects.main.flight-delay.5');
  });
});
