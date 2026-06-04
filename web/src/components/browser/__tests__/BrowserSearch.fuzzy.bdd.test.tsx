import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { BrowserPage } from '../BrowserPage';

// BDD — the Object Browser full-text search must expose Bleve fuzzy matching.
//
// Backend contract:
//   - pkg/oss/handlers.go:345 reads `?fuzziness=` off the /search request and
//     accepts 0, 1 or 2 (where.MaxFuzziness == 2); anything else is a 400
//     InvalidFuzziness. fuzziness=0 disables fuzzy matching.
//   - pkg/oss/service.go:30 carries the resolved *where.FuzzyConfig into the
//     Bleve MatchQuery (converter.go SetFuzziness).
//
// The SPA previously never sent `?fuzziness=`, so a search for "Kafca" could
// never match the indexed "kafka". These scenarios pin the user-visible
// contract: a fuzzy toggle in the search bar drives whether the /search
// request carries the `fuzziness` query param.

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

let capturedSearchUrl: string | null = null;

const server = setupServer(
  http.get('/api/v2/ontologies/:ontology/objects/:objectType', () =>
    HttpResponse.json({ data: [], totalCount: '0' }),
  ),
  http.post('/api/v2/ontologies/:ontology/objects/:objectType/search', ({ request }) => {
    capturedSearchUrl = request.url;
    return HttpResponse.json({
      data: [
        {
          __primaryKey: 'n-1',
          __apiName: 'AI_News',
          newsId: 'n-1',
          title: 'kafka ships X',
        },
      ],
      totalCount: '1',
    });
  }),
  http.post('/api/v2/ontologies/:ontology/objectSets/createTemporary', () =>
    HttpResponse.json({ objectSetRid: 'ri.objectset.x' }),
  ),
);

beforeAll(() => server.listen());
afterEach(() => {
  capturedSearchUrl = null;
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

function fuzzinessParam(rawUrl: string | null): string | null {
  if (!rawUrl) return null;
  return new URL(rawUrl).searchParams.get('fuzziness');
}

describe('BDD: Object Browser fuzzy full-text search', () => {
  it('Given fuzzy search is OFF (default), When the user searches, Then the /search request carries no fuzziness param', async () => {
    renderPage();
    const input = await screen.findByTestId('search-input');
    fireEvent.change(input, { target: { value: 'Kafca' } });
    fireEvent.keyDown(input, { key: 'Enter' });

    await waitFor(() => {
      expect(capturedSearchUrl).not.toBeNull();
    });
    expect(fuzzinessParam(capturedSearchUrl)).toBeNull();
  });

  it('Given the user enables fuzzy search, When they search, Then the /search request carries fuzziness=2', async () => {
    renderPage();

    const toggle = await screen.findByTestId('fuzzy-toggle');
    fireEvent.click(toggle);

    const input = await screen.findByTestId('search-input');
    fireEvent.change(input, { target: { value: 'Kafca' } });
    fireEvent.keyDown(input, { key: 'Enter' });

    await waitFor(() => {
      expect(capturedSearchUrl).not.toBeNull();
    });
    expect(fuzzinessParam(capturedSearchUrl)).toBe('2');
  });

  it('Given fuzzy was enabled then disabled, When the user searches, Then the fuzziness param is dropped again', async () => {
    renderPage();

    const toggle = await screen.findByTestId('fuzzy-toggle');
    fireEvent.click(toggle); // on
    fireEvent.click(toggle); // off

    const input = await screen.findByTestId('search-input');
    fireEvent.change(input, { target: { value: 'Kafca' } });
    fireEvent.keyDown(input, { key: 'Enter' });

    await waitFor(() => {
      expect(capturedSearchUrl).not.toBeNull();
    });
    expect(fuzzinessParam(capturedSearchUrl)).toBeNull();
  });
});
