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
import { BranchDiffPage } from '../BranchDiffPage';
import type { BranchDiffEntry } from '../../../api/types';

// A11Y-HEADING: BranchDiffPage is a standalone route page
// (App.tsx `explorer/:ontology/branches/:branch/diff`) but historically
// shipped without a page-level <h1> — its main title was an <h2>Branch
// Diff</h2>, and its loading/error/empty-param states had no heading at all.
// Every other page in the app exposes exactly one stable <h1>. These scenarios
// pin the contract that the diff page surfaces a single, stable page-level
// heading named "Branch Diff" across each of its render states.

function diffEntries(): BranchDiffEntry[] {
  return [
    {
      entityType: 'objectType',
      entityRid: 'ri.ontology.foundry.object-type.customer',
      changeType: 'ADDED',
      before: null,
      after: { apiName: 'customer', displayName: 'Customer' },
    },
  ];
}

const server = setupServer(
  http.get(
    '/api/v2/ontologies/foundry/branches/br-feature-x/diff',
    () => HttpResponse.json({ data: diffEntries() }),
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
        initialEntries={['/explorer/foundry/branches/br-feature-x/diff']}
      >
        <Routes>
          <Route
            path="/explorer/:ontology/branches/:branch/diff"
            element={<BranchDiffPage />}
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

describe('BDD: BranchDiffPage page-level h1', () => {
  it('Given the loaded diff page, Then exactly one level-1 heading named "Branch Diff" exists', async () => {
    renderPage();

    // Wait for the diff body to render (the entry card carries the entity rid).
    await screen.findByText('ri.ontology.foundry.object-type.customer');

    const h1s = screen.getAllByRole('heading', { level: 1 });
    expect(h1s).toHaveLength(1);
    expect(h1s[0]).toHaveAccessibleName(/branch diff/i);
  });

  it('Given the loading state, Then exactly one level-1 heading named "Branch Diff" exists', () => {
    renderPage();

    // Synchronously (before the query resolves) the loading state is shown.
    const h1s = screen.getAllByRole('heading', { level: 1 });
    expect(h1s).toHaveLength(1);
    expect(h1s[0]).toHaveAccessibleName(/branch diff/i);
  });

  it('Given the error state, Then exactly one level-1 heading named "Branch Diff" exists', async () => {
    server.use(
      http.get(
        '/api/v2/ontologies/foundry/branches/br-feature-x/diff',
        () => HttpResponse.json({ message: 'boom' }, { status: 500 }),
      ),
    );
    renderPage();

    await screen.findByText(/failed to load branch diff/i);

    const h1s = screen.getAllByRole('heading', { level: 1 });
    expect(h1s).toHaveLength(1);
    expect(h1s[0]).toHaveAccessibleName(/branch diff/i);
  });
});
