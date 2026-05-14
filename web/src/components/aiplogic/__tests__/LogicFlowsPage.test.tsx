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
import type { AIPLogicFlow, AIPLogicRun } from '../../../api/aipLogic';

// React Flow leans on browser APIs (ResizeObserver, getBoundingClientRect with
// non-zero size, …) that jsdom does not provide. The component test only
// needs to verify our own glue (toolbar, node-config panel, save / execute
// wiring) — the canvas itself is well-covered by xyflow's own suite. Mock
// the package with a minimal pass-through shell that exposes the props we
// rely on (nodes / onNodesChange / onConnect / onNodeClick) and forwards
// the toolbar children.
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
      nodes: Array<{ id: string; data?: { nodeType?: string; label?: string } }>;
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

// React Flow's stylesheet import path is bundler-specific; alias to nothing.
vi.mock('@xyflow/react/dist/style.css', () => ({}));

// Import after mocks so the page picks up the stubbed module.
import { LogicFlowsPage } from '../LogicFlowsPage';

const flowA: AIPLogicFlow = {
  id: 'flow_alpha',
  name: 'Alpha workflow',
  description: 'Greets a user via mock LLM',
  nodes: [
    {
      id: 'greet',
      type: 'llm',
      config: {
        provider: 'mock',
        model: '',
        promptTemplate: 'Hi {{input.name}}',
        __editorPosition: { x: 100, y: 100 },
      },
    },
    {
      id: 'output',
      type: 'output',
      config: { keys: [], __editorPosition: { x: 320, y: 100 } },
    },
  ],
  edges: [{ from: 'greet', to: 'output' }],
  createdBy: 'user-1',
  createdAt: '2026-04-28T10:00:00Z',
  updatedAt: '2026-04-28T10:00:00Z',
};

const flowB: AIPLogicFlow = {
  id: 'flow_beta',
  name: 'Beta',
  nodes: [
    {
      id: 'echo',
      type: 'tool',
      config: { tool: 'echo', params: { value: 'hi' } },
    },
  ],
  edges: [],
  createdBy: 'user-1',
  createdAt: '2026-04-28T11:00:00Z',
  updatedAt: '2026-04-28T11:00:00Z',
};

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

