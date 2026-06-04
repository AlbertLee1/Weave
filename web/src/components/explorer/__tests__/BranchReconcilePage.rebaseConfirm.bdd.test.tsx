import {
  describe,
  it,
  expect,
  beforeAll,
  afterAll,
  afterEach,
  beforeEach,
  vi,
} from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
// Side-effect import: bootstraps the i18next singleton so `t()` resolves the
// {{version}} interpolation rather than echoing the raw key.
import '../../../i18n';
import { BranchReconcilePage } from '../BranchReconcilePage';
import { useBranchStore } from '../../../stores/branchStore';
import type { BranchDiffPostResponse } from '../../../api/types';

// UX consistency — branch rebase confirm.
//
// Rebase rewrites the branch's history, so it is a destructive operation that
// must be confirmed. The page previously gated the rebase behind a native
// `window.confirm`, which does not honour the dark theme and clashes visually
// with the rest of the app's styled Modal dialogs (the codebase has
// standardised on `common/Modal` — see AutomationRulesPage's note about
// deliberately avoiding the unstylable window.confirm). This contract pins the
// styled two-step confirm flow: click Rebase → styled Modal explaining the
// consequence → Cancel aborts (no /rebase call), confirm Rebase fires the
// real mutation. The native window.confirm must never be invoked.
const server = setupServer();
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => {
  server.resetHandlers();
  vi.restoreAllMocks();
});
afterAll(() => server.close());

beforeEach(() => {
  useBranchStore.setState({ selections: {} });
});

const branch = {
  id: 'br-feature-x',
  ontologyRid: 'ri.ontology.foundry',
  name: 'feature-x',
  baseVersion: 1,
  status: 'open' as const,
  createdBy: 'tester',
  createdAt: '2026-05-01T00:00:00Z',
  updatedAt: '2026-05-01T00:00:00Z',
};

function diffResponse(): BranchDiffPostResponse {
  return {
    branch,
    added: [],
    modified: [],
    deleted: [],
    conflicts: [],
    hasConflicts: false,
  };
}

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter
        initialEntries={['/explorer/foundry/branches/br-feature-x/reconcile']}
      >
        <Routes>
          <Route
            path="/explorer/:ontology/branches/:branch/reconcile"
            element={<BranchReconcilePage />}
          />
          <Route
            path="/explorer/:ontology"
            element={<div data-testid="explorer-shell" />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BranchReconcilePage styled rebase-confirm Modal (UX consistency)', () => {
  it('Given the reconcile page, When Rebase is clicked, Then window.confirm is NOT called and a styled Modal appears (no rebase fired yet)', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm');
    let rebaseCalled = false;
    server.use(
      http.post(
        '/api/v2/ontologies/foundry/branches/br-feature-x/diff',
        () => HttpResponse.json(diffResponse()),
      ),
      http.post(
        '/api/v2/ontologies/foundry/branches/br-feature-x/rebase',
        () => {
          rebaseCalled = true;
          return HttpResponse.json({ ...branch, baseVersion: 7 });
        },
      ),
    );

    renderPage();
    const user = userEvent.setup();

    const rebaseBtn = await screen.findByTestId(
      'branch-reconcile-rebase-button',
    );
    await user.click(rebaseBtn);

    // No native, unstylable confirm.
    expect(confirmSpy).not.toHaveBeenCalled();

    // A styled shared-Modal dialog appears.
    const dialog = await screen.findByRole('dialog');
    expect(dialog).toBeInTheDocument();

    // Nothing rebased just by opening the confirm.
    expect(rebaseCalled).toBe(false);
  });

  it('Given the confirm Modal is open, When Cancel is clicked, Then the rebase is NOT fired and the Modal closes', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm');
    let rebaseCalled = false;
    server.use(
      http.post(
        '/api/v2/ontologies/foundry/branches/br-feature-x/diff',
        () => HttpResponse.json(diffResponse()),
      ),
      http.post(
        '/api/v2/ontologies/foundry/branches/br-feature-x/rebase',
        () => {
          rebaseCalled = true;
          return HttpResponse.json({ ...branch, baseVersion: 7 });
        },
      ),
    );

    renderPage();
    const user = userEvent.setup();

    const rebaseBtn = await screen.findByTestId(
      'branch-reconcile-rebase-button',
    );
    await user.click(rebaseBtn);

    const dialog = await screen.findByRole('dialog');
    await user.click(within(dialog).getByRole('button', { name: /cancel/i }));

    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument(),
    );
    expect(confirmSpy).not.toHaveBeenCalled();
    expect(rebaseCalled).toBe(false);
    expect(screen.queryByTestId('reconcile-rebase-success')).toBeNull();
  });

  it('Given the confirm Modal is open, When the destructive Rebase is confirmed, Then the /rebase endpoint fires and the new base version is shown', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm');
    let rebaseCalled = false;
    server.use(
      http.post(
        '/api/v2/ontologies/foundry/branches/br-feature-x/diff',
        () => HttpResponse.json(diffResponse()),
      ),
      http.post(
        '/api/v2/ontologies/foundry/branches/br-feature-x/rebase',
        () => {
          rebaseCalled = true;
          return HttpResponse.json({ ...branch, baseVersion: 7 });
        },
      ),
    );

    renderPage();
    const user = userEvent.setup();

    const rebaseBtn = await screen.findByTestId(
      'branch-reconcile-rebase-button',
    );
    await user.click(rebaseBtn);

    const dialog = await screen.findByRole('dialog');
    // The destructive confirm button inside the Modal (distinct from Cancel).
    await user.click(
      within(dialog).getByTestId('branch-reconcile-rebase-confirm-btn'),
    );

    await waitFor(() => {
      expect(rebaseCalled).toBe(true);
      expect(
        screen.getByTestId('reconcile-rebase-success'),
      ).toBeInTheDocument();
    });
    expect(screen.getByTestId('reconcile-rebase-success')).toHaveTextContent(
      '7',
    );
    expect(confirmSpy).not.toHaveBeenCalled();

    // Modal closed after a successful rebase.
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });
});
