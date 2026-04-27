import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  Controls,
  MiniMap,
  addEdge,
  applyEdgeChanges,
  applyNodeChanges,
  type Connection,
  type Edge as RFEdge,
  type EdgeChange,
  type Node as RFNode,
  type NodeChange,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';

import { ApiRequestError } from '../../api/client';
import type {
  AIPLogicEdge,
  AIPLogicFlow,
  AIPLogicNode,
  AIPLogicRun,
  LogicNodeType,
} from '../../api/aipLogic';
import { KNOWN_LOGIC_NODE_TYPES } from '../../api/aipLogic';
import {
  useAIPLogicFlow,
  useAIPLogicFlows,
  useAIPLogicRuns,
  useCreateAIPLogicFlow,
  useDeleteAIPLogicFlow,
  useExecuteAIPLogicFlow,
  useUpdateAIPLogicFlow,
} from '../../hooks/useAIPLogicFlows';
import { EmptyState } from '../common/EmptyState';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { Modal } from '../common/Modal';

interface RFNodeData extends Record<string, unknown> {
  label: string;
  nodeType: LogicNodeType | string;
  config: Record<string, unknown>;
}

const NODE_TYPE_META: Record<
  LogicNodeType,
  { label: string; color: string; description: string }
> = {
  llm: {
    label: 'LLM',
    color: '#F59E0B',
    description: 'Calls a registered AIP provider with a prompt template.',
  },
  tool: {
    label: 'Tool',
    color: '#14B8A6',
    description: 'Invokes a registered tool with substituted parameters.',
  },
  if: {
    label: 'If',
    color: '#A78BFA',
    description: 'Branches on a condition (==, !=, <, <=, >, >=, contains).',
  },
  output: {
    label: 'Output',
    color: '#10B981',
    description: 'Collects state into the run output.',
  },
};

function describeError(err: unknown): string {
  if (err instanceof ApiRequestError) {
    return `${err.errorName}: ${err.parameters?.reason ?? err.message}`;
  }
  if (err instanceof Error) return err.message;
  return 'Request failed.';
}

function defaultConfigFor(nodeType: string): Record<string, unknown> {
  switch (nodeType) {
    case 'llm':
      return { provider: 'mock', model: '', promptTemplate: '' };
    case 'tool':
      return { tool: 'echo', params: {} };
    case 'if':
      return { condition: '' };
    case 'output':
      return { keys: [] as string[] };
  }
  return {};
}

interface NodePosition {
  x: number;
  y: number;
}

function readPosition(
  config: Record<string, unknown>,
  fallback: NodePosition,
): NodePosition {
  const meta = config['__editorPosition'];
  if (meta && typeof meta === 'object') {
    const obj = meta as { x?: unknown; y?: unknown };
    if (typeof obj.x === 'number' && typeof obj.y === 'number') {
      return { x: obj.x, y: obj.y };
    }
  }
  return fallback;
}

function stripEditorMeta(
  config: Record<string, unknown>,
): Record<string, unknown> {
  // The editor stamps the canvas position into config.__editorPosition so
  // the layout survives a reload. The persisted shape is fine but we want
  // to keep that key out of node Validation messaging when the runtime
  // logs config — here we just leave it through; the backend ignores
  // unknown config keys. Helper retained for future surface tweaks.
  return config;
}

function flowToReactFlow(
  flow: AIPLogicFlow,
): { nodes: RFNode<RFNodeData>[]; edges: RFEdge[] } {
  const nodes: RFNode<RFNodeData>[] = (flow.nodes ?? []).map((n, i) => {
    const cfg = (n.config ?? {}) as Record<string, unknown>;
    const fallback: NodePosition = { x: 80 + (i % 3) * 220, y: 80 + Math.floor(i / 3) * 140 };
    const pos = readPosition(cfg, fallback);
    return {
      id: n.id,
      position: pos,
      data: {
        label: n.id,
        nodeType: n.type,
        config: cfg,
      },
      type: 'default',
      style: nodeStyle(n.type),
    } satisfies RFNode<RFNodeData>;
  });
  const edges: RFEdge[] = (flow.edges ?? []).map((e, i) => ({
    id: `e_${i}_${e.from}_${e.to}_${e.branch ?? ''}`,
    source: e.from,
    target: e.to,
    label: e.branch || undefined,
    sourceHandle: e.branch ? `branch-${e.branch}` : undefined,
    data: { branch: e.branch ?? '' },
  }));
  return { nodes, edges };
}

