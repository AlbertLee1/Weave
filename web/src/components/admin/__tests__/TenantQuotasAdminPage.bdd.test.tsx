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
import { TenantQuotasAdminPage } from '../TenantQuotasAdminPage';
import { Toaster } from '../../common/Toaster';
import { useToastStore } from '../../../stores/toastStore';

// BDD: the Tenant Quotas admin page surfaces the multi-tenant quota CRUD +
// usage view (pkg/tenants/handlers.go, US-277 / US-438) that previously had no
// UI entry point. Each scenario asserts an externally observable behavior —
// what renders, and the HTTP method/path/body the page emits — so the wire
// contract is locked, not just the component internals.

interface StoredQuota {
  tenant: string;
  maxObjects: number;
  maxStorage: number;
  maxQPS: number;
  burst: number;
  description: string;
  createdAt: string;
  updatedAt: string;
}

interface StoredUsage {
  tenant: string;
  month: string;
  metric: string;
  amount: number;
  cap: number;
  percent: number;
}

interface RecordedCall {
  method: string;
  path: string;
  body: Record<string, unknown> | null;
}

const SEED: StoredQuota[] = [
  {
    tenant: 'acme',
    maxObjects: 100000,
    maxStorage: 5000000,
    maxQPS: 50,
    burst: 100,
    description: 'Acme Corp production tenant',
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-02T00:00:00Z',
  },
  {
    tenant: 'globex',
    maxObjects: 20000,
    maxStorage: 1000000,
    maxQPS: 10,
    burst: 20,
    description: 'Globex sandbox',
    createdAt: '2026-02-01T00:00:00Z',
    updatedAt: '2026-02-01T00:00:00Z',
  },
];

const USAGE: Record<string, StoredUsage[]> = {
  acme: [
    {
      tenant: 'acme',
      month: '2026-06-01',
      metric: 'objects',
      amount: 91000,
      cap: 100000,
      percent: 91,
    },
    {
      tenant: 'acme',
      month: '2026-06-01',
      metric: 'storage',
      amount: 2500000,
      cap: 5000000,
      percent: 50,
    },
  ],
  globex: [],
};

let quotas: StoredQuota[];
let calls: RecordedCall[];
// failCreate flips the POST handler to a 409 so the create-error scenario can
// assert the toast surfaces describeApiError output.
let failCreate = false;

const server = setupServer(
  http.get('/api/admin/tenant-quotas', () => HttpResponse.json({ quotas })),

  http.post('/api/admin/tenant-quotas', async ({ request }) => {
    const body = (await request.json()) as Record<string, unknown>;
    calls.push({ method: 'POST', path: '/api/admin/tenant-quotas', body });
    if (failCreate) {
      return HttpResponse.json(
        {
          errorCode: 'CONFLICT',
          errorName: 'TenantQuotaConflict',
          errorInstanceId: 'x',
          parameters: { tenant: String(body.tenant ?? '') },
        },
        { status: 409 },
      );
    }
    const created: StoredQuota = {
      tenant: String(body.tenant ?? ''),
      maxObjects: Number(body.maxObjects ?? 0),
      maxStorage: Number(body.maxStorage ?? 0),
      maxQPS: Number(body.maxQPS ?? 0),
      burst: Number(body.burst ?? 0),
      description: String(body.description ?? ''),
      createdAt: '2026-03-01T00:00:00Z',
      updatedAt: '2026-03-01T00:00:00Z',
    };
    quotas = [...quotas, created];
    return HttpResponse.json(created, { status: 201 });
  }),

  http.put('/api/admin/tenant-quotas/:tenant', async ({ params, request }) => {
    const tenant = String(params.tenant);
    const body = (await request.json()) as Record<string, unknown>;
    calls.push({
      method: 'PUT',
      path: `/api/admin/tenant-quotas/${tenant}`,
      body,
    });
    const idx = quotas.findIndex((q) => q.tenant === tenant);
    if (idx >= 0) {
      const next = { ...quotas[idx] };
      if (body.maxObjects !== undefined) next.maxObjects = Number(body.maxObjects);
      if (body.maxStorage !== undefined) next.maxStorage = Number(body.maxStorage);
      if (body.maxQPS !== undefined) next.maxQPS = Number(body.maxQPS);
      if (body.burst !== undefined) next.burst = Number(body.burst);
      if (body.description !== undefined) next.description = String(body.description);
      quotas[idx] = next;
      return HttpResponse.json(next);
    }
    return new HttpResponse(null, { status: 404 });
  }),

  http.delete('/api/admin/tenant-quotas/:tenant', ({ params }) => {
    const tenant = String(params.tenant);
    calls.push({
      method: 'DELETE',
      path: `/api/admin/tenant-quotas/${tenant}`,
      body: null,
    });
    quotas = quotas.filter((q) => q.tenant !== tenant);
    return new HttpResponse(null, { status: 204 });
  }),

  http.get('/api/admin/tenant-usage/:tenant', ({ params }) => {
    const tenant = String(params.tenant);
    return HttpResponse.json({ usage: USAGE[tenant] ?? [] });
  }),
);

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => {
  server.resetHandlers();
  useToastStore.getState().clear();
});
afterAll(() => server.close());

