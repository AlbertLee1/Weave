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
import { RolesAdminPage } from '../RolesAdminPage';
import { Toaster } from '../../common/Toaster';
import { useToastStore } from '../../../stores/toastStore';

// BDD: the Roles admin page surfaces the backend RBAC role CRUD
// (pkg/auth/role_handlers.go) that previously had no UI entry point. Each
// scenario asserts an externally observable behavior — what renders, and the
// HTTP request method/path/body the page emits — so the wire contract is
// locked, not just the component internals.

interface StoredRole {
  name: string;
  description: string;
  builtin: boolean;
  createdAt: string;
  permissions: string[];
}

interface RecordedCall {
  method: string;
  path: string;
  body: Record<string, unknown> | null;
}

const SEED: StoredRole[] = [
  {
    name: 'admin',
    description: 'Full administrative access',
    builtin: true,
    createdAt: '2026-01-01T00:00:00Z',
    permissions: ['user.manage', 'ontology.edit'],
  },
  {
    name: 'analyst',
    description: 'Read-only analyst',
    builtin: false,
    createdAt: '2026-02-01T00:00:00Z',
    permissions: ['ontology.read'],
  },
];

let roles: StoredRole[];
let calls: RecordedCall[];
// failCreate flips the POST handler to a 409 so the create-error scenario can
// assert the toast surfaces describeApiError output.
let failCreate = false;

const server = setupServer(
  http.get('/api/admin/roles', () => HttpResponse.json({ roles })),

  http.post('/api/admin/roles', async ({ request }) => {
    const body = (await request.json()) as Record<string, unknown>;
    calls.push({ method: 'POST', path: '/api/admin/roles', body });
    if (failCreate) {
      return HttpResponse.json(
        {
          errorCode: 'CONFLICT',
          errorName: 'RoleConflict',
          errorInstanceId: 'x',
          parameters: { name: String(body.name ?? '') },
        },
        { status: 409 },
      );
    }
    const created: StoredRole = {
      name: String(body.name ?? ''),
      description: String(body.description ?? ''),
      builtin: false,
      createdAt: '2026-03-01T00:00:00Z',
      permissions: (body.permissions as string[] | undefined) ?? [],
    };
    roles = [...roles, created];
    return HttpResponse.json(created, { status: 201 });
  }),

  http.delete('/api/admin/roles/:name', ({ params }) => {
    const name = String(params.name);
    calls.push({ method: 'DELETE', path: `/api/admin/roles/${name}`, body: null });
    const target = roles.find((r) => r.name === name);
    if (target?.builtin) {
      return HttpResponse.json(
        {
          errorCode: 'CONFLICT',
          errorName: 'BuiltinRoleProtected',
          errorInstanceId: 'x',
          parameters: { name },
        },
        { status: 409 },
      );
    }
    roles = roles.filter((r) => r.name !== name);
    return new HttpResponse(null, { status: 204 });
  }),

  http.get('/api/admin/roles/:name/permissions', ({ params }) => {
    const role = roles.find((r) => r.name === String(params.name));
    return HttpResponse.json({ permissions: role?.permissions ?? [] });
  }),

  http.put('/api/admin/roles/:name/permissions', async ({ params, request }) => {
    const name = String(params.name);
    const body = (await request.json()) as Record<string, unknown>;
    calls.push({
      method: 'PUT',
      path: `/api/admin/roles/${name}/permissions`,
      body,
    });
    const idx = roles.findIndex((r) => r.name === name);
    if (idx >= 0) {
      const perms = (body.permissions as string[] | undefined) ?? [];
      roles[idx] = { ...roles[idx], permissions: perms };
      return HttpResponse.json({ permissions: perms });
    }
    return HttpResponse.json({ permissions: [] });
  }),
);

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => {
  server.resetHandlers();
  useToastStore.getState().clear();
});
afterAll(() => server.close());

