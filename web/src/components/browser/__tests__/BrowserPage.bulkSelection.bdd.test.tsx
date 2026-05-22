import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { BrowserPage } from '../BrowserPage';

// P2B-002 BDD: bulk selections belong to the visible Browser result context.
// When a text search or saved search replaces the active result set, hidden
// rows from the prior context must not remain eligible for bulk actions.

vi.mock('../../../hooks/useObjectTypes', () => ({
  useObjectType: (_ontology: string, apiName: string) => ({
    data: apiName
      ? {
          rid: 'ri.object-type.demo.task',
          apiName,
          displayName: 'Task',
          pluralDisplayName: 'Tasks',
          primaryKey: 'id',
          titleProperty: 'name',
          status: 'ACTIVE',
          visibility: 'NORMAL',
          properties: {
            id: { dataType: { type: 'string' }, rid: 'ri.property.id' },
            name: { dataType: { type: 'string' }, rid: 'ri.property.name' },
            status: { dataType: { type: 'string' }, rid: 'ri.property.status' },
          },
        }
      : undefined,
    isLoading: false,
  }),
  useOutgoingLinkTypes: () => ({ data: [], isLoading: false }),
}));

vi.mock('../../../hooks/useWebSocketSubscription', () => ({
  useWebSocketSubscription: () => undefined,
}));

const server = setupServer(
  http.get(
    '/api/v2/ontologies/:ontology/objectTypes/byRid/:objectTypeRid/properties',
    () =>
      HttpResponse.json({
        data: [
          {
            rid: 'ri.property.id',
            apiName: 'id',
            baseType: 'string',
            isArray: false,
            isNullable: false,
            isSearchable: true,
            isSortable: true,
          },
          {
            rid: 'ri.property.name',
            apiName: 'name',
            baseType: 'string',
            isArray: false,
            isNullable: false,
            isSearchable: true,
            isSortable: true,
          },
          {
            rid: 'ri.property.status',
            apiName: 'status',
            baseType: 'string',
            isArray: false,
            isNullable: false,
            isSearchable: true,
            isSortable: true,
          },
        ],
      }),
  ),
  http.get('/api/v2/ontologies/:ontology/objects/:objectType', () =>
    HttpResponse.json({
      data: [
        {
          __primaryKey: 'task-1',
          __apiName: 'Task',
          id: 'task-1',
          name: 'Unfiltered task',
          status: 'Open',
        },
      ],
      totalCount: '1',
    }),
  ),
  http.post(
    '/api/v2/ontologies/:ontology/objects/:objectType/search',
    async ({ request }) => {
      const body = (await request.json()) as {
        where?: { value?: unknown };
      };
      const value = JSON.stringify(body.where ?? {});
      const row =
        value.includes('Saved task')
          ? {
              __primaryKey: 'task-3',
              __apiName: 'Task',
              id: 'task-3',
              name: 'Saved task result',
              status: 'Escalated',
            }
          : {
              __primaryKey: 'task-2',
              __apiName: 'Task',
              id: 'task-2',
              name: 'Manual search result',
              status: 'In review',
            };

      return HttpResponse.json({
        data: [row],
        totalCount: '1',
        facets: {
          status: [{ value: row.status, count: 1 }],
        },
      });
    },
  ),
  http.get('/api/v2/saved-searches', () =>
    HttpResponse.json({
      savedSearches: [
        {
          id: 'saved-task',
          name: 'Saved task query',
          ontology: 'demo',
          objectType: 'Task',
          createdBy: 'operator',
          createdAt: '2026-05-22T00:00:00Z',
          updatedAt: '2026-05-22T00:00:00Z',
          definition: { searchText: 'Saved task' },
        },
      ],
    }),
  ),
  http.get('/api/v2/datasets/:ontology/history', () =>
    HttpResponse.json({ data: [] }),
  ),
  http.get('/api/v2/ontologies/:ontology/actionTypes', () =>
    HttpResponse.json({ data: [] }),
  ),
);

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => {
  server.resetHandlers();
});
afterAll(() => server.close());

function renderBrowserPage() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/browser/demo/Task']}>
        <Routes>
          <Route path="/browser/:ontology/:objectType" element={<BrowserPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BDD: BrowserPage bulk selection query-context reset (P2B-002)', () => {
  it('Given rows are selected, When text search and saved search replace results, Then hidden rows are cleared before bulk actions run', async () => {
    const user = userEvent.setup();
    renderBrowserPage();

    expect(await screen.findByText('Unfiltered task')).toBeInTheDocument();
    await user.click(screen.getByTestId('select-row-task-1'));
    expect(screen.getByTestId('selected-count')).toHaveTextContent('1 selected');

    const searchInput = screen.getByTestId('search-input');
    await user.type(searchInput, 'Manual');
    await user.keyboard('{Enter}');

    expect(await screen.findByText('Manual search result')).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.queryByTestId('bulk-action-toolbar')).not.toBeInTheDocument();
    });

    await user.click(screen.getByTestId('select-row-task-2'));
    expect(screen.getByTestId('selected-count')).toHaveTextContent('1 selected');

    await user.click(await screen.findByTestId('saved-search-load-saved-task'));

    expect(await screen.findByText('Saved task result')).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.queryByTestId('bulk-action-toolbar')).not.toBeInTheDocument();
    });

    await user.click(screen.getByTestId('select-row-task-3'));
    expect(screen.getByTestId('selected-count')).toHaveTextContent('1 selected');
  });
});
