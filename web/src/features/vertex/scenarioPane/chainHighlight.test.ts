import { describe, expect, it } from 'vitest';

import {
  CHAIN_HIGHLIGHT_BG_CLASS,
  CHAIN_TOOLTIP_PREFIX,
  CHAIN_TOOLTIP_SUFFIX,
  addChainEdge,
  buildChainCellKey,
  buildChainEdgesFromModelSpecs,
  buildChainTooltip,
  clearChainHighlights,
  createChainHighlightMap,
  getCellChainClassNames,
  getCellChainEdge,
  getCellChainTooltip,
  isCellChainImpacted,
  removeChainEdge,
  type ChainEdge,
  type ModelChainSpec,
} from './chainHighlight';

const m1Row = 'row-m1';
const m2Row = 'row-m2';
const m3Row = 'row-m3';

const m1ToM2: ChainEdge = {
  upstreamRowRid: m1Row,
  upstreamModelRid: 'ri.fn.model.m1',
  upstreamModelLabel: 'M1',
  upstreamProperty: 'output',
  downstreamRowRid: m2Row,
  downstreamParamName: 'input',
};

const m2ToM3: ChainEdge = {
  upstreamRowRid: m2Row,
  upstreamModelRid: 'ri.fn.model.m2',
  upstreamModelLabel: 'M2',
  upstreamProperty: 'forecast',
  downstreamRowRid: m3Row,
  downstreamParamName: 'demand',
};

describe('VTX-053 ChainHighlight cell key', () => {
  it('given_row_and_param_when_buildKey_then_returns_composite_key', () => {
    expect(buildChainCellKey(m2Row, 'input')).toBe(`${m2Row}::input`);
  });

  it('given_blank_row_when_buildKey_then_throws', () => {
    expect(() => buildChainCellKey('', 'input')).toThrow();
    expect(() => buildChainCellKey('   ', 'input')).toThrow();
  });

  it('given_blank_param_when_buildKey_then_throws', () => {
    expect(() => buildChainCellKey(m2Row, '')).toThrow();
    expect(() => buildChainCellKey(m2Row, '   ')).toThrow();
  });
});

describe('VTX-053 ChainHighlightMap factory', () => {
  it('given_no_edges_when_create_then_returns_empty_map', () => {
    const map = createChainHighlightMap();
    expect(map).toEqual({});
  });

  it('given_edges_when_create_then_indexed_by_downstream_cell_key', () => {
    const map = createChainHighlightMap([m1ToM2, m2ToM3]);
    expect(map[buildChainCellKey(m2Row, 'input')]).toEqual(m1ToM2);
    expect(map[buildChainCellKey(m3Row, 'demand')]).toEqual(m2ToM3);
  });

  it('given_duplicate_downstream_cell_when_create_then_last_edge_wins', () => {
    const overlap: ChainEdge = { ...m1ToM2, upstreamModelLabel: 'M1-bis' };
    const map = createChainHighlightMap([m1ToM2, overlap]);
    expect(map[buildChainCellKey(m2Row, 'input')]).toEqual(overlap);
  });
});

describe('VTX-053 addChainEdge / removeChainEdge', () => {
  it('given_empty_map_when_addEdge_then_returns_new_map_with_edge', () => {
    const initial = createChainHighlightMap();
    const next = addChainEdge(initial, m1ToM2);
    expect(initial).toEqual({}); // unchanged
    expect(next[buildChainCellKey(m2Row, 'input')]).toEqual(m1ToM2);
  });

  it('given_existing_cell_when_addEdge_then_replaces_existing', () => {
    const map = createChainHighlightMap([m1ToM2]);
    const replacement: ChainEdge = { ...m1ToM2, upstreamModelLabel: 'M1-renamed' };
    const next = addChainEdge(map, replacement);
    expect(next[buildChainCellKey(m2Row, 'input')]).toEqual(replacement);
  });

  it('given_map_with_edge_when_removeEdge_then_drops_entry', () => {
    const map = createChainHighlightMap([m1ToM2, m2ToM3]);
    const next = removeChainEdge(map, m2Row, 'input');
    expect(next[buildChainCellKey(m2Row, 'input')]).toBeUndefined();
    expect(next[buildChainCellKey(m3Row, 'demand')]).toEqual(m2ToM3);
  });

  it('given_unknown_cell_when_removeEdge_then_returns_same_reference', () => {
    const map = createChainHighlightMap([m1ToM2]);
    const next = removeChainEdge(map, 'row-unknown', 'x');
    expect(next).toBe(map);
  });

  it('given_any_map_when_clear_then_returns_empty_map', () => {
    const map = createChainHighlightMap([m1ToM2, m2ToM3]);
    const next = clearChainHighlights();
    expect(next).toEqual({});
    expect(next).not.toBe(map);
  });

  it('given_no_prior_when_clear_then_returns_empty_map_new_ref', () => {
    expect(clearChainHighlights()).toEqual({});
  });
});

