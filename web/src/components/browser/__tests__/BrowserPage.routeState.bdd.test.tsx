import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Link, MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { BrowserPage } from '../BrowserPage';

// P2B-001 BDD: Browser route state belongs to the active ontology/objectType
// route. Navigating from one object type to another in the reused BrowserPage
// instance must not carry stale search predicates or selected rows into the
// new grid.

vi.mock('../../../hooks/useObjectTypes', () => ({
  useObjectType: (_ontology: string, apiName: string) => {
    const isOrder = apiName === 'Order';
    return {
      data: apiName
        ? {
            rid: isOrder ? 'ri.ot.order' : 'ri.ot.customer',
            apiName,
            displayName: isOrder ? 'Order' : 'Customer',
            pluralDisplayName: isOrder ? 'Orders' : 'Customers',
            primaryKey: isOrder ? 'orderId' : 'customerId',
            titleProperty: isOrder ? 'description' : 'name',
            status: 'ACTIVE',
            visibility: 'NORMAL',
            properties: isOrder
              ? {
                  orderId: { dataType: { type: 'string' }, rid: 'ri.p.orderId' },
                  description: {
                    dataType: { type: 'string' },
                    rid: 'ri.p.description',
                  },
                }
              : {
                  customerId: {
                    dataType: { type: 'string' },
                    rid: 'ri.p.customerId',
                  },
                  name: { dataType: { type: 'string' }, rid: 'ri.p.name' },
                },
          }
        : undefined,
      isLoading: false,
    };
  },
  useOutgoingLinkTypes: () => ({ data: [], isLoading: false }),
}));

vi.mock('../../../hooks/useWebSocketSubscription', () => ({
  useWebSocketSubscription: () => undefined,
}));

const searchCalls: Array<{ objectType: string; body: unknown }> = [];

const server = setupServer(
  http.get(
    '/api/v2/ontologies/:ontology/objectTypes/byRid/:objectTypeRid/properties',
    ({ params }) => {
      const isOrder = params.objectTypeRid === 'ri.ot.order';
      return HttpResponse.json({
        data: isOrder
          ? [
              {
                rid: 'ri.p.orderId',
                apiName: 'orderId',
                baseType: 'string',
                isSearchable: true,
                isSortable: true,
              },
              {
                rid: 'ri.p.description',
                apiName: 'description',
                baseType: 'string',
                isSearchable: true,
                isSortable: true,
              },
            ]
          : [
              {
                rid: 'ri.p.customerId',
                apiName: 'customerId',
                baseType: 'string',
                isSearchable: true,
                isSortable: true,
              },
              {
                rid: 'ri.p.name',
                apiName: 'name',
                baseType: 'string',
                isSearchable: true,
                isSortable: true,
              },
            ],
      });
    },
  ),
  http.get('/api/v2/ontologies/:ontology/objects/:objectType', ({ params }) => {
    if (params.objectType === 'Order') {
      return HttpResponse.json({
        data: [
          {
            __primaryKey: 'o-1',
            __apiName: 'Order',
            orderId: 'o-1',
            description: 'Order list row',
          },
        ],
        totalCount: '1',
      });
    }
    return HttpResponse.json({
      data: [
        {
          __primaryKey: 'c-1',
          __apiName: 'Customer',
          customerId: 'c-1',
          name: 'Alice Customer',
        },
      ],
      totalCount: '1',
    });
  }),
  http.post(
    '/api/v2/ontologies/:ontology/objects/:objectType/search',
    async ({ params, request }) => {
      searchCalls.push({
        objectType: String(params.objectType),
        body: await request.json(),
      });
      if (params.objectType === 'Order') {
        return HttpResponse.json({
          data: [
            {
              __primaryKey: 'stale',
              __apiName: 'Order',
              orderId: 'stale',
              description: 'STALE SEARCH ROW',
            },
          ],
          totalCount: '1',
        });
      }
      return HttpResponse.json({
        data: [
          {
            __primaryKey: 'c-search',
            __apiName: 'Customer',
            customerId: 'c-search',
            name: 'Customer search hit',
          },
        ],
        totalCount: '1',
      });
    },
  ),
  http.get('/api/v2/saved-searches', () =>
    HttpResponse.json({ savedSearches: [] }),
  ),
  http.get('/api/v2/datasets/:ontology/history', () =>
    HttpResponse.json({ data: [] }),
  ),
  http.get('/api/v2/ontologies/:ontology/actionTypes', () =>
    HttpResponse.json({ data: [] }),
  ),
  http.post('/api/v2/ontologies/:ontology/objectSets/createTemporary', () =>
    HttpResponse.json({ objectSetRid: 'ri.objectset.route-state' }),
  ),
);

beforeAll(() => server.listen());
afterEach(() => {
  searchCalls.length = 0;
  server.resetHandlers();
});
afterAll(() => server.close());

function renderRouteHarness() {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchInterval: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/browser/demo/Customer']}>
        <Link to="/browser/demo/Order">Open Orders</Link>
        <Routes>
          <Route
            path="/browser/:ontology/:objectType"
            element={<BrowserPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BDD: BrowserPage route-scoped state reset (P2B-001)', () => {
  it('Given a search and selected row on Customer, When the operator opens Order, Then stale search and bulk selection do not carry over', async () => {
    const user = userEvent.setup();
    renderRouteHarness();

    expect(await screen.findByText('Alice Customer')).toBeInTheDocument();
    await user.click(screen.getByTestId('select-row-c-1'));
    expect(screen.getByTestId('bulk-action-toolbar')).toBeInTheDocument();

    const search = screen.getByTestId('search-input');
    await user.type(search, 'Alice');
    await user.keyboard('{Enter}');
    expect(await screen.findByText('Customer search hit')).toBeInTheDocument();
    await waitFor(() => {
      expect(searchCalls.some((c) => c.objectType === 'Customer')).toBe(true);
    });

    await user.click(screen.getByRole('link', { name: /open orders/i }));

    expect(await screen.findByRole('heading', { name: 'Orders' })).toBeInTheDocument();
    expect(await screen.findByText('Order list row')).toBeInTheDocument();
    expect(screen.queryByText('STALE SEARCH ROW')).not.toBeInTheDocument();
    expect(screen.getByTestId('search-input')).toHaveValue('');
    expect(screen.queryByTestId('bulk-action-toolbar')).not.toBeInTheDocument();
    expect(searchCalls.filter((c) => c.objectType === 'Order')).toHaveLength(0);
  });
});
