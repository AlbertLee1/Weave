import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router';
import { ApiRequestError } from '../../api/client';
import type { Pipeline } from '../../api/pipelines';
import { usePipeline, usePipelines } from '../../hooks/usePipelines';
import { EmptyState } from '../common/EmptyState';
import { LoadingSpinner } from '../common/LoadingSpinner';

function describeError(err: unknown): string {
  if (err instanceof ApiRequestError) {
    return `${err.errorName}: ${err.parameters?.reason ?? err.message}`;
  }
  if (err instanceof Error) return err.message;
  return 'Request failed.';
}

const NODE_KIND_META = {
  input: { label: 'Input', color: '#F59E0B' },
  transform: { label: 'Transform', color: '#14B8A6' },
  output: { label: 'Output', color: '#10B981' },
} as const;

type NodeKind = keyof typeof NODE_KIND_META;

interface DagNode {
  name: string;
  kind: NodeKind;
  type: string;
  upstreams: string[];
  downstreams: string[];
  column: number;
  row: number;
  config?: Record<string, unknown>;
}

interface DagLayout {
  nodes: DagNode[];
  byName: Map<string, DagNode>;
  columnCount: number;
  rowCount: number;
}

// layoutPipeline builds a left-to-right column layout. Inputs sit in column 0;
// each Transform's column is 1 + max(upstream.column); Outputs land in the
// last column. Rows are assigned greedily so every column packs from the top.
function layoutPipeline(p: Pipeline): DagLayout {
  const byName = new Map<string, DagNode>();
  for (const i of p.inputs ?? []) {
    byName.set(i.name, {
      name: i.name,
      kind: 'input',
      type: i.type,
      upstreams: [],
      downstreams: [],
      column: 0,
      row: 0,
      config: i.config,
    });
  }
  for (const t of p.transforms ?? []) {
    byName.set(t.name, {
      name: t.name,
      kind: 'transform',
      type: t.type,
      upstreams: t.inputs ? [...t.inputs] : [],
      downstreams: [],
      column: 0,
      row: 0,
      config: t.config,
    });
  }
  for (const o of p.outputs ?? []) {
    byName.set(o.name, {
      name: o.name,
      kind: 'output',
      type: o.type,
      upstreams: o.input ? [o.input] : [],
      downstreams: [],
      column: 0,
      row: 0,
      config: o.config,
    });
  }
  // Compute downstream lists.
  for (const node of byName.values()) {
    for (const up of node.upstreams) {
      const upNode = byName.get(up);
      if (upNode) upNode.downstreams.push(node.name);
    }
  }
  // Column = 0 for inputs (already set); for transforms, longest path from any
  // input. For outputs, last column.
  const transforms = (p.transforms ?? []).map((t) => byName.get(t.name)!);
  let maxTransformColumn = 0;
  for (const t of transforms) {
    let col = 1;
    for (const up of t.upstreams) {
      const upNode = byName.get(up);
      if (!upNode) continue;
      col = Math.max(col, upNode.column + 1);
    }
    t.column = col;
    if (col > maxTransformColumn) maxTransformColumn = col;
  }
  const outputColumn = maxTransformColumn + 1;
  for (const o of p.outputs ?? []) {
    const node = byName.get(o.name)!;
    node.column = outputColumn;
  }
  // Row layout: pack each column top-down.
  const nodesArr = [...byName.values()];
  const columnCount = outputColumn + 1;
  const perColumn: DagNode[][] = Array.from({ length: columnCount }, () => []);
  for (const n of nodesArr) {
    perColumn[n.column].push(n);
  }
  let maxRow = 0;
  for (const col of perColumn) {
    col.forEach((n, idx) => {
      n.row = idx;
      if (idx > maxRow) maxRow = idx;
    });
  }
  return {
    nodes: nodesArr,
    byName,
    columnCount,
    rowCount: maxRow + 1,
  };
}

interface PipelineGraphProps {
  pipeline: Pipeline;
  onSelect: (nodeName: string) => void;
  selectedNode: string | null;
}

