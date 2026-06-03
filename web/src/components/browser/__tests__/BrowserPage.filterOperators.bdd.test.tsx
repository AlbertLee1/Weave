import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { BrowserPage } from '../BrowserPage';

// Unit 3 BDD: the Browser FilterBuilder now exposes the full set of
// backend-supported Where operators. These scenarios pin the request contract
// end-to-end: when an operator+value is added via the UI and the user runs the
// query, the request body `where` carries the exact {type, field, value} that
// pkg/oss/where/converter.go expects. Critically, `isNull` must serialise its
// value as a JSON boolean (convertIsNull rejects strings), while
// `containsAllTerms` must serialise as a string.

vi.mock('../../../hooks/useObjectTypes', () => ({
  useObjectType: (_ontology: string, apiName: string) => ({
    data: apiName
      ? {
          rid: 'ri.ot.ainews.AI_News',
          apiName,
          displayName: apiName,
          pluralDisplayName: `${apiName}s`,
          primaryKey: 'newsId',
          titleProperty: 'title',
          status: 'ACTIVE',
          visibility: 'NORMAL',
          properties: {
            newsId: { dataType: { type: 'string' }, rid: 'ri.p.newsId' },
            title: { dataType: { type: 'string' }, rid: 'ri.p.title' },
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

let capturedSearchBody: unknown = null;

const server = setupServer(
  http.get('/api/v2/ontologies/:ontology/objects/:objectType', () =>
    HttpResponse.json({ data: [], totalCount: '0' }),
  ),
  http.post(
    '/api/v2/ontologies/:ontology/objects/:objectType/search',
    async ({ request }) => {
      capturedSearchBody = await request.json();
      return HttpResponse.json({ data: [], totalCount: '0' });
    },
  ),
  http.post('/api/v2/ontologies/:ontology/objectSets/createTemporary', () =>
    HttpResponse.json({ objectSetRid: 'ri.objectset.x' }),
  ),
);

beforeAll(() => server.listen());
afterEach(() => {
  capturedSearchBody = null;
  server.resetHandlers();
});
afterAll(() => server.close());

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/browser/ainews/AI_News']}>
        <Routes>
          <Route path="/browser/:ontology/:objectType" element={<BrowserPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

async function openFilters() {
  const toggle = await screen.findByTestId('toggle-filters');
  fireEvent.click(toggle);
  return screen.findByTestId('filter-op-select');
}

describe('BDD: BrowserPage filter operator request contract (Unit 3)', () => {
  it('Given isNull true is added via FilterBuilder, When the query runs, Then the request where carries a boolean value', async () => {
    renderPage();
    const opSelect = await openFilters();

    fireEvent.change(screen.getByTestId('filter-field-select'), {
      target: { value: 'title' },
    });
    fireEvent.change(opSelect, { target: { value: 'isNull' } });
    fireEvent.change(screen.getByTestId('filter-boolean-value-select'), {
      target: { value: 'true' },
    });
    fireEvent.click(screen.getByTestId('filter-add-btn'));

    await waitFor(() => {
      expect(capturedSearchBody).not.toBeNull();
    });

    const body = capturedSearchBody as {
      where?: { type?: string; field?: string; value?: unknown };
    };
    expect(body.where).toBeDefined();
    expect(body.where!.type).toBe('isNull');
    expect(body.where!.field).toBe('title');
    expect(body.where!.value).toBe(true);
    expect(typeof body.where!.value).toBe('boolean');
  });

  it('Given containsAllTerms is added via FilterBuilder, When the query runs, Then the request where carries a string value', async () => {
    renderPage();
    const opSelect = await openFilters();

    fireEvent.change(screen.getByTestId('filter-field-select'), {
      target: { value: 'title' },
    });
    fireEvent.change(opSelect, { target: { value: 'containsAllTerms' } });
    fireEvent.change(screen.getByTestId('filter-value-input'), {
      target: { value: 'machine learning' },
    });
    fireEvent.click(screen.getByTestId('filter-add-btn'));

    await waitFor(() => {
      expect(capturedSearchBody).not.toBeNull();
    });

    const body = capturedSearchBody as {
      where?: { type?: string; field?: string; value?: unknown };
    };
    expect(body.where).toBeDefined();
    expect(body.where!.type).toBe('containsAllTerms');
    expect(body.where!.field).toBe('title');
    expect(body.where!.value).toBe('machine learning');
    expect(typeof body.where!.value).toBe('string');
  });
});
