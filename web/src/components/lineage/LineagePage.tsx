import { useCallback, useEffect, useMemo, useState } from 'react';
import { useParams } from 'react-router';
import {
  Background,
  Controls,
  Handle,
  MiniMap,
  Position,
  ReactFlow,
  ReactFlowProvider,
  type Edge as RFEdge,
  type Node as RFNode,
  type NodeProps,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { useLineage } from '../../hooks/useLineage';
import {
  getLineage,
  type LineageDirection,
  type LineageEdge,
  type LineageNode,
  type LineageResponse,
} from '../../api/lineage';
import { LoadingSpinner } from '../common/LoadingSpinner';

const COLUMN_WIDTH = 280;
const ROW_HEIGHT = 120;
const ROOT_COLOR = '#F59E0B';
const PEER_COLOR = '#14B8A6';
const EXPANDED_COLOR = '#A78BFA';

interface LineageNodeData extends Record<string, unknown> {
  rid: string;
  type: string;
  isRoot: boolean;
  isSelected: boolean;
  expanded: boolean;
  canExpand: boolean;
  expanding: boolean;
  inDegree: number;
  outDegree: number;
  onSelect: (rid: string) => void;
  onToggleExpand: (rid: string) => void;
}

function nodeLabel(rid: string): string {
  const parts = rid.split('.');
  if (parts.length < 5) return rid;
  return parts.slice(4).join('.');
}

function edgeKey(e: LineageEdge): string {
  return `${e.from}__${e.to}__${e.operation ?? ''}__${e.timestamp}`;
}

function LineageNodeComponent(props: NodeProps) {
  const d = props.data as LineageNodeData;
  const accent = d.isRoot
    ? ROOT_COLOR
    : d.expanded
      ? EXPANDED_COLOR
      : PEER_COLOR;
  const handleExpandClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    d.onToggleExpand(d.rid);
  };
  return (
    <div
      data-testid="lineage-node"
      data-rid={d.rid}
      data-root={d.isRoot ? 'true' : 'false'}
      data-node-type={d.type}
      data-expanded={d.expanded ? 'true' : 'false'}
      data-selected={d.isSelected ? 'true' : 'false'}
      data-in-degree={String(d.inDegree)}
      data-out-degree={String(d.outDegree)}
      onClick={() => d.onSelect(d.rid)}
      style={{
        background: 'rgba(15,23,42,0.92)',
        border: `2px solid ${accent}`,
        borderRadius: 8,
        boxShadow: d.isSelected ? `0 0 0 3px ${accent}55` : `0 0 8px ${accent}30`,
        color: '#E5E7EB',
        cursor: 'pointer',
        fontSize: 12,
        minWidth: 200,
        padding: '10px 12px',
      }}
    >
      <Handle type="target" position={Position.Left} style={{ background: accent }} />
      <Handle type="source" position={Position.Right} style={{ background: accent }} />
      <div
        style={{
          alignItems: 'center',
          display: 'flex',
          gap: 6,
          marginBottom: 4,
        }}
      >
        <span
          data-testid="lineage-node-type-badge"
          style={{
            background: `${accent}22`,
            border: `1px solid ${accent}66`,
            borderRadius: 3,
            color: accent,
            fontSize: 9,
            fontWeight: 600,
            letterSpacing: 0.5,
            padding: '1px 6px',
            textTransform: 'uppercase',
          }}
        >
          {d.type || 'resource'}
        </span>
        {d.isRoot && (
          <span
            data-testid="lineage-node-root-badge"
            style={{
              background: `${ROOT_COLOR}22`,
              border: `1px solid ${ROOT_COLOR}66`,
              borderRadius: 3,
              color: ROOT_COLOR,
              fontSize: 9,
              fontWeight: 600,
              letterSpacing: 0.5,
              padding: '1px 6px',
              textTransform: 'uppercase',
            }}
          >
            root
          </span>
        )}
      </div>
      <div
        data-testid="lineage-node-label"
        style={{
          fontFamily: 'ui-monospace, monospace',
          fontSize: 11,
          fontWeight: 600,
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
        }}
        title={d.rid}
      >
        {nodeLabel(d.rid)}
      </div>
      <div
        style={{
          alignItems: 'center',
          color: '#94A3B8',
          display: 'flex',
          fontSize: 10,
          gap: 8,
          marginTop: 6,
        }}
      >
        <span>↓{d.inDegree}</span>
        <span>↑{d.outDegree}</span>
        {!d.isRoot && d.canExpand && (
          <button
            type="button"
            data-testid="lineage-node-expand-btn"
            data-rid={d.rid}
            data-expanded={d.expanded ? 'true' : 'false'}
            disabled={d.expanding}
            onClick={handleExpandClick}
            style={{
              background: d.expanded ? `${EXPANDED_COLOR}22` : `${accent}22`,
              border: `1px solid ${d.expanded ? EXPANDED_COLOR : accent}66`,
              borderRadius: 3,
              color: d.expanded ? EXPANDED_COLOR : accent,
              cursor: d.expanding ? 'wait' : 'pointer',
              fontSize: 9,
              fontWeight: 600,
              letterSpacing: 0.5,
              marginLeft: 'auto',
              padding: '2px 6px',
              textTransform: 'uppercase',
            }}
          >
            {d.expanding ? '…' : d.expanded ? 'collapse' : 'expand'}
          </button>
        )}
      </div>
    </div>
  );
}

