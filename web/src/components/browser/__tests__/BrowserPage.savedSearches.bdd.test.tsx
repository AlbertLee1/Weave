import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { BrowserPage } from '../BrowserPage';

// P2B-001 BDD: saved Browser searches are operator-visible state, not only
// query state. Loading a saved search that contains `searchText` must hydrate
// the SearchBar input so the active object search can be audited, edited, and
// cleared from the same control that originally creates ad hoc searches.
//
// P2B-003 BDD: saved Browser views also carry presentation state. Operators
// should be able to save a map/gantt/sankey/pivot mode and later restore that
// mode alongside filters and sort.

vi.mock('../../../hooks/useObjectTypes', () => ({
  useObjectType: (_ontology: string, apiName: string) => ({
    data: apiName
      ? {
          rid: 'ri.object-type.demo.article',
          apiName,
          displayName: 'Article',
          pluralDisplayName: 'Articles',
          primaryKey: 'id',
          titleProperty: 'title',
          status: 'ACTIVE',
          visibility: 'NORMAL',
          properties: {
            id: { dataType: { type: 'string' }, rid: 'ri.property.id' },
            title: { dataType: { type: 'string' }, rid: 'ri.property.title' },
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

const searchBodies: Array<Record<string, unknown>> = [];
const savedSearchCreates: Array<Record<string, unknown>> = [];

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
            rid: 'ri.property.title',
            apiName: 'title',
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
          __primaryKey: 'article-1',
          __apiName: 'Article',
          id: 'article-1',
          title: 'Unfiltered list row',
        },
      ],
      totalCount: '1',
    }),
  ),
  http.post(
    '/api/v2/ontologies/:ontology/objects/:objectType/search',
    async ({ request }) => {
      searchBodies.push((await request.json()) as Record<string, unknown>);
      return HttpResponse.json({
        data: [
          {
            __primaryKey: 'article-2',
            __apiName: 'Article',
            id: 'article-2',
            title: 'OpenAI saved-search row',
          },
        ],
        totalCount: '1',
      });
    },
  ),
  http.get('/api/v2/saved-searches', () =>
    HttpResponse.json({
      savedSearches: [
        {
          id: 'saved-openai',
          name: 'OpenAI articles',
          ontology: 'demo',
          objectType: 'Article',
          createdBy: 'operator',
          createdAt: '2026-05-22T00:00:00Z',
          updatedAt: '2026-05-22T00:00:00Z',
          definition: { searchText: 'OpenAI' },
        },
      ],
    }),
  ),
  http.post('/api/v2/saved-searches', async ({ request }) => {
    const body = (await request.json()) as Record<string, unknown>;
    savedSearchCreates.push(body);
    return HttpResponse.json({
      id: 'saved-created',
      name: String(body.name ?? ''),
      ontology: String(body.ontology ?? ''),
      objectType: String(body.objectType ?? ''),
      createdBy: 'operator',
      createdAt: '2026-05-22T00:00:00Z',
      updatedAt: '2026-05-22T00:00:00Z',
      definition: body.definition ?? {},
    });
  }),
  http.get('/api/v2/datasets/:ontology/history', () =>
    HttpResponse.json({ data: [] }),
  ),
  http.get('/api/v2/ontologies/:ontology/actionTypes', () =>
    HttpResponse.json({ data: [] }),
  ),
);

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => {
  searchBodies.length = 0;
  savedSearchCreates.length = 0;
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
      <MemoryRouter initialEntries={['/browser/demo/Article']}>
        <Routes>
          <Route path="/browser/:ontology/:objectType" element={<BrowserPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BDD: BrowserPage saved-search searchText hydration (P2B-001)', () => {
  it('Given a saved search has searchText, When loaded and cleared, Then the visible input mirrors the active query state', async () => {
    const user = userEvent.setup();
    renderBrowserPage();

    expect(await screen.findByText('Unfiltered list row')).toBeInTheDocument();

    await user.click(await screen.findByTestId('saved-search-load-saved-openai'));

    const searchInput = screen.getByTestId('search-input');
    await waitFor(() => {
      expect(searchInput).toHaveValue('OpenAI');
    });
    expect(await screen.findByText('OpenAI saved-search row')).toBeInTheDocument();
    expect(searchBodies).toContainEqual(
      expect.objectContaining({
        where: { type: 'containsAnyTerm', field: 'title', value: 'OpenAI' },
      }),
    );

    await user.clear(searchInput);

    await waitFor(() => {
      expect(searchInput).toHaveValue('');
      expect(screen.getByText('Unfiltered list row')).toBeInTheDocument();
    });
    expect(screen.queryByText('OpenAI saved-search row')).not.toBeInTheDocument();
  });
});

describe('BDD: BrowserPage saved-view presentation mode hydration (P2B-003)', () => {
  it('Given the operator saves a non-table view, When the definition is created, Then it includes the selected view mode', async () => {
    const user = userEvent.setup();
    renderBrowserPage();

    expect(await screen.findByText('Unfiltered list row')).toBeInTheDocument();

    await user.click(screen.getByTestId('view-mode-map'));

    await waitFor(() => {
      expect(screen.getByTestId('map-view-empty')).toBeInTheDocument();
      expect(screen.getByTestId('view-mode-map')).toHaveAttribute(
        'aria-pressed',
        'true',
      );
    });

    await user.click(screen.getByTestId('saved-searches-save'));
    await user.type(screen.getByTestId('saved-searches-name-input'), 'Map view');
    await user.click(screen.getByTestId('saved-searches-confirm'));

    await waitFor(() => {
      expect(savedSearchCreates).toHaveLength(1);
    });
    expect(savedSearchCreates[0]).toMatchObject({
      definition: { viewMode: 'map' },
    });
  });

  it('Given a saved view has a non-table viewMode, When loaded, Then BrowserPage restores that presentation mode', async () => {
    const user = userEvent.setup();
    server.use(
      http.get('/api/v2/saved-searches', () =>
        HttpResponse.json({
          savedSearches: [
            {
              id: 'saved-map-view',
              name: 'Map view',
              ontology: 'demo',
              objectType: 'Article',
              createdBy: 'operator',
              createdAt: '2026-05-22T00:00:00Z',
              updatedAt: '2026-05-22T00:00:00Z',
              definition: { viewMode: 'map' },
            },
          ],
        }),
      ),
    );

    renderBrowserPage();

    expect(await screen.findByText('Unfiltered list row')).toBeInTheDocument();
    expect(screen.getByTestId('view-mode-table')).toHaveAttribute(
      'aria-pressed',
      'true',
    );

    await user.click(await screen.findByTestId('saved-search-load-saved-map-view'));

    await waitFor(() => {
      expect(screen.getByTestId('view-mode-map')).toHaveAttribute(
        'aria-pressed',
        'true',
      );
      expect(screen.getByTestId('map-view-empty')).toBeInTheDocument();
    });
  });

  it('Given a saved search is loaded, When the operator reviews the panel, Then the matching row is marked aria-current and tagged "Active"', async () => {
    const user = userEvent.setup();
    renderBrowserPage();

    expect(await screen.findByText('Unfiltered list row')).toBeInTheDocument();

    const row = await screen.findByTestId('saved-search-saved-openai');
    expect(row).not.toHaveAttribute('aria-current', 'true');
    expect(
      screen.queryByTestId('saved-search-active-badge-saved-openai'),
    ).not.toBeInTheDocument();

    await user.click(screen.getByTestId('saved-search-load-saved-openai'));

    await waitFor(() => {
      expect(screen.getByTestId('saved-search-saved-openai')).toHaveAttribute(
        'aria-current',
        'true',
      );
      expect(
        screen.getByTestId('saved-search-active-badge-saved-openai'),
      ).toBeInTheDocument();
    });
  });

  it('Given a saved search is active, When the operator edits the search input, Then the active indicator is cleared', async () => {
    const user = userEvent.setup();
    renderBrowserPage();

    expect(await screen.findByText('Unfiltered list row')).toBeInTheDocument();

    await user.click(await screen.findByTestId('saved-search-load-saved-openai'));

    await waitFor(() => {
      expect(screen.getByTestId('saved-search-saved-openai')).toHaveAttribute(
        'aria-current',
        'true',
      );
    });

    const searchInput = screen.getByTestId('search-input');
    await user.clear(searchInput);

    await waitFor(() => {
      expect(
        screen.getByTestId('saved-search-saved-openai'),
      ).not.toHaveAttribute('aria-current', 'true');
      expect(
        screen.queryByTestId('saved-search-active-badge-saved-openai'),
      ).not.toBeInTheDocument();
    });
  });

  it('Given a saved search is active, When the operator switches the view mode, Then the active indicator is cleared', async () => {
    const user = userEvent.setup();
    renderBrowserPage();

    expect(await screen.findByText('Unfiltered list row')).toBeInTheDocument();

    await user.click(await screen.findByTestId('saved-search-load-saved-openai'));

    await waitFor(() => {
      expect(screen.getByTestId('saved-search-saved-openai')).toHaveAttribute(
        'aria-current',
        'true',
      );
    });

    await user.click(screen.getByTestId('view-mode-map'));

    await waitFor(() => {
      expect(
        screen.getByTestId('saved-search-saved-openai'),
      ).not.toHaveAttribute('aria-current', 'true');
    });
  });

  it('Given an older saved view has no viewMode, When loaded from another mode, Then BrowserPage falls back to table mode', async () => {
    const user = userEvent.setup();
    server.use(
      http.get('/api/v2/saved-searches', () =>
        HttpResponse.json({
          savedSearches: [
            {
              id: 'saved-legacy',
              name: 'Legacy view',
              ontology: 'demo',
              objectType: 'Article',
              createdBy: 'operator',
              createdAt: '2026-05-22T00:00:00Z',
              updatedAt: '2026-05-22T00:00:00Z',
              definition: { searchText: 'OpenAI' },
            },
          ],
        }),
      ),
    );

    renderBrowserPage();

    expect(await screen.findByText('Unfiltered list row')).toBeInTheDocument();
    await user.click(screen.getByTestId('view-mode-map'));

    await waitFor(() => {
      expect(screen.getByTestId('view-mode-map')).toHaveAttribute(
        'aria-pressed',
        'true',
      );
    });

    await user.click(await screen.findByTestId('saved-search-load-saved-legacy'));

    await waitFor(() => {
      expect(screen.getByTestId('view-mode-table')).toHaveAttribute(
        'aria-pressed',
        'true',
      );
      expect(screen.getByTestId('search-input')).toHaveValue('OpenAI');
    });
  });
});
