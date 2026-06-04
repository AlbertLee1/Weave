import {
  describe,
  it,
  expect,
  beforeAll,
  afterAll,
  afterEach,
} from 'vitest';
import { render, screen } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
// Side-effect import: bootstraps the i18next singleton so `t()` resolves real
// copy ("Branch Reconcile") rather than echoing raw keys.
import '../../../i18n';
import { BranchReconcilePage } from '../BranchReconcilePage';
import type { BranchDiffPostResponse } from '../../../api/types';

// A11Y-HEADING: BranchReconcilePage is a standalone route page
// (App.tsx `explorer/:ontology/branches/:branch/reconcile`) but historically
// shipped without a page-level <h1> — its main title was an <h2>Branch
// Reconcile</h2>. Every other page in the app exposes exactly one <h1>. This
// scenario pins the contract that the reconcile page surfaces a single, stable
// page-level heading named "Branch Reconcile".
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

const server = setupServer(
  http.post(
    '/api/v2/ontologies/foundry/branches/br-feature-x/diff',
    () => HttpResponse.json(diffResponse()),
  ),
);
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

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

describe('BDD: BranchReconcilePage page-level h1', () => {
  it('Given the loaded reconcile page, Then exactly one level-1 heading named "reconcile" exists', async () => {
    renderPage();

    // Wait for the diff to load (the reconcile body renders the page chrome).
    await screen.findByTestId('branch-reconcile-page');

    const h1s = screen.getAllByRole('heading', { level: 1 });
    expect(h1s).toHaveLength(1);
    expect(h1s[0]).toHaveAccessibleName(/reconcile/i);
  });
});
