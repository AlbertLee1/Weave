import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { BrowserPage } from '../BrowserPage';

// DOG-004 BDD: the dogfood operator typed "OpenAI" on /browser/ainews/AI_News
// and got `INVALID_ARGUMENT: SearchObjectsFailed` because BrowserPage sent
// containsAnyTerm.value as an array but the backend expects a string. These
// scenarios pin the contract end-to-end:
//   - the request body sent to /search uses containsAnyTerm.value : string;
//   - the error overlay surfaces the backend `parameters.reason` when an
//     error does come back, not just the generic `errorCode: errorName`.

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
  http.post('/api/v2/ontologies/:ontology/objects/:objectType/search', async ({ request }) => {
    capturedSearchBody = await request.json();
    return HttpResponse.json({
      data: [
        { __primaryKey: 'n-1', __apiName: 'AI_News', newsId: 'n-1', title: 'OpenAI ships X' },
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

describe('BDD: BrowserPage full-text search contract (DOG-004)', () => {
  it('Given the user types OpenAI and presses Enter, When the request is sent, Then containsAnyTerm.value is a string', async () => {
    renderPage();
    const input = await screen.findByTestId('search-input');
    fireEvent.change(input, { target: { value: 'OpenAI' } });
    fireEvent.keyDown(input, { key: 'Enter' });

    await waitFor(() => {
      expect(capturedSearchBody).not.toBeNull();
    });

    const body = capturedSearchBody as { where?: { type?: string; field?: string; value?: unknown } };
    expect(body.where).toBeDefined();
    expect(body.where!.type).toBe('containsAnyTerm');
    expect(body.where!.field).toBe('title');
    expect(typeof body.where!.value).toBe('string');
    expect(body.where!.value).toBe('OpenAI');
  });

  it('Given a multi-word search OpenAI Codex, When submitted, Then the request value is the space-joined string', async () => {
    renderPage();
    const input = await screen.findByTestId('search-input');
    fireEvent.change(input, { target: { value: '  OpenAI   Codex  ' } });
    fireEvent.keyDown(input, { key: 'Enter' });

    await waitFor(() => {
      expect(capturedSearchBody).not.toBeNull();
    });
    const body = capturedSearchBody as { where?: { value?: unknown } };
    expect(body.where!.value).toBe('OpenAI Codex');
  });

  it('Given the search API returns a structured error, When BrowserPage renders the overlay, Then it includes the backend parameters.reason alongside the errorCode/errorName summary', async () => {
    server.use(
      http.post('/api/v2/ontologies/:ontology/objects/:objectType/search', () =>
        HttpResponse.json(
          {
            errorCode: 'INVALID_ARGUMENT',
            errorName: 'SearchObjectsFailed',
            errorInstanceId: 'err-1',
            parameters: { reason: 'containsAnyTerm value must be a string' },
            statusCode: 400,
          },
          { status: 400 },
        ),
      ),
    );

    renderPage();
    const input = await screen.findByTestId('search-input');
    fireEvent.change(input, { target: { value: 'OpenAI' } });
    fireEvent.keyDown(input, { key: 'Enter' });

    const errorEl = await screen.findByTestId('browser-error');
    expect(errorEl.textContent).toMatch(/INVALID_ARGUMENT: SearchObjectsFailed/);
    expect(errorEl.textContent).toMatch(/containsAnyTerm value must be a string/);
  });
});
