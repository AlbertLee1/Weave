// VertexWorkspacePage — Vertex workspace shell (VTX-017) + payload
// rendering (VTX-018) + node DOM overlay for extended labels (VTX-019)
// + selection interactions / right sidebar (VTX-020) + hierarchical
// layout (VTX-022) + force / circular / auto layouts (VTX-023).
//
// /vertex/new mounts an empty Sigma canvas immediately; /vertex/{rid}
// fetches `/api/vertex/v1/graphs/{rid}` and either renders the graph
// (TopBar + canvas with the loaded nodes/edges) or surfaces "Graph not
// found" + a Dashboard back-link when the backend returns 404.
//
// Node/edge projection is delegated to features/vertex/render/payloadToGraph
// so the heavy logic stays pure + Vitest-friendly. Zoom/pan come for free
// from Sigma's default camera controls — no custom event wiring needed.

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useParams } from 'react-router';
import Graph, { MultiGraph } from 'graphology';
import { SigmaContainer, useLoadGraph, useSigma } from '@react-sigma/core';
import '@react-sigma/core/lib/style.css';

import {
  payloadToGraph,
  type VertexPayloadGraph,
} from '../features/vertex/render/payloadToGraph';
import {
  mergeAddedNodes,
  type AddedObject,
} from '../features/vertex/render/mergeAddedNodes';
import { mergeEdgesByLinkType } from '../features/vertex/render/mergeEdges';
import {
  extractExtendedLabels,
  type ExtendedLabel,
} from '../features/vertex/render/extendedLabels';
import {
  EMPTY_SELECTION,
  type SelectionState,
} from '../features/vertex/selections/selectionState';
import { payloadToObjectSummaries } from '../features/vertex/selections/objectSummaries';
import { hierarchicalLayout } from '../features/vertex/layouts/hierarchicalLayout';
import { forceAtlas2Layout } from '../features/vertex/layouts/forceAtlas2Layout';
import { circularLayout } from '../features/vertex/layouts/circularLayout';
import { pickAutoLayoutKind } from '../features/vertex/layouts/autoLayout';
import {
  formatLayoutPatchBody,
  pinnedPositionsFromPayload,
  type LayoutPoint as PinnedLayoutPoint,
} from '../features/vertex/drag/dragPersistence';
import { VertexNodeOverlay } from './VertexNodeOverlay';
import { VertexSelectionLayer } from './VertexSelectionLayer';
import { VertexDragLayer } from './VertexDragLayer';
import { VertexNodeContextMenu } from './VertexNodeContextMenu';
import {
  VertexSelectionSidebar,
  type VertexObjectSummary,
} from './VertexSelectionSidebar';
import {
  VertexAddObjectsDialog,
  type AddedObjectInput,
} from './VertexAddObjectsDialog';

export interface HierarchicalLayoutSpec {
  kind: 'hierarchical';
  reverse: boolean;
  rootNodes: string[];
}

export interface ForceLayoutSpec {
  kind: 'force';
}

export interface CircularLayoutSpec {
  kind: 'circular';
}

export interface AutoLayoutSpec {
  kind: 'auto';
}

export type LayoutSpec =
  | HierarchicalLayoutSpec
  | ForceLayoutSpec
  | CircularLayoutSpec
  | AutoLayoutSpec;

export type LayoutKind = LayoutSpec['kind'];

const SELECTED_NODE_COLOR = '#3B82F6';
const DEFAULT_NODE_COLOR = '#6B7280';

interface GraphPayloadResponse {
  rid: string;
  name?: string;
  version?: number;
  payload?: unknown;
}

const CANVAS_STYLE: React.CSSProperties = {
  height: '100%',
  width: '100%',
  background: 'var(--bg-primary, #0b0d12)',
};

