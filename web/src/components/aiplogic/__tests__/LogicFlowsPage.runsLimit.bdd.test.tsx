import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import * as logicApi from '../../../api/aipLogic';
import type { AIPLogicFlow, AIPLogicRun } from '../../../api/aipLogic';

// React Flow leans on browser APIs jsdom does not provide. We only exercise
// the runs-history limit control here, so stub the canvas with a minimal
// pass-through shell (mirrors LogicFlowsPage.test.tsx).
vi.mock('@xyflow/react', () => {
  return {
    ReactFlowProvider: ({ children }: { children: React.ReactNode }) => (
      <>{children}</>
    ),
    ReactFlow: ({
      nodes,
      children,
    }: {
      nodes: Array<{ id: string }>;
      children?: React.ReactNode;
    }) => (
      <div data-testid="rf-mock">
        <ul data-testid="rf-mock-nodes">
          {nodes.map((n) => (
            <li key={n.id} data-testid="rf-mock-node" data-node-id={n.id} />
          ))}
        </ul>
        {children}
      </div>
    ),
    Background: () => null,
    Controls: () => null,
    MiniMap: () => null,
    addEdge: <T,>(_conn: unknown, edges: T) => edges,
    applyNodeChanges: <T,>(_changes: unknown, nodes: T) => nodes,
    applyEdgeChanges: <T,>(_changes: unknown, edges: T) => edges,
  };
});

vi.mock('@xyflow/react/dist/style.css', () => ({}));

import { LogicFlowsPage } from '../LogicFlowsPage';

const flowA: AIPLogicFlow = {
  id: 'flow_alpha',
  name: 'Alpha workflow',
  nodes: [{ id: 'output', type: 'output', config: {} }],
  edges: [],
  createdBy: 'user-1',
  createdAt: '2026-04-28T10:00:00Z',
  updatedAt: '2026-04-28T10:00:00Z',
};

function makeRun(id: number): AIPLogicRun {
  return {
    id,
    flowId: flowA.id,
    status: 'success',
    createdAt: '2026-04-28T13:00:00Z',
  };
}

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

describe('LogicFlowsPage run-history limit control (BDD)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('Given the runs view, When no limit is chosen, Then listLogicRuns is called with the backend default (no explicit limit)', async () => {
    vi.spyOn(logicApi, 'listLogicFlows').mockResolvedValue({ flows: [flowA] });
    vi.spyOn(logicApi, 'getLogicFlow').mockResolvedValue(flowA);
    const runsSpy = vi
      .spyOn(logicApi, 'listLogicRuns')
      .mockResolvedValue({ runs: [makeRun(1)] });

    renderPage();
    await screen.findAllByTestId('rf-mock-node');

    fireEvent.click(screen.getByTestId('toggle-execute-panel'));

    await waitFor(() => expect(runsSpy).toHaveBeenCalled());
    // Default invocation must NOT pin an explicit limit — the backend default
    // (50) governs. listLogicRuns is either called with no params or with an
    // undefined limit.
    const params = runsSpy.mock.calls[0]![1];
    expect(params?.limit).toBeUndefined();
  });

  it('Given the runs view, When the limit control is set to 200, Then listLogicRuns is called with limit=200', async () => {
    vi.spyOn(logicApi, 'listLogicFlows').mockResolvedValue({ flows: [flowA] });
    vi.spyOn(logicApi, 'getLogicFlow').mockResolvedValue(flowA);
    const runsSpy = vi
      .spyOn(logicApi, 'listLogicRuns')
      .mockResolvedValue({ runs: [makeRun(1)] });

    renderPage();
    await screen.findAllByTestId('rf-mock-node');

    fireEvent.click(screen.getByTestId('toggle-execute-panel'));
    await waitFor(() => expect(runsSpy).toHaveBeenCalled());
    runsSpy.mockClear();

    const limitControl = (await screen.findByTestId(
      'runs-limit-input',
    )) as HTMLInputElement;
    fireEvent.change(limitControl, { target: { value: '200' } });

    await waitFor(() => {
      expect(runsSpy).toHaveBeenCalled();
      const params = runsSpy.mock.calls.at(-1)![1];
      expect(params?.limit).toBe(200);
    });
  });

  it('listLogicRuns builds a URL with ?limit=N when a limit is supplied', async () => {
    // Verify the api function itself appends ?limit=. We spy on the underlying
    // request transport via the exported function contract: calling with
    // { limit } must hit a URL that carries limit=42; calling without a limit
    // must omit it entirely so the backend default governs.
    const requestModule = await import('../../../api/client');
    const reqSpy = vi
      .spyOn(requestModule, 'request')
      .mockResolvedValue({ runs: [] });

    await logicApi.listLogicRuns('flow_alpha', { limit: 42 });
    expect(reqSpy).toHaveBeenCalled();
    const url = reqSpy.mock.calls[0]![1] as string;
    expect(url).toContain('/runs');
    expect(url).toMatch(/[?&]limit=42(\b|&|$)/);

    reqSpy.mockClear();
    await logicApi.listLogicRuns('flow_alpha');
    const urlNoLimit = reqSpy.mock.calls[0]![1] as string;
    expect(urlNoLimit).not.toContain('limit=');
  });
});
