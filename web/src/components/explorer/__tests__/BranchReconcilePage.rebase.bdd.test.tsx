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
import { render, screen, waitFor } from '@testing-library/react';
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
import type {
  AnnotatedMergeConflict,
  BranchDiffPostResponse,
} from '../../../api/types';

// US-383 Rebase button on the reconcile surface. Given an open branch behind
// main, When the operator confirms a rebase, Then the /rebase endpoint is hit
// and the rebased base-version is surfaced; a REBASE_CONFLICT 409 surfaces an
// error banner instead.
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

const conflictEntry: AnnotatedMergeConflict = {
  entityType: 'actionType',
  entityRid: 'ri.action.x.conflict',
  apiName: 'updateOrder',
  resolutionKey: 'actionType:updateOrder',
  changeType: 'MODIFIED',
  branchState: { apiName: 'updateOrder', displayName: 'Branch label' },
  mainState: { apiName: 'updateOrder', displayName: 'Main label' },
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

describe('BranchReconcilePage rebase (US-383)', () => {
  it('rebases the branch onto main and shows the new base version', async () => {
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
    vi.spyOn(window, 'confirm').mockReturnValue(true);

    renderPage();

    const rebaseBtn = await screen.findByTestId('branch-reconcile-rebase-button');
    const user = userEvent.setup();
    await user.click(rebaseBtn);

    await waitFor(() => {
      expect(rebaseCalled).toBe(true);
      expect(screen.getByTestId('reconcile-rebase-success')).toBeInTheDocument();
    });
    expect(screen.getByTestId('reconcile-rebase-success')).toHaveTextContent('7');
  });

  it('does not call the endpoint when the confirm is dismissed', async () => {
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
    vi.spyOn(window, 'confirm').mockReturnValue(false);

    renderPage();

    const rebaseBtn = await screen.findByTestId('branch-reconcile-rebase-button');
    const user = userEvent.setup();
    await user.click(rebaseBtn);

    // Give any (non-)request a chance to fire.
    await new Promise((r) => setTimeout(r, 30));
    expect(rebaseCalled).toBe(false);
    expect(screen.queryByTestId('reconcile-rebase-success')).toBeNull();
  });

  it('surfaces an error banner on a REBASE_CONFLICT 409', async () => {
    server.use(
      http.post(
        '/api/v2/ontologies/foundry/branches/br-feature-x/diff',
        () => HttpResponse.json(diffResponse()),
      ),
      http.post(
        '/api/v2/ontologies/foundry/branches/br-feature-x/rebase',
        () =>
          HttpResponse.json(
            { errorCode: 'REBASE_CONFLICT', conflicts: [conflictEntry] },
            { status: 409 },
          ),
      ),
    );
    vi.spyOn(window, 'confirm').mockReturnValue(true);

    renderPage();

    const rebaseBtn = await screen.findByTestId('branch-reconcile-rebase-button');
    const user = userEvent.setup();
    await user.click(rebaseBtn);

    expect(
      await screen.findByTestId('reconcile-rebase-error'),
    ).toBeInTheDocument();
  });
});