function reactFlowToWire(
  nodes: RFNode<RFNodeData>[],
  edges: RFEdge[],
): { nodes: AIPLogicNode[]; edges: AIPLogicEdge[] } {
  const wireNodes: AIPLogicNode[] = nodes.map((n) => {
    const cfg = { ...(n.data?.config ?? {}) } as Record<string, unknown>;
    cfg['__editorPosition'] = { x: n.position.x, y: n.position.y };
    return {
      id: n.id,
      type: String(n.data?.nodeType ?? 'llm'),
      config: stripEditorMeta(cfg),
    };
  });
  const wireEdges: AIPLogicEdge[] = edges.map((e) => ({
    from: e.source,
    to: e.target,
    branch:
      typeof e.data?.branch === 'string' && e.data.branch !== ''
        ? (e.data.branch as string)
        : typeof e.label === 'string' && e.label !== ''
          ? e.label
          : undefined,
  }));
  return { nodes: wireNodes, edges: wireEdges };
}

function nodeStyle(nodeType: string): React.CSSProperties {
  const meta = NODE_TYPE_META[nodeType as LogicNodeType];
  const accent = meta?.color ?? '#9CA3AF';
  return {
    background: 'rgba(30,36,51,0.92)',
    border: `1px solid ${accent}80`,
    borderRadius: 8,
    color: '#E5E7EB',
    padding: 8,
    fontSize: 12,
    minWidth: 140,
    boxShadow: `0 0 12px ${accent}30`,
  };
}

let nodeIdCounter = 1;
function newNodeId(existing: Set<string>, base: string): string {
  while (true) {
    const candidate = `${base}_${nodeIdCounter++}`;
    if (!existing.has(candidate)) return candidate;
  }
}

interface NewFlowDraft {
  id: string;
  name: string;
  description: string;
}

const EMPTY_NEW_FLOW: NewFlowDraft = {
  id: '',
  name: '',
  description: '',
};

export function LogicFlowsPage() {
  return (
    <ReactFlowProvider>
      <LogicFlowsInner />
    </ReactFlowProvider>
  );
}

function LogicFlowsInner() {
  const flowsQuery = useAIPLogicFlows();
  const flows = useMemo(
    () => flowsQuery.data?.flows ?? [],
    [flowsQuery.data],
  );

  const [activeFlowId, setActiveFlowId] = useState<string | null>(null);

  useEffect(() => {
    if (activeFlowId !== null) return;
    if (flows.length === 0) return;
    setActiveFlowId(flows[0].id);
  }, [flows, activeFlowId]);

  useEffect(() => {
    if (activeFlowId === null) return;
    if (!flows.some((f) => f.id === activeFlowId)) {
      setActiveFlowId(flows[0]?.id ?? null);
    }
  }, [flows, activeFlowId]);

  const flowDetailQuery = useAIPLogicFlow(activeFlowId);
  const activeFlow = flowDetailQuery.data ?? null;

  const [newFlowOpen, setNewFlowOpen] = useState(false);
  const [draft, setDraft] = useState<NewFlowDraft>(EMPTY_NEW_FLOW);
  const [draftError, setDraftError] = useState<string | null>(null);

  const createMutation = useCreateAIPLogicFlow();
  const deleteMutation = useDeleteAIPLogicFlow();

  const openNewFlow = () => {
    setDraft(EMPTY_NEW_FLOW);
    setDraftError(null);
    setNewFlowOpen(true);
  };

  const submitDraft = () => {
    const name = draft.name.trim();
    if (!name) {
      setDraftError('Name is required.');
      return;
    }
    const seedNode: AIPLogicNode = {
      id: 'output',
      type: 'output',
      config: { keys: [], __editorPosition: { x: 200, y: 120 } },
    };
    createMutation.mutate(
      {
        id: draft.id.trim() || undefined,
        name,
        description: draft.description.trim() || undefined,
        nodes: [seedNode],
        edges: [],
      },
      {
        onSuccess: (created) => {
          setActiveFlowId(created.id);
          setNewFlowOpen(false);
        },
        onError: (err) => setDraftError(describeError(err)),
      },
    );
  };

  const onDeleteFlow = (flowId: string) => {
    if (typeof window !== 'undefined') {
      const ok = window.confirm('Delete this flow? This cannot be undone.');
      if (!ok) return;
    }
    deleteMutation.mutate(flowId, {
      onSuccess: () => {
        if (activeFlowId === flowId) setActiveFlowId(null);
      },
    });
  };

  return (
    <div className="mx-auto flex h-[calc(100vh-9rem)] max-w-[1400px] gap-4">
      <FlowList
        flows={flows}
        loading={flowsQuery.isLoading}
        error={flowsQuery.error}
        activeFlowId={activeFlowId}
        onSelect={setActiveFlowId}
        onNew={openNewFlow}
        onDelete={onDeleteFlow}
      />
      <FlowEditor
        key={activeFlow?.id ?? '__none__'}
        flow={activeFlow}
        loading={flowDetailQuery.isLoading}
      />

      <Modal
        open={newFlowOpen}
        onClose={() => setNewFlowOpen(false)}
        title="New Logic Flow"
        size="lg"
      >
        <div className="space-y-4">
          <label className="flex flex-col gap-1.5 text-xs text-text-secondary">
            Name
            <input
              type="text"
              value={draft.name}
              onChange={(e) =>
                setDraft((d) => ({ ...d, name: e.target.value }))
              }
              data-testid="new-flow-name"
              placeholder="My workflow"
              className="rounded-md border border-border/50 bg-bg-primary px-2.5 py-2 text-sm text-text-primary outline-none focus:border-amber-500/60"
            />
          </label>
          <label className="flex flex-col gap-1.5 text-xs text-text-secondary">
            ID (optional)
            <input
              type="text"
              value={draft.id}
              onChange={(e) =>
                setDraft((d) => ({ ...d, id: e.target.value }))
              }
              placeholder="auto-generated when blank (allowed: A-Z a-z 0-9 . _ -)"
              data-testid="new-flow-id"
              className="rounded-md border border-border/50 bg-bg-primary px-2.5 py-2 font-mono text-xs text-text-primary outline-none focus:border-amber-500/60"
            />
          </label>
          <label className="flex flex-col gap-1.5 text-xs text-text-secondary">
            Description (optional)
            <textarea
              value={draft.description}
              onChange={(e) =>
                setDraft((d) => ({ ...d, description: e.target.value }))
              }
              rows={3}
              data-testid="new-flow-description"
              className="rounded-md border border-border/50 bg-bg-primary px-2.5 py-2 text-xs text-text-primary outline-none focus:border-amber-500/60"
            />
          </label>
          {draftError && (
            <div
              role="alert"
              className="rounded-md border border-rose-500/40 bg-rose-500/10 px-3 py-2 text-xs text-rose-300"
            >
              {draftError}
            </div>
          )}
          <div className="flex justify-end gap-2">
            <button
              type="button"
              onClick={() => setNewFlowOpen(false)}
              className="rounded-md border border-border/60 px-3 py-1.5 text-xs text-text-secondary hover:bg-bg-tertiary"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={submitDraft}
              disabled={createMutation.isPending}
              data-testid="new-flow-submit"
              className="rounded-md bg-amber-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-amber-500 disabled:opacity-60"
            >
              {createMutation.isPending ? 'Creating…' : 'Create'}
            </button>
          </div>
        </div>
      </Modal>
    </div>
  );
}

