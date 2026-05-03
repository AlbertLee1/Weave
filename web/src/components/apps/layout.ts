import type {
  AppEvent,
  AppEventMap,
  AppVariable,
  AppVariableType,
  LayoutNode,
} from '../../api/apps';

export type { AppEvent, AppEventMap, AppVariable, AppVariableType };

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
    // US-395: Table binds an ObjectSet (RID or base ObjectType API name)
    // + a columns list. pageSize / orderByField / filterField are
    // optional runtime knobs surfaced in the property panel.
    defaultProps: {
      objectSet: '',
      columns: [],
      pageSize: 25,
      orderByField: '',
      orderByDirection: 'asc',
      filterField: '',
      filterOp: 'eq',
      filterValue: '',
    },
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
  // events is the optional onClick handler bag (US-393). Older fixtures
  // pre-US-393 don't set this; callers that need to read/write it
  // should default-spread an empty object via `instance.events ?? {}`.
  events?: AppEventMap;
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
    events: {},
  };
}

// US-393: app-level Variables. Names are unique and must match
// /^[A-Za-z_][A-Za-z0-9_]*$/ so {{var}} template substitution is
// unambiguous. Defaults travel as strings on the wire and are coerced to
// the declared type at runtime by `coerceVariableValue`.
export const VARIABLE_NAME_PATTERN = /^[A-Za-z_][A-Za-z0-9_]*$/;
export const VARIABLE_TYPES: AppVariableType[] = [
  'string',
  'number',
  'boolean',
];

export function makeVariable(
  name: string,
  type: AppVariableType = 'string',
  defaultValue = '',
): AppVariable {
  return { name, type, default: defaultValue };
}

export function coerceVariableValue(
  raw: string,
  type: AppVariableType,
): string | number | boolean {
  if (type === 'number') {
    const n = Number(raw);
    return Number.isFinite(n) ? n : 0;
  }
  if (type === 'boolean') {
    const v = raw.trim().toLowerCase();
    return v === 'true' || v === '1' || v === 'yes';
  }
  return raw;
}

// substituteVariables replaces `{{name}}` / `{{ name }}` references in
// `input` with the live variable value. Unknown names are left as-is so
// authors can spot typos at preview time. Uses a single-pass scan so
// nested `{{a}}{{b}}` references do not require recursion.
export function substituteVariables(
  input: string,
  state: Record<string, string | number | boolean>,
): string {
  if (!input.includes('{{')) return input;
  return input.replace(/\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}/g, (full, key) => {
    if (Object.prototype.hasOwnProperty.call(state, key)) {
      return String(state[key]);
    }
    return full;
  });
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
//
// The optional `variables` array rides on the root row as an extra
// field — Go's validator ignores unknown row keys so this round-trips
// without a backend schema change.
export function instancesToLayout(
  instances: ComponentInstance[],
  variables: AppVariable[] = [],
): LayoutNode {
  const root: LayoutNode =
    instances.length === 0
      ? {
          type: 'row',
          children: [
            {
              type: 'col',
              width: 12,
              child: { type: 'component', componentType: 'text', props: {} },
            },
          ],
        }
      : {
          type: 'row',
          children: instances.map((inst, idx) => ({
            type: 'col',
            width: distributeWidths(instances.length)[idx],
            child: encodeComponentNode(inst),
          })),
        };
  if (variables.length > 0 && root.type === 'row') {
    root.variables = variables.map((v) => ({ ...v }));
  }
  return root;
}

function encodeComponentNode(inst: ComponentInstance): LayoutNode {
  const node: LayoutNode = {
    type: 'component',
    componentType: inst.componentType,
    props: inst.props,
  };
  if (node.type === 'component' && inst.events && hasAnyEvent(inst.events)) {
    node.events = { ...inst.events };
  }
  return node;
}

function hasAnyEvent(events: AppEventMap | undefined): boolean {
  if (!events) return false;
  return Boolean(events.onClick);
}

// instancesFromLayout walks a layout JSON and returns its component
// children as a flat list, plus any variables declared at the root.
// Multi-row layouts (nested rows inside cols) are flattened depth-first
// so future stories that build richer layouts still load round-trip in
// the simple canvas. Anything it doesn't recognise lands as an empty
// list — the editor falls back to a blank canvas rather than crashing
// on unknown payloads.
export interface DecodedLayout {
  instances: ComponentInstance[];
  variables: AppVariable[];
}

export function instancesFromLayout(
  node: LayoutNode | undefined,
): ComponentInstance[] {
  return decodeLayout(node).instances;
}

export function decodeLayout(node: LayoutNode | undefined): DecodedLayout {
  if (!node) return { instances: [], variables: [] };
  const instances: ComponentInstance[] = [];
  walkLayout(node, instances);
  const variables =
    node.type === 'row' && Array.isArray(node.variables)
      ? node.variables.map((v) => ({ ...v }))
      : [];
  return { instances, variables };
}

function walkLayout(node: LayoutNode, out: ComponentInstance[]): void {
  if (node.type === 'component') {
    out.push({
      id: nextInstanceId(),
      componentType: node.componentType as ComponentType,
      props: node.props ?? {},
      events: node.events ? { ...node.events } : {},
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

// makeEvent builds a fresh AppEvent of the requested kind with sane
// empty defaults. Used by the editor's "Add onClick handler" UI.
export function makeEvent(kind: AppEvent['kind']): AppEvent {
  if (kind === 'setVariable') return { kind, name: '', value: '' };
  if (kind === 'runAction') return { kind, actionType: '', params: {} };
  return { kind, to: '' };
}
