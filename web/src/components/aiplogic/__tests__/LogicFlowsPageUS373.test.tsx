import { describe, it, expect, vi, beforeEach } from 'vitest';
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import * as logicApi from '../../../api/aipLogic';
import type { AIPLogicFlow } from '../../../api/aipLogic';

vi.mock('@xyflow/react', () => {
  return {
    ReactFlowProvider: ({ children }: { children: React.ReactNode }) => (
      <>{children}</>
    ),
    ReactFlow: ({
      nodes,
      onNodeClick,
      children,
    }: {
      nodes: Array<{ id: string; data?: { nodeType?: string; label?: string }; style?: React.CSSProperties }>;
      onNodeClick?: (e: unknown, node: { id: string }) => void;
      children?: React.ReactNode;
    }) => (
      <div data-testid="rf-mock">
        <ul data-testid="rf-mock-nodes">
          {nodes.map((n) => (
            <li
              key={n.id}
              data-testid="rf-mock-node"
              data-node-id={n.id}
              data-node-type={String(n.data?.nodeType ?? '')}
              data-node-border={String(n.style?.border ?? '')}
            >
              <button
                type="button"
                data-testid={`rf-mock-select-${n.id}`}
                onClick={(e) => onNodeClick?.(e, { id: n.id })}
              >
                {String(n.data?.label ?? n.id)} ({String(n.data?.nodeType ?? '')})
              </button>
            </li>
          ))}
        </ul>
        {children}
      </div>
    ),
    Background: () => null,
    Controls: () => null,
    MiniMap: () => null,
    addEdge: (
      conn: { source: string; target: string },
      edges: Array<{ id: string; source: string; target: string }>,
    ) => [
      ...edges,
      {
        id: `e_${edges.length}_${conn.source}_${conn.target}`,
        source: conn.source,
        target: conn.target,
      },
    ],
    applyNodeChanges: <T,>(_changes: unknown, nodes: T) => nodes,
    applyEdgeChanges: <T,>(_changes: unknown, edges: T) => edges,
  };
});

vi.mock('@xyflow/react/dist/style.css', () => ({}));