const SIGMA_SETTINGS = {
  allowInvalidContainer: true,
  defaultNodeType: 'circle' as const,
  defaultEdgeType: 'arrow' as const,
  // Subtitle below the node center; Sigma 3 renders labels on every node
  // when renderLabels is true (the default).
  labelSize: 11,
  labelDensity: 1,
  renderEdgeLabels: false,
};

function GraphLoader({
  projection,
  mergeEnabled,
  hiddenNodeIds,
}: {
  projection: VertexPayloadGraph;
  mergeEnabled: boolean;
  hiddenNodeIds: ReadonlySet<string>;
}) {
  const loadGraph = useLoadGraph();
  useEffect(() => {
    // MultiGraph (vs Graph) so two same-direction edges between A and B
    // can both live on the canvas when merge=off; the merge reducer is
    // what collapses them when merge=on.
    const g = new MultiGraph();
    for (const n of projection.nodes) {
      // VTX-026: Hide-via-context-menu is a pure UI filter — the node
      // stays in the payload (no data delete) but disappears from the
      // canvas. Edges with a hidden endpoint are dropped below.
      if (hiddenNodeIds.has(n.id)) continue;
      g.addNode(n.id, {
        label: n.label,
        x: n.x,
        y: n.y,
        size: n.size,
        color: n.color,
        highlighted: false,
      });
    }
    const edgesToRender = mergeEdgesByLinkType(projection.edges, {
      merge: mergeEnabled,
    });
    for (const e of edgesToRender) {
      if (!g.hasNode(e.source) || !g.hasNode(e.target)) continue;
      // Use addEdgeWithKey so a stable id survives re-renders + drag
      // persistence in VTX-024. Graphology rejects duplicate keys, so
      // dedupe by key.
      if (g.hasEdge(e.key)) continue;
      const attrs: Record<string, unknown> = {
        type: e.type,
        bothArrows: e.bothArrows === true,
        size: typeof e.size === 'number' ? e.size : 1,
      };
      if (typeof e.label === 'string') attrs.label = e.label;
      if (typeof e.count === 'number') attrs.count = e.count;
      g.addEdgeWithKey(e.key, e.source, e.target, attrs);
    }
    loadGraph(g as unknown as Graph);
  }, [loadGraph, projection, mergeEnabled, hiddenNodeIds]);
  return null;
}

// SelectionHighlighter mutates loaded-graph node attributes when the
// selection state changes so Sigma's next paint colours selected nodes
// in the highlight color. Lives alongside GraphLoader inside
// <SigmaContainer> so useSigma() resolves.
function SelectionHighlighter({ selection }: { selection: SelectionState }) {
  const sigma = useSigma();
  useEffect(() => {
    const graph = sigma.getGraph();
    if (!graph || typeof graph.forEachNode !== 'function') return;
    graph.forEachNode((id: string) => {
      const shouldHighlight = selection.has(id);
      const wasHighlighted = graph.getNodeAttribute(id, 'highlighted') === true;
      if (wasHighlighted !== shouldHighlight) {
        graph.setNodeAttribute(id, 'highlighted', shouldHighlight);
        graph.setNodeAttribute(
          id,
          'color',
          shouldHighlight ? SELECTED_NODE_COLOR : DEFAULT_NODE_COLOR,
        );
      }
    });
    if (typeof sigma.refresh === 'function') sigma.refresh();
  }, [sigma, selection]);
  return null;
}

interface GraphSummary {
  rid: string;
  name?: string;
  version?: number;
}

interface TopBarProps {
  graph?: GraphSummary | null;
  onApplyLayout: (spec: LayoutSpec) => void;
  mergeEnabled: boolean;
  onToggleMerge: () => void;
  onOpenAddObjects: () => void;
  addObjectsDisabled?: boolean;
}

const PASSIVE_TOPBAR_BUTTONS: Array<[string, string]> = [
  ['vertex-topbar-save', 'Save'],
  ['vertex-topbar-share', 'Share'],
  ['vertex-topbar-time-selection', 'Time'],
  ['vertex-topbar-run', 'Run'],
];

