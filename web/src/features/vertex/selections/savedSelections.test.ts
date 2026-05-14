import { describe, it, expect } from 'vitest';
import {
  computeNodeBorders,
  createSelection,
  toggleSelectionVisibility,
  type SavedSelection,
} from './savedSelections';

describe('VTX-066 createSelection', () => {
  it('given_5_NodeIds_when_Created_then_ReturnsSelectionWithMembers', () => {
    const s = createSelection({
      id: 'sel1',
      name: 'Hubs',
      color: '#3FB36F',
      nodeIds: ['n1', 'n2', 'n3', 'n4', 'n5'],
    });
    expect(s.id).toBe('sel1');
    expect(s.name).toBe('Hubs');
    expect(s.color).toBe('#3FB36F');
    expect(s.nodeIds.size).toBe(5);
    expect(s.visible).toBe(true);
  });

  it('given_DuplicateNodeIds_when_Created_then_Deduplicated', () => {
    const s = createSelection({ id: 's', name: 'x', color: '#000', nodeIds: ['a', 'a', 'b'] });
    expect(s.nodeIds.size).toBe(2);
  });
});

describe('VTX-066 computeNodeBorders', () => {
  const sel1: SavedSelection = createSelection({
    id: 'sel1',
    name: 'Hubs',
    color: '#3FB36F',
    nodeIds: ['n1', 'n2', 'n3'],
  });
  const sel2: SavedSelection = createSelection({
    id: 'sel2',
    name: 'Critical',
    color: '#EF4444',
    nodeIds: ['n2', 'n5'],
  });

  it('given_NodeInOneSelection_then_OneBorder', () => {
    const borders = computeNodeBorders('n1', [sel1, sel2]);
    expect(borders).toEqual([{ color: '#3FB36F', selectionId: 'sel1' }]);
  });

  it('given_NodeInTwoSelections_then_TwoBordersInOrder', () => {
    const borders = computeNodeBorders('n2', [sel1, sel2]);
    expect(borders).toEqual([
      { color: '#3FB36F', selectionId: 'sel1' },
      { color: '#EF4444', selectionId: 'sel2' },
    ]);
  });

  it('given_NodeInNoSelection_then_EmptyBorders', () => {
    const borders = computeNodeBorders('nX', [sel1, sel2]);
    expect(borders).toEqual([]);
  });

  it('given_SelectionIsHidden_then_BorderOmitted', () => {
    const hiddenSel1 = toggleSelectionVisibility(sel1);
    const borders = computeNodeBorders('n2', [hiddenSel1, sel2]);
    expect(borders).toEqual([{ color: '#EF4444', selectionId: 'sel2' }]);
  });
});

describe('VTX-066 toggleSelectionVisibility', () => {
  it('given_VisibleSelection_when_Toggled_then_BecomesHidden', () => {
    const s = createSelection({ id: 's', name: 'x', color: '#000', nodeIds: ['a'] });
    expect(s.visible).toBe(true);
    expect(toggleSelectionVisibility(s).visible).toBe(false);
  });

  it('given_Toggle_then_DoesNotMutateOriginal', () => {
    const s = createSelection({ id: 's', name: 'x', color: '#000', nodeIds: ['a'] });
    toggleSelectionVisibility(s);
    expect(s.visible).toBe(true);
  });
});