describe('VTX-053 isCellChainImpacted', () => {
  it('given_map_with_edge_when_check_matched_cell_then_true', () => {
    const map = createChainHighlightMap([m1ToM2]);
    expect(isCellChainImpacted(map, m2Row, 'input')).toBe(true);
  });

  it('given_map_when_check_unrelated_cell_then_false', () => {
    const map = createChainHighlightMap([m1ToM2]);
    expect(isCellChainImpacted(map, m2Row, 'other-param')).toBe(false);
    expect(isCellChainImpacted(map, 'row-other', 'input')).toBe(false);
  });

  it('given_empty_map_when_check_any_cell_then_false', () => {
    const map = createChainHighlightMap();
    expect(isCellChainImpacted(map, m2Row, 'input')).toBe(false);
  });
});

describe('VTX-053 getCellChainEdge', () => {
  it('given_impacted_cell_when_get_then_returns_edge', () => {
    const map = createChainHighlightMap([m1ToM2]);
    expect(getCellChainEdge(map, m2Row, 'input')).toEqual(m1ToM2);
  });

  it('given_no_edge_when_get_then_returns_null', () => {
    const map = createChainHighlightMap();
    expect(getCellChainEdge(map, m2Row, 'input')).toBeNull();
  });
});

describe('VTX-053 getCellChainTooltip', () => {
  it('given_impacted_cell_when_getTooltip_then_returns_BDD_phrasing', () => {
    const map = createChainHighlightMap([m1ToM2]);
    const tooltip = getCellChainTooltip(map, m2Row, 'input');
    expect(tooltip).toBe('Value from M1.output (not Ontology current state)');
  });

  it('given_unrelated_cell_when_getTooltip_then_null', () => {
    const map = createChainHighlightMap([m1ToM2]);
    expect(getCellChainTooltip(map, m2Row, 'other')).toBeNull();
  });

  it('given_edge_when_buildTooltip_directly_then_uses_constants', () => {
    expect(buildChainTooltip(m1ToM2)).toBe(
      `${CHAIN_TOOLTIP_PREFIX}M1.output${CHAIN_TOOLTIP_SUFFIX}`,
    );
  });

  it('given_constants_when_inspected_then_match_spec_phrasing', () => {
    expect(CHAIN_TOOLTIP_PREFIX).toBe('Value from ');
    expect(CHAIN_TOOLTIP_SUFFIX).toBe(' (not Ontology current state)');
  });
});

describe('VTX-053 getCellChainClassNames', () => {
  it('given_impacted_cell_when_getClassNames_then_returns_BG_class', () => {
    const map = createChainHighlightMap([m1ToM2]);
    expect(getCellChainClassNames(map, m2Row, 'input')).toBe(
      CHAIN_HIGHLIGHT_BG_CLASS,
    );
  });

  it('given_unrelated_cell_when_getClassNames_then_returns_empty_string', () => {
    const map = createChainHighlightMap([m1ToM2]);
    expect(getCellChainClassNames(map, m2Row, 'other')).toBe('');
  });

  it('given_bg_constant_when_inspected_then_is_blue_token', () => {
    // 与 spec "M2 的 input 单元格背景色变蓝（impacted by chained model）" 对齐
    expect(CHAIN_HIGHLIGHT_BG_CLASS).toContain('blue');
  });
});

