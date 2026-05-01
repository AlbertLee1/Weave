/**
 * US-351: WCAG 2.1 AA accessibility audit.
 *
 * Runs axe-core against Dashboard, Explorer, and Browser pages and asserts
 * no critical/serious violations. The test suite intentionally mocks data
 * hooks rather than the wider Shell so each page renders standalone with
 * predictable DOM, matching how each page is mounted at the route boundary.
 */
import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { I18nextProvider } from 'react-i18next';
import i18n from '../../i18n';
import { expectNoCriticalViolations } from '../../test/axe';

vi.mock('../../hooks/useOntologies', () => ({
  useOntologies: () => ({
    data: [
      {
        rid: 'ri.weave.main.ontology.northwind',
        apiName: 'northwind',
        displayName: 'Northwind',
        description: 'Demo ontology for accessibility audit',
      },
    ],
    isLoading: false,
    error: null,
  }),
  useOntology: () => ({ data: undefined, isLoading: false }),
}));

vi.mock('../../hooks/useObjectTypes', () => ({
  useObjectTypes: () => ({
    data: [
      {
        rid: 'ri.ot.employee',
        apiName: 'Employee',
        displayName: 'Employee',
        primaryKey: 'employeeId',
        status: 'ACTIVE',
        visibility: 'NORMAL',
      },
      {
        rid: 'ri.ot.customer',
        apiName: 'Customer',
        displayName: 'Customer',
        primaryKey: 'customerId',
        status: 'EXPERIMENTAL',
        visibility: 'NORMAL',
      },
    ],
    isLoading: false,
    error: null,
  }),
  useObjectType: (_ontology: string, apiName: string) => ({
    data: apiName
      ? {
          rid: 'ri.ot.test',
          apiName,
          displayName: apiName,
          pluralDisplayName: `${apiName}s`,
          primaryKey: 'id',
          status: 'ACTIVE',
          visibility: 'NORMAL',
          titleProperty: 'name',
          properties: {
            id: { dataType: { type: 'string' }, rid: 'ri.p.id' },
            name: { dataType: { type: 'string' }, rid: 'ri.p.name' },
          },
        }
      : undefined,
    isLoading: false,
  }),
  useOutgoingLinkTypes: () => ({ data: [], isLoading: false }),
}));

// Realtime subscriptions are network-touching surfaces — keep them inert here so
// the axe scan only sees the stable initial DOM.
vi.mock('../../hooks/useWebSocketSubscription', () => ({
  useWebSocketSubscription: () => undefined,
}));
vi.mock('../../hooks/useObjectSetSubscription', () => ({
  useObjectSetSubscription: () => undefined,
}));

const server = setupServer(
  http.get('/api/v2/ontologies/:ontology/objects/:objectType', () =>
    HttpResponse.json({
      data: [
        { __primaryKey: '1', __apiName: 'Employee', id: '1', name: 'Alice' },
        { __primaryKey: '2', __apiName: 'Employee', id: '2', name: 'Bob' },
      ],
      totalCount: '2',
    }),
  ),
  http.post('/api/v2/ontologies/:ontology/objects/:objectType/search', () =>
    HttpResponse.json({
      data: [
        { __primaryKey: '1', __apiName: 'Employee', id: '1', name: 'Alice' },
      ],
      totalCount: '1',
    }),
  ),
  http.get('/api/v2/saved-searches/:ontology/:objectType', () =>
    HttpResponse.json({ data: [] }),
  ),
  http.get('/api/v2/ontologies/:ontology/objects/:objectType/aggregate', () =>
    HttpResponse.json({ groups: [] }),
  ),
);

beforeAll(() => server.listen({ onUnhandledRequest: 'bypass' }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function renderWithProviders(initialPath: string, element: React.ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={[initialPath]}>
          <Routes>{element}</Routes>
        </MemoryRouter>
      </QueryClientProvider>
    </I18nextProvider>,
  );
}

describe('WCAG 2.1 AA accessibility audit', () => {
  it('Dashboard page has no critical violations', async () => {
    const { DashboardPage } = await import('../dashboard/DashboardPage');
    const { container } = renderWithProviders(
      '/',
      <Route path="/" element={<DashboardPage />} />,
    );
    await expectNoCriticalViolations(container);
  });

  it('Explorer page (no objectType selected) has no critical violations', async () => {
    const { ExplorerPage } = await import('../explorer/ExplorerPage');
    const { container } = renderWithProviders(
      '/explorer/northwind',
      <Route path="/explorer/:ontology" element={<ExplorerPage />} />,
    );
    await expectNoCriticalViolations(container);
  });

  it('Explorer page (object type detail) has no critical violations', async () => {
    const { ExplorerPage } = await import('../explorer/ExplorerPage');
    const { container } = renderWithProviders(
      '/explorer/northwind/Employee',
      <Route
        path="/explorer/:ontology/:objectType"
        element={<ExplorerPage />}
      />,
    );
    await expectNoCriticalViolations(container);
  });

  it('Browser page has no critical violations', async () => {
    const { BrowserPage } = await import('../browser/BrowserPage');
    const { container } = renderWithProviders(
      '/browser/northwind/Employee',
      <Route
        path="/browser/:ontology/:objectType"
        element={<BrowserPage />}
      />,
    );
    await expectNoCriticalViolations(container);
  });

  it('Browser page with filter builder open has no critical violations', async () => {
    const { BrowserPage } = await import('../browser/BrowserPage');
    const { container } = renderWithProviders(
      '/browser/northwind/Employee',
      <Route
        path="/browser/:ontology/:objectType"
        element={<BrowserPage />}
      />,
    );
    fireEvent.click(screen.getByTestId('toggle-filters'));
    await waitFor(() =>
      expect(screen.getByTestId('filter-field-select')).toBeInTheDocument(),
    );
    await expectNoCriticalViolations(container);
  });

  it('OntologyCard exposes a discoverable accessible name (role+name pair)', async () => {
    const { OntologyCard } = await import('../dashboard/OntologyCard');
    const { container, getByRole } = render(
      <OntologyCard
        ontology={{
          rid: 'ri.weave.main.ontology.demo',
          apiName: 'demo',
          displayName: 'Demo Ontology',
          description: 'Just a demo',
        }}
        objectTypeCount={3}
        onClick={() => {}}
      />,
    );
    // Spot-check: aria-label keeps the visible name discoverable for SR users.
    expect(getByRole('button', { name: /open ontology demo ontology/i })).toBeInTheDocument();
    await expectNoCriticalViolations(container);
  });
});