function TopBar({
  graph,
  onApplyLayout,
  mergeEnabled,
  onToggleMerge,
  onOpenAddObjects,
  addObjectsDisabled,
}: TopBarProps) {
  return (
    <header
      data-testid="vertex-topbar"
      className="flex items-center justify-between border-b border-zinc-800 bg-zinc-950 px-3 py-2 text-xs text-zinc-100"
    >
      <span data-testid="vertex-topbar-graph-name" className="font-mono text-sm">
        {graph?.name ?? graph?.rid ?? 'Untitled Graph'}
      </span>
      <nav className="flex items-center gap-2">
        <button
          type="button"
          data-testid="vertex-topbar-add-objects"
          onClick={onOpenAddObjects}
          disabled={addObjectsDisabled}
          className="rounded border border-zinc-700 bg-zinc-900 px-2 py-1 hover:bg-zinc-800 disabled:cursor-not-allowed disabled:opacity-50"
        >
          + Add objects
        </button>
        <LayoutMenu onApply={onApplyLayout} />
        <button
          type="button"
          data-testid="vertex-topbar-merge-toggle"
          aria-pressed={mergeEnabled}
          onClick={onToggleMerge}
          className={
            'rounded border px-2 py-1 ' +
            (mergeEnabled
              ? 'border-blue-500 bg-blue-600 text-white hover:bg-blue-500'
              : 'border-zinc-700 bg-zinc-900 hover:bg-zinc-800')
          }
        >
          Merge links
        </button>
        {PASSIVE_TOPBAR_BUTTONS.map(([id, label]) => (
          <button
            key={id}
            type="button"
            data-testid={id}
            className="rounded border border-zinc-700 bg-zinc-900 px-2 py-1 hover:bg-zinc-800"
          >
            {label}
          </button>
        ))}
      </nav>
    </header>
  );
}

const LAYOUT_KINDS: Array<{ id: LayoutKind; label: string }> = [
  { id: 'hierarchical', label: 'Hierarchical' },
  { id: 'force', label: 'Force-directed' },
  { id: 'circular', label: 'Circular' },
  { id: 'auto', label: 'Auto' },
];

