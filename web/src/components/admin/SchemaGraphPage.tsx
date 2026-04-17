import { useMemo, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router';
import { useQueries } from '@tanstack/react-query';
import {
  forceCenter,
  forceCollide,
  forceLink,
  forceManyBody,
  forceSimulation,
  type SimulationLinkDatum,
  type SimulationNodeDatum,
} from 'd3-force';
import type { LinkType, ObjectType } from '../../api/types';
import { listObjectTypeInterfaces } from '../../api/ontologies';
import { useObjectTypes } from '../../hooks/useObjectTypes';
import { useLinkTypes } from '../../hooks/useLinkTypes';
import { useInterfacesAdmin } from '../../hooks/useInterfaces';
import { LoadingSpinner } from '../common/LoadingSpinner';

const WIDTH = 1200;
const HEIGHT = 800;
const DEFAULT_COLOR = '#67e8f9';
const NODE_RADIUS = 32;
const SIM_ITERATIONS = 300;

interface GraphNode extends SimulationNodeDatum {
  id: string;
  rid: string;
  displayName: string;
  color?: string;
  icon?: string;
}

interface GraphEdge extends SimulationLinkDatum<GraphNode> {
  id: string;
  source: string | GraphNode;
  target: string | GraphNode;
  cardinality: LinkType['cardinality'];
  label: string;
}

function cardinalityLabel(c: LinkType['cardinality']): string {
  switch (c) {
    case 'ONE_TO_ONE':
      return '1:1';
    case 'ONE_TO_MANY':
      return '1:N';
    case 'MANY_TO_MANY':
      return 'N:N';
  }
}

function iconInitial(ot: ObjectType): string {
  const src = ot.icon || ot.displayName || ot.apiName;
  return src.charAt(0).toUpperCase();
}

export function SchemaGraphPage() {
  const { ontology } = useParams<{ ontology: string }>();
  const ontologyApiName = ontology ?? '';
  const navigate = useNavigate();

  const {
    data: objectTypes,
    isLoading: loadingOT,
    error: errOT,
  } = useObjectTypes(ontologyApiName);
  const {
    data: linkTypes,
    isLoading: loadingLT,
    error: errLT,
  } = useLinkTypes(ontologyApiName);
  const { data: interfaces } = useInterfacesAdmin(ontologyApiName);

  const [interfaceFilter, setInterfaceFilter] = useState<string>('ALL');

  const attachmentQueries = useQueries({
    queries: (objectTypes ?? []).map((ot) => ({
      queryKey: ['objectTypeInterfaces', ontologyApiName, ot.rid],
      queryFn: () => listObjectTypeInterfaces(ontologyApiName, ot.rid),
      enabled:
        !!ontologyApiName && !!ot.rid && interfaceFilter !== 'ALL',
      staleTime: 30_000,
    })),
  });

  const attachmentSignature = attachmentQueries
    .map((q, i) =>
      (q.data ?? [])
        .map((row) => row.interfaceRid)
        .sort()
        .join(',') + '@' + i,
    )
    .join('|');

  const attachmentsByRid = useMemo(() => {
    const map = new Map<string, string[]>();
    (objectTypes ?? []).forEach((ot, i) => {
      const rows = attachmentQueries[i]?.data ?? [];
      map.set(
        ot.rid,
        rows.map((row) => row.interfaceRid),
      );
    });
    return map;
    // attachmentSignature is a stable string hash of the attachment data.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [objectTypes, attachmentSignature]);

  const filteredObjectTypes = useMemo(() => {
    if (!objectTypes) return [];
    if (interfaceFilter === 'ALL') return objectTypes;
    return objectTypes.filter((ot) =>
      (attachmentsByRid.get(ot.rid) ?? []).includes(interfaceFilter),
    );
  }, [objectTypes, interfaceFilter, attachmentsByRid]);

  const visibleApiNames = useMemo(
    () => new Set(filteredObjectTypes.map((ot) => ot.apiName)),
    [filteredObjectTypes],
  );

  const filteredLinkTypes = useMemo(() => {
    if (!linkTypes) return [];
    return linkTypes.filter(
      (lt) =>
        visibleApiNames.has(lt.objectTypeApiName) &&
        visibleApiNames.has(lt.linkedObjectTypeApiName),
    );
  }, [linkTypes, visibleApiNames]);

  const nodes: GraphNode[] = useMemo(
    () =>
      filteredObjectTypes.map((ot) => ({
        id: ot.apiName,
        rid: ot.rid,
        displayName: ot.displayName,
        color: ot.color,
        icon: iconInitial(ot),
      })),
    [filteredObjectTypes],
  );

  const edges: GraphEdge[] = useMemo(
    () =>
      filteredLinkTypes.map((lt) => ({
        id: lt.rid,
        source: lt.objectTypeApiName,
        target: lt.linkedObjectTypeApiName,
        cardinality: lt.cardinality,
        label: lt.displayName,
      })),
    [filteredLinkTypes],
  );

  const graphSignature =
    nodes.map((n) => n.id).sort().join(',') +
    '||' +
    edges
      .map((e) => `${e.id}:${e.source as string}>${e.target as string}`)
      .sort()
      .join(',');

  const positions = useMemo<Map<string, { x: number; y: number }>>(() => {
    const map = new Map<string, { x: number; y: number }>();
    if (nodes.length === 0) return map;
    const simNodes: GraphNode[] = nodes.map((n) => ({ ...n }));
    const simEdges = edges.map((e) => ({ ...e }));
    const sim = forceSimulation<GraphNode>(simNodes)
      .force(
        'link',
        forceLink<GraphNode, (typeof simEdges)[number]>(simEdges)
          .id((n) => n.id)
          .distance(180)
          .strength(0.5),
      )
      .force('charge', forceManyBody().strength(-400))
      .force('center', forceCenter(WIDTH / 2, HEIGHT / 2))
      .force('collide', forceCollide(NODE_RADIUS + 18))
      .stop();

    for (let i = 0; i < SIM_ITERATIONS; i++) sim.tick();

    simNodes.forEach((n) => {
      map.set(n.id, {
        x: n.x ?? WIDTH / 2,
        y: n.y ?? HEIGHT / 2,
      });
    });
    return map;
    // graphSignature captures the identity of nodes+edges as a stable string.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [graphSignature]);

  // Zoom & pan
  const [tx, setTx] = useState(0);
  const [ty, setTy] = useState(0);
  const [scale, setScale] = useState(1);
  const svgRef = useRef<SVGSVGElement | null>(null);
  const dragRef = useRef<{
    startX: number;
    startY: number;
    startTx: number;
    startTy: number;
  } | null>(null);

  const onWheel = (e: React.WheelEvent<SVGSVGElement>) => {
    // Use deltaY (negative = zoom in)
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

  const handleNodeClick = () => {
    navigate(`/admin/${encodeURIComponent(ontologyApiName)}/objectTypes`);
  };

  if (!ontologyApiName) {
    return (
      <div className="flex items-center justify-center h-[calc(100vh-3rem)] text-text-secondary text-sm">
        Select an ontology from the dashboard first.
      </div>
    );
  }

  const loading = loadingOT || loadingLT;
  const error = errOT ?? errLT;
  const nodeCount = nodes.length;
  const edgeCount = edges.length;

  return (
    <div className="flex flex-col h-[calc(100vh-3rem)] bg-bg-primary overflow-hidden">
      <header
        className="px-6 py-4 border-b flex flex-wrap items-center gap-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <h1 className="text-base font-semibold tracking-wide text-text-primary">
          Ontology Manager — Schema Graph
        </h1>
        <span className="text-xs text-text-secondary uppercase tracking-widest">
          {ontologyApiName}
        </span>
        <div className="flex-1" />
        <span className="text-xs text-text-secondary">
          {nodeCount} types · {edgeCount} links
        </span>
      </header>

      <div
        className="px-6 py-3 border-b flex flex-wrap items-center gap-3"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <label className="text-xs text-text-secondary flex items-center gap-2">
          <span className="uppercase tracking-widest">Interface</span>
          <select
            aria-label="Filter by interface"
            value={interfaceFilter}
            onChange={(e) => setInterfaceFilter(e.target.value)}
            className="px-2 py-1 rounded bg-bg-tertiary text-text-primary text-xs outline-none border border-transparent focus:border-accent-cyan/40"
          >
            <option value="ALL">All interfaces</option>
            {(interfaces ?? []).map((iface) => (
              <option key={iface.rid} value={iface.rid}>
                {iface.displayName}
              </option>
            ))}
          </select>
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
        {loading && (
          <div className="absolute inset-0 flex items-center justify-center">
            <LoadingSpinner size="lg" />
          </div>
        )}
        {!loading && error && (
          <div className="absolute inset-0 flex items-center justify-center px-6">
            <p className="text-sm text-accent-error">
              Failed to load schema: {(error as Error).message}
            </p>
          </div>
        )}
        {!loading && !error && nodeCount === 0 && (
          <div className="absolute inset-0 flex items-center justify-center">
            <div className="max-w-md text-center">
              <p className="text-sm text-text-primary font-semibold">
                No object types match the filters
              </p>
              <p className="text-xs text-text-secondary mt-2">
                Adjust the interface filter or create new Object Types.
              </p>
            </div>
          </div>
        )}
        {!loading && !error && nodeCount > 0 && (
          <svg
            ref={svgRef}
            viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
            preserveAspectRatio="xMidYMid meet"
            className="w-full h-full select-none"
            data-testid="schema-graph-svg"
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
                id="schema-arrow"
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
              {edges.map((edge) => {
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
                  <g key={edge.id} data-testid="graph-edge">
                    <line
                      x1={s.x}
                      y1={s.y}
                      x2={t.x}
                      y2={t.y}
                      stroke="#475569"
                      strokeWidth={1.5}
                      markerEnd="url(#schema-arrow)"
                    />
                    <g transform={`translate(${midX},${midY})`}>
                      <rect
                        x={-16}
                        y={-9}
                        width={32}
                        height={16}
                        rx={3}
                        fill="rgba(15,23,42,0.9)"
                        stroke="#334155"
                      />
                      <text
                        textAnchor="middle"
                        dominantBaseline="central"
                        fontSize={10}
                        fill="#cbd5e1"
                        data-testid="edge-cardinality"
                      >
                        {cardinalityLabel(edge.cardinality)}
                      </text>
                    </g>
                  </g>
                );
              })}
              {nodes.map((node) => {
                const p = positions.get(node.id);
                if (!p) return null;
                const fill = node.color ?? DEFAULT_COLOR;
                return (
                  <g
                    key={node.id}
                    transform={`translate(${p.x},${p.y})`}
                    className="cursor-pointer"
                    data-testid="graph-node"
                    data-api-name={node.id}
                    onClick={handleNodeClick}
                  >
                    <circle
                      r={NODE_RADIUS}
                      fill={fill}
                      fillOpacity={0.18}
                      stroke={fill}
                      strokeWidth={2}
                    />
                    <text
                      textAnchor="middle"
                      dominantBaseline="central"
                      fontSize={16}
                      fontWeight={600}
                      fill="#e2e8f0"
                    >
                      {node.icon}
                    </text>
                    <text
                      textAnchor="middle"
                      y={NODE_RADIUS + 16}
                      fontSize={12}
                      fill="#cbd5e1"
                    >
                      {node.displayName}
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
