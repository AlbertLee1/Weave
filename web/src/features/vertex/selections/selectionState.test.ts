import { describe, it, expect } from 'vitest';

import {
  EMPTY_SELECTION,
  selectionAdd,
  selectionClear,
  selectionHas,
  selectionReplace,
  selectionSingle,
  selectionSize,
  selectionToggle,
  type SelectionState,
} from './selectionState';

describe('VTX-020 selectionState helpers', () => {
  it('Given_emptyState_When_selectionSingle_Then_returnsStateWithOnlyThatRid', () => {
    const next = selectionSingle(EMPTY_SELECTION, 'ri.airport.JFK');
    expect(selectionSize(next)).toBe(1);
    expect(selectionHas(next, 'ri.airport.JFK')).toBe(true);
  });

  it('Given_stateWithRids_When_selectionSingle_Then_replacesPreviousRids', () => {
    const start = selectionReplace(EMPTY_SELECTION, ['ri.A', 'ri.B', 'ri.C']);
    const next = selectionSingle(start, 'ri.D');
    expect(selectionSize(next)).toBe(1);
    expect(selectionHas(next, 'ri.D')).toBe(true);
    expect(selectionHas(next, 'ri.A')).toBe(false);
  });

  it('Given_stateMissingRid_When_selectionToggle_Then_addsIt', () => {
    const start = selectionReplace(EMPTY_SELECTION, ['ri.A']);
    const next = selectionToggle(start, 'ri.B');
    expect(selectionSize(next)).toBe(2);
    expect(selectionHas(next, 'ri.A')).toBe(true);
    expect(selectionHas(next, 'ri.B')).toBe(true);
  });

  it('Given_stateContainsRid_When_selectionToggle_Then_removesIt', () => {
    const start = selectionReplace(EMPTY_SELECTION, ['ri.A', 'ri.B']);
    const next = selectionToggle(start, 'ri.A');
    expect(selectionSize(next)).toBe(1);
    expect(selectionHas(next, 'ri.A')).toBe(false);
    expect(selectionHas(next, 'ri.B')).toBe(true);
  });

  it('Given_anyState_When_selectionClear_Then_returnsEmpty', () => {
    const start = selectionReplace(EMPTY_SELECTION, ['ri.A', 'ri.B']);
    const next = selectionClear();
    expect(selectionSize(next)).toBe(0);
    expect(next).toBe(EMPTY_SELECTION);
    expect(start).not.toBe(next);
  });

  it('Given_emptyState_When_selectionAdd_Then_unionsRids', () => {
    const next = selectionAdd(EMPTY_SELECTION, ['ri.A', 'ri.B', 'ri.A']);
    expect(selectionSize(next)).toBe(2);
    expect(selectionHas(next, 'ri.A')).toBe(true);
    expect(selectionHas(next, 'ri.B')).toBe(true);
  });

  it('Given_existingState_When_selectionAddOverlapping_Then_dedupes', () => {
    const start = selectionReplace(EMPTY_SELECTION, ['ri.A']);
    const next = selectionAdd(start, ['ri.A', 'ri.B']);
    expect(selectionSize(next)).toBe(2);
  });

  it('selectionReplace returns the same identity when input is empty + state is empty', () => {
    // Helps consumers skip rerenders by reference compare.
    const next = selectionReplace(EMPTY_SELECTION, []);
    expect(next).toBe(EMPTY_SELECTION);
  });

  it('Selection state is immutable: helpers do not mutate input', () => {
    const start = selectionReplace(EMPTY_SELECTION, ['ri.A']);
    const startSnapshot: SelectionState = start;
    selectionToggle(start, 'ri.B');
    selectionSingle(start, 'ri.C');
    selectionAdd(start, ['ri.D']);
    // start still has exactly {ri.A}
    expect(selectionSize(startSnapshot)).toBe(1);
    expect(selectionHas(startSnapshot, 'ri.A')).toBe(true);
  });
});
