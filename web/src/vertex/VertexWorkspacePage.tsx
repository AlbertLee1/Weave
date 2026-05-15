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

import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useParams } from 'react-router';
import Graph from 'graphology';
import { SigmaContainer, useLoadGraph, useSigma } from '@react-sigma/core';
import '@react-sigma/core/lib/style.css';

import {
  payloadToGraph,
  type VertexPayloadGraph,
} from '../features/vertex/render/payloadToGraph';
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
import { VertexNodeOverlay } from './VertexNodeOverlay';
import { VertexSelectionLayer } from './VertexSelectionLayer';
import {
  VertexSelectionSidebar,
  type VertexObjectSummary,
} from './VertexSelectionSidebar';

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

function GraphLoader({ projection }: { projection: VertexPayloadGraph }) {
  const loadGraph = useLoadGraph();
  useEffect(() => {
    const g = new Graph();
    for (const n of projection.nodes) {
      g.addNode(n.id, {
        label: n.label,
        x: n.x,
        y: n.y,
        size: n.size,
        color: n.color,
        highlighted: false,
      });
    }
    for (const e of projection.edges) {
      if (!g.hasNode(e.source) || !g.hasNode(e.target)) continue;
      // Use addEdgeWithKey so a stable id survives re-renders + drag
      // persistence in VTX-024. Graphology rejects duplicate keys, so
      // dedupe by source/target/key triple.
      if (g.hasEdge(e.key)) continue;
      g.addEdgeWithKey(e.key, e.source, e.target, {
        type: e.type,
        bothArrows: e.bothArrows === true,
        size: 1,
      });
    }
    loadGraph(g);
  }, [loadGraph, projection]);
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
}

const PASSIVE_TOPBAR_BUTTONS: Array<[string, string]> = [
  ['vertex-topbar-save', 'Save'],
  ['vertex-topbar-share', 'Share'],
  ['vertex-topbar-time-selection', 'Time'],
  ['vertex-topbar-run', 'Run'],
];

function TopBar({ graph, onApplyLayout }: TopBarProps) {
  return (
    <header
      data-testid="vertex-topbar"
      className="flex items-center justify-between border-b border-zinc-800 bg-zinc-950 px-3 py-2 text-xs text-zinc-100"
    >
      <span data-testid="vertex-topbar-graph-name" className="font-mono text-sm">
        {graph?.name ?? graph?.rid ?? 'Untitled Graph'}
      </span>
      <nav className="flex items-center gap-2">
        <LayoutMenu onApply={onApplyLayout} />
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
    });
  }
  if (kind === 'force') {
    return forceAtlas2Layout({ nodes, edges });
  }
  return circularLayout({ nodes });
}

function LayoutApplier({
  pending,
  onComplete,
}: {
  pending: LayoutSpec | null;
  onComplete: () => void;
}) {
  const sigma = useSigma();
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
    const positions = computeLayoutPositions(pending, nodes, edges);
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

  const projection = useMemo<VertexPayloadGraph>(() => {
    if (state.kind === 'ready') return payloadToGraph(state.graph.payload);
    return { nodes: [], edges: [] };
  }, [state]);

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
    if (state.kind !== 'ready') return new Map();
    return payloadToObjectSummaries(state.graph.payload);
  }, [state]);

  const [selection, setSelection] = useState<SelectionState>(EMPTY_SELECTION);
  const [pendingLayout, setPendingLayout] = useState<LayoutSpec | null>(null);
  const handleApplyLayout = useCallback((spec: LayoutSpec) => {
    setPendingLayout(spec);
  }, []);
  const handleLayoutComplete = useCallback(() => {
    setPendingLayout(null);
  }, []);

  if (state.kind === 'not-found') return <NotFound rid={rid} />;

  return (
    <div className="flex h-full min-h-[400px] flex-col" data-testid="vertex-workspace">
      <TopBar graph={summary} onApplyLayout={handleApplyLayout} />
      <div className="flex flex-1 overflow-hidden">
        <div className="relative flex-1" data-testid="vertex-canvas-host">
          <SigmaContainer style={CANVAS_STYLE} settings={SIGMA_SETTINGS}>
            <GraphLoader projection={projection} />
            <VertexNodeOverlay labelsByRid={labelsByRid} />
            <VertexSelectionLayer
              selection={selection}
              onSelectionChange={setSelection}
            />
            <SelectionHighlighter selection={selection} />
            <LayoutApplier pending={pendingLayout} onComplete={handleLayoutComplete} />
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
    </div>
  );
}
