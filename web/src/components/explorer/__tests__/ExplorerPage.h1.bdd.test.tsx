import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { I18nextProvider } from 'react-i18next';
import i18n from '../../../i18n';

// A11Y-HEADING: ExplorerPage is a standalone route page (App.tsx
// `explorer/:ontology`) but historically shipped without a page-level <h1> —
// its main title was an <h2>Schema Graph</h2>. Every other page in the app
// exposes exactly one <h1>. These scenarios pin the contract that the Explorer
// page surfaces a single, stable page-level heading across its main states
// (Schema Graph view AND object-type-detail view).

vi.mock('../../../hooks/useObjectTypes', () => ({
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
        status: 'ACTIVE',
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

const server = setupServer(
  http.get('/api/v2/ontologies/:ontology/objects/:objectType', () =>
    HttpResponse.json({ data: [], totalCount: '0' }),
  ),
);

beforeAll(() => server.listen({ onUnhandledRequest: 'bypass' }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function renderAt(initialPath: string, element: React.ReactNode) {
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

describe('BDD: ExplorerPage page-level h1', () => {
  it('Given the Schema Graph state, Then exactly one level-1 heading exists', async () => {
    const { ExplorerPage } = await import('../ExplorerPage');
    renderAt(
      '/explorer/northwind',
      <Route path="/explorer/:ontology" element={<ExplorerPage />} />,
    );

    const h1s = screen.getAllByRole('heading', { level: 1 });
    expect(h1s).toHaveLength(1);
    expect(h1s[0]).toHaveAccessibleName(/explorer/i);
  });

  it('Given the object-type-detail state, Then exactly one level-1 heading exists', async () => {
    const { ExplorerPage } = await import('../ExplorerPage');
    renderAt(
      '/explorer/northwind/Employee',
      <Route
        path="/explorer/:ontology/:objectType"
        element={<ExplorerPage />}
      />,
    );

    const h1s = screen.getAllByRole('heading', { level: 1 });
    expect(h1s).toHaveLength(1);
    expect(h1s[0]).toHaveAccessibleName(/explorer/i);
  });
});