interface FlowListProps {
  flows: AIPLogicFlow[];
  loading: boolean;
  error: unknown;
  activeFlowId: string | null;
  onSelect: (id: string) => void;
  onNew: () => void;
  onDelete: (id: string) => void;
}

function FlowList({
  flows,
  loading,
  error,
  activeFlowId,
  onSelect,
  onNew,
  onDelete,
}: FlowListProps) {
  return (
    <aside
      className="flex w-72 shrink-0 flex-col rounded-lg border border-border/50 bg-bg-secondary/60"
      aria-label="Logic Flow list"
      data-testid="logic-flow-list"
    >
      <div className="flex items-center justify-between border-b border-border/50 px-3 py-3">
        <div>
          <h2 className="text-sm font-semibold tracking-tight text-text-primary">
            AIP Logic Flows
          </h2>
          <p className="text-[11px] text-text-secondary">
            Visual editor for executable LLM / tool workflows.
          </p>
        </div>
        <button
          type="button"
          onClick={onNew}
          data-testid="new-flow-btn"
          className="rounded-md bg-amber-600 px-2.5 py-1 text-xs font-semibold text-white hover:bg-amber-500"
        >
          New
        </button>
      </div>
      <div className="flex-1 overflow-y-auto">
        {loading ? (
          <div className="flex items-center justify-center py-10">
            <LoadingSpinner />
          </div>
        ) : error ? (
          <div className="px-3 py-3 text-xs text-rose-300">
            {describeError(error)}
          </div>
        ) : flows.length === 0 ? (
          <EmptyState
            title="No flows yet"
            description="Create a new flow to start designing a workflow."
          />
        ) : (
          flows.map((flow) => (
            <button
              key={flow.id}
              type="button"
              data-testid="flow-list-item"
              data-flow-id={flow.id}
              onClick={() => onSelect(flow.id)}
              className={`flex w-full flex-col gap-1 border-b border-border/30 px-3 py-2.5 text-left text-xs transition hover:bg-bg-tertiary/60 ${
                flow.id === activeFlowId ? 'bg-bg-tertiary/80' : ''
              }`}
            >
              <span className="text-sm font-medium text-text-primary">
                {flow.name || flow.id}
              </span>
              <span className="text-[11px] text-text-muted">{flow.id}</span>
              <span className="flex items-center gap-2 text-[11px] text-text-secondary">
                <span>
                  {(flow.nodes ?? []).length} node
                  {(flow.nodes ?? []).length === 1 ? '' : 's'}
                </span>
                <span>
                  {(flow.edges ?? []).length} edge
                  {(flow.edges ?? []).length === 1 ? '' : 's'}
                </span>
              </span>
              <span className="flex justify-end">
                <span
                  role="button"
                  tabIndex={0}
                  onClick={(e) => {
                    e.stopPropagation();
                    onDelete(flow.id);
                  }}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault();
                      e.stopPropagation();
                      onDelete(flow.id);
                    }
                  }}
                  className="cursor-pointer text-[10px] text-text-muted underline-offset-2 hover:text-rose-300 hover:underline"
                  data-testid="flow-list-delete"
                >
                  delete
                </span>
              </span>
            </button>
          ))
        )}
      </div>
    </aside>
  );
}

