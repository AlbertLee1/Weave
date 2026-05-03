import type { LayoutNode } from '../../api/apps';

// US-392: shared layout helpers + constants for the App Editor. Lives
// in its own module so the page component file can stay component-only
// (eslint react-refresh rule complains otherwise).

export const MIN_COL_WIDTH = 2; // 12 / 6 = 2, so up to 6 cols fit.
export const MAX_COLUMNS = 6;
export const PALETTE_DND_MIME = 'application/x-weave-app-palette';
export const INSTANCE_DND_MIME = 'application/x-weave-app-instance';

export type ComponentType =
  | 'table'
  | 'form'
  | 'chart'
  | 'button'
  | 'objectCard'
  | 'text';

export interface ComponentTypeMeta {
  type: ComponentType;
  label: string;
  description: string;
  defaultProps: Record<string, unknown>;
}

export const COMPONENT_TYPES: ComponentTypeMeta[] = [
  {
    type: 'table',
    label: 'Table',
    description: 'Tabular ObjectSet view',
    defaultProps: { objectSet: '', columns: [] },
  },
  {
    type: 'form',
    label: 'Form',
    description: 'ActionType form input',
    defaultProps: { actionType: '' },
  },
  {
    type: 'chart',
    label: 'Chart',
    description: 'Aggregation chart',
    defaultProps: { chartType: 'bar', title: 'Chart' },
  },
  {
    type: 'button',
    label: 'Action Button',
    description: 'Trigger an ActionType',
    defaultProps: { label: 'Run Action', actionType: '' },
  },
  {
    type: 'objectCard',
    label: 'Object Card',
    description: 'Single-object summary',
    defaultProps: { objectType: '', objectId: '' },
  },
  {
    type: 'text',
    label: 'Text',
    description: 'Markdown / static copy',
    defaultProps: { content: '' },
  },
];

export interface ComponentInstance {
  id: string;
  componentType: ComponentType;
  props: Record<string, unknown>;
}

let instanceIdCounter = 0;
export function nextInstanceId(): string {
  instanceIdCounter += 1;
  return `inst-${instanceIdCounter}-${Date.now().toString(36)}`;
}

export function makeInstance(type: ComponentType): ComponentInstance {
  const meta = COMPONENT_TYPES.find((c) => c.type === type);
  return {
    id: nextInstanceId(),
    componentType: type,
    props: { ...(meta?.defaultProps ?? {}) },
  };
}

// distributeWidths splits 12 grid columns across N components such that
// the sum is exactly 12 and every share is ≥ MIN_COL_WIDTH (=2).
// Remainder is biased to the leading cols (so a 5-component row is
// [3,3,2,2,2]).
export function distributeWidths(count: number): number[] {
  if (count <= 0) return [];
  const total = 12;
  const base = Math.floor(total / count);
  const remainder = total - base * count;
  return Array.from({ length: count }, (_, i) =>
    Math.max(MIN_COL_WIDTH, base + (i < remainder ? 1 : 0)),
  );
}

// instancesToLayout encodes the canvas as the canonical layout DSL: a
// single top-level row with one col per component. Empty canvases land
// as a single-component placeholder so the resulting JSON still passes
// pkg/apps/layout.go::ValidateLayout (the AC for US-391's wire shape).
export function instancesToLayout(
  instances: ComponentInstance[],
): LayoutNode {
  if (instances.length === 0) {
    return {
      type: 'row',
      children: [
        {
          type: 'col',
          width: 12,
          child: { type: 'component', componentType: 'text', props: {} },
        },
      ],
    };
  }
  const widths = distributeWidths(instances.length);
  return {
    type: 'row',
    children: instances.map((inst, idx) => ({
      type: 'col',
      width: widths[idx],
      child: {
        type: 'component',
        componentType: inst.componentType,
        props: inst.props,
      },
    })),
  };
}

// instancesFromLayout walks a layout JSON and returns its component
// children as a flat list. Multi-row layouts (nested rows inside cols)
// are flattened depth-first so future stories that build richer
// layouts still load round-trip in the simple canvas. Anything it
// doesn't recognise lands as an empty list — the editor falls back to
// a blank canvas rather than crashing on unknown payloads.
export function instancesFromLayout(
  node: LayoutNode | undefined,
): ComponentInstance[] {
  if (!node) return [];
  const out: ComponentInstance[] = [];
  walkLayout(node, out);
  return out;
}

function walkLayout(node: LayoutNode, out: ComponentInstance[]): void {
  if (node.type === 'component') {
    out.push({
      id: nextInstanceId(),
      componentType: node.componentType as ComponentType,
      props: node.props ?? {},
    });
    return;
  }
  if (node.type === 'col') {
    walkLayout(node.child, out);
    return;
  }
  for (const child of node.children) {
    walkLayout(child, out);
  }
}