beforeEach(() => {
  roles = JSON.parse(JSON.stringify(SEED)) as StoredRole[];
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
      <MemoryRouter initialEntries={['/admin/roles']}>
        <RolesAdminPage />
        <Toaster />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BDD: RolesAdminPage', () => {
  it('renders the role list and badges the built-in role', async () => {
    renderPage();

    expect(
      await screen.findByRole('heading', { level: 1, name: /Roles/i }),
    ).toBeInTheDocument();

    await screen.findByText('admin');
    await screen.findByText('analyst');

    const adminRow = screen.getByTestId('role-row-admin');
    expect(within(adminRow).getByTestId('role-builtin-badge')).toBeInTheDocument();

    const analystRow = screen.getByTestId('role-row-analyst');
    expect(within(analystRow).queryByTestId('role-builtin-badge')).toBeNull();
  });

  it('shows an empty state when no roles exist', async () => {
    roles = [];
    renderPage();
    expect(await screen.findByTestId('roles-empty')).toBeInTheDocument();
  });

  it('creates a role via POST and refreshes the list', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText('analyst');

    await user.click(screen.getByTestId('role-new-btn'));
    await screen.findByTestId('role-create-form');

    await user.type(screen.getByTestId('role-name-input'), 'auditor');
    await user.type(
      screen.getByTestId('role-description-input'),
      'Reviews audit logs',
    );
    await user.click(screen.getByTestId('role-create-submit'));

    await waitFor(() => {
      const post = calls.find((c) => c.method === 'POST');
      expect(post).toBeTruthy();
    });
    const post = calls.find((c) => c.method === 'POST')!;
    expect(post.path).toBe('/api/admin/roles');
    expect(post.body).toMatchObject({
      name: 'auditor',
      description: 'Reviews audit logs',
    });

    // List refreshes to include the new role.
    await screen.findByText('auditor');
  });

  it('surfaces a toast when role creation fails', async () => {
    failCreate = true;
    const user = userEvent.setup();
    renderPage();
    await screen.findByText('analyst');

    await user.click(screen.getByTestId('role-new-btn'));
    await screen.findByTestId('role-create-form');
    await user.type(screen.getByTestId('role-name-input'), 'analyst');
    await user.click(screen.getByTestId('role-create-submit'));

    const toaster = await screen.findByTestId('toaster');
    expect(within(toaster).getByText(/RoleConflict/i)).toBeInTheDocument();
  });

  it('disables delete for built-in roles and requires confirmation for custom roles', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText('admin');

    // Built-in role delete button is disabled.
    const builtinDelete = within(
      screen.getByTestId('role-row-admin'),
    ).getByTestId('role-delete-btn');
    expect(builtinDelete).toBeDisabled();

    // Custom role: clicking delete opens a confirmation modal, not a direct
    // request.
    const analystDelete = within(
      screen.getByTestId('role-row-analyst'),
    ).getByTestId('role-delete-btn');
    expect(analystDelete).not.toBeDisabled();
    await user.click(analystDelete);

    await screen.findByTestId('role-delete-modal');
    expect(calls.find((c) => c.method === 'DELETE')).toBeUndefined();

    await user.click(screen.getByTestId('role-delete-confirm'));
    await waitFor(() => {
      expect(calls.find((c) => c.method === 'DELETE')).toBeTruthy();
    });
    const del = calls.find((c) => c.method === 'DELETE')!;
    expect(del.path).toBe('/api/admin/roles/analyst');
  });

  it('edits permissions for a custom role and saves via PUT', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText('analyst');

    await user.click(
      within(screen.getByTestId('role-row-analyst')).getByTestId(
        'role-permissions-btn',
      ),
    );

    // Permissions editor loads the current list.
    await screen.findByTestId('role-permissions-editor');
    await screen.findByText('ontology.read');

    // Add a new permission.
    await user.type(
      screen.getByTestId('role-permission-add-input'),
      'ontology.edit',
    );
    await user.click(screen.getByTestId('role-permission-add-btn'));

    await user.click(screen.getByTestId('role-permissions-save'));

    await waitFor(() => {
      expect(calls.find((c) => c.method === 'PUT')).toBeTruthy();
    });
    const put = calls.find((c) => c.method === 'PUT')!;
    expect(put.path).toBe('/api/admin/roles/analyst/permissions');
    expect(put.body?.permissions).toEqual(
      expect.arrayContaining(['ontology.read', 'ontology.edit']),
    );
  });

  it('disables permission editing for built-in roles but still shows them', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText('admin');

    await user.click(
      within(screen.getByTestId('role-row-admin')).getByTestId(
        'role-permissions-btn',
      ),
    );

    await screen.findByTestId('role-permissions-editor');
    // Built-in permissions are visible (read-only) ...
    await screen.findByText('user.manage');
    // ... but the save / add controls are disabled.
    expect(screen.getByTestId('role-permissions-save')).toBeDisabled();
  });
});
