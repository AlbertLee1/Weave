export interface LayerNode {
  id: string;
  objectType: string;
  color?: string;
}

export interface Layer {
  objectType: string;
  count: number;
  visible: boolean;
  color: string;
}

const DEFAULT_LAYER_COLOR = '#6B7280';

export function groupNodesByLayer(nodes: LayerNode[]): Layer[] {
  const byType = new Map<string, { count: number; color: string }>();
  for (const node of nodes) {
    const existing = byType.get(node.objectType);
    if (existing) {
      existing.count += 1;
    } else {
      byType.set(node.objectType, {
        count: 1,
        color: node.color ?? DEFAULT_LAYER_COLOR,
      });
    }
  }
  return [...byType.entries()].map(([objectType, { count, color }]) => ({
    objectType,
    count,
    visible: true,
    color,
  }));
}

export function toggleLayerVisibility(layers: Layer[], objectType: string): Layer[] {
  return layers.map((l) =>
    l.objectType === objectType ? { ...l, visible: !l.visible } : l,
  );
}

export function computeHiddenNodes(
  nodes: LayerNode[],
  layers: Layer[],
): Set<string> {
  const hiddenTypes = new Set(
    layers.filter((l) => !l.visible).map((l) => l.objectType),
  );
  const hidden = new Set<string>();
  for (const node of nodes) {
    if (hiddenTypes.has(node.objectType)) hidden.add(node.id);
  }
  return hidden;
}
