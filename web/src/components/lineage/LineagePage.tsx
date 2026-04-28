import { useEffect, useMemo, useRef, useState } from 'react';
import { useParams } from 'react-router';
import {
  forceCenter,
  forceCollide,
  forceLink,
  forceManyBody,
  forceSimulation,
  type SimulationLinkDatum,
  type SimulationNodeDatum,
} from 'd3-force';
import { useLineage } from '../../hooks/useLineage';
import {
  getLineage,
  type LineageDirection,
  type LineageEdge,
  type LineageNode,
  type LineageResponse,
} from '../../api/lineage';
import { LoadingSpinner } from '../common/LoadingSpinner';

const WIDTH = 1200;
const HEIGHT = 800;
const NODE_RADIUS = 28;
const SIM_ITERATIONS = 300;
const ROOT_COLOR = '#F59E0B';
const PEER_COLOR = '#14B8A6';

interface GraphNode extends SimulationNodeDatum {
  id: string;
  type?: string;
  isRoot: boolean;
}

interface GraphEdge extends SimulationLinkDatum<GraphNode> {
  id: string;
  source: string | GraphNode;
  target: string | GraphNode;
  operation?: string;
  timestamp: string;
}

function nodeLabel(rid: string): string {
  const parts = rid.split('.');
  if (parts.length < 5) return rid;
  return parts.slice(4).join('.');
}

function nodeInitial(rid: string): string {
  const parts = rid.split('.');
  const seg = parts.length >= 5 ? parts[3] : rid;
  return (seg || '?').charAt(0).toUpperCase();
}

function edgeKey(e: LineageEdge): string {
  return `${e.from}__${e.to}__${e.operation ?? ''}__${e.timestamp}`;
}

