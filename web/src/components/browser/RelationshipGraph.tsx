import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  forceCenter,
  forceCollide,
  forceLink,
  forceManyBody,
  forceSimulation,
  type SimulationLinkDatum,
  type SimulationNodeDatum,
} from 'd3-force';
import { listLinkedObjects } from '../../api/objects';
import { listOutgoingLinkTypes } from '../../api/ontologies';
import type { LinkType } from '../../api/types';
import { LoadingSpinner } from '../common/LoadingSpinner';

const WIDTH = 1000;
const HEIGHT = 600;
const NODE_RADIUS = 24;
const SIM_ITERATIONS = 300;
const ROOT_COLOR = '#F59E0B';
const PEER_COLOR = '#14B8A6';
const PAGE_SIZE = 25;

interface GraphNode extends SimulationNodeDatum {
  id: string;
  objectType: string;
  primaryKey: string;
  isRoot: boolean;
}

interface GraphEdge extends SimulationLinkDatum<GraphNode> {
  id: string;
  source: string | GraphNode;
  target: string | GraphNode;
  linkApiName: string;
  linkLabel: string;
}

interface RelationshipGraphProps {
  ontologyApiName: string;
  rootObjectType: string;
  rootPrimaryKey: string;
}

function nodeKey(apiName: string, pk: string): string {
  return `${apiName}:${pk}`;
}

function edgeKey(from: string, to: string, link: string): string {
  return `${from}__${to}__${link}`;
}

function nodeLabel(objectType: string, primaryKey: string): string {
  return `${objectType} · ${primaryKey}`;
}

function nodeInitial(objectType: string): string {
  return (objectType || '?').charAt(0).toUpperCase();
}

