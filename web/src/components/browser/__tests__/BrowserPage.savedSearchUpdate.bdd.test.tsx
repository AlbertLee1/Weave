import {
  describe,
  it,
  expect,
  vi,
  beforeAll,
  afterAll,
  afterEach,
} from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { BrowserPage } from '../BrowserPage';

// P2B-S005 BDD: once an operator has loaded a saved search, drifting
// the view away from the saved definition (search-text change, filter
// toggle, etc.) should expose an "Update" affordance on the originated
// row. Clicking Update PUTs the current view back onto the same saved
// search and re-applies the Active indicator. Without this, operators
// must delete-and-recreate to capture small edits, which silently
// breaks every other operator who already bookmarked the same ID.

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

interface UpdateCall {
  id: string;
  body: Record<string, unknown>;
}

const updateCalls: UpdateCall[] = [];
let updateResponseStatus = 200;

const buildSavedSearchHandlers = () => [
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
    () =>
      HttpResponse.json({
        data: [
          {
            __primaryKey: 'article-2',
            __apiName: 'Article',
            id: 'article-2',
            title: 'Filtered saved-search row',
          },
        ],
        totalCount: '1',
      }),
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
  http.put('/api/v2/saved-searches/:id', async ({ request, params }) => {
    const id = String(params.id);
    const body = (await request.json()) as Record<string, unknown>;
    updateCalls.push({ id, body });
    if (updateResponseStatus >= 400) {
      return HttpResponse.json(
        { errorName: 'Boom', message: 'update failed' },
        { status: updateResponseStatus },
      );
    }
    return HttpResponse.json({
      id,
      name: String(body.name ?? 'OpenAI articles'),
      ontology: 'demo',
      objectType: 'Article',
      createdBy: 'operator',
      createdAt: '2026-05-22T00:00:00Z',
      updatedAt: '2026-05-23T00:00:00Z',
      definition: body.definition ?? {},
    });
  }),
  http.get('/api/v2/datasets/:ontology/history', () =>
    HttpResponse.json({ data: [] }),
  ),
  http.get('/api/v2/ontologies/:ontology/actionTypes', () =>
    HttpResponse.json({ data: [] }),
  ),
];

const server = setupServer(...buildSavedSearchHandlers());

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => {
  updateCalls.length = 0;
  updateResponseStatus = 200;
  server.resetHandlers(...buildSavedSearchHandlers());
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

describe('BDD: BrowserPage saved-search in-place update (P2B-S005)', () => {
  it('Given no saved search has been loaded, When the operator opens the panel, Then no Update affordance is rendered', async () => {
    renderBrowserPage();

    expect(await screen.findByText('Unfiltered list row')).toBeInTheDocument();
    expect(
      await screen.findByTestId('saved-search-load-saved-openai'),
    ).toBeInTheDocument();
    expect(
      screen.queryByTestId('saved-search-update-saved-openai'),
    ).not.toBeInTheDocument();
  });

  it('Given a saved search is loaded and still matches, When the operator inspects the panel, Then no Update affordance is rendered yet', async () => {
    const user = userEvent.setup();
    renderBrowserPage();

    expect(await screen.findByText('Unfiltered list row')).toBeInTheDocument();
    await user.click(await screen.findByTestId('saved-search-load-saved-openai'));

    await waitFor(() => {
      expect(
        screen.getByTestId('saved-search-saved-openai'),
      ).toHaveAttribute('aria-current', 'true');
    });

    expect(
      screen.queryByTestId('saved-search-update-saved-openai'),
    ).not.toBeInTheDocument();
  });

  it('Given a loaded saved search has drifted, When the operator clicks Update, Then the saved search is PUT with the current definition and the Active indicator returns', async () => {
    const user = userEvent.setup();
    renderBrowserPage();

    expect(await screen.findByText('Unfiltered list row')).toBeInTheDocument();
    await user.click(await screen.findByTestId('saved-search-load-saved-openai'));

    await waitFor(() => {
      expect(
        screen.getByTestId('saved-search-saved-openai'),
      ).toHaveAttribute('aria-current', 'true');
    });

    const searchInput = screen.getByTestId('search-input');
    await user.clear(searchInput);
    await user.type(searchInput, 'Anthropic{enter}');

    await waitFor(() => {
      expect(
        screen.getByTestId('saved-search-saved-openai'),
      ).not.toHaveAttribute('aria-current', 'true');
    });

    const updateBtn = await screen.findByTestId(
      'saved-search-update-saved-openai',
    );
    expect(updateBtn).toBeEnabled();
    await user.click(updateBtn);

    await waitFor(() => {
      expect(updateCalls).toHaveLength(1);
    });
    expect(updateCalls[0].id).toBe('saved-openai');
    expect(updateCalls[0].body).toMatchObject({
      definition: { searchText: 'Anthropic' },
    });

    await waitFor(() => {
      expect(
        screen.getByTestId('saved-search-saved-openai'),
      ).toHaveAttribute('aria-current', 'true');
    });
    expect(
      screen.queryByTestId('saved-search-update-saved-openai'),
    ).not.toBeInTheDocument();
  });

  it('Given the update request fails, When the response returns 500, Then an inline error is surfaced and the Active indicator stays cleared', async () => {
    updateResponseStatus = 500;
    const user = userEvent.setup();
    renderBrowserPage();

    expect(await screen.findByText('Unfiltered list row')).toBeInTheDocument();
    await user.click(await screen.findByTestId('saved-search-load-saved-openai'));

    await waitFor(() => {
      expect(
        screen.getByTestId('saved-search-saved-openai'),
      ).toHaveAttribute('aria-current', 'true');
    });

    const searchInput = screen.getByTestId('search-input');
    await user.clear(searchInput);
    await user.type(searchInput, 'Anthropic{enter}');

    const updateBtn = await screen.findByTestId(
      'saved-search-update-saved-openai',
    );
    await user.click(updateBtn);

    await waitFor(() => {
      expect(updateCalls).toHaveLength(1);
    });

    expect(
      await screen.findByTestId('saved-search-update-error-saved-openai'),
    ).toBeInTheDocument();
    expect(
      screen.getByTestId('saved-search-saved-openai'),
    ).not.toHaveAttribute('aria-current', 'true');
  });
});
