import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { BranchReconcilePage } from '../BranchReconcilePage';
import type {
  AnnotatedDiffEntry,
  AnnotatedMergeConflict,
  BranchDiffPostResponse,
} from '../../../api/types';

const server = setupServer();
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

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

const addedEntry: AnnotatedDiffEntry = {
  entityType: 'objectType',
  entityRid: 'ri.objectType.x.added',
  apiName: 'NewType',
  resolutionKey: 'objectType:NewType',
  changeType: 'ADDED',
  hasConflict: false,
  before: null,
  after: { apiName: 'NewType', displayName: 'New Type' },
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

function diffResponse(opts?: {
  conflicts?: AnnotatedMergeConflict[];
  added?: AnnotatedDiffEntry[];
  modified?: AnnotatedDiffEntry[];
  deleted?: AnnotatedDiffEntry[];
}): BranchDiffPostResponse {
  const conflicts = opts?.conflicts ?? [];
  return {
    branch,
    added: opts?.added ?? [],
    modified: opts?.modified ?? [],
    deleted: opts?.deleted ?? [],
    conflicts,
    hasConflicts: conflicts.length > 0,
  };
}

function renderPage(initialPath = '/explorer/foundry/branches/br-feature-x/reconcile') {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[initialPath]}>
        <Routes>
          <Route
            path="/explorer/:ontology/branches/:branch/reconcile"
            element={<BranchReconcilePage />}
          />
          <Route path="/explorer/:ontology" element={<div data-testid="explorer-shell" />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BranchReconcilePage (US-387)', () => {
  it('renders the side-by-side diff with no conflicts and merges directly', async () => {
    server.use(
      http.post(
        '/api/v2/ontologies/foundry/branches/br-feature-x/diff',
        () => HttpResponse.json(diffResponse({ added: [addedEntry] })),
      ),
      http.post(
        '/api/v2/ontologies/foundry/branches/br-feature-x/merge',
        () =>
          HttpResponse.json({
            branch: { ...branch, status: 'merged' },
            appliedCount: 1,
            skippedCount: 0,
          }),
      ),
    );

    renderPage();

    expect(await screen.findByTestId('branch-reconcile-page')).toBeInTheDocument();
    // Non-conflict entry surfaces under Other changes.
    expect(
      screen.getByTestId('reconcile-entry-objectType:NewType'),
    ).toBeInTheDocument();
    // No conflict count badge since hasConflicts=false.
    expect(screen.queryByTestId('reconcile-conflict-count')).toBeNull();

    const button = screen.getByTestId('branch-reconcile-merge-button');
    expect(button).not.toBeDisabled();
    const user = userEvent.setup();
    await user.click(button);

    await waitFor(() => {
      // After a successful merge the page navigates to the explorer shell.
      expect(screen.getByTestId('explorer-shell')).toBeInTheDocument();
    });
  });

  it('disables merge until every conflict has a resolution choice', async () => {
    server.use(
      http.post(
        '/api/v2/ontologies/foundry/branches/br-feature-x/diff',
        () => HttpResponse.json(diffResponse({ conflicts: [conflictEntry] })),
      ),
    );

    renderPage();

    expect(
      await screen.findByTestId('reconcile-conflict-count'),
    ).toHaveTextContent('1');
    const button = screen.getByTestId('branch-reconcile-merge-button');
    expect(button).toBeDisabled();

    const user = userEvent.setup();
    await user.click(
      screen.getByTestId(
        'reconcile-conflict-actionType:updateOrder-use-branch',
      ),
    );

    await waitFor(() => {
      expect(button).not.toBeDisabled();
    });
  });

  it('renders the inline conflict radios with both sides visible', async () => {
    server.use(
      http.post(
        '/api/v2/ontologies/foundry/branches/br-feature-x/diff',
        () => HttpResponse.json(diffResponse({ conflicts: [conflictEntry] })),
      ),
    );

    renderPage();

    expect(
      await screen.findByTestId('reconcile-conflict-actionType:updateOrder'),
    ).toBeInTheDocument();
    // Both sides of the conflict are rendered.
    expect(screen.getByText('"Branch label"')).toBeInTheDocument();
    expect(screen.getByText('"Main label"')).toBeInTheDocument();
    // Both choices are wired up.
    expect(
      screen.getByTestId(
        'reconcile-conflict-actionType:updateOrder-use-main',
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByTestId(
        'reconcile-conflict-actionType:updateOrder-use-branch',
      ),
    ).toBeInTheDocument();
  });

  it('shows server-reported conflicts after a 409 merge response', async () => {
    server.use(
      http.post(
        '/api/v2/ontologies/foundry/branches/br-feature-x/diff',
        () => HttpResponse.json(diffResponse({ added: [addedEntry] })),
      ),
      http.post(
        '/api/v2/ontologies/foundry/branches/br-feature-x/merge',
        () =>
          HttpResponse.json(
            {
              errorCode: 'MERGE_CONFLICT',
              conflicts: [conflictEntry],
              unresolved: [conflictEntry],
            },
            { status: 409 },
          ),
      ),
    );

    renderPage();

    const button = await screen.findByTestId('branch-reconcile-merge-button');
    const user = userEvent.setup();
    await user.click(button);

    expect(
      await screen.findByTestId('reconcile-conflict-actionType:updateOrder'),
    ).toBeInTheDocument();
    expect(
      await screen.findByTestId('reconcile-error'),
    ).toBeInTheDocument();
  });
});