interface FlowEditorProps {
  flow: AIPLogicFlow | null;
  loading: boolean;
}

function FlowEditor({ flow, loading }: FlowEditorProps) {
  const [nodes, setNodes] = useState<RFNode<RFNodeData>[]>([]);
  const [edges, setEdges] = useState<RFEdge[]>([]);
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [dirty, setDirty] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [executeError, setExecuteError] = useState<string | null>(null);
  const [lastRun, setLastRun] = useState<AIPLogicRun | null>(null);
  const [executePanelOpen, setExecutePanelOpen] = useState(false);
  const [executeInput, setExecuteInput] = useState('{}');

  const updateMutation = useUpdateAIPLogicFlow();
  const executeMutation = useExecuteAIPLogicFlow(flow?.id ?? '');
  const runsQuery = useAIPLogicRuns(flow?.id ?? null);

  // Hydrate state when the flow id changes.
  useEffect(() => {
    if (!flow) {
      setNodes([]);
      setEdges([]);
      setSelectedNodeId(null);
      setDirty(false);
      setSaveError(null);
      setExecuteError(null);
      setLastRun(null);
      return;
    }
    const rf = flowToReactFlow(flow);
    setNodes(rf.nodes);
    setEdges(rf.edges);
    setSelectedNodeId(null);
    setDirty(false);
    setSaveError(null);
    setExecuteError(null);
    setLastRun(null);
  }, [flow]);

  const onNodesChange = useCallback((changes: NodeChange[]) => {
    setNodes((curr) => {
      const next = applyNodeChanges(changes, curr) as RFNode<RFNodeData>[];
      return next;
    });
    if (changes.some((c) => c.type === 'position' || c.type === 'remove')) {
      setDirty(true);
    }
  }, []);

  const onEdgesChange = useCallback((changes: EdgeChange[]) => {
    setEdges((curr) => applyEdgeChanges(changes, curr));
    if (changes.some((c) => c.type === 'remove')) setDirty(true);
  }, []);

  const onConnect = useCallback((connection: Connection) => {
    setEdges((curr) => addEdge({ ...connection }, curr));
    setDirty(true);
  }, []);

  const handleAddNode = (type: LogicNodeType) => {
    setNodes((curr) => {
      const existing = new Set(curr.map((n) => n.id));
      const id = newNodeId(existing, type);
      const offset = curr.length * 24;
      const newNode: RFNode<RFNodeData> = {
        id,
        position: { x: 120 + offset, y: 120 + offset },
        data: {
          label: id,
          nodeType: type,
          config: defaultConfigFor(type),
        },
        type: 'default',
        style: nodeStyle(type),
      };
      return [...curr, newNode];
    });
    setDirty(true);
  };

  const handleDeleteNode = (nodeId: string) => {
    setNodes((curr) => curr.filter((n) => n.id !== nodeId));
    setEdges((curr) => curr.filter((e) => e.source !== nodeId && e.target !== nodeId));
    if (selectedNodeId === nodeId) setSelectedNodeId(null);
    setDirty(true);
  };

  const updateNode = (
    nodeId: string,
    fn: (n: RFNode<RFNodeData>) => RFNode<RFNodeData>,
  ) => {
    setNodes((curr) => curr.map((n) => (n.id === nodeId ? fn(n) : n)));
    setDirty(true);
  };

  const renameNode = (oldId: string, newId: string) => {
    if (!newId || oldId === newId) return;
    if (nodes.some((n) => n.id === newId)) return;
    setNodes((curr) =>
      curr.map((n) =>
        n.id === oldId ? { ...n, id: newId, data: { ...n.data, label: newId } } : n,
      ),
    );
    setEdges((curr) =>
      curr.map((e) => ({
        ...e,
        source: e.source === oldId ? newId : e.source,
        target: e.target === oldId ? newId : e.target,
      })),
    );
    setSelectedNodeId(newId);
    setDirty(true);
  };

  const onSave = () => {
    if (!flow) return;
    const wire = reactFlowToWire(nodes, edges);
    setSaveError(null);
    updateMutation.mutate(
      {
        flowId: flow.id,
        body: { nodes: wire.nodes, edges: wire.edges },
      },
      {
        onSuccess: () => setDirty(false),
        onError: (err) => setSaveError(describeError(err)),
      },
    );
  };

  const onExecute = () => {
    if (!flow) return;
    setExecuteError(null);
    let parsed: Record<string, unknown> = {};
    if (executeInput.trim() !== '') {
      try {
        const candidate = JSON.parse(executeInput);
        if (
          candidate &&
          typeof candidate === 'object' &&
          !Array.isArray(candidate)
        ) {
          parsed = candidate as Record<string, unknown>;
        } else {
          setExecuteError('Input must be a JSON object.');
          return;
        }
      } catch (e) {
        setExecuteError(`Invalid JSON: ${(e as Error).message}`);
        return;
      }
    }
    executeMutation.mutate(
      { input: parsed },
      {
        onSuccess: (run) => setLastRun(run),
        onError: (err) => {
          setExecuteError(describeError(err));
          // Errors from the executor are returned as 422 with run body — the
          // hook surfaces them as ApiRequestError; we still show the run
          // panel with the error from describeError.
        },
      },
    );
  };

  if (loading && !flow) {
    return (
      <section className="flex flex-1 items-center justify-center rounded-lg border border-border/50 bg-bg-secondary/60">
        <LoadingSpinner />
      </section>
    );
  }

  if (!flow) {
    return (
      <section className="flex flex-1 items-center justify-center rounded-lg border border-border/50 bg-bg-secondary/60">
        <EmptyState
          title="No flow selected"
          description="Pick a flow on the left, or create a new one to start editing."
        />
      </section>
    );
  }

  const selectedNode = nodes.find((n) => n.id === selectedNodeId) ?? null;

  return (
    <section
      className="flex flex-1 flex-col rounded-lg border border-border/50 bg-bg-secondary/60"
      data-testid="logic-flow-editor"
    >
      <header className="flex flex-wrap items-center justify-between gap-3 border-b border-border/50 px-3 py-2">
        <div>
          <h2 className="text-sm font-semibold tracking-tight text-text-primary">
            {flow.name || flow.id}
          </h2>
          <p className="text-[11px] text-text-muted">{flow.id}</p>
        </div>
        <div className="flex items-center gap-2">
          <div className="flex items-center gap-1 rounded-md border border-border/50 bg-bg-primary/40 px-1 py-0.5">
            {KNOWN_LOGIC_NODE_TYPES.map((t) => (
              <button
                key={t}
                type="button"
                onClick={() => handleAddNode(t)}
                data-testid={`add-node-${t}`}
                className="rounded px-2 py-1 text-[11px] text-text-secondary hover:bg-bg-tertiary hover:text-text-primary"
                style={{ borderLeft: `2px solid ${NODE_TYPE_META[t].color}` }}
                title={NODE_TYPE_META[t].description}
              >
                + {NODE_TYPE_META[t].label}
              </button>
            ))}
          </div>
          <button
            type="button"
            onClick={() => setExecutePanelOpen((o) => !o)}
            data-testid="toggle-execute-panel"
            className="rounded-md border border-border/60 px-3 py-1.5 text-xs text-text-secondary hover:bg-bg-tertiary"
          >
            {executePanelOpen ? 'Hide Run' : 'Run…'}
          </button>
          <button
            type="button"
            onClick={onSave}
            disabled={!dirty || updateMutation.isPending}
            data-testid="save-flow-btn"
            className="rounded-md bg-amber-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-amber-500 disabled:opacity-50"
          >
            {updateMutation.isPending ? 'Saving…' : dirty ? 'Save changes' : 'Saved'}
          </button>
        </div>
      </header>

      {saveError && (
        <div
          role="alert"
          data-testid="save-error"
          className="border-b border-rose-500/40 bg-rose-500/10 px-3 py-1.5 text-xs text-rose-300"
        >
          {saveError}
        </div>
      )}

      {executePanelOpen && (
        <div
          className="flex flex-col gap-2 border-b border-border/50 bg-bg-primary/40 px-3 py-2"
          data-testid="execute-panel"
        >
          <label className="flex flex-col gap-1 text-[11px] text-text-secondary">
            Execution input (JSON object)
            <textarea
              value={executeInput}
              onChange={(e) => setExecuteInput(e.target.value)}
              rows={3}
              data-testid="execute-input"
              className="rounded-md border border-border/50 bg-bg-primary px-2 py-1.5 font-mono text-[11px] text-text-primary outline-none focus:border-amber-500/60"
            />
          </label>
          {executeError && (
            <div
              role="alert"
              data-testid="execute-error"
              className="rounded-md border border-rose-500/40 bg-rose-500/10 px-2 py-1 text-[11px] text-rose-300"
            >
              {executeError}
            </div>
          )}
          <div className="flex justify-end">
            <button
              type="button"
              onClick={onExecute}
              disabled={executeMutation.isPending || dirty}
              data-testid="execute-flow-btn"
              className="rounded-md bg-teal-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-teal-500 disabled:opacity-50"
              title={
                dirty
                  ? 'Save your changes before running.'
                  : 'Run the flow with the JSON input above.'
              }
            >
              {executeMutation.isPending ? 'Running…' : 'Run'}
            </button>
          </div>
          {lastRun && <RunPanel run={lastRun} />}
          <RunsHistory runs={runsQuery.data?.runs ?? []} />
        </div>
      )}

      <div className="flex flex-1 min-h-0">
        <div className="relative flex-1" data-testid="logic-flow-canvas">
          <ReactFlow
            nodes={nodes}
            edges={edges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onConnect={onConnect}
            onNodeClick={(_e, node) => setSelectedNodeId(node.id)}
            fitView
            proOptions={{ hideAttribution: true }}
          >
            <Background gap={16} />
            <MiniMap pannable zoomable />
            <Controls position="bottom-right" />
          </ReactFlow>
        </div>
        <NodeConfigPanel
          node={selectedNode}
          onClose={() => setSelectedNodeId(null)}
          onChangeConfig={(cfg) => {
            if (!selectedNode) return;
            updateNode(selectedNode.id, (n) => ({
              ...n,
              data: { ...n.data, config: cfg },
            }));
          }}
          onChangeType={(t) => {
            if (!selectedNode) return;
            updateNode(selectedNode.id, (n) => ({
              ...n,
              data: {
                ...n.data,
                nodeType: t,
                config: defaultConfigFor(t),
              },
              style: nodeStyle(t),
            }));
          }}
          onRename={(newId) => {
            if (!selectedNode) return;
            renameNode(selectedNode.id, newId.trim());
          }}
          onDelete={() => {
            if (!selectedNode) return;
            handleDeleteNode(selectedNode.id);
          }}
        />
      </div>
    </section>
  );
}

interface NodeConfigPanelProps {
  node: RFNode<RFNodeData> | null;
  onClose: () => void;
  onChangeConfig: (cfg: Record<string, unknown>) => void;
  onChangeType: (t: LogicNodeType) => void;
  onRename: (id: string) => void;
  onDelete: () => void;
}

function NodeConfigPanel({
  node,
  onClose,
  onChangeConfig,
  onChangeType,
  onRename,
  onDelete,
}: NodeConfigPanelProps) {
  if (!node) {
    return (
      <aside
        data-testid="node-config-panel"
        className="hidden w-80 shrink-0 flex-col border-l border-border/50 bg-bg-secondary/40 p-4 text-xs text-text-muted lg:flex"
      >
        <p>Select a node on the canvas to edit its configuration.</p>
      </aside>
    );
  }

  const nodeType = String(node.data?.nodeType ?? 'llm');
  const meta = NODE_TYPE_META[nodeType as LogicNodeType] ?? null;
  const cfg = node.data?.config ?? {};

  return (
    <aside
      data-testid="node-config-panel"
      className="flex w-80 shrink-0 flex-col border-l border-border/50 bg-bg-secondary/40"
    >
      <header className="flex items-center justify-between border-b border-border/50 px-3 py-2">
        <div>
          <h3 className="text-sm font-semibold text-text-primary">
            Node config
          </h3>
          {meta && (
            <p
              className="text-[10px]"
              style={{ color: meta.color }}
            >
              {meta.label} — {meta.description}
            </p>
          )}
        </div>
        <button
          type="button"
          onClick={onClose}
          aria-label="Close node panel"
          data-testid="node-config-close"
          className="rounded-md px-2 py-0.5 text-text-muted hover:bg-bg-tertiary hover:text-text-primary"
        >
          ×
        </button>
      </header>

      <div className="flex-1 space-y-4 overflow-y-auto px-3 py-3">
        <label className="flex flex-col gap-1 text-[11px] text-text-secondary">
          Node ID
          <input
            type="text"
            defaultValue={node.id}
            onBlur={(e) => onRename(e.target.value)}
            data-testid="node-id-input"
            className="rounded-md border border-border/50 bg-bg-primary px-2 py-1.5 font-mono text-xs text-text-primary outline-none focus:border-amber-500/60"
          />
        </label>

        <label className="flex flex-col gap-1 text-[11px] text-text-secondary">
          Type
          <select
            value={nodeType}
            onChange={(e) => onChangeType(e.target.value as LogicNodeType)}
            data-testid="node-type-select"
            className="rounded-md border border-border/50 bg-bg-primary px-2 py-1.5 text-xs text-text-primary outline-none focus:border-amber-500/60"
          >
            {KNOWN_LOGIC_NODE_TYPES.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>
        </label>

        <NodeConfigFields
          nodeType={nodeType}
          config={cfg}
          onChange={onChangeConfig}
        />
      </div>

      <footer className="border-t border-border/50 px-3 py-2">
        <button
          type="button"
          onClick={onDelete}
          data-testid="node-config-delete"
          className="w-full rounded-md border border-rose-500/40 px-3 py-1.5 text-xs text-rose-300 hover:bg-rose-500/10"
        >
          Delete node
        </button>
      </footer>
    </aside>
  );
}

interface NodeConfigFieldsProps {
  nodeType: string;
  config: Record<string, unknown>;
  onChange: (cfg: Record<string, unknown>) => void;
}

function NodeConfigFields({
  nodeType,
  config,
  onChange,
}: NodeConfigFieldsProps) {
  const setField = (key: string, value: unknown) => {
    onChange({ ...config, [key]: value });
  };

  const stringField = (
    key: string,
    label: string,
    placeholder?: string,
    multiline = false,
  ) => {
    const value = typeof config[key] === 'string' ? (config[key] as string) : '';
    const testid = `node-cfg-${key}`;
    return (
      <label className="flex flex-col gap-1 text-[11px] text-text-secondary" key={key}>
        {label}
        {multiline ? (
          <textarea
            rows={3}
            value={value}
            onChange={(e) => setField(key, e.target.value)}
            placeholder={placeholder}
            data-testid={testid}
            className="rounded-md border border-border/50 bg-bg-primary px-2 py-1.5 font-mono text-[11px] text-text-primary outline-none focus:border-amber-500/60"
          />
        ) : (
          <input
            type="text"
            value={value}
            onChange={(e) => setField(key, e.target.value)}
            placeholder={placeholder}
            data-testid={testid}
            className="rounded-md border border-border/50 bg-bg-primary px-2 py-1.5 font-mono text-[11px] text-text-primary outline-none focus:border-amber-500/60"
          />
        )}
      </label>
    );
  };

  switch (nodeType) {
    case 'llm':
      return (
        <>
          {stringField('provider', 'Provider', 'mock | openai | anthropic')}
          {stringField('model', 'Model (optional)', 'gpt-4o-mini')}
          {stringField(
            'promptTemplate',
            'Prompt template',
            'Hello {{input.name}} — supports {{node.field}} substitution.',
            true,
          )}
          {stringField(
            'systemPrompt',
            'System prompt (optional)',
            'You are a helpful assistant.',
            true,
          )}
        </>
      );
    case 'tool':
      return (
        <>
          {stringField('tool', 'Tool name', 'echo | concat | …')}
          <ParamsEditor
            value={(config.params as Record<string, unknown>) ?? {}}
            onChange={(v) => setField('params', v)}
          />
        </>
      );
    case 'if':
      return (
        <>
          {stringField(
            'condition',
            'Condition',
            '{{input.score}} > 0.5',
          )}
          <p className="text-[10px] text-text-muted">
            Operators: == != &lt; &lt;= &gt; &gt;= contains. Outgoing edges
            are routed by their <code>branch</code> label (true / false).
          </p>
        </>
      );
    case 'output':
      return (
        <KeysEditor
          value={Array.isArray(config.keys) ? (config.keys as string[]) : []}
          onChange={(v) => setField('keys', v)}
        />
      );
    default:
      return (
        <p className="text-[11px] text-text-muted">
          No config fields for this node type.
        </p>
      );
  }
}

interface ParamsEditorProps {
  value: Record<string, unknown>;
  onChange: (next: Record<string, unknown>) => void;
}

function ParamsEditor({ value, onChange }: ParamsEditorProps) {
  const [text, setText] = useState(() => JSON.stringify(value ?? {}, null, 2));
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setText(JSON.stringify(value ?? {}, null, 2));
  }, [value]);

  return (
    <label className="flex flex-col gap-1 text-[11px] text-text-secondary">
      Tool params (JSON object)
      <textarea
        rows={4}
        value={text}
        onChange={(e) => {
          const next = e.target.value;
          setText(next);
          if (next.trim() === '') {
            setError(null);
            onChange({});
            return;
          }
          try {
            const parsed = JSON.parse(next);
            if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
              setError(null);
              onChange(parsed as Record<string, unknown>);
            } else {
              setError('Must be a JSON object.');
            }
          } catch (e) {
            setError((e as Error).message);
          }
        }}
        data-testid="node-cfg-params"
        className="rounded-md border border-border/50 bg-bg-primary px-2 py-1.5 font-mono text-[11px] text-text-primary outline-none focus:border-amber-500/60"
      />
      {error && (
        <span className="text-[10px] text-rose-300" role="alert">
          {error}
        </span>
      )}
    </label>
  );
}