import { LogicFlowsPage, validateGraph } from '../LogicFlowsPage';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/logic-flows']}>
        <Routes>
          <Route path="/logic-flows" element={<LogicFlowsPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('LogicFlowsPage US-373 — validateGraph (unit)', () => {
  it('reports cycle issues for nodes that participate in a directed cycle', () => {
    const nodes = [
      { id: 'a', data: { nodeType: 'tool', config: {} } },
      { id: 'b', data: { nodeType: 'tool', config: {} } },
      { id: 'c', data: { nodeType: 'tool', config: {} } },
    ];
    const edges = [
      { source: 'a', target: 'b' },
      { source: 'b', target: 'c' },
      { source: 'c', target: 'a' },
    ];
    const report = validateGraph(nodes, edges);
    const cycleIds = report.issues
      .filter((i) => i.kind === 'cycle')
      .map((i) => i.nodeId)
      .sort();
    expect(cycleIds).toEqual(['a', 'b', 'c']);
  });

  it('does not flag a tree-shaped DAG as a cycle', () => {
    const nodes = [
      { id: 'a', data: { nodeType: 'tool', config: {} } },
      { id: 'b', data: { nodeType: 'output', config: {} } },
    ];
    const edges = [{ source: 'a', target: 'b' }];
    const report = validateGraph(nodes, edges);
    expect(report.issues.filter((i) => i.kind === 'cycle')).toHaveLength(0);
  });

  it('reports unconnected node issues for orphans in a multi-node graph', () => {
    const nodes = [
      { id: 'connected1', data: { nodeType: 'tool', config: {} } },
      { id: 'connected2', data: { nodeType: 'output', config: {} } },
      { id: 'orphan', data: { nodeType: 'tool', config: {} } },
    ];
    const edges = [{ source: 'connected1', target: 'connected2' }];
    const report = validateGraph(nodes, edges);
    const unconnectedIds = report.issues
      .filter((i) => i.kind === 'unconnected')
      .map((i) => i.nodeId);
    expect(unconnectedIds).toEqual(['orphan']);
  });

  it('does not flag a single-node flow as unconnected', () => {
    const nodes = [{ id: 'only', data: { nodeType: 'output', config: {} } }];
    const edges: { source: string; target: string }[] = [];
    const report = validateGraph(nodes, edges);
    expect(report.issues).toHaveLength(0);
  });

  it('flags unbound {{nodeId}} placeholders that do not match any node id', () => {
    const nodes = [
      {
        id: 'llm1',
        data: {
          nodeType: 'llm',
          config: {
            provider: 'mock',
            promptTemplate: 'Hi {{ghost.name}}',
          },
        },
      },
    ];
    const report = validateGraph(nodes, []);
    const unbound = report.issues.filter((i) => i.kind === 'unboundParam');
    expect(unbound).toHaveLength(1);
    expect(unbound[0].ref).toBe('ghost');
    expect(unbound[0].nodeId).toBe('llm1');
  });

  it('accepts {{input.x}} and {{iterate.body.item}} as bound roots', () => {
    const nodes = [
      {
        id: 'n',
        data: {
          nodeType: 'tool',
          config: {
            tool: 'echo',
            params: {
              a: '{{input.x}}',
              b: '{{iterate.body.item.foo}}',
            },
          },
        },
      },
    ];
    const report = validateGraph(nodes, []);
    expect(report.issues.filter((i) => i.kind === 'unboundParam')).toHaveLength(0);
  });

  it('accepts an upstream sibling node id as a bound root', () => {
    const nodes = [
      {
        id: 'fetch',
        data: { nodeType: 'tool', config: { tool: 'echo' } },
      },
      {
        id: 'render',
        data: {
          nodeType: 'llm',
          config: {
            provider: 'mock',
            promptTemplate: '{{fetch.value}}',
          },
        },
      },
    ];
    const report = validateGraph(nodes, [
      { source: 'fetch', target: 'render' },
    ]);
    expect(report.issues.filter((i) => i.kind === 'unboundParam')).toHaveLength(0);
  });
});

const flowWithIterate: AIPLogicFlow = {
  id: 'flow_iter',
  name: 'iter',
  nodes: [
    {
      id: 'output',
      type: 'output',
      config: { keys: [], __editorPosition: { x: 320, y: 100 } },
    },
  ],
  edges: [],
  fallbackModel: '',
  maxRetries: 0,
  createdBy: 'user:alice',
  createdAt: '2026-04-30T00:00:00Z',
  updatedAt: '2026-04-30T00:00:00Z',
};

describe('LogicFlowsPage US-373 — UI integration', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('exposes the iterate node type in the toolbar (5 known types)', async () => {
    vi.spyOn(logicApi, 'listLogicFlows').mockResolvedValue({ flows: [flowWithIterate] });
    vi.spyOn(logicApi, 'getLogicFlow').mockResolvedValue(flowWithIterate);
    vi.spyOn(logicApi, 'listLogicRuns').mockResolvedValue({ runs: [] });

    renderPage();

    expect(await screen.findByTestId('add-node-iterate')).toBeInTheDocument();
    // 5 known types per US-373: llm / tool / if / iterate / output
    expect(screen.getByTestId('add-node-llm')).toBeInTheDocument();
    expect(screen.getByTestId('add-node-tool')).toBeInTheDocument();
    expect(screen.getByTestId('add-node-if')).toBeInTheDocument();
    expect(screen.getByTestId('add-node-output')).toBeInTheDocument();
  });

  it('renders the iterate config panel when an iterate node is selected', async () => {
    vi.spyOn(logicApi, 'listLogicFlows').mockResolvedValue({ flows: [flowWithIterate] });
    vi.spyOn(logicApi, 'getLogicFlow').mockResolvedValue(flowWithIterate);
    vi.spyOn(logicApi, 'listLogicRuns').mockResolvedValue({ runs: [] });

    renderPage();
    await screen.findAllByTestId('rf-mock-node');

    fireEvent.click(screen.getByTestId('add-node-iterate'));
    await waitFor(() => {
      expect(
        screen.getAllByTestId('rf-mock-node').some(
          (n) => n.getAttribute('data-node-type') === 'iterate',
        ),
      ).toBe(true);
    });
    const iterateNode = screen
      .getAllByTestId('rf-mock-node')
      .find((n) => n.getAttribute('data-node-type') === 'iterate')!;
    const iterateId = iterateNode.getAttribute('data-node-id')!;
    fireEvent.click(screen.getByTestId(`rf-mock-select-${iterateId}`));

    const panel = await screen.findByTestId('node-config-panel');
    expect(within(panel).getByTestId('node-cfg-forEach')).toBeInTheDocument();
    expect(within(panel).getByTestId('node-cfg-body')).toBeInTheDocument();
  });

  it('shows the validation banner with no issues for a freshly seeded single-node flow', async () => {
    vi.spyOn(logicApi, 'listLogicFlows').mockResolvedValue({ flows: [flowWithIterate] });
    vi.spyOn(logicApi, 'getLogicFlow').mockResolvedValue(flowWithIterate);
    vi.spyOn(logicApi, 'listLogicRuns').mockResolvedValue({ runs: [] });

    renderPage();
    const banner = await screen.findByTestId('validation-banner');
    expect(banner.getAttribute('data-issue-count')).toBe('0');
  });

  it('flags an unconnected node added to a 2+ node flow', async () => {
    const flow: AIPLogicFlow = {
      ...flowWithIterate,
      nodes: [
        ...flowWithIterate.nodes,
        {
          id: 'orphan',
          type: 'tool',
          config: { tool: 'echo', __editorPosition: { x: 80, y: 200 } },
        },
      ],
    };
    vi.spyOn(logicApi, 'listLogicFlows').mockResolvedValue({ flows: [flow] });
    vi.spyOn(logicApi, 'getLogicFlow').mockResolvedValue(flow);
    vi.spyOn(logicApi, 'listLogicRuns').mockResolvedValue({ runs: [] });

    renderPage();
    const banner = await screen.findByTestId('validation-banner');
    await waitFor(() => {
      expect(Number(banner.getAttribute('data-issue-count'))).toBeGreaterThanOrEqual(
        2,
      ); // both nodes are unconnected (no edges in the seed)
    });
    const issues = within(banner).getAllByTestId('validation-issue');
    expect(issues.length).toBeGreaterThan(0);
    expect(
      issues.every((el) => el.getAttribute('data-issue-kind') === 'unconnected'),
    ).toBe(true);
  });

  it('flags an unbound placeholder reference', async () => {
    const flow: AIPLogicFlow = {
      ...flowWithIterate,
      nodes: [
        {
          id: 'lone',
          type: 'llm',
          config: {
            provider: 'mock',
            promptTemplate: 'Hi {{ghost.name}}',
            __editorPosition: { x: 80, y: 80 },
          },
        },
      ],
    };
    vi.spyOn(logicApi, 'listLogicFlows').mockResolvedValue({ flows: [flow] });
    vi.spyOn(logicApi, 'getLogicFlow').mockResolvedValue(flow);
    vi.spyOn(logicApi, 'listLogicRuns').mockResolvedValue({ runs: [] });

    renderPage();
    const banner = await screen.findByTestId('validation-banner');
    await waitFor(() => {
      const issues = within(banner).getAllByTestId('validation-issue');
      expect(issues.length).toBeGreaterThan(0);
      expect(
        issues.some((el) => el.getAttribute('data-issue-kind') === 'unboundParam'),
      ).toBe(true);
    });
  });

  it('runs node-level dry-run and renders the trace output', async () => {
    vi.spyOn(logicApi, 'listLogicFlows').mockResolvedValue({ flows: [flowWithIterate] });
    vi.spyOn(logicApi, 'getLogicFlow').mockResolvedValue(flowWithIterate);
    vi.spyOn(logicApi, 'listLogicRuns').mockResolvedValue({ runs: [] });
    const dryRunSpy = vi.spyOn(logicApi, 'dryRunLogicNode').mockResolvedValue({
      trace: {
        nodeId: 'output',
        type: 'output',
        status: 'success',
        output: { hello: 'world' },
        attempts: 1,
      },
    });

    renderPage();
    await screen.findAllByTestId('rf-mock-node');
    fireEvent.click(screen.getByTestId('rf-mock-select-output'));

    const panel = await screen.findByTestId('node-config-panel');
    fireEvent.click(within(panel).getByTestId('dry-run-btn'));

    await waitFor(() => expect(dryRunSpy).toHaveBeenCalled());
    const result = await within(panel).findByTestId('dry-run-result');
    expect(result.getAttribute('data-status')).toBe('success');
    expect(within(result).getByTestId('dry-run-output').textContent).toContain('hello');
  });

  it('surfaces dry-run state JSON parse errors before calling the API', async () => {
    vi.spyOn(logicApi, 'listLogicFlows').mockResolvedValue({ flows: [flowWithIterate] });
    vi.spyOn(logicApi, 'getLogicFlow').mockResolvedValue(flowWithIterate);
    vi.spyOn(logicApi, 'listLogicRuns').mockResolvedValue({ runs: [] });
    const dryRunSpy = vi.spyOn(logicApi, 'dryRunLogicNode').mockResolvedValue({
      trace: { nodeId: 'output', type: 'output', status: 'success' },
    });

    renderPage();
    await screen.findAllByTestId('rf-mock-node');
    fireEvent.click(screen.getByTestId('rf-mock-select-output'));

    const panel = await screen.findByTestId('node-config-panel');
    const stateBox = within(panel).getByTestId('dry-run-state') as HTMLTextAreaElement;
    fireEvent.change(stateBox, { target: { value: '{not json' } });
    fireEvent.click(within(panel).getByTestId('dry-run-btn'));

    expect(await within(panel).findByTestId('dry-run-error')).toHaveTextContent(/invalid json/i);
    expect(dryRunSpy).not.toHaveBeenCalled();
  });

  it('saves the flow with fallbackModel and maxRetries', async () => {
    vi.spyOn(logicApi, 'listLogicFlows').mockResolvedValue({ flows: [flowWithIterate] });
    vi.spyOn(logicApi, 'getLogicFlow').mockResolvedValue(flowWithIterate);
    vi.spyOn(logicApi, 'listLogicRuns').mockResolvedValue({ runs: [] });
    const updateSpy = vi
      .spyOn(logicApi, 'updateLogicFlow')
      .mockImplementation((_id, body) =>
        Promise.resolve({ ...flowWithIterate, ...body }),
      );

    renderPage();
    await screen.findByTestId('flow-settings');
    const fallback = screen.getByTestId('flow-fallback-model') as HTMLInputElement;
    fireEvent.change(fallback, { target: { value: 'gpt-fallback' } });
    const retries = screen.getByTestId('flow-max-retries') as HTMLInputElement;
    fireEvent.change(retries, { target: { value: '3' } });

    fireEvent.click(screen.getByTestId('save-flow-btn'));
    await waitFor(() => expect(updateSpy).toHaveBeenCalled());
    const body = updateSpy.mock.calls[0]![1];
    expect(body.fallbackModel).toBe('gpt-fallback');
    expect(body.maxRetries).toBe(3);
  });

  it('clamps maxRetries to [0, 8] on the input control', async () => {
    vi.spyOn(logicApi, 'listLogicFlows').mockResolvedValue({ flows: [flowWithIterate] });
    vi.spyOn(logicApi, 'getLogicFlow').mockResolvedValue(flowWithIterate);
    vi.spyOn(logicApi, 'listLogicRuns').mockResolvedValue({ runs: [] });
    const updateSpy = vi
      .spyOn(logicApi, 'updateLogicFlow')
      .mockImplementation((_id, body) =>
        Promise.resolve({ ...flowWithIterate, ...body }),
      );

    renderPage();
    await screen.findByTestId('flow-settings');
    const retries = screen.getByTestId('flow-max-retries') as HTMLInputElement;
    fireEvent.change(retries, { target: { value: '20' } });

    fireEvent.click(screen.getByTestId('save-flow-btn'));
    await waitFor(() => expect(updateSpy).toHaveBeenCalled());
    expect(updateSpy.mock.calls[0]![1].maxRetries).toBe(8);
  });
});
