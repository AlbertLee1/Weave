// VertexWorkspacePage (VTX-017) — minimum-viable Vertex workspace shell.
//
// Mounts a Sigma.js / Graphology canvas inside a SigmaContainer and a
// fixed TopBar with the 5 core actions (save/share/layout/timeSelection
// /run). The page handles two URL shapes:
//   • /vertex/new  → fresh workspace, no fetch, empty canvas.
//   • /vertex/:rid → load the graph by RID. On 404 render a "Graph not
//     found" empty state with a back-to-Dashboard link so deep links to
//     deleted/unknown graphs don't dead-end.
//
// Interaction surface (selection, layouts, sidebars, etc.) lands in the
// follow-up stories VTX-018+; this story is intentionally just the
// scaffold.

import { useEffect, useMemo, useState } from 'react';
import { Link, useParams } from 'react-router';
import Graph from 'graphology';
import { SigmaContainer, useLoadGraph } from '@react-sigma/core';
import '@react-sigma/core/lib/style.css';

interface GraphSummary {
  rid: string;
  name?: string;
  version?: number;
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
};

function EmptyGraphLoader() {
  const loadGraph = useLoadGraph();
  useEffect(() => {
    loadGraph(new Graph());
  }, [loadGraph]);
  return null;
}

interface TopBarProps {
  graph?: GraphSummary | null;
}

function TopBar({ graph }: TopBarProps) {
  const buttons: Array<[string, string]> = [
    ['vertex-topbar-save', 'Save'],
    ['vertex-topbar-share', 'Share'],
    ['vertex-topbar-layout', 'Layout'],
    ['vertex-topbar-time-selection', 'Time'],
    ['vertex-topbar-run', 'Run'],
  ];
  return (
    <header
      data-testid="vertex-topbar"
      className="flex items-center justify-between border-b border-zinc-800 bg-zinc-950 px-3 py-2 text-xs text-zinc-100"
    >
      <span data-testid="vertex-topbar-graph-name" className="font-mono text-sm">
        {graph?.name ?? graph?.rid ?? 'Untitled Graph'}
      </span>
      <nav className="flex items-center gap-2">
        {buttons.map(([id, label]) => (
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
  | { kind: 'ready'; graph: GraphSummary }
  | { kind: 'not-found' }
  | { kind: 'error'; message: string };

async function fetchGraphSummary(rid: string): Promise<GraphSummary | 'not-found'> {
  const res = await fetch(`/api/vertex/v1/graphs/${encodeURIComponent(rid)}`);
  if (res.status === 404) return 'not-found';
  if (!res.ok) throw new Error(`graph load failed: ${res.status}`);
  const body = (await res.json()) as GraphSummary;
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
    fetchGraphSummary(rid)
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

  const graph = useMemo<GraphSummary | null>(() => {
    if (state.kind === 'ready') return state.graph;
    if (isNew) return null;
    return { rid };
  }, [state, isNew, rid]);

  if (state.kind === 'not-found') return <NotFound rid={rid} />;

  return (
    <div className="flex h-full min-h-[400px] flex-col" data-testid="vertex-workspace">
      <TopBar graph={graph} />
      <div className="relative flex-1" data-testid="vertex-canvas-host">
        <SigmaContainer style={CANVAS_STYLE} settings={SIGMA_SETTINGS}>
          <EmptyGraphLoader />
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
    </div>
  );
}
