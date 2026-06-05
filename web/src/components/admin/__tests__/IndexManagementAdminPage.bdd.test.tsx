import {
  describe,
  it,
  expect,
  beforeAll,
  afterAll,
  afterEach,
  beforeEach,
} from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { IndexManagementAdminPage } from '../IndexManagementAdminPage';
import { Toaster } from '../../common/Toaster';
import { useToastStore } from '../../../stores/toastStore';

// BDD: the Index Management admin page surfaces the heavyweight Bleve
// rebuild/reindex ops endpoints (cmd/server/admin_index.go,
// POST /api/admin/indexes/rebuild) that previously had no UI entry point.
// Each scenario asserts an externally observable behavior — what renders, the
// confirmation gate, and the HTTP method/path/body the page emits — so the
// wire contract and the "destructive op needs confirmation" guarantee are
// locked, not just component internals.

interface OntologyRow {
  rid: string;
  apiName: string;
  displayName: string;
}

interface ObjectTypeRow {
  rid: string;
  apiName: string;
  displayName: string;
  primaryKey: string;
  status: string;
  visibility: string;
}

interface RecordedCall {
  method: string;
  path: string;
  body: Record<string, unknown> | null;
}

const ONTOLOGIES: OntologyRow[] = [
  { rid: 'ri.o.1', apiName: 'northwind', displayName: 'Northwind' },
  { rid: 'ri.o.2', apiName: 'chinook', displayName: 'Chinook' },
];

const OBJECT_TYPES: Record<string, ObjectTypeRow[]> = {
  northwind: [
    {
      rid: 'ri.ot.cust',
      apiName: 'Customer',
      displayName: 'Customer',
      primaryKey: 'id',
      status: 'ACTIVE',
      visibility: 'NORMAL',
    },
    {
      rid: 'ri.ot.order',
      apiName: 'Order',
      displayName: 'Order',
      primaryKey: 'id',
      status: 'ACTIVE',
      visibility: 'NORMAL',
    },
  ],
  chinook: [],
};

let calls: RecordedCall[];
// failRebuild flips the POST handler to a 503 so the failure scenario can
// assert the toast surfaces describeApiError output.
let failRebuild = false;

const server = setupServer(
  http.get('/api/v2/ontologies', () => HttpResponse.json({ data: ONTOLOGIES })),

  http.get('/api/v2/ontologies/:ontology/objectTypes', ({ params }) => {
    const ontology = String(params.ontology);
    return HttpResponse.json({ data: OBJECT_TYPES[ontology] ?? [] });
  }),

  http.post('/api/admin/indexes/rebuild', async ({ request }) => {
    const body = (await request.json()) as Record<string, unknown>;
    calls.push({ method: 'POST', path: '/api/admin/indexes/rebuild', body });
    if (failRebuild) {
      return HttpResponse.json(
        {
          errorCode: 'SERVICE_UNAVAILABLE',
          errorName: 'IndexRebuildNotConfigured',
          errorInstanceId: 'x',
          parameters: { reason: 'Bleve backend not configured' },
        },
        { status: 503 },
      );
    }
    return HttpResponse.json({
      scopedKey: `${String(body.ontology)}:${String(body.objectType)}`,
      indexedCount: 42,
    });
  }),
);

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => {
  server.resetHandlers();
  useToastStore.getState().clear();
});
afterAll(() => server.close());

beforeEach(() => {
  calls = [];
  failRebuild = false;
});

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/admin/indexes']}>
        <IndexManagementAdminPage />
        <Toaster />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BDD: IndexManagementAdminPage', () => {
  it('renders the heading and an ontology selector populated from the API', async () => {
    renderPage();

    expect(
      await screen.findByRole('heading', { level: 1, name: /Index Management/i }),
    ).toBeInTheDocument();

    const select = (await screen.findByTestId(
      'index-ontology-select',
    )) as HTMLSelectElement;
    // Both ontologies are offered as options.
    await within(select).findByRole('option', { name: /Northwind/i });
    expect(
      within(select).getByRole('option', { name: /Chinook/i }),
    ).toBeInTheDocument();
  });

  it('lists the objectTypes for the selected ontology', async () => {
    const user = userEvent.setup();
    renderPage();

    const select = (await screen.findByTestId(
      'index-ontology-select',
    )) as HTMLSelectElement;
    await user.selectOptions(select, 'northwind');

    await screen.findByTestId('index-objecttype-row-Customer');
    expect(
      screen.getByTestId('index-objecttype-row-Order'),
    ).toBeInTheDocument();
  });

  it('shows an empty state when the ontology has no objectTypes', async () => {
    const user = userEvent.setup();
    renderPage();

    const select = (await screen.findByTestId(
      'index-ontology-select',
    )) as HTMLSelectElement;
    await user.selectOptions(select, 'chinook');

    expect(await screen.findByTestId('index-objecttype-empty')).toBeInTheDocument();
  });

  it('requires confirmation before rebuilding, then POSTs and shows the indexedCount', async () => {
    const user = userEvent.setup();
    renderPage();

    const select = (await screen.findByTestId(
      'index-ontology-select',
    )) as HTMLSelectElement;
    await user.selectOptions(select, 'northwind');

    const row = await screen.findByTestId('index-objecttype-row-Customer');
    await user.click(within(row).getByTestId('index-rebuild-btn'));

    // A confirmation modal opens — no request fires yet.
    await screen.findByTestId('index-rebuild-confirm-modal');
    expect(calls.find((c) => c.method === 'POST')).toBeUndefined();

    await user.click(screen.getByTestId('index-rebuild-confirm'));

    await waitFor(() => {
      expect(calls.find((c) => c.method === 'POST')).toBeTruthy();
    });
    const post = calls.find((c) => c.method === 'POST')!;
    expect(post.path).toBe('/api/admin/indexes/rebuild');
    expect(post.body).toMatchObject({
      ontology: 'northwind',
      objectType: 'Customer',
    });

    // The success toast carries the indexedCount returned by the server.
    const toaster = await screen.findByTestId('toaster');
    await within(toaster).findByText(/42/);
  });

  it('does not POST when the confirmation is cancelled', async () => {
    const user = userEvent.setup();
    renderPage();

    const select = (await screen.findByTestId(
      'index-ontology-select',
    )) as HTMLSelectElement;
    await user.selectOptions(select, 'northwind');

    const row = await screen.findByTestId('index-objecttype-row-Customer');
    await user.click(within(row).getByTestId('index-rebuild-btn'));

    await screen.findByTestId('index-rebuild-confirm-modal');
    await user.click(screen.getByTestId('index-rebuild-cancel'));

    await waitFor(() => {
      expect(
        screen.queryByTestId('index-rebuild-confirm-modal'),
      ).not.toBeInTheDocument();
    });
    expect(calls.find((c) => c.method === 'POST')).toBeUndefined();
  });

  it('surfaces an error toast when the rebuild fails', async () => {
    failRebuild = true;
    const user = userEvent.setup();
    renderPage();

    const select = (await screen.findByTestId(
      'index-ontology-select',
    )) as HTMLSelectElement;
    await user.selectOptions(select, 'northwind');

    const row = await screen.findByTestId('index-objecttype-row-Customer');
    await user.click(within(row).getByTestId('index-rebuild-btn'));
    await screen.findByTestId('index-rebuild-confirm-modal');
    await user.click(screen.getByTestId('index-rebuild-confirm'));

    const toaster = await screen.findByTestId('toaster');
    await within(toaster).findByText(/IndexRebuildNotConfigured/i);
    // POST was attempted exactly once.
    expect(calls.filter((c) => c.method === 'POST')).toHaveLength(1);
  });
});