beforeEach(() => {
  quotas = JSON.parse(JSON.stringify(SEED)) as StoredQuota[];
  calls = [];
  failCreate = false;
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
      <MemoryRouter initialEntries={['/admin/tenant-quotas']}>
        <TenantQuotasAdminPage />
        <Toaster />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BDD: TenantQuotasAdminPage', () => {
  it('renders the quota list with per-tenant caps', async () => {
    renderPage();

    expect(
      await screen.findByRole('heading', { level: 1, name: /Tenant Quotas/i }),
    ).toBeInTheDocument();

    await screen.findByText('acme');
    await screen.findByText('globex');

    const acmeRow = screen.getByTestId('quota-row-acme');
    expect(within(acmeRow).getByText('Acme Corp production tenant')).toBeInTheDocument();
    // maxQPS appears in the row.
    expect(within(acmeRow).getByText('50')).toBeInTheDocument();
  });

  it('shows an empty state when no quotas exist', async () => {
    quotas = [];
    renderPage();
    expect(await screen.findByTestId('quotas-empty')).toBeInTheDocument();
  });

  it('creates a quota via POST and refreshes the list', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText('acme');

    await user.click(screen.getByTestId('quota-new-btn'));
    await screen.findByTestId('quota-create-form');

    await user.type(screen.getByTestId('quota-tenant-input'), 'initech');
    await user.type(screen.getByTestId('quota-maxObjects-input'), '5000');
    await user.type(screen.getByTestId('quota-maxStorage-input'), '250000');
    await user.type(screen.getByTestId('quota-maxQPS-input'), '5');
    await user.type(screen.getByTestId('quota-burst-input'), '10');
    await user.type(
      screen.getByTestId('quota-description-input'),
      'Initech trial',
    );
    await user.click(screen.getByTestId('quota-create-submit'));

    await waitFor(() => {
      expect(calls.find((c) => c.method === 'POST')).toBeTruthy();
    });
    const post = calls.find((c) => c.method === 'POST')!;
    expect(post.path).toBe('/api/admin/tenant-quotas');
    expect(post.body).toMatchObject({
      tenant: 'initech',
      maxObjects: 5000,
      maxStorage: 250000,
      maxQPS: 5,
      burst: 10,
      description: 'Initech trial',
    });

    // List refreshes to include the new tenant.
    await screen.findByText('initech');
  });

  it('rejects negative numeric input on the create form', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText('acme');

    await user.click(screen.getByTestId('quota-new-btn'));
    await screen.findByTestId('quota-create-form');

    await user.type(screen.getByTestId('quota-tenant-input'), 'badtenant');
    await user.type(screen.getByTestId('quota-maxObjects-input'), '-5');
    await user.click(screen.getByTestId('quota-create-submit'));

    // Validation blocks the request — no POST is fired.
    expect(screen.getByTestId('quota-form-error')).toBeInTheDocument();
    expect(calls.find((c) => c.method === 'POST')).toBeUndefined();
  });

  it('edits a quota via PUT and refreshes the list', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText('globex');

    await user.click(
      within(screen.getByTestId('quota-row-globex')).getByTestId(
        'quota-edit-btn',
      ),
    );
    await screen.findByTestId('quota-edit-form');

    const maxQps = screen.getByTestId('quota-maxQPS-input') as HTMLInputElement;
    await user.clear(maxQps);
    await user.type(maxQps, '25');
    await user.click(screen.getByTestId('quota-edit-submit'));

    await waitFor(() => {
      expect(calls.find((c) => c.method === 'PUT')).toBeTruthy();
    });
    const put = calls.find((c) => c.method === 'PUT')!;
    expect(put.path).toBe('/api/admin/tenant-quotas/globex');
    expect(put.body).toMatchObject({ maxQPS: 25 });
  });

  it('requires confirmation before deleting a quota and then sends DELETE', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText('globex');

    await user.click(
      within(screen.getByTestId('quota-row-globex')).getByTestId(
        'quota-delete-btn',
      ),
    );

    // A confirmation modal opens, not a direct request.
    await screen.findByTestId('quota-delete-modal');
    expect(calls.find((c) => c.method === 'DELETE')).toBeUndefined();

    await user.click(screen.getByTestId('quota-delete-confirm'));
    await waitFor(() => {
      expect(calls.find((c) => c.method === 'DELETE')).toBeTruthy();
    });
    const del = calls.find((c) => c.method === 'DELETE')!;
    expect(del.path).toBe('/api/admin/tenant-quotas/globex');
  });

  it('shows monthly usage with a percent bar when a tenant is selected', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText('acme');

    await user.click(
      within(screen.getByTestId('quota-row-acme')).getByTestId(
        'quota-select-btn',
      ),
    );

    // Usage rows for acme render with amount/cap/percent.
    await screen.findByTestId('usage-row-objects');
    const objectsRow = screen.getByTestId('usage-row-objects');
    expect(within(objectsRow).getByText(/91%/)).toBeInTheDocument();

    // The >=80% metric carries the warning flag; the <80% one does not.
    expect(objectsRow.getAttribute('data-warn')).toBe('true');
    const storageRow = screen.getByTestId('usage-row-storage');
    expect(storageRow.getAttribute('data-warn')).toBe('false');
  });

  it('shows an empty usage state for a tenant with no usage rows', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText('globex');

    await user.click(
      within(screen.getByTestId('quota-row-globex')).getByTestId(
        'quota-select-btn',
      ),
    );

    expect(await screen.findByTestId('usage-empty')).toBeInTheDocument();
  });

  it('surfaces a toast when quota creation fails', async () => {
    failCreate = true;
    const user = userEvent.setup();
    renderPage();
    await screen.findByText('acme');

    await user.click(screen.getByTestId('quota-new-btn'));
    await screen.findByTestId('quota-create-form');
    await user.type(screen.getByTestId('quota-tenant-input'), 'acme');
    await user.type(screen.getByTestId('quota-maxObjects-input'), '1');
    await user.type(screen.getByTestId('quota-maxStorage-input'), '1');
    await user.type(screen.getByTestId('quota-maxQPS-input'), '1');
    await user.type(screen.getByTestId('quota-burst-input'), '1');
    await user.click(screen.getByTestId('quota-create-submit'));

    const toaster = await screen.findByTestId('toaster');
    expect(within(toaster).getByText(/TenantQuotaConflict/i)).toBeInTheDocument();
  });
});
