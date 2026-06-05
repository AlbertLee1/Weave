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
import { FeatureFlagsAdminPage } from '../FeatureFlagsAdminPage';
import { Toaster } from '../../common/Toaster';
import { useToastStore } from '../../../stores/toastStore';

// BDD: the Feature Flags admin page surfaces the named-flag CRUD
// (pkg/featureflags/handlers.go, US-276) that previously had no UI entry
// point. Each scenario asserts an externally observable behavior — what
// renders, and the HTTP method/path/body the page emits — so the wire
// contract is locked, not just the component internals.

interface StoredFlag {
  name: string;
  description: string;
  enabled: boolean;
  realms: string[];
  users: string[];
  createdAt: string;
  updatedAt: string;
}

interface RecordedCall {
  method: string;
  path: string;
  body: Record<string, unknown> | null;
}

const SEED: StoredFlag[] = [
  {
    name: 'new-search',
    description: 'Rolls out the rewritten search backend',
    enabled: true,
    realms: ['prod', 'staging'],
    users: ['alice'],
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-02T00:00:00Z',
  },
  {
    name: 'beta-graph',
    description: 'Graph workbench beta',
    enabled: false,
    realms: [],
    users: ['bob', 'carol'],
    createdAt: '2026-02-01T00:00:00Z',
    updatedAt: '2026-02-01T00:00:00Z',
  },
];

let flags: StoredFlag[];
let calls: RecordedCall[];
// failCreate flips the POST handler to a 409 so the create-error scenario can
// assert the toast surfaces describeApiError output.
let failCreate = false;

const server = setupServer(
  http.get('/api/admin/feature-flags', () => HttpResponse.json({ flags })),

  http.post('/api/admin/feature-flags', async ({ request }) => {
    const body = (await request.json()) as Record<string, unknown>;
    calls.push({ method: 'POST', path: '/api/admin/feature-flags', body });
    if (failCreate) {
      return HttpResponse.json(
        {
          errorCode: 'CONFLICT',
          errorName: 'FeatureFlagAlreadyExists',
          errorInstanceId: 'x',
          parameters: { name: String(body.name ?? '') },
        },
        { status: 409 },
      );
    }
    const created: StoredFlag = {
      name: String(body.name ?? ''),
      description: String(body.description ?? ''),
      enabled: Boolean(body.enabled ?? false),
      realms: (body.realms as string[]) ?? [],
      users: (body.users as string[]) ?? [],
      createdAt: '2026-03-01T00:00:00Z',
      updatedAt: '2026-03-01T00:00:00Z',
    };
    flags = [...flags, created];
    return HttpResponse.json(created, { status: 201 });
  }),

  http.put('/api/admin/feature-flags/:name', async ({ params, request }) => {
    const name = String(params.name);
    const body = (await request.json()) as Record<string, unknown>;
    calls.push({
      method: 'PUT',
      path: `/api/admin/feature-flags/${name}`,
      body,
    });
    const idx = flags.findIndex((f) => f.name === name);
    if (idx >= 0) {
      const next = { ...flags[idx] };
      if (body.description !== undefined) next.description = String(body.description);
      if (body.enabled !== undefined) next.enabled = Boolean(body.enabled);
      if (body.realms !== undefined) next.realms = body.realms as string[];
      if (body.users !== undefined) next.users = body.users as string[];
      flags[idx] = next;
      return HttpResponse.json(next);
    }
    return new HttpResponse(null, { status: 404 });
  }),

  http.delete('/api/admin/feature-flags/:name', ({ params }) => {
    const name = String(params.name);
    calls.push({
      method: 'DELETE',
      path: `/api/admin/feature-flags/${name}`,
      body: null,
    });
    flags = flags.filter((f) => f.name !== name);
    return new HttpResponse(null, { status: 204 });
  }),
);

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => {
  server.resetHandlers();
  useToastStore.getState().clear();
});
afterAll(() => server.close());

