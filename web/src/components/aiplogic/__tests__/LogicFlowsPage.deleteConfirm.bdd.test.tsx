import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import * as logicApi from '../../../api/aipLogic';
import type { AIPLogicFlow } from '../../../api/aipLogic';

// React Flow leans on browser APIs jsdom does not provide. The delete-confirm
// flow lives entirely in the left-hand FlowList + a shared Modal, so we stub
// the canvas with a minimal pass-through shell (mirrors the other BDD tests).
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

const flowB: AIPLogicFlow = {
  id: 'flow_beta',
  name: 'Beta workflow',
  nodes: [{ id: 'output', type: 'output', config: {} }],
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

function deleteTriggerFor(flowId: string): HTMLElement {
  const item = screen
    .getAllByTestId('flow-list-item')
    .find((el) => el.getAttribute('data-flow-id') === flowId);
  if (!item) throw new Error(`flow list item ${flowId} not found`);
  return within(item).getByTestId('flow-list-delete');
}

describe('LogicFlowsPage delete confirmation (BDD)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(logicApi, 'listLogicFlows').mockResolvedValue({
      flows: [flowA, flowB],
    });
    vi.spyOn(logicApi, 'getLogicFlow').mockImplementation((id: string) =>
      Promise.resolve(id === flowB.id ? flowB : flowA),
    );
    vi.spyOn(logicApi, 'listLogicRuns').mockResolvedValue({ runs: [] });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('Given flows are listed, When Delete is clicked, Then window.confirm is NOT used and a styled Modal confirm dialog appears', async () => {
    const user = userEvent.setup();
    const confirmSpy = vi.spyOn(window, 'confirm');
    const deleteSpy = vi
      .spyOn(logicApi, 'deleteLogicFlow')
      .mockResolvedValue(undefined);

    renderPage();
    await screen.findByText('Beta workflow');

    await user.click(deleteTriggerFor(flowB.id));

    // The native blocking confirm must never be used.
    expect(confirmSpy).not.toHaveBeenCalled();

    // A styled shared Modal dialog appears with the destructive copy and both
    // a Cancel and a Delete affordance.
    const dialog = await screen.findByRole('dialog');
    expect(dialog).toHaveTextContent('This cannot be undone');
    expect(
      within(dialog).getByRole('button', { name: /cancel/i }),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByRole('button', { name: /^delete$/i }),
    ).toBeInTheDocument();

    // Opening the dialog must not have fired the deletion.
    expect(deleteSpy).not.toHaveBeenCalled();
  });

  it('Given the confirm dialog is open, When Cancel is clicked, Then the flow is not deleted and the dialog closes', async () => {
    const user = userEvent.setup();
    const confirmSpy = vi.spyOn(window, 'confirm');
    const deleteSpy = vi
      .spyOn(logicApi, 'deleteLogicFlow')
      .mockResolvedValue(undefined);

    renderPage();
    await screen.findByText('Beta workflow');

    await user.click(deleteTriggerFor(flowB.id));
    const dialog = await screen.findByRole('dialog');

    await user.click(within(dialog).getByRole('button', { name: /cancel/i }));

    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument(),
    );
    expect(deleteSpy).not.toHaveBeenCalled();
    expect(confirmSpy).not.toHaveBeenCalled();
    // The flow is still present.
    expect(screen.getByText('Beta workflow')).toBeInTheDocument();
  });

  it('Given the confirm dialog is open, When Delete is confirmed, Then deleteLogicFlow is called and the flow disappears from the list', async () => {
    const user = userEvent.setup();
    const confirmSpy = vi.spyOn(window, 'confirm');
    const deleteSpy = vi
      .spyOn(logicApi, 'deleteLogicFlow')
      .mockResolvedValue(undefined);

    renderPage();
    await screen.findByText('Beta workflow');

    await user.click(deleteTriggerFor(flowB.id));
    const dialog = await screen.findByRole('dialog');

    // After the delete resolves, the list query refetches without the removed
    // flow — model the server-side removal.
    (logicApi.listLogicFlows as ReturnType<typeof vi.fn>).mockResolvedValue({
      flows: [flowA],
    });

    await user.click(within(dialog).getByRole('button', { name: /^delete$/i }));

    await waitFor(() => expect(deleteSpy).toHaveBeenCalledWith(flowB.id));
    // window.confirm is never used anywhere in this flow.
    expect(confirmSpy).not.toHaveBeenCalled();

    // The dialog closes and the deleted flow is gone from the list.
    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument(),
    );
    await waitFor(() =>
      expect(screen.queryByText('Beta workflow')).not.toBeInTheDocument(),
    );
    // Alpha survives — its name may render in both the list item and the
    // editor header now that it is the sole (and active) flow.
    expect(screen.getAllByText('Alpha workflow').length).toBeGreaterThan(0);
  });
});