const NODE_TYPES = {
  lineage: LineageNodeComponent,
};

interface ExtraGraph {
  nodes: LineageNode[];
  edges: LineageEdge[];
  // Records which RID owns which expansion (so collapse can undo it)
  byOwner: Record<string, { nodes: string[]; edges: string[] }>;
}

const EMPTY_EXTRA: ExtraGraph = { nodes: [], edges: [], byOwner: {} };

function computeLayout(
  rootRid: string,
  nodes: LineageNode[],
  edges: LineageEdge[],
  direction: LineageDirection,
): Map<string, { x: number; y: number }> {
  // Layered DAG layout: BFS from root using edge orientation that matches the
  // active direction. Upstream renders ancestors to the left of the root;
  // downstream renders descendants to the right; "both" puts ancestors left
  // and descendants right, mirrored around the root column.
  const positions = new Map<string, { x: number; y: number }>();
  if (nodes.length === 0) return positions;

  const distance = new Map<string, number>();
  distance.set(rootRid, 0);
  const queue: string[] = [rootRid];
  while (queue.length > 0) {
    const cur = queue.shift()!;
    const d = distance.get(cur)!;
    for (const e of edges) {
      // For upstream: edges go upstream→downstream, so neighbor of cur is e.from when cur === e.to.
      // For downstream: neighbor of cur is e.to when cur === e.from.
      // For both: treat both directions.
      if (
        (direction === 'upstream' || direction === 'both') &&
        e.to === cur &&
        !distance.has(e.from)
      ) {
        distance.set(e.from, direction === 'both' ? d - 1 : d + 1);
        queue.push(e.from);
      }
      if (
        (direction === 'downstream' || direction === 'both') &&
        e.from === cur &&
        !distance.has(e.to)
      ) {
        distance.set(e.to, direction === 'both' ? d + 1 : d + 1);
        queue.push(e.to);
      }
    }
  }

  // Any nodes the BFS didn't reach (orphans) get pushed into a fallback column.
  let fallbackLevel = 0;
  for (const n of nodes) {
    if (!distance.has(n.rid)) {
      distance.set(n.rid, ++fallbackLevel);
    }
  }

  const byLevel = new Map<number, string[]>();
  for (const n of nodes) {
    const lvl = distance.get(n.rid) ?? 0;
    const bucket = byLevel.get(lvl) ?? [];
    bucket.push(n.rid);
    byLevel.set(lvl, bucket);
  }

  const sortedLevels = Array.from(byLevel.keys()).sort((a, b) => a - b);
  const minLevel = sortedLevels[0] ?? 0;
  for (const lvl of sortedLevels) {
    const bucket = byLevel.get(lvl)!;
    bucket.sort();
    const col = lvl - minLevel;
    bucket.forEach((rid, idx) => {
      positions.set(rid, {
        x: col * COLUMN_WIDTH,
        y: idx * ROW_HEIGHT - ((bucket.length - 1) * ROW_HEIGHT) / 2,
      });
    });
  }

  return positions;
}

interface LineagePageBodyProps {
  rootRid: string;
}