describe('VTX-053 buildChainEdgesFromModelSpecs', () => {
  it('given_two_models_with_matching_output_and_input_when_build_then_one_edge', () => {
    const specs: ModelChainSpec[] = [
      {
        rowRid: m1Row,
        modelRid: 'ri.fn.model.m1',
        modelLabel: 'M1',
        outputProperties: ['output'],
        inputBindings: [],
      },
      {
        rowRid: m2Row,
        modelRid: 'ri.fn.model.m2',
        modelLabel: 'M2',
        outputProperties: [],
        inputBindings: [{ paramName: 'input', sourceProperty: 'output' }],
      },
    ];
    const edges = buildChainEdgesFromModelSpecs(specs);
    expect(edges).toHaveLength(1);
    expect(edges[0]).toEqual({
      upstreamRowRid: m1Row,
      upstreamModelRid: 'ri.fn.model.m1',
      upstreamModelLabel: 'M1',
      upstreamProperty: 'output',
      downstreamRowRid: m2Row,
      downstreamParamName: 'input',
    });
  });

  it('given_three_model_chain_when_build_then_two_edges', () => {
    const specs: ModelChainSpec[] = [
      {
        rowRid: m1Row,
        modelRid: 'ri.fn.model.m1',
        modelLabel: 'M1',
        outputProperties: ['output'],
        inputBindings: [],
      },
      {
        rowRid: m2Row,
        modelRid: 'ri.fn.model.m2',
        modelLabel: 'M2',
        outputProperties: ['forecast'],
        inputBindings: [{ paramName: 'input', sourceProperty: 'output' }],
      },
      {
        rowRid: m3Row,
        modelRid: 'ri.fn.model.m3',
        modelLabel: 'M3',
        outputProperties: [],
        inputBindings: [{ paramName: 'demand', sourceProperty: 'forecast' }],
      },
    ];
    const edges = buildChainEdgesFromModelSpecs(specs);
    expect(edges).toHaveLength(2);
    const cellKeys = edges.map(e =>
      buildChainCellKey(e.downstreamRowRid, e.downstreamParamName),
    );
    expect(cellKeys).toContain(buildChainCellKey(m2Row, 'input'));
    expect(cellKeys).toContain(buildChainCellKey(m3Row, 'demand'));
  });

  it('given_no_property_overlap_when_build_then_empty', () => {
    const specs: ModelChainSpec[] = [
      {
        rowRid: m1Row,
        modelRid: 'ri.fn.model.m1',
        modelLabel: 'M1',
        outputProperties: ['alpha'],
        inputBindings: [],
      },
      {
        rowRid: m2Row,
        modelRid: 'ri.fn.model.m2',
        modelLabel: 'M2',
        outputProperties: [],
        inputBindings: [{ paramName: 'input', sourceProperty: 'beta' }],
      },
    ];
    expect(buildChainEdgesFromModelSpecs(specs)).toEqual([]);
  });

  it('given_model_with_self_output_input_match_when_build_then_skips_self_loop', () => {
    const specs: ModelChainSpec[] = [
      {
        rowRid: m1Row,
        modelRid: 'ri.fn.model.m1',
        modelLabel: 'M1',
        outputProperties: ['p'],
        inputBindings: [{ paramName: 'p', sourceProperty: 'p' }],
      },
    ];
    expect(buildChainEdgesFromModelSpecs(specs)).toEqual([]);
  });

  it('given_input_binding_without_sourceProperty_when_build_then_skipped', () => {
    const specs: ModelChainSpec[] = [
      {
        rowRid: m1Row,
        modelRid: 'ri.fn.model.m1',
        modelLabel: 'M1',
        outputProperties: ['output'],
        inputBindings: [],
      },
      {
        rowRid: m2Row,
        modelRid: 'ri.fn.model.m2',
        modelLabel: 'M2',
        outputProperties: [],
        inputBindings: [
          { paramName: 'input', sourceProperty: undefined },
          { paramName: 'capacity', sourceProperty: 'output' },
        ],
      },
    ];
    const edges = buildChainEdgesFromModelSpecs(specs);
    expect(edges).toHaveLength(1);
    expect(edges[0].downstreamParamName).toBe('capacity');
  });

  it('given_multiple_upstreams_for_same_input_when_build_then_first_match_wins', () => {
    // 两个上游 model 都声明 output property "p"；语义是 ambiguous，本 helper
    // 选 "specs 中靠前的 producer 胜出" 让结果稳定（与 modelmesh BuildDependencyGraph
    // 的 sorted-edges 思路一致）。React 接线层若要呈现冲突，可在构建 specs 前先去重。
    const specs: ModelChainSpec[] = [
      {
        rowRid: m1Row,
        modelRid: 'ri.fn.model.m1',
        modelLabel: 'M1',
        outputProperties: ['p'],
        inputBindings: [],
      },
      {
        rowRid: 'row-mA',
        modelRid: 'ri.fn.model.mA',
        modelLabel: 'MA',
        outputProperties: ['p'],
        inputBindings: [],
      },
      {
        rowRid: m2Row,
        modelRid: 'ri.fn.model.m2',
        modelLabel: 'M2',
        outputProperties: [],
        inputBindings: [{ paramName: 'input', sourceProperty: 'p' }],
      },
    ];
    const edges = buildChainEdgesFromModelSpecs(specs);
    expect(edges).toHaveLength(1);
    expect(edges[0].upstreamRowRid).toBe(m1Row);
  });

  it('given_blank_rowRid_in_spec_when_build_then_throws', () => {
    const specs: ModelChainSpec[] = [
      {
        rowRid: '',
        modelRid: 'ri.fn.model.m1',
        modelLabel: 'M1',
        outputProperties: ['output'],
        inputBindings: [],
      },
    ];
    expect(() => buildChainEdgesFromModelSpecs(specs)).toThrow();
  });

  it('given_blank_modelLabel_when_build_then_throws', () => {
    const specs: ModelChainSpec[] = [
      {
        rowRid: m1Row,
        modelRid: 'ri.fn.model.m1',
        modelLabel: '   ',
        outputProperties: ['output'],
        inputBindings: [],
      },
    ];
    expect(() => buildChainEdgesFromModelSpecs(specs)).toThrow();
  });
});

