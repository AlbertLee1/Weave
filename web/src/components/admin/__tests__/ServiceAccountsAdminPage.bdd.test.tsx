import {
  describe,
  it,
  expect,
  beforeAll,
  afterAll,
  afterEach,
  vi,
} from 'vitest';
import { createElement } from 'react';
import { render, screen, within, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { ServiceAccountsAdminPage } from '../ServiceAccountsAdminPage';

// BDD: the global Service Accounts admin page drives the CRUD surface that the
// backend already exposes under /api/admin/service-accounts (see
// pkg/auth/service_account_handlers.go). Each scenario asserts both the
// rendered behaviour and — for mutations — the exact HTTP method/path/body the
// SPA fires, so the wire contract is locked from the outside.

interface StoredServiceAccount {
  id: string;
  name: string;
  description: string;
  ownerUserId: string;
  scopes: string[];
  expiresAt?: string | null;
  disabledAt?: string | null;
  createdAt: string;
  updatedAt: string;
}

const ACTIVE: StoredServiceAccount = {
  id: 'sa-active',
  name: 'ci-runner',
  description: 'CI pipeline identity',
  ownerUserId: 'user:alice',
  scopes: ['ontology.read', 'action.apply'],
  expiresAt: '2027-01-01T00:00:00Z',
  disabledAt: null,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-02-01T00:00:00Z',
};

const DISABLED: StoredServiceAccount = {
  id: 'sa-disabled',
  name: 'legacy-bot',
  description: 'retired integration',
  ownerUserId: 'user:bob',
  scopes: [],
  expiresAt: null,
  disabledAt: '2026-03-01T00:00:00Z',
  createdAt: '2025-06-01T00:00:00Z',
  updatedAt: '2026-03-01T00:00:00Z',
};

interface Call {
  method: string;
  path: string;
  body: Record<string, unknown> | null;
}

let accounts: StoredServiceAccount[] = [];
let calls: Call[] = [];
let failCreate = false;

const server = setupServer(
  http.get('/api/admin/service-accounts', () =>
    HttpResponse.json({ serviceAccounts: accounts }),
  ),
  http.post('/api/admin/service-accounts', async ({ request }) => {
    const body = (await request.json()) as Record<string, unknown>;
    calls.push({ method: 'POST', path: '/api/admin/service-accounts', body });
    if (failCreate) {
      return HttpResponse.json(
        {
          errorCode: 'CONFLICT',
          errorName: 'ServiceAccountNameConflict',
          parameters: { reason: 'name already taken' },
        },
        { status: 409 },
      );
    }
    const created: StoredServiceAccount = {
      id: `sa-${String(body.name)}`,
      name: String(body.name ?? ''),
      description: String(body.description ?? ''),
      ownerUserId: 'user:alice',
      scopes: (body.scopes as string[] | undefined) ?? [],
      expiresAt: (body.expiresAt as string | undefined) ?? null,
      disabledAt: null,
      createdAt: '2026-04-01T00:00:00Z',
      updatedAt: '2026-04-01T00:00:00Z',
    };
    accounts = [...accounts, created];
    return HttpResponse.json(created, { status: 201 });
  }),
  http.patch('/api/admin/service-accounts/:id', async ({ request, params }) => {
    const id = String(params.id);
    const body = (await request.json()) as Record<string, unknown>;
    calls.push({
      method: 'PATCH',
      path: `/api/admin/service-accounts/${id}`,
      body,
    });
    const idx = accounts.findIndex((a) => a.id === id);
    if (idx < 0) return HttpResponse.json({}, { status: 404 });
    const prev = accounts[idx];
    const next: StoredServiceAccount = {
      ...prev,
      description:
        body.description !== undefined
          ? String(body.description)
          : prev.description,
      scopes:
        body.scopes !== undefined ? (body.scopes as string[]) : prev.scopes,
      expiresAt:
        body.expiresAt !== undefined
          ? (body.expiresAt as string) || null
          : prev.expiresAt,
    };
    accounts = accounts.map((a, i) => (i === idx ? next : a));
    return HttpResponse.json(next, { status: 200 });
  }),
  http.delete('/api/admin/service-accounts/:id', ({ params }) => {
    const id = String(params.id);
    calls.push({
      method: 'DELETE',
      path: `/api/admin/service-accounts/${id}`,
      body: null,
    });
    accounts = accounts.filter((a) => a.id !== id);
    return new HttpResponse(null, { status: 204 });
  }),
);

beforeAll(() => server.listen({ onUnhandledRequest: 'bypass' }));
afterEach(() => {
  server.resetHandlers();
  accounts = [];
  calls = [];
  failCreate = false;
  vi.restoreAllMocks();
});
afterAll(() => server.close());

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
  return render(
    createElement(
      QueryClientProvider,
      { client: qc },
      createElement(
        MemoryRouter,
        { initialEntries: ['/admin/service-accounts'] },
        createElement(ServiceAccountsAdminPage, null),
      ),
    ),
  );
}