export function RelationshipGraph({
  ontologyApiName,
  rootObjectType,
  rootPrimaryKey,
}: RelationshipGraphProps) {
  const rootKey = nodeKey(rootObjectType, rootPrimaryKey);

  const [nodes, setNodes] = useState<Map<string, GraphNode>>(
    () =>
      new Map([
        [
          rootKey,
          {
            id: rootKey,
            objectType: rootObjectType,
            primaryKey: rootPrimaryKey,
            isRoot: true,
          },
        ],
      ]),
  );
  const [edges, setEdges] = useState<Map<string, GraphEdge>>(() => new Map());
  const [expanding, setExpanding] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [emptyAtRoot, setEmptyAtRoot] = useState(false);

  // Refs track in-flight + completed expansions so the expandNode callback
  // can stay stable (only ontologyApiName as a dep) without falling into
  // the "ran twice" trap.
  const inFlightRef = useRef<Set<string>>(new Set());
  const completedRef = useRef<Set<string>>(new Set());

  const expandNode = useCallback(
    async (key: string, objectType: string, primaryKey: string, isRoot: boolean) => {
      if (inFlightRef.current.has(key) || completedRef.current.has(key)) return;
      inFlightRef.current.add(key);
      setExpanding(key);
      try {
        const linkTypes: LinkType[] = await listOutgoingLinkTypes(
          ontologyApiName,
          objectType,
        );
        if (linkTypes.length === 0) {
          if (isRoot) setEmptyAtRoot(true);
          completedRef.current.add(key);
          return;
        }
        const results = await Promise.all(
          linkTypes.map(async (lt) => ({
            lt,
            page: await listLinkedObjects({
              ontologyApiName,
              objectType,
              primaryKey,
              linkType: lt.apiName,
              pageSize: PAGE_SIZE,
            }),
          })),
        );

        setNodes((prev) => {
          const next = new Map(prev);
          for (const { lt, page } of results) {
            for (const obj of page.data) {
              const childPk = String(obj.__primaryKey ?? '');
              if (!childPk) continue;
              const childKey = nodeKey(lt.linkedObjectTypeApiName, childPk);
              if (!next.has(childKey)) {
                next.set(childKey, {
                  id: childKey,
                  objectType: lt.linkedObjectTypeApiName,
                  primaryKey: childPk,
                  isRoot: false,
                });
              }
            }
          }
          return next;
        });
        setEdges((prev) => {
          const next = new Map(prev);
          for (const { lt, page } of results) {
            for (const obj of page.data) {
              const childPk = String(obj.__primaryKey ?? '');
              if (!childPk) continue;
              const childKey = nodeKey(lt.linkedObjectTypeApiName, childPk);
              const eid = edgeKey(key, childKey, lt.apiName);
              if (!next.has(eid)) {
                next.set(eid, {
                  id: eid,
                  source: key,
                  target: childKey,
                  linkApiName: lt.apiName,
                  linkLabel: lt.displayName || lt.apiName,
                });
              }
            }
          }
          return next;
        });
        completedRef.current.add(key);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to expand');
      } finally {
        inFlightRef.current.delete(key);
        setExpanding((cur) => (cur === key ? null : cur));
      }
    },
    [ontologyApiName],
  );

  // Reset on root change and auto-expand the root.
  useEffect(() => {
    inFlightRef.current = new Set();
    completedRef.current = new Set();
    setNodes(
      new Map([
        [
          rootKey,
          {
            id: rootKey,
            objectType: rootObjectType,
            primaryKey: rootPrimaryKey,
            isRoot: true,
          },
        ],
      ]),
    );
    setEdges(new Map());
    setExpanding(null);
    setError(null);
    setEmptyAtRoot(false);
    expandNode(rootKey, rootObjectType, rootPrimaryKey, true);
  }, [rootKey, rootObjectType, rootPrimaryKey, expandNode]);

  const graphNodes = useMemo(() => Array.from(nodes.values()), [nodes]);
  const graphEdges = useMemo(() => Array.from(edges.values()), [edges]);

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
          .distance(140)
          .strength(0.5),
      )
      .force('charge', forceManyBody().strength(-340))
      .force('center', forceCenter(WIDTH / 2, HEIGHT / 2))
      .force('collide', forceCollide(NODE_RADIUS + 14))
      .stop();

    for (let i = 0; i < SIM_ITERATIONS; i++) sim.tick();

    simNodes.forEach((n) => {
      map.set(n.id, {
        x: n.x ?? WIDTH / 2,
        y: n.y ?? HEIGHT / 2,
      });
    });
    return map;
    // graphSignature is a stable string hash of node + edge identity.
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

  const handleNodeClick = (node: GraphNode) => {
    expandNode(node.id, node.objectType, node.primaryKey, false);
  };

  const nodeCount = graphNodes.length;
  const edgeCount = graphEdges.length;
  const showEmpty = emptyAtRoot && edgeCount === 0;

  return (
    <div
      className="flex flex-col gap-2"
      data-testid="relationship-graph"
    >
      <div className="flex items-center gap-2 text-xs text-text-secondary">
        <span>
          {nodeCount} nodes · {edgeCount} edges
        </span>
        {expanding && (
          <span data-testid="relationship-expanding" className="flex items-center gap-1">
            <LoadingSpinner size="sm" />
            <span>Expanding…</span>
          </span>
        )}
        <div className="flex-1" />
        <button
          type="button"
          aria-label="Reset view"
          onClick={resetView}
          className="px-2 py-1 text-xs rounded bg-bg-tertiary text-text-primary border border-transparent hover:border-accent-cyan/40"
        >
          Reset view
        </button>
      </div>

      {error && (
        <p
          className="text-xs text-accent-error"
          data-testid="relationship-error"
        >
          Failed to load relationships: {error}
        </p>
      )}

      {showEmpty && !error && (
        <div
          className="flex items-center justify-center py-12 text-xs text-text-secondary"
          data-testid="relationship-empty"
        >
          This object type has no outgoing link types.
        </div>
      )}

      {!showEmpty && (
        <svg
          viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
          preserveAspectRatio="xMidYMid meet"
          className="w-full h-[480px] select-none border border-border rounded bg-bg-secondary"
          data-testid="relationship-graph-svg"
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
              id="relationship-arrow"
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
                typeof edge.source === 'string' ? edge.source : edge.source.id;
              const dst =
                typeof edge.target === 'string' ? edge.target : edge.target.id;
              const s = positions.get(src);
              const t = positions.get(dst);
              if (!s || !t) return null;
              const midX = (s.x + t.x) / 2;
              const midY = (s.y + t.y) / 2;
              return (
                <g
                  key={edge.id}
                  data-testid="relationship-edge"
                  data-edge-from={src}
                  data-edge-to={dst}
                  data-link-api-name={edge.linkApiName}
                >
                  <line
                    x1={s.x}
                    y1={s.y}
                    x2={t.x}
                    y2={t.y}
                    stroke="#475569"
                    strokeWidth={1.5}
                    markerEnd="url(#relationship-arrow)"
                  />
                  <g transform={`translate(${midX},${midY})`}>
                    <rect
                      x={-40}
                      y={-9}
                      width={80}
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
                    >
                      {edge.linkLabel}
                    </text>
                  </g>
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
                  data-testid="relationship-node"
                  data-node-id={node.id}
                  data-root={node.isRoot ? 'true' : 'false'}
                  onClick={() => handleNodeClick(node)}
                >
                  <title>{nodeLabel(node.objectType, node.primaryKey)}</title>
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
                    fontSize={14}
                    fontWeight={600}
                    fill="#e2e8f0"
                  >
                    {nodeInitial(node.objectType)}
                  </text>
                  <text
                    textAnchor="middle"
                    y={NODE_RADIUS + 14}
                    fontSize={10}
                    fill="#cbd5e1"
                  >
                    {node.primaryKey}
                  </text>
                </g>
              );
            })}
          </g>
        </svg>
      )}
    </div>
  );
}
