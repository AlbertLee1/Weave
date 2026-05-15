// VTX-020: pure selection state for the Vertex canvas.
//
// The selection layer + sidebar + node-highlight reducer all share this
// shape. Treat it as an immutable read-only set of objectRids; mutation
// helpers return a new state so consumers can compare by reference and
// skip rerenders when nothing changed (e.g. clicking the same node).
//
// Lives under features/vertex/selections so the saved-selection helpers
// (savedSelections.ts) and the active-selection helpers stay namespaced
// together — they share the rid string contract but otherwise serve
// different concerns (named, persistable groups vs. one ephemeral set).

export type SelectionState = ReadonlySet<string>;

export const EMPTY_SELECTION: SelectionState = Object.freeze(new Set<string>()) as ReadonlySet<string>;

function clone(state: SelectionState): Set<string> {
  return new Set(state);
}

export function selectionSize(state: SelectionState): number {
  return state.size;
}

export function selectionHas(state: SelectionState, rid: string): boolean {
  return state.has(rid);
}

export function selectionSingle(_state: SelectionState, rid: string): SelectionState {
  return new Set([rid]);
}

export function selectionToggle(state: SelectionState, rid: string): SelectionState {
  const next = clone(state);
  if (next.has(rid)) next.delete(rid);
  else next.add(rid);
  return next;
}

export function selectionReplace(
  state: SelectionState,
  rids: Iterable<string>,
): SelectionState {
  const next = new Set(rids);
  if (state.size === 0 && next.size === 0) return EMPTY_SELECTION;
  return next;
}

export function selectionAdd(
  state: SelectionState,
  rids: Iterable<string>,
): SelectionState {
  const next = clone(state);
  for (const rid of rids) next.add(rid);
  return next;
}

export function selectionClear(): SelectionState {
  return EMPTY_SELECTION;
}

export function selectionToArray(state: SelectionState): string[] {
  return Array.from(state);
}