const NODE_WIDTH = 160;
const NODE_HEIGHT = 56;
const COLUMN_GAP = 80;
const ROW_GAP = 24;
const PADDING = 20;

function PipelineGraph({ pipeline, onSelect, selectedNode }: PipelineGraphProps) {
  const layout = useMemo(() => layoutPipeline(pipeline), [pipeline]);
  if (layout.nodes.length === 0) {
    return (
      <EmptyState
        title="Empty pipeline"
        description="This pipeline has no nodes yet."
      />
    );
  }
  const colStep = NODE_WIDTH + COLUMN_GAP;
  const rowStep = NODE_HEIGHT + ROW_GAP;
  const width = PADDING * 2 + layout.columnCount * NODE_WIDTH + (layout.columnCount - 1) * COLUMN_GAP;
  const height = PADDING * 2 + layout.rowCount * NODE_HEIGHT + (layout.rowCount - 1) * ROW_GAP;
  const positionFor = (n: DagNode) => ({
    x: PADDING + n.column * colStep,
    y: PADDING + n.row * rowStep,
  });
  const edges: { from: DagNode; to: DagNode }[] = [];
  for (const n of layout.nodes) {
    for (const up of n.upstreams) {
      const upNode = layout.byName.get(up);
      if (upNode) edges.push({ from: upNode, to: n });
    }
  }
  return (
    <div className="overflow-auto rounded-lg border border-border/40 bg-bg-primary/40 p-2">
      <svg
        role="img"
        aria-label="Pipeline execution graph"
        data-testid="pipeline-graph"
        width={width}
        height={height}
        className="block"
      >
        {edges.map((e, i) => {
          const a = positionFor(e.from);
          const b = positionFor(e.to);
          const x1 = a.x + NODE_WIDTH;
          const y1 = a.y + NODE_HEIGHT / 2;
          const x2 = b.x;
          const y2 = b.y + NODE_HEIGHT / 2;
          const midX = (x1 + x2) / 2;
          const path = `M ${x1} ${y1} C ${midX} ${y1} ${midX} ${y2} ${x2} ${y2}`;
          return (
            <path
              key={`edge-${i}`}
              d={path}
              data-testid="pipeline-graph-edge"
              data-from={e.from.name}
              data-to={e.to.name}
              fill="none"
              stroke="rgba(148,163,184,0.5)"
              strokeWidth={1.5}
            />
          );
        })}
        {layout.nodes.map((n) => {
          const { x, y } = positionFor(n);
          const meta = NODE_KIND_META[n.kind];
          const selected = selectedNode === n.name;
          return (
            <g
              key={n.name}
              data-testid="pipeline-graph-node"
              data-node-name={n.name}
              data-node-kind={n.kind}
              transform={`translate(${x}, ${y})`}
              tabIndex={0}
              role="button"
              aria-label={`${meta.label} ${n.name}`}
              onClick={() => onSelect(n.name)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  onSelect(n.name);
                }
              }}
              style={{ cursor: 'pointer' }}
            >
              <rect
                width={NODE_WIDTH}
                height={NODE_HEIGHT}
                rx={8}
                fill="rgba(30,36,51,0.92)"
                stroke={selected ? meta.color : `${meta.color}80`}
                strokeWidth={selected ? 2 : 1}
                style={{
                  filter: selected
                    ? `drop-shadow(0 0 6px ${meta.color}80)`
                    : undefined,
                }}
              />
              <text
                x={12}
                y={20}
                fill={meta.color}
                style={{ fontSize: 10, fontFamily: 'var(--font-sans)', fontWeight: 600, letterSpacing: '0.05em', textTransform: 'uppercase' }}
              >
                {meta.label}
              </text>
              <text
                x={12}
                y={36}
                fill="#E5E7EB"
                style={{ fontSize: 13, fontFamily: 'var(--font-sans)', fontWeight: 500 }}
              >
                {n.name}
              </text>
              <text
                x={12}
                y={50}
                fill="#9CA3AF"
                style={{ fontSize: 11, fontFamily: 'var(--font-mono, monospace)' }}
              >
                {n.type}
              </text>
            </g>
          );
        })}
      </svg>
    </div>
  );
}