function LayoutMenu({ onApply }: { onApply: (spec: LayoutSpec) => void }) {
  const [open, setOpen] = useState(false);
  const [kind, setKind] = useState<LayoutKind>('hierarchical');
  const [reverse, setReverse] = useState(false);
  const [rootsText, setRootsText] = useState('');

  const apply = () => {
    if (kind === 'hierarchical') {
      const rootNodes = rootsText
        .split(/[,\n]/)
        .map((s) => s.trim())
        .filter(Boolean);
      onApply({ kind: 'hierarchical', reverse, rootNodes });
    } else if (kind === 'force') {
      onApply({ kind: 'force' });
    } else if (kind === 'circular') {
      onApply({ kind: 'circular' });
    } else {
      onApply({ kind: 'auto' });
    }
    setOpen(false);
  };

  return (
    <div className="relative" data-testid="vertex-topbar-layout-wrap">
      <button
        type="button"
        data-testid="vertex-topbar-layout"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className="rounded border border-zinc-700 bg-zinc-900 px-2 py-1 hover:bg-zinc-800"
      >
        Layout
      </button>
      {open && (
        <div
          data-testid="vertex-layout-popover"
          className="absolute right-0 top-full z-20 mt-1 w-64 rounded border border-zinc-700 bg-zinc-950 p-3 shadow-lg"
          role="dialog"
          aria-label="Layout options"
        >
          <div className="mb-2 text-xs font-semibold uppercase tracking-wide text-zinc-300">
            Algorithm
          </div>
          <div className="mb-2 flex flex-col gap-1" role="radiogroup">
            {LAYOUT_KINDS.map((opt) => (
              <label key={opt.id} className="flex items-center gap-2">
                <input
                  type="radio"
                  name="vertex-layout-kind"
                  value={opt.id}
                  data-testid={`vertex-layout-kind-${opt.id}`}
                  checked={kind === opt.id}
                  onChange={() => setKind(opt.id)}
                />
                <span>{opt.label}</span>
              </label>
            ))}
          </div>
          {kind === 'hierarchical' && (
            <div data-testid="vertex-layout-hierarchical-controls">
              <label className="mb-2 flex items-center gap-2">
                <input
                  type="checkbox"
                  data-testid="vertex-layout-hierarchical-reverse"
                  checked={reverse}
                  onChange={(e) => setReverse(e.target.checked)}
                />
                <span>Reverse direction</span>
              </label>
              <label className="mb-2 block">
                <span className="mb-1 block text-zinc-400">Root nodes</span>
                <input
                  type="text"
                  data-testid="vertex-layout-hierarchical-roots"
                  value={rootsText}
                  onChange={(e) => setRootsText(e.target.value)}
                  placeholder="objectRid, objectRid, …"
                  className="w-full rounded border border-zinc-700 bg-zinc-900 px-2 py-1 text-zinc-100"
                />
              </label>
            </div>
          )}
          <div className="flex justify-end">
            <button
              type="button"
              data-testid={
                kind === 'hierarchical'
                  ? 'vertex-layout-hierarchical-apply'
                  : `vertex-layout-${kind}-apply`
              }
              onClick={apply}
              className="rounded bg-blue-600 px-3 py-1 text-white hover:bg-blue-500"
            >
              Apply
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

// LayoutApplier mounts inside <SigmaContainer> so it can access the
// graphology Graph via useSigma. When `pending` becomes non-null, it
// computes positions via the pure hierarchicalLayout helper, writes them
// onto the graph node attributes, refreshes Sigma, and clears `pending`
// through the supplied callback.
function computeLayoutPositions(
  spec: LayoutSpec,
  nodes: Array<{ id: string }>,
  edges: Array<{ source: string; target: string }>,
  pinnedPositions?: Map<string, PinnedLayoutPoint>,
): Map<string, { x: number; y: number }> {
  // Auto resolves to one of the concrete kinds based on the node count
  // heuristic, then falls through. < 100 → force, otherwise hierarchical.
  let kind: LayoutKind = spec.kind;
  if (kind === 'auto') {
    kind = pickAutoLayoutKind(nodes.length);
  }
  if (kind === 'hierarchical') {
    return hierarchicalLayout({
      nodes,
      edges,
      reverse: spec.kind === 'hierarchical' ? spec.reverse : false,
      rootNodes: spec.kind === 'hierarchical' ? spec.rootNodes : [],
      pinnedPositions,
    });
  }
  if (kind === 'force') {
    return forceAtlas2Layout({ nodes, edges, pinnedPositions });
  }
  return circularLayout({ nodes, pinnedPositions });
}

function LayoutApplier({
  pending,
  pinnedPositions,
  onComplete,
}: {
  pending: LayoutSpec | null;
  pinnedPositions: Map<string, PinnedLayoutPoint>;
  onComplete: () => void;
}) {
  const sigma = useSigma();
  // Keep latest pinned set readable from the effect without re-running on
  // every drag commit — the effect only runs when `pending` flips on.
  const pinnedRef = useRef(pinnedPositions);
  useEffect(() => {
    pinnedRef.current = pinnedPositions;
  }, [pinnedPositions]);
  useEffect(() => {
    if (!pending) return;
    const graph = sigma.getGraph() as unknown as Graph | undefined;
    if (!graph || typeof graph.nodes !== 'function') {
      onComplete();
      return;
    }
    const nodes = graph.nodes().map((id) => ({ id }));
    const edges: Array<{ source: string; target: string }> = [];
    if (typeof graph.forEachEdge === 'function') {
      graph.forEachEdge((_key: string, _attrs: unknown, source: string, target: string) => {
        edges.push({ source, target });
      });
    }
    const positions = computeLayoutPositions(pending, nodes, edges, pinnedRef.current);
    for (const [id, p] of positions) {
      if (graph.hasNode(id)) {
        graph.setNodeAttribute(id, 'x', p.x);
        graph.setNodeAttribute(id, 'y', p.y);
      }
    }
    if (typeof sigma.refresh === 'function') sigma.refresh();
    onComplete();
  }, [sigma, pending, onComplete]);
  return null;
}

function NotFound({ rid }: { rid: string }) {
  return (
    <main
      data-testid="vertex-not-found"
      className="mx-auto flex max-w-2xl flex-col items-start gap-4 p-6"
    >
      <h1 className="text-xl font-semibold">Graph not found</h1>
      <p className="text-sm text-zinc-600 dark:text-zinc-400">
        No graph is registered at <code className="font-mono">{rid}</code>. It
        may have been deleted, or you may not have access to it.
      </p>
      <Link
        to="/"
        data-testid="vertex-not-found-home"
        className="rounded bg-blue-600 px-3 py-1 text-sm text-white"
      >
        Back to Dashboard
      </Link>
    </main>
  );
}

type LoadState =
  | { kind: 'idle' }
  | { kind: 'loading' }
  | { kind: 'ready'; graph: GraphPayloadResponse }
  | { kind: 'not-found' }
  | { kind: 'error'; message: string };

async function fetchGraph(rid: string): Promise<GraphPayloadResponse | 'not-found'> {
  const res = await fetch(`/api/vertex/v1/graphs/${encodeURIComponent(rid)}`);
  if (res.status === 404) return 'not-found';
  if (!res.ok) throw new Error(`graph load failed: ${res.status}`);
  const body = (await res.json()) as GraphPayloadResponse;
  return body;
}

export function VertexWorkspacePage() {
  const params = useParams<{ rid: string }>();
  const rid = params.rid ?? 'new';
  const isNew = rid === 'new';

  // Reset by remount when the rid changes — keeps the effect free of any
  // synchronous setState (lint: react-hooks/set-state-in-effect).
  return <VertexWorkspaceForRid key={rid} rid={rid} isNew={isNew} />;
}

function VertexWorkspaceForRid({ rid, isNew }: { rid: string; isNew: boolean }) {
  const [state, setState] = useState<LoadState>(() =>
    isNew ? { kind: 'idle' } : { kind: 'loading' },
  );

  useEffect(() => {
    if (isNew) return;
    let cancelled = false;
    fetchGraph(rid)
      .then((res) => {
        if (cancelled) return;
        if (res === 'not-found') setState({ kind: 'not-found' });
        else setState({ kind: 'ready', graph: res });
      })
      .catch((e: unknown) => {
        if (cancelled) return;
        setState({ kind: 'error', message: e instanceof Error ? e.message : String(e) });
      });
    return () => {
      cancelled = true;
    };
  }, [isNew, rid]);

  const summary = useMemo<GraphSummary | null>(() => {
    if (state.kind === 'ready') {
      return { rid: state.graph.rid, name: state.graph.name, version: state.graph.version };
    }
    if (isNew) return null;
    return { rid };
  }, [state, isNew, rid]);

  const baseProjection = useMemo<VertexPayloadGraph>(() => {
    if (state.kind === 'ready') return payloadToGraph(state.graph.payload);
    return { nodes: [], edges: [] };
  }, [state]);

  // VTX-027: user-added objects layered on top of the payload's projection.
  // Stored as an ordered list (drives mergeAddedNodes, which handles
  // dedupe + deterministic positioning).
  const [addedObjects, setAddedObjects] = useState<AddedObject[]>([]);
  // Per-rid summary metadata so the SelectionSidebar's per-tab API calls
  // (OSS get / activity / timeseries) work for added objects too.
  const [addedSummariesByRid, setAddedSummariesByRid] = useState<
    ReadonlyMap<string, VertexObjectSummary>
  >(() => new Map());

  const projection = useMemo<VertexPayloadGraph>(
    () => mergeAddedNodes(baseProjection, addedObjects),
    [baseProjection, addedObjects],
  );

  const labelsByRid = useMemo<Map<string, ExtendedLabel[]>>(() => {
    if (state.kind !== 'ready') return new Map();
    const map = new Map<string, ExtendedLabel[]>();
    for (const n of projection.nodes) {
      const labels = extractExtendedLabels(state.graph.payload, n.id);
      if (labels.length > 0) map.set(n.id, labels);
    }
    return map;
  }, [state, projection]);

  const objectsByRid = useMemo<Map<string, VertexObjectSummary>>(() => {
    const fromPayload =
      state.kind === 'ready'
        ? payloadToObjectSummaries(state.graph.payload)
        : new Map<string, VertexObjectSummary>();
    // VTX-027: layer added-object summaries on top so the SelectionSidebar
    // and ContextMenu can resolve them too. Payload-resident objects keep
    // priority (the user can't shadow an existing entity by re-adding it).
    for (const [rid, summary] of addedSummariesByRid) {
      if (!fromPayload.has(rid)) fromPayload.set(rid, summary);
    }
    return fromPayload;
  }, [state, addedSummariesByRid]);

  // Derive the ontology context for the "+ Add objects" dialog from the
  // first layer that carries an explicit `ontology` api name. Falls back
  // to undefined for /vertex/new (the button stays disabled in that case
  // since there's no payload to infer from).
  const ontologyApiName = useMemo<string | undefined>(() => {
    if (state.kind !== 'ready') return undefined;
    const payload = state.graph.payload;
    if (!payload || typeof payload !== 'object') return undefined;
    const layers = (payload as { layers?: unknown }).layers;
    if (!Array.isArray(layers)) return undefined;
    for (const layer of layers) {
      if (layer && typeof layer === 'object') {
        const ont = (layer as { ontology?: unknown }).ontology;
        if (typeof ont === 'string' && ont !== '') return ont;
      }
    }
    return undefined;
  }, [state]);

  const defaultObjectType = useMemo<string | undefined>(() => {
    if (state.kind !== 'ready') return undefined;
    const payload = state.graph.payload;
    if (!payload || typeof payload !== 'object') return undefined;
    const layers = (payload as { layers?: unknown }).layers;
    if (!Array.isArray(layers)) return undefined;
    for (const layer of layers) {
      if (layer && typeof layer === 'object') {
        const t = (layer as { objectType?: unknown }).objectType;
        if (typeof t === 'string' && t !== '') return t;
      }
    }
    return undefined;
  }, [state]);

  const [selection, setSelection] = useState<SelectionState>(EMPTY_SELECTION);
  const [pendingLayout, setPendingLayout] = useState<LayoutSpec | null>(null);
  const handleApplyLayout = useCallback((spec: LayoutSpec) => {
    setPendingLayout(spec);
  }, []);
  const handleLayoutComplete = useCallback(() => {
    setPendingLayout(null);
  }, []);

  // VTX-025: link merging toggle. Default OFF so payloads that already
  // expect N parallel edges (test fixtures, dashboards built against the
  // pre-merge era) keep their previous look until the user opts in.
  const [mergeEnabled, setMergeEnabled] = useState(false);
  const handleToggleMerge = useCallback(() => {
    setMergeEnabled((v) => !v);
  }, []);

  // VTX-024: track which nodes the user has pinned + their coords.
  //
  // The set is split into a *seed* derived from payload.positions (any
  // entry with pinned===true) and a *user diff* the drag + unpin paths
  // mutate. Keeping the two layers separate avoids the
  // react-hooks/set-state-in-effect anti-pattern that an effect→setState
  // seed would create whenever fetch resolves.
  const seedPinnedPositions = useMemo<Map<string, PinnedLayoutPoint>>(() => {
    if (state.kind !== 'ready') return new Map();
    return pinnedPositionsFromPayload(state.graph.payload);
  }, [state]);

  const [pinnedDiff, setPinnedDiff] = useState<{
    /** Coords the user set via drag — overrides seed entries with matching ids. */
    set: Map<string, PinnedLayoutPoint>;
    /** Ids the user removed via Unpin — drop matching seed entries. */
    cleared: Set<string>;
  }>(() => ({ set: new Map(), cleared: new Set() }));

  const pinnedPositions = useMemo<Map<string, PinnedLayoutPoint>>(() => {
    const out = new Map(seedPinnedPositions);
    for (const id of pinnedDiff.cleared) out.delete(id);
    for (const [id, p] of pinnedDiff.set) out.set(id, p);
    return out;
  }, [seedPinnedPositions, pinnedDiff]);

  const pinnedNodeIds = useMemo(
    () => new Set(pinnedPositions.keys()),
    [pinnedPositions],
  );

  // Keep the latest rid in a ref so the long-lived drag/unpin callbacks
  // don't churn on every render — the page-level state changes plenty.
  const ridRef = useRef(rid);
  useEffect(() => {
    ridRef.current = rid;
  }, [rid]);

  // Keep the latest merged pinnedPositions readable from the long-lived
  // handlers without re-creating them on every drag (which would also
  // re-subscribe Sigma event listeners). Synced via a tiny effect — same
  // pattern as selectionRef in VertexSelectionLayer.
  const pinnedPositionsRef = useRef(pinnedPositions);
  useEffect(() => {
    pinnedPositionsRef.current = pinnedPositions;
  }, [pinnedPositions]);

  const handleDragEnd = useCallback(
    (nodeId: string, x: number, y: number) => {
      if (!Number.isFinite(x) || !Number.isFinite(y)) return;
      setPinnedDiff((prev) => {
        const set = new Map(prev.set);
        set.set(nodeId, { x, y });
        const cleared = new Set(prev.cleared);
        cleared.delete(nodeId);
        return { set, cleared };
      });
      if (ridRef.current === 'new' || !ridRef.current) {
        // /vertex/new isn't persisted yet — keep the local pinned state but
        // don't PATCH a non-existent resource.
        return;
      }
      void fetch(`/api/vertex/v1/graphs/${encodeURIComponent(ridRef.current)}/layout`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(formatLayoutPatchBody(nodeId, x, y, true)),
      }).catch(() => {
        // Drag persistence is opportunistic — leave the in-memory pinned
        // state in place even if the network hiccups; the next successful
        // PATCH will reconcile.
      });
    },
    [],
  );

  const handleUnpin = useCallback(
    (nodeId: string) => {
      const current = pinnedPositionsRef.current.get(nodeId);
      setPinnedDiff((prev) => {
        const set = new Map(prev.set);
        set.delete(nodeId);
        const cleared = new Set(prev.cleared);
        cleared.add(nodeId);
        return { set, cleared };
      });
      if (ridRef.current === 'new' || !ridRef.current) return;
      const x = current?.x ?? 0;
      const y = current?.y ?? 0;
      void fetch(`/api/vertex/v1/graphs/${encodeURIComponent(ridRef.current)}/layout`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(formatLayoutPatchBody(nodeId, x, y, false)),
      }).catch(() => {});
    },
    [],
  );

  // VTX-026: Hide is a pure UI filter — the node stays in the underlying
  // payload (no data delete) but is dropped from the rendered graph (with
  // its incident edges) by GraphLoader's hiddenNodeIds filter.
  const [hiddenNodeIds, setHiddenNodeIds] = useState<ReadonlySet<string>>(
    () => new Set(),
  );
  const handleHide = useCallback((nodeId: string) => {
    setHiddenNodeIds((prev) => {
      if (prev.has(nodeId)) return prev;
      const next = new Set(prev);
      next.add(nodeId);
      return next;
    });
  }, []);

  // VTX-027: "+ Add objects" dialog state + commit handler. Ingests
  // user-picked OSS objects into addedObjects (drives mergeAddedNodes)
  // and addedSummariesByRid (drives the SelectionSidebar's per-tab
  // OSS calls + the ContextMenu's Open-in-Object-Explorer URL).
  const [addObjectsOpen, setAddObjectsOpen] = useState(false);
  const handleOpenAddObjects = useCallback(() => setAddObjectsOpen(true), []);
  const handleCloseAddObjects = useCallback(() => setAddObjectsOpen(false), []);
  const handleAddObjects = useCallback((picked: AddedObjectInput[]) => {
    if (picked.length === 0) return;
    setAddedObjects((prev) => {
      const seen = new Set(prev.map((o) => o.rid));
      const next = prev.slice();
      for (const obj of picked) {
        if (seen.has(obj.rid)) continue;
        seen.add(obj.rid);
        next.push({ rid: obj.rid, label: obj.label });
      }
      return next;
    });
    setAddedSummariesByRid((prev) => {
      const next = new Map(prev);
      for (const obj of picked) {
        if (next.has(obj.rid)) continue;
        next.set(obj.rid, {
          rid: obj.rid,
          label: obj.label,
          properties: {},
          ontologyApiName: obj.ontologyApiName,
          objectType: obj.objectType,
        });
      }
      return next;
    });
  }, []);

  if (state.kind === 'not-found') return <NotFound rid={rid} />;

  return (
    <div className="flex h-full min-h-[400px] flex-col" data-testid="vertex-workspace">
      <TopBar
        graph={summary}
        onApplyLayout={handleApplyLayout}
        mergeEnabled={mergeEnabled}
        onToggleMerge={handleToggleMerge}
        onOpenAddObjects={handleOpenAddObjects}
        addObjectsDisabled={!ontologyApiName}
      />
      <div className="flex flex-1 overflow-hidden">
        <div className="relative flex-1" data-testid="vertex-canvas-host">
          <SigmaContainer style={CANVAS_STYLE} settings={SIGMA_SETTINGS}>
            <GraphLoader
              projection={projection}
              mergeEnabled={mergeEnabled}
              hiddenNodeIds={hiddenNodeIds}
            />
            <VertexNodeOverlay labelsByRid={labelsByRid} />
            <VertexSelectionLayer
              selection={selection}
              onSelectionChange={setSelection}
            />
            <SelectionHighlighter selection={selection} />
            <VertexDragLayer onDragEnd={handleDragEnd} />
            <VertexNodeContextMenu
              pinnedNodeIds={pinnedNodeIds}
              objectsByRid={objectsByRid}
              onPin={handleDragEnd}
              onUnpin={handleUnpin}
              onHide={handleHide}
            />
            <LayoutApplier
              pending={pendingLayout}
              pinnedPositions={pinnedPositions}
              onComplete={handleLayoutComplete}
            />
          </SigmaContainer>
          {state.kind === 'loading' && (
            <div
              data-testid="vertex-canvas-loading"
              className="absolute inset-0 flex items-center justify-center text-xs text-zinc-400"
            >
              Loading graph…
            </div>
          )}
          {state.kind === 'error' && (
            <div
              data-testid="vertex-canvas-error"
              className="absolute inset-0 flex items-center justify-center text-xs text-red-400"
            >
              {state.message}
            </div>
          )}
        </div>
        <VertexSelectionSidebar
          selection={selection}
          objectsByRid={objectsByRid}
          extendedLabelsByRid={labelsByRid}
        />
      </div>
      {ontologyApiName && addObjectsOpen && (
        <VertexAddObjectsDialog
          open
          ontologyApiName={ontologyApiName}
          defaultObjectType={defaultObjectType}
          onClose={handleCloseAddObjects}
          onAdd={handleAddObjects}
        />
      )}
    </div>
  );
}