describe('BDD: ServiceAccountsAdminPage', () => {
  it('Given accounts exist, Then the list renders with a Disabled badge on soft-disabled rows', async () => {
    accounts = [structuredClone(ACTIVE), structuredClone(DISABLED)];
    renderPage();

    // The page exposes an h1 landmark.
    expect(
      await screen.findByRole('heading', { level: 1, name: /service accounts/i }),
    ).toBeInTheDocument();

    const activeRow = await screen.findByTestId('service-account-row-sa-active');
    expect(within(activeRow).getByText('ci-runner')).toBeInTheDocument();
    expect(
      within(activeRow).getByText('CI pipeline identity'),
    ).toBeInTheDocument();
    expect(within(activeRow).getByText(/ontology\.read/)).toBeInTheDocument();
    // Active row carries no Disabled badge.
    expect(
      within(activeRow).queryByTestId('service-account-disabled-badge'),
    ).toBeNull();

    const disabledRow = await screen.findByTestId(
      'service-account-row-sa-disabled',
    );
    expect(
      within(disabledRow).getByTestId('service-account-disabled-badge'),
    ).toHaveTextContent(/disabled/i);
  });

  it('Given no accounts, Then an empty state is shown', async () => {
    accounts = [];
    renderPage();
    expect(
      await screen.findByTestId('service-accounts-empty'),
    ).toBeInTheDocument();
  });

  it('When the create form is submitted, Then a POST is sent with parsed scopes and the list refreshes', async () => {
    accounts = [];
    const user = userEvent.setup();
    renderPage();

    await screen.findByTestId('service-accounts-empty');

    await user.click(screen.getByTestId('service-account-new-btn'));
    await screen.findByTestId('service-account-create-form');

    await user.type(screen.getByTestId('service-account-name'), 'deploy-bot');
    await user.type(
      screen.getByTestId('service-account-description'),
      'deploy pipeline',
    );
    await user.type(
      screen.getByTestId('service-account-scopes'),
      'ontology.read, action.apply',
    );

    await user.click(screen.getByTestId('service-account-create-submit'));

    await waitFor(() => {
      const post = calls.find((c) => c.method === 'POST');
      expect(post).toBeTruthy();
    });
    const post = calls.find((c) => c.method === 'POST')!;
    expect(post.path).toBe('/api/admin/service-accounts');
    expect(post.body).toMatchObject({
      name: 'deploy-bot',
      description: 'deploy pipeline',
      scopes: ['ontology.read', 'action.apply'],
    });

    // The new account appears after the invalidate-and-refetch loop.
    await waitFor(() =>
      expect(
        screen.getByTestId('service-account-row-sa-deploy-bot'),
      ).toBeInTheDocument(),
    );
  });

  it('When delete is confirmed through the second-step modal, Then DELETE is sent', async () => {
    accounts = [structuredClone(ACTIVE)];
    const user = userEvent.setup();
    renderPage();

    const row = await screen.findByTestId('service-account-row-sa-active');
    await user.click(within(row).getByTestId('service-account-delete-btn'));

    // A confirmation modal appears; nothing fired yet.
    await screen.findByTestId('service-account-delete-confirm');
    expect(calls.find((c) => c.method === 'DELETE')).toBeFalsy();

    await user.click(screen.getByTestId('service-account-delete-confirm-btn'));

    await waitFor(() => {
      const del = calls.find((c) => c.method === 'DELETE');
      expect(del).toBeTruthy();
    });
    expect(calls.find((c) => c.method === 'DELETE')!.path).toBe(
      '/api/admin/service-accounts/sa-active',
    );
    await waitFor(() =>
      expect(
        screen.queryByTestId('service-account-row-sa-active'),
      ).toBeNull(),
    );
  });

  it('When the edit form is submitted, Then PATCH is sent with updated fields (name immutable, untouched expiry preserved)', async () => {
    // A non-midnight expiry: the date input only carries day precision, so the
    // page must NOT re-send (and thereby truncate) an expiry the operator did
    // not touch.
    const withTimeExpiry = {
      ...structuredClone(ACTIVE),
      expiresAt: '2027-01-01T14:30:00Z',
    };
    accounts = [withTimeExpiry];
    const user = userEvent.setup();
    renderPage();

    const row = await screen.findByTestId('service-account-row-sa-active');
    await user.click(within(row).getByTestId('service-account-edit-btn'));
    await screen.findByTestId('service-account-edit-form');

    const desc = screen.getByTestId('service-account-description');
    await user.clear(desc);
    await user.type(desc, 'rotated identity');

    await user.click(screen.getByTestId('service-account-edit-submit'));

    await waitFor(() => {
      const patch = calls.find((c) => c.method === 'PATCH');
      expect(patch).toBeTruthy();
    });
    const patch = calls.find((c) => c.method === 'PATCH')!;
    expect(patch.path).toBe('/api/admin/service-accounts/sa-active');
    expect(patch.body).toMatchObject({ description: 'rotated identity' });
    // name is immutable: the PATCH body never carries it.
    expect(patch.body).not.toHaveProperty('name');
    // The untouched expiry is preserved (not truncated to midnight): omit it
    // so the backend keeps the stored timestamp.
    expect(patch.body).not.toHaveProperty('expiresAt');
  });

  it('When the expiry date is changed on edit, Then PATCH carries the new RFC3339 expiresAt', async () => {
    const withTimeExpiry = {
      ...structuredClone(ACTIVE),
      expiresAt: '2027-01-01T14:30:00Z',
    };
    accounts = [withTimeExpiry];
    const user = userEvent.setup();
    renderPage();

    const row = await screen.findByTestId('service-account-row-sa-active');
    await user.click(within(row).getByTestId('service-account-edit-btn'));
    await screen.findByTestId('service-account-edit-form');

    const expires = screen.getByTestId(
      'service-account-expires-at',
    ) as HTMLInputElement;
    await user.clear(expires);
    await user.type(expires, '2028-06-15');

    await user.click(screen.getByTestId('service-account-edit-submit'));

    await waitFor(() => {
      const patch = calls.find((c) => c.method === 'PATCH');
      expect(patch).toBeTruthy();
    });
    const patch = calls.find((c) => c.method === 'PATCH')!;
    expect(patch.body).toMatchObject({
      expiresAt: '2028-06-15T00:00:00.000Z',
    });
  });

  it('When create fails, Then an error is surfaced and the modal stays open', async () => {
    accounts = [];
    failCreate = true;
    const user = userEvent.setup();
    renderPage();

    await screen.findByTestId('service-accounts-empty');
    await user.click(screen.getByTestId('service-account-new-btn'));
    await screen.findByTestId('service-account-create-form');

    await user.type(screen.getByTestId('service-account-name'), 'dupe');
    await user.click(screen.getByTestId('service-account-create-submit'));

    expect(
      await screen.findByTestId('service-account-create-error'),
    ).toHaveTextContent(/conflict|taken|ServiceAccountNameConflict/i);
    // The form is still mounted so the operator can correct and retry.
    expect(screen.getByTestId('service-account-create-form')).toBeInTheDocument();
  });
});