interface PipelineLogPanelProps {
  pipeline: Pipeline;
  selectedNode: string | null;
}

// PipelineLogPanel renders structural details + selected-node config. The
// "log viewer" name reflects the spec; runs persistence lands in US-298 and
// will plug live execution traces into the same panel.
function PipelineLogPanel({ pipeline, selectedNode }: PipelineLogPanelProps) {
  const layout = useMemo(() => layoutPipeline(pipeline), [pipeline]);
  const node = selectedNode ? layout.byName.get(selectedNode) ?? null : null;
  return (
    <aside
      data-testid="pipeline-log-panel"
      className="flex w-80 shrink-0 flex-col border-l border-border/50 bg-bg-secondary/40"
    >
      <header className="border-b border-border/50 px-3 py-2">
        <h3 className="text-sm font-semibold text-text-primary">Pipeline log</h3>
        <p className="text-[11px] text-text-muted">
          Live run history will appear here once US-298 lands; for now this
          panel surfaces the pipeline definition and selected-node config.
        </p>
      </header>
      <div className="flex-1 space-y-4 overflow-y-auto px-3 py-3 text-xs text-text-secondary">
        <section>
          <h4 className="mb-1 text-[11px] font-semibold uppercase tracking-wider text-text-muted">
            Pipeline
          </h4>
          <dl className="space-y-1">
            <div className="flex justify-between gap-2">
              <dt className="text-text-muted">Schedule</dt>
              <dd className="font-mono text-[11px]" data-testid="pipeline-log-schedule">
                {pipeline.schedule || 'on demand'}
              </dd>
            </div>
            <div className="flex justify-between gap-2">
              <dt className="text-text-muted">Enabled</dt>
              <dd>
                <span
                  data-testid="pipeline-log-enabled"
                  className={`rounded px-1.5 py-0.5 text-[10px] font-semibold ${
                    pipeline.enabled
                      ? 'bg-teal-500/15 text-teal-300'
                      : 'bg-rose-500/15 text-rose-300'
                  }`}
                >
                  {pipeline.enabled ? 'enabled' : 'disabled'}
                </span>
              </dd>
            </div>
            <div className="flex justify-between gap-2">
              <dt className="text-text-muted">Inputs</dt>
              <dd>{(pipeline.inputs ?? []).length}</dd>
            </div>
            <div className="flex justify-between gap-2">
              <dt className="text-text-muted">Transforms</dt>
              <dd>{(pipeline.transforms ?? []).length}</dd>
            </div>
            <div className="flex justify-between gap-2">
              <dt className="text-text-muted">Outputs</dt>
              <dd>{(pipeline.outputs ?? []).length}</dd>
            </div>
            <div className="flex justify-between gap-2">
              <dt className="text-text-muted">Updated</dt>
              <dd className="font-mono text-[11px]">
                {new Date(pipeline.updatedAt).toLocaleString()}
              </dd>
            </div>
          </dl>
        </section>

        {node ? (
          <section data-testid="pipeline-log-selected">
            <h4 className="mb-1 text-[11px] font-semibold uppercase tracking-wider text-text-muted">
              Selected node
            </h4>
            <dl className="space-y-1">
              <div className="flex justify-between gap-2">
                <dt className="text-text-muted">Name</dt>
                <dd className="font-mono text-[11px]">{node.name}</dd>
              </div>
              <div className="flex justify-between gap-2">
                <dt className="text-text-muted">Kind</dt>
                <dd>{NODE_KIND_META[node.kind].label}</dd>
              </div>
              <div className="flex justify-between gap-2">
                <dt className="text-text-muted">Type</dt>
                <dd className="font-mono text-[11px]">{node.type}</dd>
              </div>
              {node.upstreams.length > 0 && (
                <div className="flex flex-col gap-0.5">
                  <dt className="text-text-muted">Upstream</dt>
                  <dd className="font-mono text-[11px]">
                    {node.upstreams.join(', ')}
                  </dd>
                </div>
              )}
              {node.downstreams.length > 0 && (
                <div className="flex flex-col gap-0.5">
                  <dt className="text-text-muted">Downstream</dt>
                  <dd className="font-mono text-[11px]">
                    {node.downstreams.join(', ')}
                  </dd>
                </div>
              )}
            </dl>
            {node.config && Object.keys(node.config).length > 0 && (
              <pre
                data-testid="pipeline-log-config"
                className="mt-2 max-h-48 overflow-y-auto rounded bg-bg-primary/80 p-2 font-mono text-[10px] text-text-primary"
              >
                {JSON.stringify(node.config, null, 2)}
              </pre>
            )}
          </section>
        ) : (
          <section data-testid="pipeline-log-no-selection" className="text-text-muted">
            Click a node on the graph to inspect its configuration.
          </section>
        )}
      </div>
    </aside>
  );
}