describe('LogicFlowsPage (US-282)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('shows the empty state when no flows exist', async () => {
    vi.spyOn(logicApi, 'listLogicFlows').mockResolvedValue({ flows: [] });
    renderPage();
    expect(await screen.findByText(/no flows yet/i)).toBeInTheDocument();
    expect(screen.getByText(/no flow selected/i)).toBeInTheDocument();
  });

  it('renders a "New flow" CTA inside the empty state (dogfood #6)', async () => {
    vi.spyOn(logicApi, 'listLogicFlows').mockResolvedValue({ flows: [] });
    renderPage();
    const emptyBlock = await screen.findByTestId('logic-flow-list-empty');
    const cta = within(emptyBlock).getByRole('button', { name: /new flow/i });
    expect(cta).toBeInTheDocument();
  });

  it('lists flows, auto-selects the first, and renders nodes on the canvas', async () => {
    vi.spyOn(logicApi, 'listLogicFlows').mockResolvedValue({
      flows: [flowA, flowB],
    });
    vi.spyOn(logicApi, 'getLogicFlow').mockImplementation((id: string) =>
      Promise.resolve(id === flowA.id ? flowA : flowB),
    );
    vi.spyOn(logicApi, 'listLogicRuns').mockResolvedValue({ runs: [] });

    renderPage();

    const items = await screen.findAllByTestId('flow-list-item');
    expect(items).toHaveLength(2);
    // Wait for the auto-loaded detail to populate the canvas.
    const canvasNodes = await screen.findAllByTestId('rf-mock-node');
    expect(canvasNodes.map((n) => n.getAttribute('data-node-id'))).toEqual([
      'greet',
      'output',
    ]);
  });

  it('switches the editor when another flow is clicked', async () => {
    vi.spyOn(logicApi, 'listLogicFlows').mockResolvedValue({
      flows: [flowA, flowB],
    });
    vi.spyOn(logicApi, 'getLogicFlow').mockImplementation((id: string) =>
      Promise.resolve(id === flowA.id ? flowA : flowB),
    );
    vi.spyOn(logicApi, 'listLogicRuns').mockResolvedValue({ runs: [] });

    renderPage();
    await screen.findAllByTestId('flow-list-item');
    await screen.findAllByTestId('rf-mock-node');

    const items = screen.getAllByTestId('flow-list-item');
    fireEvent.click(items[1]);

    await waitFor(() => {
      const ids = screen
        .getAllByTestId('rf-mock-node')
        .map((n) => n.getAttribute('data-node-id'));
      expect(ids).toEqual(['echo']);
    });
  });

  it('opens the node config panel and edits prompt template (LLM)', async () => {
    vi.spyOn(logicApi, 'listLogicFlows').mockResolvedValue({ flows: [flowA] });
    vi.spyOn(logicApi, 'getLogicFlow').mockResolvedValue(flowA);
    vi.spyOn(logicApi, 'listLogicRuns').mockResolvedValue({ runs: [] });
    const updateSpy = vi
      .spyOn(logicApi, 'updateLogicFlow')
      .mockImplementation((_id, body) =>
        Promise.resolve({ ...flowA, ...body, updatedAt: '2026-04-28T12:00:00Z' }),
      );

    renderPage();

    await screen.findAllByTestId('rf-mock-node');
    fireEvent.click(screen.getByTestId('rf-mock-select-greet'));

    const panel = await screen.findByTestId('node-config-panel');
    const promptInput = within(panel).getByTestId(
      'node-cfg-promptTemplate',
    ) as HTMLTextAreaElement;
    fireEvent.change(promptInput, {
      target: { value: 'Hello {{input.name}} from the editor.' },
    });

    fireEvent.click(screen.getByTestId('save-flow-btn'));

    await waitFor(() => {
      expect(updateSpy).toHaveBeenCalled();
    });
    const body = updateSpy.mock.calls[0]![1];
    expect(body.nodes).toBeDefined();
    const greetNode = body.nodes!.find((n) => n.id === 'greet');
    expect(greetNode?.config?.promptTemplate).toBe(
      'Hello {{input.name}} from the editor.',
    );
  });

  it('adds a new node from the toolbar and persists it on save', async () => {
    vi.spyOn(logicApi, 'listLogicFlows').mockResolvedValue({ flows: [flowA] });
    vi.spyOn(logicApi, 'getLogicFlow').mockResolvedValue(flowA);
    vi.spyOn(logicApi, 'listLogicRuns').mockResolvedValue({ runs: [] });
    const updateSpy = vi
      .spyOn(logicApi, 'updateLogicFlow')
      .mockImplementation((_id, body) =>
        Promise.resolve({ ...flowA, ...body, updatedAt: '2026-04-28T13:00:00Z' }),
      );

    renderPage();

    await screen.findAllByTestId('rf-mock-node');

    fireEvent.click(screen.getByTestId('add-node-tool'));
    await waitFor(() => {
      const ids = screen
        .getAllByTestId('rf-mock-node')
        .map((n) => n.getAttribute('data-node-id'));
      expect(ids).toContain('greet');
      expect(ids).toContain('output');
      expect(ids.some((id) => id?.startsWith('tool_'))).toBe(true);
    });

    fireEvent.click(screen.getByTestId('save-flow-btn'));

    await waitFor(() => {
      expect(updateSpy).toHaveBeenCalled();
    });
    const body = updateSpy.mock.calls[0]![1];
    const types = body.nodes!.map((n) => n.type);
    expect(types).toContain('tool');
  });

  it('runs the flow with JSON input and renders the trace', async () => {
    vi.spyOn(logicApi, 'listLogicFlows').mockResolvedValue({ flows: [flowA] });
    vi.spyOn(logicApi, 'getLogicFlow').mockResolvedValue(flowA);
    vi.spyOn(logicApi, 'listLogicRuns').mockResolvedValue({ runs: [] });

    const run: AIPLogicRun = {
      id: 42,
      flowId: flowA.id,
      status: 'success',
      input: { name: 'Ada' },
      output: { greet: { content: 'Hi Ada' } },
      trace: [
        { nodeId: 'greet', type: 'llm', status: 'success' },
        { nodeId: 'output', type: 'output', status: 'success' },
      ],
      createdBy: 'user-1',
      createdAt: '2026-04-28T13:00:00Z',
    };
    const execSpy = vi
      .spyOn(logicApi, 'executeLogicFlow')
      .mockResolvedValue(run);

    renderPage();
    await screen.findAllByTestId('rf-mock-node');

    fireEvent.click(screen.getByTestId('toggle-execute-panel'));
    const inputBox = screen.getByTestId('execute-input') as HTMLTextAreaElement;
    fireEvent.change(inputBox, { target: { value: '{"name":"Ada"}' } });
    fireEvent.click(screen.getByTestId('execute-flow-btn'));

    await waitFor(() => expect(execSpy).toHaveBeenCalled());
    expect(execSpy.mock.calls[0]![1].input).toEqual({ name: 'Ada' });

    const runPanel = await screen.findByTestId('run-panel');
    expect(runPanel.textContent).toContain('run #42');
    expect(runPanel.textContent).toContain('success');
    expect(within(runPanel).getByTestId('run-output').textContent).toContain(
      'Hi Ada',
    );
  });

  it('rejects malformed execution input before calling the API', async () => {
    vi.spyOn(logicApi, 'listLogicFlows').mockResolvedValue({ flows: [flowA] });
    vi.spyOn(logicApi, 'getLogicFlow').mockResolvedValue(flowA);
    vi.spyOn(logicApi, 'listLogicRuns').mockResolvedValue({ runs: [] });
    const execSpy = vi.spyOn(logicApi, 'executeLogicFlow').mockResolvedValue({
      id: 1,
      flowId: flowA.id,
      status: 'success',
      createdAt: 'x',
    } as AIPLogicRun);

    renderPage();
    await screen.findAllByTestId('rf-mock-node');

    fireEvent.click(screen.getByTestId('toggle-execute-panel'));
    const inputBox = screen.getByTestId('execute-input') as HTMLTextAreaElement;
    fireEvent.change(inputBox, { target: { value: '{not json' } });
    fireEvent.click(screen.getByTestId('execute-flow-btn'));

    expect(await screen.findByTestId('execute-error')).toHaveTextContent(
      /invalid json/i,
    );
    expect(execSpy).not.toHaveBeenCalled();
  });

  it('creates a new flow via the modal and selects it', async () => {
    const flows: AIPLogicFlow[] = [];
    vi.spyOn(logicApi, 'listLogicFlows').mockImplementation(() =>
      Promise.resolve({ flows: [...flows] }),
    );
    const newFlow: AIPLogicFlow = {
      id: 'flow_new',
      name: 'Fresh',
      nodes: [{ id: 'output', type: 'output', config: {} }],
      edges: [],
      createdBy: 'user-1',
      createdAt: '2026-04-28T14:00:00Z',
      updatedAt: '2026-04-28T14:00:00Z',
    };
    vi.spyOn(logicApi, 'getLogicFlow').mockResolvedValue(newFlow);
    vi.spyOn(logicApi, 'listLogicRuns').mockResolvedValue({ runs: [] });
    const createSpy = vi
      .spyOn(logicApi, 'createLogicFlow')
      .mockImplementation(async (body) => {
        const created = {
          ...newFlow,
          name: body.name ?? 'Fresh',
        };
        flows.push(created);
        return created;
      });

    renderPage();
    expect(await screen.findByText(/no flows yet/i)).toBeInTheDocument();

    fireEvent.click(screen.getByTestId('new-flow-btn'));
    fireEvent.change(screen.getByTestId('new-flow-name'), {
      target: { value: 'Fresh' },
    });
    fireEvent.click(screen.getByTestId('new-flow-submit'));

    await waitFor(() => expect(createSpy).toHaveBeenCalled());
    expect(createSpy.mock.calls[0]![0].nodes.length).toBeGreaterThan(0);

    // After creation, the editor flips from empty state to the canvas.
    await waitFor(() =>
      expect(screen.queryByText(/no flow selected/i)).not.toBeInTheDocument(),
    );
  });

  it('rejects an empty new-flow name', async () => {
    vi.spyOn(logicApi, 'listLogicFlows').mockResolvedValue({ flows: [] });

    renderPage();
    await screen.findByText(/no flows yet/i);
    fireEvent.click(screen.getByTestId('new-flow-btn'));
    fireEvent.click(screen.getByTestId('new-flow-submit'));

    await screen.findByText(/name is required/i);
  });
});
