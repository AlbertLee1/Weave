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

// P2B-S006 BDD: the Save dialog currently posts every name the operator
// types, then surfaces a generic `CONFLICT: SavedSearchNameConflict`
// when the backend rejects a duplicate. Because the panel already
// renders the operator's existing saved searches, the duplicate is
// detectable client-side — warning inline before POST removes a wasted
// round-trip and an unactionable error string. The 409 path stays
// guarded so a stale list (concurrent tab) still produces a friendly
// message instead of the raw error code.

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

const createCalls: Array<Record<string, unknown>> = [];
let createResponseStatus = 201;

const buildHandlers = () => [
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
            title: 'Filtered search row',
          },
        ],
        totalCount: '1',
      }),
  ),
  http.get('/api/v2/saved-searches', () =>
    HttpResponse.json({
      savedSearches: [
        {
          id: 'saved-existing',
          name: 'Apples',
          ontology: 'demo',
          objectType: 'Article',
          createdBy: 'operator',
          createdAt: '2026-05-22T00:00:00Z',
          updatedAt: '2026-05-22T00:00:00Z',
          definition: { searchText: 'apples' },
        },
      ],
    }),
  ),
  http.post('/api/v2/saved-searches', async ({ request }) => {
    const body = (await request.json()) as Record<string, unknown>;
    createCalls.push(body);
    if (createResponseStatus === 409) {
      return HttpResponse.json(
        {
          statusCode: 409,
          errorCode: 'CONFLICT',
          errorName: 'SavedSearchNameConflict',
          errorInstanceId: 'inst',
          parameters: { name: String(body.name ?? '') },
        },
        { status: 409 },
      );
    }
    return HttpResponse.json(
      {
        id: 'saved-new',
        name: body.name,
        ontology: 'demo',
        objectType: 'Article',
        createdBy: 'operator',
        createdAt: '2026-05-24T00:00:00Z',
        updatedAt: '2026-05-24T00:00:00Z',
        definition: body.definition ?? {},
      },
      { status: 201 },
    );
  }),
  http.get('/api/v2/datasets/:ontology/history', () =>
    HttpResponse.json({ data: [] }),
  ),
  http.get('/api/v2/ontologies/:ontology/actionTypes', () =>
    HttpResponse.json({ data: [] }),
  ),
];

const server = setupServer(...buildHandlers());

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => {
  createCalls.length = 0;
  createResponseStatus = 201;
  server.resetHandlers(...buildHandlers());
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

async function openSaveDialog(user: ReturnType<typeof userEvent.setup>) {
  expect(await screen.findByText('Unfiltered list row')).toBeInTheDocument();
  expect(
    await screen.findByTestId('saved-search-load-saved-existing'),
  ).toBeInTheDocument();

  // Drift the view away from default so the Save affordance enables.
  const searchInput = screen.getByTestId('search-input');
  await user.clear(searchInput);
  await user.type(searchInput, 'cherries{enter}');

  await waitFor(() => {
    expect(screen.getByTestId('saved-searches-save')).toBeEnabled();
  });
  await user.click(screen.getByTestId('saved-searches-save'));
  return screen.findByTestId('saved-searches-name-input');
}

describe('BDD: BrowserPage saved-search duplicate name (P2B-S006)', () => {
  it('Given a saved row named "Apples" exists, When the operator types "Apples" into the Save dialog, Then a duplicate warning appears and Save is disabled', async () => {
    const user = userEvent.setup();
    renderBrowserPage();

    const input = await openSaveDialog(user);
    await user.type(input, 'Apples');

    expect(
      await screen.findByTestId('saved-searches-duplicate-warning'),
    ).toBeInTheDocument();
    expect(screen.getByTestId('saved-searches-confirm')).toBeDisabled();
    expect(createCalls).toHaveLength(0);
  });

  it('Given a duplicate warning is showing, When the operator edits the name to a unique value, Then the warning disappears and Save re-enables', async () => {
    const user = userEvent.setup();
    renderBrowserPage();

    const input = await openSaveDialog(user);
    await user.type(input, 'Apples');
    expect(
      await screen.findByTestId('saved-searches-duplicate-warning'),
    ).toBeInTheDocument();

    await user.type(input, ' 2024');
    await waitFor(() => {
      expect(
        screen.queryByTestId('saved-searches-duplicate-warning'),
      ).not.toBeInTheDocument();
    });
    expect(screen.getByTestId('saved-searches-confirm')).toBeEnabled();
  });

  it('Given the operator types a name that only differs by surrounding whitespace, When the dialog re-evaluates, Then the duplicate warning still appears (the backend trims before storing)', async () => {
    const user = userEvent.setup();
    renderBrowserPage();

    const input = await openSaveDialog(user);
    await user.type(input, '  Apples  ');

    expect(
      await screen.findByTestId('saved-searches-duplicate-warning'),
    ).toBeInTheDocument();
    expect(screen.getByTestId('saved-searches-confirm')).toBeDisabled();
  });

  it('Given the saved-search list is stale, When a POST returns 409 SavedSearchNameConflict, Then the dialog surfaces the duplicate warning instead of the raw error code', async () => {
    createResponseStatus = 409;
    const user = userEvent.setup();
    renderBrowserPage();

    const input = await openSaveDialog(user);
    // Type a name we treat as unique locally so the POST actually fires.
    await user.type(input, 'Bananas');
    await user.click(screen.getByTestId('saved-searches-confirm'));

    await waitFor(() => {
      expect(createCalls).toHaveLength(1);
    });
    expect(
      await screen.findByTestId('saved-searches-duplicate-warning'),
    ).toBeInTheDocument();
    // The raw error code must not leak through the friendly path.
    expect(
      screen.queryByText(/SavedSearchNameConflict/),
    ).not.toBeInTheDocument();
  });
});