interface PipelineListProps {
  pipelines: Pipeline[];
  loading: boolean;
  error: unknown;
  activeId: string | null;
  onSelect: (id: string) => void;
}

function PipelineList({
  pipelines,
  loading,
  error,
  activeId,
  onSelect,
}: PipelineListProps) {
  const navigate = useNavigate();
  return (
    <aside
      data-testid="pipeline-list"
      aria-label="Pipeline list"
      className="flex w-72 shrink-0 flex-col rounded-lg border border-border/50 bg-bg-secondary/60"
    >
      <header className="border-b border-border/50 px-3 py-3">
        <h2 className="text-sm font-semibold tracking-tight text-text-primary">
          Pipelines
        </h2>
        <p className="text-[11px] text-text-secondary">
          Monitor pipeline structure, schedule, and per-node configuration.
        </p>
      </header>
      <div className="flex-1 overflow-y-auto">
        {loading ? (
          <div
            data-testid="pipeline-list-loading"
            className="flex items-center justify-center py-10"
          >
            <LoadingSpinner />
          </div>
        ) : error ? (
          <div
            data-testid="pipeline-list-error"
            className="px-3 py-3 text-xs text-rose-300"
            role="alert"
          >
            {describeError(error)}
          </div>
        ) : pipelines.length === 0 ? (
          <div data-testid="pipeline-list-empty">
            <EmptyState
              title="No pipelines yet"
              description="Pipelines created via POST /api/v2/pipelines will appear here."
              action={
                <button
                  type="button"
                  data-testid="pipeline-empty-cta"
                  onClick={() => navigate('/pipelines/new')}
                  className="rounded-md bg-amber-600 px-3 py-1.5 text-sm font-semibold text-white shadow hover:bg-amber-500"
                >
                  + New pipeline
                </button>
              }
            />
          </div>
        ) : (
          pipelines.map((p) => (
            <button
              key={p.id}
              type="button"
              data-testid="pipeline-list-item"
              data-pipeline-id={p.id}
              onClick={() => onSelect(p.id)}
              className={`flex w-full flex-col gap-1 border-b border-border/30 px-3 py-2.5 text-left text-xs transition hover:bg-bg-tertiary/60 ${
                p.id === activeId ? 'bg-bg-tertiary/80' : ''
              }`}
            >
              <span className="text-sm font-medium text-text-primary">
                {p.name || p.id}
              </span>
              <span className="text-[11px] text-text-muted">{p.id}</span>
              <span className="flex items-center gap-2 text-[11px] text-text-secondary">
                <span>
                  {(p.inputs ?? []).length} in
                </span>
                <span>
                  {(p.transforms ?? []).length} tx
                </span>
                <span>
                  {(p.outputs ?? []).length} out
                </span>
                <span className="ml-auto">
                  <span
                    className={`rounded px-1.5 py-0.5 text-[10px] font-semibold ${
                      p.enabled
                        ? 'bg-teal-500/15 text-teal-300'
                        : 'bg-rose-500/15 text-rose-300'
                    }`}
                  >
                    {p.enabled ? 'on' : 'off'}
                  </span>
                </span>
              </span>
            </button>
          ))
        )}
      </div>
    </aside>
  );
}

interface PipelineDetailProps {
  pipelineId: string | null;
}