beforeEach(() => {
  flags = JSON.parse(JSON.stringify(SEED)) as StoredFlag[];
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
      <MemoryRouter initialEntries={['/admin/feature-flags']}>
        <FeatureFlagsAdminPage />
        <Toaster />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BDD: FeatureFlagsAdminPage', () => {
  it('renders the flag list with name, description and enabled state', async () => {
    renderPage();

    expect(
      await screen.findByRole('heading', { level: 1, name: /Feature Flags/i }),
    ).toBeInTheDocument();

    await screen.findByText('new-search');
    await screen.findByText('beta-graph');

    const enabledRow = screen.getByTestId('flag-row-new-search');
    expect(
      within(enabledRow).getByText('Rolls out the rewritten search backend'),
    ).toBeInTheDocument();
    // enabled state is rendered with a machine-readable marker per row.
    expect(enabledRow.getAttribute('data-enabled')).toBe('true');

    const disabledRow = screen.getByTestId('flag-row-beta-graph');
    expect(disabledRow.getAttribute('data-enabled')).toBe('false');
  });

  it('shows an empty state when no flags exist', async () => {
    flags = [];
    renderPage();
    expect(await screen.findByTestId('flags-empty')).toBeInTheDocument();
  });

  it('creates a flag via POST and refreshes the list', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText('new-search');

    await user.click(screen.getByTestId('flag-new-btn'));
    await screen.findByTestId('flag-create-form');

    await user.type(screen.getByTestId('flag-name-input'), 'dark-mode');
    await user.type(
      screen.getByTestId('flag-description-input'),
      'Dark theme rollout',
    );
    await user.click(screen.getByTestId('flag-enabled-input'));
    await user.type(screen.getByTestId('flag-realms-input'), 'prod, staging');
    await user.type(screen.getByTestId('flag-users-input'), 'dave');
    await user.click(screen.getByTestId('flag-create-submit'));

    await waitFor(() => {
      expect(calls.find((c) => c.method === 'POST')).toBeTruthy();
    });
    const post = calls.find((c) => c.method === 'POST')!;
    expect(post.path).toBe('/api/admin/feature-flags');
    expect(post.body).toMatchObject({
      name: 'dark-mode',
      description: 'Dark theme rollout',
      enabled: true,
      realms: ['prod', 'staging'],
      users: ['dave'],
    });

    // List refreshes to include the new flag.
    await screen.findByText('dark-mode');
  });

  it('disables the create submit until a name is entered', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText('new-search');

    await user.click(screen.getByTestId('flag-new-btn'));
    await screen.findByTestId('flag-create-form');

    const submit = screen.getByTestId('flag-create-submit') as HTMLButtonElement;
    expect(submit.disabled).toBe(true);

    await user.type(screen.getByTestId('flag-name-input'), 'x');
    expect(submit.disabled).toBe(false);

    // Whitespace-only name does not count.
    await user.clear(screen.getByTestId('flag-name-input'));
    await user.type(screen.getByTestId('flag-name-input'), '   ');
    expect(submit.disabled).toBe(true);
  });

  it('toggles enabled inline via PUT', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText('beta-graph');

    // beta-graph starts disabled; the inline toggle flips it on.
    await user.click(
      within(screen.getByTestId('flag-row-beta-graph')).getByTestId(
        'flag-toggle-beta-graph',
      ),
    );

    await waitFor(() => {
      expect(calls.find((c) => c.method === 'PUT')).toBeTruthy();
    });
    const put = calls.find((c) => c.method === 'PUT')!;
    expect(put.path).toBe('/api/admin/feature-flags/beta-graph');
    expect(put.body).toEqual({ enabled: true });

    // Row reflects the new state after invalidation.
    await waitFor(() => {
      expect(
        screen.getByTestId('flag-row-beta-graph').getAttribute('data-enabled'),
      ).toBe('true');
    });
  });

  it('edits a flag via PUT and refreshes the list', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText('new-search');

    await user.click(
      within(screen.getByTestId('flag-row-new-search')).getByTestId(
        'flag-edit-btn',
      ),
    );
    await screen.findByTestId('flag-edit-form');

    const desc = screen.getByTestId('flag-description-input') as HTMLInputElement;
    await user.clear(desc);
    await user.type(desc, 'Updated search description');

    const realms = screen.getByTestId('flag-realms-input') as HTMLInputElement;
    await user.clear(realms);
    await user.type(realms, 'prod');

    await user.click(screen.getByTestId('flag-edit-submit'));

    await waitFor(() => {
      expect(calls.find((c) => c.method === 'PUT')).toBeTruthy();
    });
    const put = calls.find((c) => c.method === 'PUT')!;
    expect(put.path).toBe('/api/admin/feature-flags/new-search');
    expect(put.body).toMatchObject({
      description: 'Updated search description',
      realms: ['prod'],
    });
  });

  it('requires confirmation before deleting a flag and then sends DELETE', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText('beta-graph');

    await user.click(
      within(screen.getByTestId('flag-row-beta-graph')).getByTestId(
        'flag-delete-btn',
      ),
    );

    // A confirmation modal opens, not a direct request.
    await screen.findByTestId('flag-delete-modal');
    expect(calls.find((c) => c.method === 'DELETE')).toBeUndefined();

    await user.click(screen.getByTestId('flag-delete-confirm'));
    await waitFor(() => {
      expect(calls.find((c) => c.method === 'DELETE')).toBeTruthy();
    });
    const del = calls.find((c) => c.method === 'DELETE')!;
    expect(del.path).toBe('/api/admin/feature-flags/beta-graph');
  });

  it('surfaces a toast when flag creation fails', async () => {
    failCreate = true;
    const user = userEvent.setup();
    renderPage();
    await screen.findByText('new-search');

    await user.click(screen.getByTestId('flag-new-btn'));
    await screen.findByTestId('flag-create-form');
    await user.type(screen.getByTestId('flag-name-input'), 'new-search');
    await user.click(screen.getByTestId('flag-create-submit'));

    const toaster = await screen.findByTestId('toaster');
    expect(
      within(toaster).getByText(/FeatureFlagAlreadyExists/i),
    ).toBeInTheDocument();
  });
});
