import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import * as logicApi from '../../../api/aipLogic';
import type { AIPLogicFlow } from '../../../api/aipLogic';

// React Flow leans on browser APIs jsdom does not provide. This a11y test only
// inspects the page-level heading structure, so stub the canvas with a minimal
// pass-through shell (mirrors LogicFlowsPage.runsLimit.bdd.test.tsx).
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

describe('LogicFlowsPage page-level heading (a11y BDD)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('Given the Logic Flows route, When the page renders empty, Then it exposes exactly one level-1 heading named /logic flow/i', async () => {
    vi.spyOn(logicApi, 'listLogicFlows').mockResolvedValue({ flows: [] });

    renderPage();

    const h1s = await screen.findAllByRole('heading', { level: 1 });
    expect(h1s).toHaveLength(1);
    expect(h1s[0]).toHaveAccessibleName(/logic flow/i);

    // Sanity: the standard query-by-name resolves the same single heading.
    expect(
      screen.getByRole('heading', { level: 1, name: /logic flow/i }),
    ).toBe(h1s[0]);
  });

  it('Given a populated Logic Flows page, When flows load, Then there is still exactly one level-1 heading', async () => {
    vi.spyOn(logicApi, 'listLogicFlows').mockResolvedValue({ flows: [flowA] });
    vi.spyOn(logicApi, 'getLogicFlow').mockResolvedValue(flowA);

    renderPage();

    // Wait for the editor canvas to mount so any later-rendering subtree
    // (which only ever uses <h2>+) is included in the heading count.
    await screen.findAllByTestId('rf-mock-node');

    const h1s = within(document.body).queryAllByRole('heading', { level: 1 });
    expect(h1s).toHaveLength(1);
    expect(h1s[0]).toHaveAccessibleName(/logic flow/i);
  });
});
