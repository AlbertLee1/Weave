import { describe, it, expect } from 'vitest';
import {
  groupNodesByLayer,
  toggleLayerVisibility,
  computeHiddenNodes,
  type LayerNode,
  type Layer,
} from './layerGrouping';

const nodes: LayerNode[] = [
  { id: 'a1', objectType: 'Airport' },
  { id: 'a2', objectType: 'Airport' },
  { id: 'a3', objectType: 'Airport' },
  { id: 'r1', objectType: 'Route' },
  { id: 'r2', objectType: 'Route' },
  { id: 'f1', objectType: 'Flight' },
];

describe('VTX-071 groupNodesByLayer', () => {
  it('given_MixedNodes_when_Group_then_OneLayerPerObjectType', () => {
    const layers = groupNodesByLayer(nodes);
    expect(layers).toHaveLength(3);
    const byType = Object.fromEntries(layers.map((l) => [l.objectType, l]));
    expect(byType.Airport.count).toBe(3);
    expect(byType.Route.count).toBe(2);
    expect(byType.Flight.count).toBe(1);
  });

  it('given_AllNodesSameType_when_Group_then_OneLayer', () => {
    const all = [
      { id: 'a1', objectType: 'Airport' },
      { id: 'a2', objectType: 'Airport' },
    ];
    const layers = groupNodesByLayer(all);
    expect(layers).toHaveLength(1);
    expect(layers[0].count).toBe(2);
  });

  it('given_EmptyNodes_when_Group_then_EmptyLayers', () => {
    expect(groupNodesByLayer([])).toEqual([]);
  });

  it('given_Layers_when_Group_then_AllVisibleByDefault', () => {
    const layers = groupNodesByLayer(nodes);
    expect(layers.every((l) => l.visible)).toBe(true);
  });

  it('given_NodeCustomColor_when_Group_then_LayerHasFirstNodeColor', () => {
    const withColors = [
      { id: 'a1', objectType: 'Airport', color: '#FF0000' },
      { id: 'a2', objectType: 'Airport', color: '#00FF00' },
    ];
    const layers = groupNodesByLayer(withColors);
    expect(layers[0].color).toBe('#FF0000');
  });
});

describe('VTX-071 toggleLayerVisibility', () => {
  it('given_VisibleLayer_when_Toggle_then_Hidden', () => {
    const layers = groupNodesByLayer(nodes);
    const next = toggleLayerVisibility(layers, 'Airport');
    const airport = next.find((l) => l.objectType === 'Airport')!;
    expect(airport.visible).toBe(false);
  });

  it('given_Toggle_then_OnlyTargetLayerChanges', () => {
    const layers = groupNodesByLayer(nodes);
    const next = toggleLayerVisibility(layers, 'Route');
    expect(next.find((l) => l.objectType === 'Airport')!.visible).toBe(true);
    expect(next.find((l) => l.objectType === 'Route')!.visible).toBe(false);
    expect(next.find((l) => l.objectType === 'Flight')!.visible).toBe(true);
  });

  it('given_Toggle_then_OriginalArrayNotMutated', () => {
    const layers = groupNodesByLayer(nodes);
    toggleLayerVisibility(layers, 'Airport');
    expect(layers.find((l) => l.objectType === 'Airport')!.visible).toBe(true);
  });
});

describe('VTX-071 computeHiddenNodes', () => {
  it('given_OneHiddenLayer_when_Compute_then_AllItsNodesHidden', () => {
    const layers: Layer[] = [
      { objectType: 'Airport', count: 3, visible: false, color: '#000' },
      { objectType: 'Route', count: 2, visible: true, color: '#000' },
    ];
    const hidden = computeHiddenNodes(nodes, layers);
    expect([...hidden].sort()).toEqual(['a1', 'a2', 'a3']);
  });

  it('given_AllLayersVisible_when_Compute_then_EmptySet', () => {
    const layers = groupNodesByLayer(nodes);
    expect(computeHiddenNodes(nodes, layers).size).toBe(0);
  });
});