describe('VTX-053 end-to-end happy path', () => {
  it('given_M1_runs_M2_input_from_M1_output_when_render_M2_input_cell_then_blue_bg_and_BDD_tooltip', () => {
    // BDD #1: M1 已跑 + M2 input 从 M1 output → M2 input 单元格背景色变蓝。
    // BDD #2: hover → tooltip 显示 Value from M1.output (not Ontology current state)。
    const specs: ModelChainSpec[] = [
      {
        rowRid: m1Row,
        modelRid: 'ri.fn.model.m1',
        modelLabel: 'M1',
        outputProperties: ['output'],
        inputBindings: [],
      },
      {
        rowRid: m2Row,
        modelRid: 'ri.fn.model.m2',
        modelLabel: 'M2',
        outputProperties: [],
        inputBindings: [{ paramName: 'input', sourceProperty: 'output' }],
      },
    ];
    const map = createChainHighlightMap(buildChainEdgesFromModelSpecs(specs));

    expect(isCellChainImpacted(map, m2Row, 'input')).toBe(true);
    expect(getCellChainClassNames(map, m2Row, 'input')).toBe(
      CHAIN_HIGHLIGHT_BG_CLASS,
    );
    expect(getCellChainTooltip(map, m2Row, 'input')).toBe(
      'Value from M1.output (not Ontology current state)',
    );

    // 同 M2 的另一个普通 input 不受 chain 影响
    expect(isCellChainImpacted(map, m2Row, 'other')).toBe(false);
    expect(getCellChainClassNames(map, m2Row, 'other')).toBe('');
    expect(getCellChainTooltip(map, m2Row, 'other')).toBeNull();
  });
});