export function LineagePage() {
  const { rid } = useParams<{ rid: string }>();
  const rootRid = rid ?? '';

  const [direction, setDirection] = useState<LineageDirection>('upstream');
  const [depthRaw, setDepthRaw] = useState<string>('1');
  const depth = useMemo(() => {
    const n = Number(depthRaw);
    if (Number.isFinite(n) && n >= 1 && n <= 10) return n;
    return 1;
  }, [depthRaw]);

  const { data, isLoading, error } = useLineage(rootRid, { direction, depth });

  // Merged graph: starts from the initial fetch and grows when the user
  // clicks a node to expand it.
  const [extra, setExtra] = useState<{
    nodes: LineageNode[];
    edges: LineageEdge[];
  }>({ nodes: [], edges: [] });
  const [expanding, setExpanding] = useState<string | null>(null);
  const [truncatedExtra, setTruncatedExtra] = useState(false);

  // Reset extras whenever the root / direction / depth changes.
  useEffect(() => {
    setExtra({ nodes: [], edges: [] });
    setTruncatedExtra(false);
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

  const handleExpand = async (clickedRid: string) => {
    if (clickedRid === rootRid) return;
    if (expanding === clickedRid) return;
    setExpanding(clickedRid);
    try {
      const resp: LineageResponse = await getLineage(clickedRid, {
        direction,
        depth: 1,
      });
      setExtra((prev) => ({
        nodes: [...prev.nodes, ...resp.nodes],
        edges: [...prev.edges, ...resp.edges],
      }));
      if (resp.truncated) setTruncatedExtra(true);
    } catch {
      // Surfaced via the top-level error banner is enough; expansion just
      // doesn't merge anything.
    } finally {
      setExpanding(null);
    }
  };

  const graphNodes: GraphNode[] = useMemo(
    () =>
      merged.nodes.map((n) => ({
        id: n.rid,
        type: n.type,
        isRoot: n.rid === rootRid,
      })),
    [merged.nodes, rootRid],
  );

  const graphEdges: GraphEdge[] = useMemo(
    () =>
      merged.edges.map((e) => ({
        id: edgeKey(e),
        source: e.from,
        target: e.to,
        operation: e.operation,
        timestamp: e.timestamp,
      })),
    [merged.edges],
  );

  const graphSignature =
    graphNodes
      .map((n) => n.id)
      .sort()
      .join(',') +
    '||' +
    graphEdges
      .map((e) => e.id)
      .sort()
      .join(',');

  const positions = useMemo<Map<string, { x: number; y: number }>>(() => {
    const map = new Map<string, { x: number; y: number }>();
    if (graphNodes.length === 0) return map;
    const simNodes: GraphNode[] = graphNodes.map((n) => ({ ...n }));
    const simEdges = graphEdges.map((e) => ({ ...e }));
    const sim = forceSimulation<GraphNode>(simNodes)
      .force(
        'link',
        forceLink<GraphNode, (typeof simEdges)[number]>(simEdges)
          .id((n) => n.id)
          .distance(160)
          .strength(0.5),
      )
      .force('charge', forceManyBody().strength(-380))
      .force('center', forceCenter(WIDTH / 2, HEIGHT / 2))
      .force('collide', forceCollide(NODE_RADIUS + 16))
      .stop();

    for (let i = 0; i < SIM_ITERATIONS; i++) sim.tick();

    simNodes.forEach((n) => {
      map.set(n.id, {
        x: n.x ?? WIDTH / 2,
        y: n.y ?? HEIGHT / 2,
      });
    });
    return map;
    // graphSignature is a stable string hash of nodes+edges identity.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [graphSignature]);

  // Pan / zoom
  const [tx, setTx] = useState(0);
  const [ty, setTy] = useState(0);
  const [scale, setScale] = useState(1);
  const dragRef = useRef<{
    startX: number;
    startY: number;
    startTx: number;
    startTy: number;
  } | null>(null);

  const onWheel = (e: React.WheelEvent<SVGSVGElement>) => {
    const delta = -e.deltaY * 0.0015;
    setScale((s) => Math.max(0.25, Math.min(4, s + delta * s)));
  };
  const isPanTarget = (target: EventTarget | null): boolean => {
    if (!(target instanceof Element)) return false;
    if (target.tagName === 'svg') return true;
    return target.getAttribute('data-pan') === 'true';
  };
  const onMouseDown = (e: React.MouseEvent<SVGSVGElement>) => {
    if (!isPanTarget(e.target)) return;
    dragRef.current = {
      startX: e.clientX,
      startY: e.clientY,
      startTx: tx,
      startTy: ty,
    };
  };
  const onMouseMove = (e: React.MouseEvent<SVGSVGElement>) => {
    if (!dragRef.current) return;
    setTx(dragRef.current.startTx + (e.clientX - dragRef.current.startX));
    setTy(dragRef.current.startTy + (e.clientY - dragRef.current.startY));
  };
  const endDrag = () => {
    dragRef.current = null;
  };
  const resetView = () => {
    setTx(0);
    setTy(0);
    setScale(1);
  };

  const truncated = (data?.truncated ?? false) || truncatedExtra;
  const nodeCount = graphNodes.length;
  const edgeCount = graphEdges.length;

  if (!rootRid) {
    return (
      <div className="flex items-center justify-center h-[calc(100vh-3rem)] text-text-secondary text-sm">
        Provide an RID in the URL to view its lineage.
      </div>
    );
  }

  return (
    <div className="flex flex-col h-[calc(100vh-3rem)] bg-bg-primary overflow-hidden">
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
        <span className="text-xs text-text-secondary">
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
            type="number"
            min={1}
            max={10}
            value={depthRaw}
            onChange={(e) => setDepthRaw(e.target.value)}
            className="w-16 px-2 py-1 rounded bg-bg-tertiary text-text-primary text-xs outline-none border border-transparent focus:border-accent-cyan/40"
          />
        </label>
        <div className="flex-1" />
        <button
          type="button"
          aria-label="Reset view"
          onClick={resetView}
          className="px-3 py-1.5 text-xs font-semibold rounded bg-bg-tertiary text-text-primary border border-transparent hover:border-accent-cyan/40 transition-colors"
        >
          Reset view
        </button>
      </div>

      <div className="flex-1 relative">
        {isLoading && (
          <div className="absolute inset-0 flex items-center justify-center">
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
        {!isLoading && !error && edgeCount === 0 && (
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
        {!isLoading && !error && edgeCount > 0 && (
          <svg
            viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
            preserveAspectRatio="xMidYMid meet"
            className="w-full h-full select-none"
            data-testid="lineage-graph-svg"
            onWheel={onWheel}
            onMouseDown={onMouseDown}
            onMouseMove={onMouseMove}
            onMouseUp={endDrag}
            onMouseLeave={endDrag}
            style={{ cursor: dragRef.current ? 'grabbing' : 'grab' }}
          >
            <rect
              width={WIDTH}
              height={HEIGHT}
              fill="transparent"
              data-pan="true"
            />
            <defs>
              <marker
                id="lineage-arrow"
                viewBox="0 -5 10 10"
                refX={NODE_RADIUS + 8}
                refY="0"
                markerWidth="6"
                markerHeight="6"
                orient="auto"
              >
                <path d="M0,-5L10,0L0,5" fill="#64748b" />
              </marker>
            </defs>
            <g transform={`translate(${tx},${ty}) scale(${scale})`}>
              {graphEdges.map((edge) => {
                const src =
                  typeof edge.source === 'string'
                    ? edge.source
                    : edge.source.id;
                const dst =
                  typeof edge.target === 'string'
                    ? edge.target
                    : edge.target.id;
                const s = positions.get(src);
                const t = positions.get(dst);
                if (!s || !t) return null;
                const midX = (s.x + t.x) / 2;
                const midY = (s.y + t.y) / 2;
                return (
                  <g key={edge.id} data-testid="lineage-edge">
                    <line
                      x1={s.x}
                      y1={s.y}
                      x2={t.x}
                      y2={t.y}
                      stroke="#475569"
                      strokeWidth={1.5}
                      markerEnd="url(#lineage-arrow)"
                    />
                    {edge.operation && (
                      <g transform={`translate(${midX},${midY})`}>
                        <rect
                          x={-30}
                          y={-9}
                          width={60}
                          height={16}
                          rx={3}
                          fill="rgba(15,23,42,0.9)"
                          stroke="#334155"
                        />
                        <text
                          textAnchor="middle"
                          dominantBaseline="central"
                          fontSize={9}
                          fill="#cbd5e1"
                          data-testid="lineage-edge-operation"
                        >
                          {edge.operation}
                        </text>
                      </g>
                    )}
                  </g>
                );
              })}
              {graphNodes.map((node) => {
                const p = positions.get(node.id);
                if (!p) return null;
                const fill = node.isRoot ? ROOT_COLOR : PEER_COLOR;
                return (
                  <g
                    key={node.id}
                    transform={`translate(${p.x},${p.y})`}
                    className="cursor-pointer"
                    data-testid="lineage-node"
                    data-rid={node.id}
                    data-root={node.isRoot ? 'true' : 'false'}
                    onClick={() => handleExpand(node.id)}
                  >
                    <title>{node.id}</title>
                    <circle
                      r={NODE_RADIUS}
                      fill={fill}
                      fillOpacity={0.18}
                      stroke={fill}
                      strokeWidth={node.isRoot ? 3 : 2}
                    />
                    <text
                      textAnchor="middle"
                      dominantBaseline="central"
                      fontSize={16}
                      fontWeight={600}
                      fill="#e2e8f0"
                    >
                      {nodeInitial(node.id)}
                    </text>
                    <text
                      textAnchor="middle"
                      y={NODE_RADIUS + 16}
                      fontSize={11}
                      fill="#cbd5e1"
                    >
                      {nodeLabel(node.id)}
                    </text>
                  </g>
                );
              })}
            </g>
          </svg>
        )}
      </div>
    </div>
  );
}
