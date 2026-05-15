export interface SavedSelection {
  id: string;
  name: string;
  color: string;
  nodeIds: Set<string>;
  visible: boolean;
}

export interface NodeBorder {
  color: string;
  selectionId: string;
}

export function createSelection(input: {
  id: string;
  name: string;
  color: string;
  nodeIds: Iterable<string>;
}): SavedSelection {
  return {
    id: input.id,
    name: input.name,
    color: input.color,
    nodeIds: new Set(input.nodeIds),
    visible: true,
  };
}

export function toggleSelectionVisibility(sel: SavedSelection): SavedSelection {
  return { ...sel, visible: !sel.visible };
}

export function computeNodeBorders(
  nodeId: string,
  selections: SavedSelection[],
): NodeBorder[] {
  const borders: NodeBorder[] = [];
  for (const sel of selections) {
    if (!sel.visible) continue;
    if (sel.nodeIds.has(nodeId)) {
      borders.push({ color: sel.color, selectionId: sel.id });
    }
  }
  return borders;
}