function LineagePageBody({ rootRid }: LineagePageBodyProps) {
  const [direction, setDirection] = useState<LineageDirection>('upstream');
  const [depthRaw, setDepthRaw] = useState<string>('1');
  const depth = useMemo(() => {
    const n = Number(depthRaw);
    if (Number.isFinite(n) && n >= 1 && n <= 10) return n;
    return 1;
  }, [depthRaw]);

  const { data, isLoading, error } = useLineage(rootRid, { direction, depth });

  const [extra, setExtra] = useState<ExtraGraph>(EMPTY_EXTRA);
  const [expanding, setExpanding] = useState<string | null>(null);
  const [truncatedExtra, setTruncatedExtra] = useState(false);
  const [selectedRid, setSelectedRid] = useState<string | null>(null);

  // Reset extras when the root / direction / depth changes so the merged
  // graph stays consistent with the latest fetch.
  useEffect(() => {
    setExtra(EMPTY_EXTRA);
    setTruncatedExtra(false);
    setSelectedRid(null);
  }, [rootRid, direction, depth]);

  const merged = useMemo(() => {
    const nodeMap = new Map<string, LineageNode>();
    const edgeMap = new Map<string, LineageEdge>();
    if (data) {
      data.nodes.forEach((n) => nodeMap.set(n.rid, n));
      data.edges.forEach((e) => edgeMap.set(edgeKey(e), e));
    }
    extra.nodes.forEach((n) => {
      if (!nodeMap.has(n.rid)) nodeMap.set(n.rid, n);
    });
    extra.edges.forEach((e) => {
      const k = edgeKey(e);
      if (!edgeMap.has(k)) edgeMap.set(k, e);
    });
    return {
      nodes: Array.from(nodeMap.values()),
      edges: Array.from(edgeMap.values()),
    };
  }, [data, extra]);

  const handleToggleExpand = useCallback(
    async (clickedRid: string) => {
      if (clickedRid === rootRid) return;
      const owned = extra.byOwner[clickedRid];
      if (owned) {
        // Already expanded — collapse: remove only the contributions that this
        // node introduced and weren't already provided by another expansion.
        setExtra((prev) => {
          const ownedSet = new Set(owned.edges);
          const nodeOwnedSet = new Set(owned.nodes);
          const newEdges = prev.edges.filter((e) => !ownedSet.has(edgeKey(e)));
          const newNodes = prev.nodes.filter((n) => !nodeOwnedSet.has(n.rid));
          const newByOwner: ExtraGraph['byOwner'] = {};
          Object.entries(prev.byOwner).forEach(([owner, val]) => {
            if (owner === clickedRid) return;
            newByOwner[owner] = val;
          });
          return { nodes: newNodes, edges: newEdges, byOwner: newByOwner };
        });
        return;
      }
      if (expanding === clickedRid) return;
      setExpanding(clickedRid);
      try {
        const resp: LineageResponse = await getLineage(clickedRid, {
          direction,
          depth: 1,
        });
        setExtra((prev) => {
          const newNodes = [...prev.nodes];
          const newEdges = [...prev.edges];
          const addedNodes: string[] = [];
          const addedEdges: string[] = [];
          const existingNodeRids = new Set(prev.nodes.map((n) => n.rid));
          const allKnownNodeRids = new Set([
            ...(data?.nodes.map((n) => n.rid) ?? []),
            ...prev.nodes.map((n) => n.rid),
          ]);
          const existingEdgeKeys = new Set(prev.edges.map((e) => edgeKey(e)));
          const allKnownEdgeKeys = new Set([
            ...(data?.edges.map((e) => edgeKey(e)) ?? []),
            ...prev.edges.map((e) => edgeKey(e)),
          ]);
          resp.nodes.forEach((n) => {
            if (!existingNodeRids.has(n.rid) && !allKnownNodeRids.has(n.rid)) {
              newNodes.push(n);
              addedNodes.push(n.rid);
              existingNodeRids.add(n.rid);
              allKnownNodeRids.add(n.rid);
            }
          });
          resp.edges.forEach((e) => {
            const k = edgeKey(e);
            if (!existingEdgeKeys.has(k) && !allKnownEdgeKeys.has(k)) {
              newEdges.push(e);
              addedEdges.push(k);
              existingEdgeKeys.add(k);
              allKnownEdgeKeys.add(k);
            }
          });
          return {
            nodes: newNodes,
            edges: newEdges,
            byOwner: {
              ...prev.byOwner,
              [clickedRid]: { nodes: addedNodes, edges: addedEdges },
            },
          };
        });
        if (resp.truncated) setTruncatedExtra(true);
      } catch {
        // The top-level error banner is enough; the expand action just doesn't merge anything.
      } finally {
        setExpanding(null);
      }
    },
    [data, direction, extra.byOwner, expanding, rootRid],
  );

  const positions = useMemo(
    () => computeLayout(rootRid, merged.nodes, merged.edges, direction),
    [rootRid, merged.nodes, merged.edges, direction],
  );

  const degreeMap = useMemo(() => {
    const m = new Map<string, { in: number; out: number }>();
    merged.nodes.forEach((n) => m.set(n.rid, { in: 0, out: 0 }));
    merged.edges.forEach((e) => {
      const dst = m.get(e.to);
      const src = m.get(e.from);
      if (dst) dst.in++;
      if (src) src.out++;
    });
    return m;
  }, [merged.nodes, merged.edges]);

  const rfNodes: RFNode<LineageNodeData>[] = useMemo(
    () =>
      merged.nodes.map((n) => {
        const pos = positions.get(n.rid) ?? { x: 0, y: 0 };
        const deg = degreeMap.get(n.rid) ?? { in: 0, out: 0 };
        return {
          id: n.rid,
          type: 'lineage',
          position: pos,
          data: {
            rid: n.rid,
            type: n.type ?? '',
            isRoot: n.rid === rootRid,
            isSelected: selectedRid === n.rid,
            expanded: Boolean(extra.byOwner[n.rid]),
            canExpand: n.rid !== rootRid,
            expanding: expanding === n.rid,
            inDegree: deg.in,
            outDegree: deg.out,
            onSelect: setSelectedRid,
            onToggleExpand: handleToggleExpand,
          } as LineageNodeData,
        } satisfies RFNode<LineageNodeData>;
      }),
    [
      merged.nodes,
      positions,
      degreeMap,
      rootRid,
      selectedRid,
      extra.byOwner,
      expanding,
      handleToggleExpand,
    ],
  );

  const rfEdges: RFEdge[] = useMemo(
    () =>
      merged.edges.map((e) => ({
        id: edgeKey(e),
        source: e.from,
        target: e.to,
        label: e.operation || undefined,
        labelStyle: { fill: '#cbd5e1', fontSize: 10 },
        labelBgStyle: { fill: 'rgba(15,23,42,0.9)' },
        labelBgPadding: [4, 2] as [number, number],
        style: { stroke: '#64748b', strokeWidth: 1.5 },
        animated: false,
        data: {
          operation: e.operation,
          timestamp: e.timestamp,
        },
      })),
    [merged.edges],
  );

  const truncated = (data?.truncated ?? false) || truncatedExtra;
  const nodeCount = rfNodes.length;
  const edgeCount = rfEdges.length;

  const selectedNode = useMemo(
    () =>
      selectedRid
        ? (merged.nodes.find((n) => n.rid === selectedRid) ?? null)
        : null,
    [merged.nodes, selectedRid],
  );

  const selectedDegree = selectedRid
    ? (degreeMap.get(selectedRid) ?? { in: 0, out: 0 })
    : { in: 0, out: 0 };

  const selectedIncomingEdges = useMemo(
    () => merged.edges.filter((e) => e.to === selectedRid),
    [merged.edges, selectedRid],
  );
  const selectedOutgoingEdges = useMemo(
    () => merged.edges.filter((e) => e.from === selectedRid),
    [merged.edges, selectedRid],
  );

  return (
    <div
      className="flex flex-col h-[calc(100vh-3rem)] bg-bg-primary overflow-hidden"
      data-testid="lineage-page"
    >
      <header
        className="px-6 py-4 border-b flex flex-wrap items-center gap-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <h1 className="text-base font-semibold tracking-wide text-text-primary">
          Lineage
        </h1>
        <span
          className="text-xs text-text-secondary uppercase tracking-widest"
          data-testid="lineage-root-rid"
        >
          {rootRid}
        </span>
        <div className="flex-1" />
        <span
          className="text-xs text-text-secondary"
          data-testid="lineage-counts"
          data-node-count={String(nodeCount)}
          data-edge-count={String(edgeCount)}
        >
          {nodeCount} nodes · {edgeCount} edges
        </span>
        {truncated && (
          <span
            className="text-xs px-2 py-1 rounded bg-amber-500/10 text-amber-300 border border-amber-500/30"
            data-testid="lineage-truncated"
          >
            Truncated
          </span>
        )}
      </header>

      <div
        className="px-6 py-3 border-b flex flex-wrap items-center gap-3"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <label className="text-xs text-text-secondary flex items-center gap-2">
          <span className="uppercase tracking-widest">Direction</span>
          <select
            aria-label="Direction"
            data-testid="lineage-direction-select"
            value={direction}
            onChange={(e) => setDirection(e.target.value as LineageDirection)}
            className="px-2 py-1 rounded bg-bg-tertiary text-text-primary text-xs outline-none border border-transparent focus:border-accent-cyan/40"
          >
            <option value="upstream">Upstream</option>
            <option value="downstream">Downstream</option>
            <option value="both">Both</option>
          </select>
        </label>
        <label className="text-xs text-text-secondary flex items-center gap-2">
          <span className="uppercase tracking-widest">Depth</span>
          <input
            aria-label="Depth"
            data-testid="lineage-depth-input"
            type="number"
            min={1}
            max={10}
            value={depthRaw}
            onChange={(e) => setDepthRaw(e.target.value)}
            className="w-16 px-2 py-1 rounded bg-bg-tertiary text-text-primary text-xs outline-none border border-transparent focus:border-accent-cyan/40"
          />
        </label>
        <div className="flex-1" />
        {selectedRid && (
          <button
            type="button"
            data-testid="lineage-clear-selection-btn"
            onClick={() => setSelectedRid(null)}
            className="px-3 py-1.5 text-xs font-semibold rounded bg-bg-tertiary text-text-primary border border-transparent hover:border-accent-cyan/40 transition-colors"
          >
            Clear selection
          </button>
        )}
      </div>

      <div className="flex-1 relative flex">
        <div className="flex-1 relative">
          {isLoading && (
            <div
              className="absolute inset-0 flex items-center justify-center"
              data-testid="lineage-loading"
            >
              <LoadingSpinner size="lg" />
            </div>
          )}
          {!isLoading && error && (
            <div
              className="absolute inset-0 flex items-center justify-center px-6"
              data-testid="lineage-error"
            >
              <p className="text-sm text-accent-error">
                Failed to load lineage: {(error as Error).message}
              </p>
            </div>
          )}
          {!isLoading && !error && edgeCount === 0 && nodeCount <= 1 && (
            <div
              className="absolute inset-0 flex items-center justify-center"
              data-testid="lineage-empty"
            >
              <div className="max-w-md text-center">
                <p className="text-sm text-text-primary font-semibold">
                  No lineage edges
                </p>
                <p className="text-xs text-text-secondary mt-2">
                  This RID has no recorded {direction} provenance.
                </p>
              </div>
            </div>
          )}
          {!isLoading && !error && nodeCount > 0 && (
            <div
              className="absolute inset-0"
              data-testid="lineage-graph"
              data-direction={direction}
              data-depth={String(depth)}
            >
              <ReactFlow
                nodes={rfNodes}
                edges={rfEdges}
                nodeTypes={NODE_TYPES}
                fitView
                proOptions={{ hideAttribution: true }}
                nodesDraggable={false}
                nodesConnectable={false}
                elementsSelectable={true}
                onPaneClick={() => setSelectedRid(null)}
              >
                <Background gap={20} color="#1F2937" />
                <Controls
                  showInteractive={false}
                  position="bottom-left"
                />
                <MiniMap
                  pannable
                  zoomable
                  position="bottom-right"
                  nodeColor={(n: RFNode<LineageNodeData>) =>
                    n.data?.isRoot
                      ? ROOT_COLOR
                      : n.data?.expanded
                        ? EXPANDED_COLOR
                        : PEER_COLOR
                  }
                  style={{ background: 'rgba(15,23,42,0.92)' }}
                />
              </ReactFlow>
            </div>
          )}
        </div>

        {selectedNode && (
          <aside
            data-testid="lineage-detail-panel"
            data-rid={selectedNode.rid}
            data-node-type={selectedNode.type ?? ''}
            className="w-[360px] border-l flex flex-col overflow-y-auto"
            style={{
              background: 'rgba(15,23,42,0.96)',
              borderColor: 'rgba(31,41,55,0.5)',
            }}
          >
            <div
              className="px-4 py-3 border-b flex items-center justify-between"
              style={{ borderColor: 'rgba(31,41,55,0.5)' }}
            >
              <h2 className="text-sm font-semibold text-text-primary">
                Node details
              </h2>
              <button
                type="button"
                data-testid="lineage-detail-close-btn"
                onClick={() => setSelectedRid(null)}
                className="text-xs text-text-secondary hover:text-text-primary"
              >
                Close
              </button>
            </div>
            <div className="px-4 py-3 space-y-3 text-xs">
              <div>
                <div className="text-text-secondary uppercase tracking-widest mb-1">
                  RID
                </div>
                <div
                  data-testid="lineage-detail-rid"
                  className="font-mono text-text-primary break-all"
                >
                  {selectedNode.rid}
                </div>
              </div>
              <div>
                <div className="text-text-secondary uppercase tracking-widest mb-1">
                  Resource type
                </div>
                <div
                  data-testid="lineage-detail-type"
                  className="text-text-primary"
                >
                  {selectedNode.type || '(unknown)'}
                </div>
              </div>
              <div className="grid grid-cols-2 gap-2">
                <div>
                  <div className="text-text-secondary uppercase tracking-widest mb-1">
                    Upstream edges
                  </div>
                  <div
                    data-testid="lineage-detail-in-count"
                    className="text-text-primary text-sm"
                  >
                    {selectedDegree.in}
                  </div>
                </div>
                <div>
                  <div className="text-text-secondary uppercase tracking-widest mb-1">
                    Downstream edges
                  </div>
                  <div
                    data-testid="lineage-detail-out-count"
                    className="text-text-primary text-sm"
                  >
                    {selectedDegree.out}
                  </div>
                </div>
              </div>

              {selectedIncomingEdges.length > 0 && (
                <div>
                  <div className="text-text-secondary uppercase tracking-widest mb-1">
                    Upstream transforms
                  </div>
                  <ul
                    data-testid="lineage-detail-in-edges"
                    className="space-y-1"
                  >
                    {selectedIncomingEdges.map((e) => (
                      <li
                        key={edgeKey(e)}
                        data-testid="lineage-detail-edge"
                        data-edge-direction="in"
                        data-edge-operation={e.operation ?? ''}
                        className="px-2 py-1 rounded bg-bg-tertiary border border-bg-tertiary/40"
                      >
                        <div className="font-mono text-[10px] text-text-secondary break-all">
                          {nodeLabel(e.from)}
                        </div>
                        <div className="text-text-primary">
                          {e.operation || '(no operation)'}
                        </div>
                        <div className="text-text-secondary text-[10px]">
                          {e.timestamp}
                        </div>
                      </li>
                    ))}
                  </ul>
                </div>
              )}

              {selectedOutgoingEdges.length > 0 && (
                <div>
                  <div className="text-text-secondary uppercase tracking-widest mb-1">
                    Downstream transforms
                  </div>
                  <ul
                    data-testid="lineage-detail-out-edges"
                    className="space-y-1"
                  >
                    {selectedOutgoingEdges.map((e) => (
                      <li
                        key={edgeKey(e)}
                        data-testid="lineage-detail-edge"
                        data-edge-direction="out"
                        data-edge-operation={e.operation ?? ''}
                        className="px-2 py-1 rounded bg-bg-tertiary border border-bg-tertiary/40"
                      >
                        <div className="font-mono text-[10px] text-text-secondary break-all">
                          {nodeLabel(e.to)}
                        </div>
                        <div className="text-text-primary">
                          {e.operation || '(no operation)'}
                        </div>
                        <div className="text-text-secondary text-[10px]">
                          {e.timestamp}
                        </div>
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          </aside>
        )}
      </div>
    </div>
  );
}

export function LineagePage() {
  const { rid } = useParams<{ rid: string }>();
  const rootRid = rid ?? '';

  if (!rootRid) {
    return (
      <div className="flex items-center justify-center h-[calc(100vh-3rem)] text-text-secondary text-sm">
        Provide an RID in the URL to view its lineage.
      </div>
    );
  }

  return (
    <ReactFlowProvider>
      <LineagePageBody rootRid={rootRid} />
    </ReactFlowProvider>
  );
}
