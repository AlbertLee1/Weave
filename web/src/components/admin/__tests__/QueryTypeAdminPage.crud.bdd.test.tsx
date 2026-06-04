import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { QueryTypeAdminPage } from '../QueryTypeAdminPage';

// BDD: the QueryType admin CRUD page drives create / edit / delete through the
// V2 ontology routes that this batch newly mounted in cmd/server/routes.go:
//
//   POST   /api/v2/ontologies/{o}/queryTypes
//   PUT    /api/v2/ontologies/{o}/queryTypes/byRid/{rid}
//   DELETE /api/v2/ontologies/{o}/queryTypes/byRid/{rid}
//
// Each scenario asserts the request method + path + body the page emits so the
// wire contract (not just the rendered UI) is locked.

interface StoredQueryType {
  rid: string;
  apiName: string;
  displayName: string;
  description?: string;
  parameters?: unknown;
  output?: unknown;
  query?: unknown;
  status: string;
}

interface Call {
  method: string;
  path: string;
  body: Record<string, unknown> | null;
}

interface StubState {
  queryTypes: StoredQueryType[];
  calls: Call[];
}

const SEED: StoredQueryType[] = [
  {
    rid: 'ri.ontology.main.query-type.existing',
    apiName: 'openOrders',
    displayName: 'Open Orders',
    description: 'orders not yet shipped',
    parameters: [],
    output: {},
    query: { kind: 'filter' },
    status: 'ACTIVE',
  },
];

function makeStub(): StubState {
  return { queryTypes: JSON.parse(JSON.stringify(SEED)), calls: [] };
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function installFetch(state: StubState) {
  vi.stubGlobal(
    'fetch',
    vi.fn(
      async (
        input: RequestInfo | URL,
        init?: RequestInit,
      ): Promise<Response> => {
        const raw = typeof input === 'string' ? input : input.toString();
        const path = raw.replace(/^https?:\/\/[^/]+/, '');
        const method = (init?.method ?? 'GET').toUpperCase();
        const body = init?.body
          ? (JSON.parse(init.body as string) as Record<string, unknown>)
          : null;
        state.calls.push({ method, path, body });

        // LIST
        if (
          method === 'GET' &&
          /\/api\/v2\/ontologies\/northwind\/queryTypes$/.test(path)
        ) {
          return jsonResponse({ data: state.queryTypes });
        }

        // CREATE
        if (
          method === 'POST' &&
          /\/api\/v2\/ontologies\/northwind\/queryTypes$/.test(path)
        ) {
          const created: StoredQueryType = {
            rid: `ri.ontology.main.query-type.${body?.apiName}`,
            apiName: String(body?.apiName ?? ''),
            displayName: String(body?.displayName ?? ''),
            description: body?.description as string | undefined,
            parameters: body?.parameters,
            output: body?.output,
            query: body?.query,
            status: String(body?.status ?? 'ACTIVE'),
          };
          state.queryTypes.push(created);
          return jsonResponse(created, 201);
        }

        // UPDATE / DELETE by RID
        const ridMatch = path.match(
          /\/api\/v2\/ontologies\/northwind\/queryTypes\/byRid\/([^?]+)/,
        );
        if (ridMatch) {
          const rid = decodeURIComponent(ridMatch[1]);
          const idx = state.queryTypes.findIndex((q) => q.rid === rid);
          if (method === 'PUT') {
            if (idx < 0) return jsonResponse({ errorCode: 'NotFound' }, 404);
            const prev = state.queryTypes[idx];
            state.queryTypes[idx] = {
              ...prev,
              displayName: (body?.displayName as string) ?? prev.displayName,
              description:
                (body?.description as string | undefined) ?? prev.description,
              status: (body?.status as string) ?? prev.status,
            };
            return jsonResponse(state.queryTypes[idx]);
          }
          if (method === 'DELETE') {
            if (idx >= 0) state.queryTypes.splice(idx, 1);
            return new Response(null, { status: 204 });
          }
        }

        return new Response('{}', { status: 200 });
      },
    ),
  );
}

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchInterval: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/admin/northwind/queryTypes']}>
        <Routes>
          <Route
            path="/admin/:ontology/queryTypes"
            element={<QueryTypeAdminPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('QueryTypeAdminPage CRUD', () => {
  let state: StubState;

  beforeEach(() => {
    state = makeStub();
    installFetch(state);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('lists existing query types', async () => {
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Open Orders')).toBeInTheDocument();
    });
  });

  it('creates a query type via POST with the core fields', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Open Orders')).toBeInTheDocument();
    });

    await user.click(screen.getByTestId('query-type-new-btn'));
    await screen.findByTestId('query-type-create-form');

    await user.type(screen.getByTestId('query-type-apiName'), 'topCustomers');
    await user.type(
      screen.getByTestId('query-type-displayName'),
      'Top Customers',
    );
    await user.type(
      screen.getByTestId('query-type-description'),
      'ranked by spend',
    );

    await user.click(screen.getByTestId('query-type-submit'));

    await waitFor(() => {
      const post = state.calls.find(
        (c) =>
          c.method === 'POST' &&
          c.path === '/api/v2/ontologies/northwind/queryTypes',
      );
      expect(post).toBeTruthy();
    });

    const post = state.calls.find(
      (c) =>
        c.method === 'POST' &&
        c.path === '/api/v2/ontologies/northwind/queryTypes',
    )!;
    expect(post.body).toMatchObject({
      apiName: 'topCustomers',
      displayName: 'Top Customers',
      description: 'ranked by spend',
      status: 'ACTIVE',
    });
  });

  it('edits a query type via PUT byRid (apiName immutable)', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Open Orders')).toBeInTheDocument();
    });

    const row = screen.getByText('Open Orders').closest('tr')!;
    await user.click(within(row).getByRole('button', { name: /Edit/i }));
    await screen.findByTestId('query-type-edit-form');

    // apiName field is disabled on edit (immutable).
    const apiNameInput = screen.getByTestId(
      'query-type-apiName',
    ) as HTMLInputElement;
    expect(apiNameInput.disabled).toBe(true);

    const displayNameInput = screen.getByTestId('query-type-displayName');
    await user.clear(displayNameInput);
    await user.type(displayNameInput, 'Open Orders (Revised)');

    await user.click(screen.getByTestId('query-type-submit'));

    await waitFor(() => {
      const put = state.calls.find((c) => c.method === 'PUT');
      expect(put).toBeTruthy();
    });

    const put = state.calls.find((c) => c.method === 'PUT')!;
    expect(put.path).toBe(
      '/api/v2/ontologies/northwind/queryTypes/byRid/ri.ontology.main.query-type.existing',
    );
    expect(put.body).toMatchObject({ displayName: 'Open Orders (Revised)' });
    expect(put.body).not.toHaveProperty('apiName');
  });

  it('deletes a query type via DELETE byRid', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Open Orders')).toBeInTheDocument();
    });

    const row = screen.getByText('Open Orders').closest('tr')!;
    await user.click(within(row).getByRole('button', { name: /Delete/i }));
    await screen.findByTestId('query-type-delete-confirm');

    await user.click(screen.getByTestId('query-type-delete-confirm-btn'));

    await waitFor(() => {
      const del = state.calls.find((c) => c.method === 'DELETE');
      expect(del).toBeTruthy();
    });

    const del = state.calls.find((c) => c.method === 'DELETE')!;
    expect(del.path).toBe(
      '/api/v2/ontologies/northwind/queryTypes/byRid/ri.ontology.main.query-type.existing',
    );
  });
});