interface KeysEditorProps {
  value: string[];
  onChange: (next: string[]) => void;
}

function KeysEditor({ value, onChange }: KeysEditorProps) {
  const text = value.join(', ');
  return (
    <label className="flex flex-col gap-1 text-[11px] text-text-secondary">
      Output keys (comma-separated)
      <input
        type="text"
        value={text}
        onChange={(e) => {
          const next = e.target.value
            .split(',')
            .map((s) => s.trim())
            .filter((s) => s !== '');
          onChange(next);
        }}
        placeholder="leave blank to return everything"
        data-testid="node-cfg-keys"
        className="rounded-md border border-border/50 bg-bg-primary px-2 py-1.5 font-mono text-[11px] text-text-primary outline-none focus:border-amber-500/60"
      />
    </label>
  );
}

function RunPanel({ run }: { run: AIPLogicRun }) {
  return (
    <div
      data-testid="run-panel"
      className="rounded-md border border-border/50 bg-bg-primary/60 p-2 text-[11px] text-text-secondary"
    >
      <div className="flex items-center justify-between">
        <span>
          run #{run.id} —{' '}
          <span
            className={
              run.status === 'success'
                ? 'text-teal-300'
                : 'text-rose-300'
            }
          >
            {run.status}
          </span>
        </span>
        <span className="text-text-muted">
          {(run.trace ?? []).length} step
          {(run.trace ?? []).length === 1 ? '' : 's'}
        </span>
      </div>
      {run.error && (
        <div
          role="alert"
          className="mt-1 rounded border border-rose-500/40 bg-rose-500/10 px-2 py-1 text-rose-300"
        >
          {run.error}
        </div>
      )}
      {run.output && Object.keys(run.output).length > 0 && (
        <pre
          data-testid="run-output"
          className="mt-1 max-h-40 overflow-y-auto rounded bg-bg-primary/80 p-2 font-mono text-[10px] text-text-primary"
        >
          {JSON.stringify(run.output, null, 2)}
        </pre>
      )}
      {(run.trace ?? []).length > 0 && (
        <ul className="mt-1 space-y-0.5">
          {run.trace?.map((t, i) => (
            <li
              key={`${t.nodeId}-${i}`}
              className={`flex items-center gap-2 rounded px-1 py-0.5 ${
                t.status === 'failed'
                  ? 'bg-rose-500/10 text-rose-300'
                  : t.status === 'skipped'
                    ? 'text-text-muted'
                    : 'text-text-secondary'
              }`}
            >
              <span className="font-mono">{t.nodeId}</span>
              <span className="text-text-muted">{t.type}</span>
              <span className="ml-auto">{t.status}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function RunsHistory({ runs }: { runs: AIPLogicRun[] }) {
  if (!runs || runs.length === 0) return null;
  return (
    <details
      className="rounded-md border border-border/50 bg-bg-primary/40 px-2 py-1 text-[11px] text-text-secondary"
      data-testid="runs-history"
    >
      <summary className="cursor-pointer text-text-secondary">
        Recent runs ({runs.length})
      </summary>
      <ul className="mt-1 space-y-0.5">
        {runs.map((r) => (
          <li
            key={r.id}
            className="flex items-center gap-2 text-[10px] text-text-muted"
          >
            <span className="font-mono">#{r.id}</span>
            <span
              className={
                r.status === 'success' ? 'text-teal-300' : 'text-rose-300'
              }
            >
              {r.status}
            </span>
            <span className="ml-auto">
              {new Date(r.createdAt).toLocaleTimeString()}
            </span>
          </li>
        ))}
      </ul>
    </details>
  );
}