function PipelineDetail({ pipelineId }: PipelineDetailProps) {
  const detailQuery = usePipeline(pipelineId);
  const pipeline = detailQuery.data ?? null;
  const [selectedNode, setSelectedNode] = useState<string | null>(null);

  useEffect(() => {
    setSelectedNode(null);
  }, [pipelineId]);

  if (!pipelineId) {
    return (
      <section
        data-testid="pipeline-detail-empty"
        className="flex flex-1 items-center justify-center rounded-lg border border-border/50 bg-bg-secondary/60"
      >
        <EmptyState
          title="No pipeline selected"
          description="Pick a pipeline on the left to inspect its execution graph."
        />
      </section>
    );
  }

  if (detailQuery.isLoading && !pipeline) {
    return (
      <section
        data-testid="pipeline-detail-loading"
        className="flex flex-1 items-center justify-center rounded-lg border border-border/50 bg-bg-secondary/60"
      >
        <LoadingSpinner />
      </section>
    );
  }

  if (detailQuery.error || !pipeline) {
    return (
      <section
        data-testid="pipeline-detail-error"
        className="flex flex-1 items-center justify-center rounded-lg border border-border/50 bg-bg-secondary/60"
      >
        <EmptyState
          title="Failed to load pipeline"
          description={
            detailQuery.error
              ? describeError(detailQuery.error)
              : 'Pipeline not found.'
          }
        />
      </section>
    );
  }

  return (
    <section
      data-testid="pipeline-detail"
      className="flex flex-1 flex-col rounded-lg border border-border/50 bg-bg-secondary/60"
    >
      <header className="flex flex-wrap items-center justify-between gap-3 border-b border-border/50 px-3 py-2">
        <div>
          <h2 className="text-sm font-semibold tracking-tight text-text-primary">
            {pipeline.name || pipeline.id}
          </h2>
          <p className="text-[11px] text-text-muted">{pipeline.id}</p>
          {pipeline.description && (
            <p className="mt-1 max-w-2xl text-xs text-text-secondary">
              {pipeline.description}
            </p>
          )}
        </div>
        <div className="flex items-center gap-2 text-[11px] text-text-secondary">
          <span className="rounded-md border border-border/60 px-2 py-1 font-mono">
            {pipeline.schedule || 'on demand'}
          </span>
          <span
            className={`rounded-md px-2 py-1 font-semibold ${
              pipeline.enabled
                ? 'bg-teal-500/15 text-teal-300'
                : 'bg-rose-500/15 text-rose-300'
            }`}
          >
            {pipeline.enabled ? 'enabled' : 'disabled'}
          </span>
        </div>
      </header>
      <div className="flex flex-1 min-h-0">
        <div className="flex-1 overflow-auto p-3" data-testid="pipeline-graph-canvas">
          <PipelineGraph
            pipeline={pipeline}
            onSelect={setSelectedNode}
            selectedNode={selectedNode}
          />
        </div>
        <PipelineLogPanel pipeline={pipeline} selectedNode={selectedNode} />
      </div>
    </section>
  );
}

export function PipelinesPage() {
  const listQuery = usePipelines();
  const pipelines = useMemo(
    () => listQuery.data?.pipelines ?? [],
    [listQuery.data],
  );
  const [activeId, setActiveId] = useState<string | null>(null);

  useEffect(() => {
    if (activeId !== null) return;
    if (pipelines.length === 0) return;
    setActiveId(pipelines[0].id);
  }, [pipelines, activeId]);

  useEffect(() => {
    if (activeId === null) return;
    if (!pipelines.some((p) => p.id === activeId)) {
      setActiveId(pipelines[0]?.id ?? null);
    }
  }, [pipelines, activeId]);

  return (
    <div
      data-testid="pipelines-page"
      className="mx-auto flex h-[calc(100vh-9rem)] max-w-[1400px] gap-4"
    >
      <PipelineList
        pipelines={pipelines}
        loading={listQuery.isLoading}
        error={listQuery.error}
        activeId={activeId}
        onSelect={setActiveId}
      />
      <PipelineDetail pipelineId={activeId} />
    </div>
  );
}
