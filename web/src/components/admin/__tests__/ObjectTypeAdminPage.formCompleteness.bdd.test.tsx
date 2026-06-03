import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ObjectTypeAdminPage } from '../ObjectTypeAdminPage';

// US-211 / US-212 / US-264 — ObjectType admin form completeness.
// Given an admin authoring or editing an ObjectType, the Create and Edit
// forms must expose: a composite primary-key input, an "Extends" parent
// selector, and an "Audit data access" toggle, and forward those values to
// the backend request body verbatim.

const OBJECT_TYPES = [
  {
    rid: 'ri.ontology.main.object-type.emp-1',
    apiName: 'Employee',
    displayName: 'Employee',
    pluralDisplayName: 'Employees',
    primaryKey: 'employeeId',
    status: 'ACTIVE',
    visibility: 'PROMINENT',
  },
  {
    rid: 'ri.ontology.main.object-type.dept-1',
    apiName: 'Department',
    displayName: 'Department',
    pluralDisplayName: 'Departments',
    primaryKey: 'departmentId',
    status: 'EXPERIMENTAL',
    visibility: 'NORMAL',
    auditDataAccess: true,
    extendsRid: 'ri.ontology.main.object-type.emp-1',
  },
];

interface StubState {
  objectTypes: typeof OBJECT_TYPES;
  createCalls: Array<{ body: Record<string, unknown> }>;
  updateCalls: Array<{ rid: string; body: Record<string, unknown> }>;
}

function makeStub(): StubState {
  return {
    objectTypes: OBJECT_TYPES.map((ot) => ({ ...ot })),
    createCalls: [],
    updateCalls: [],
  };
}

function installFetch(state: StubState) {
  vi.stubGlobal(
    'fetch',
    vi.fn(
      async (
        input: RequestInfo | URL,
        init?: RequestInit,
      ): Promise<Response> => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = (init?.method ?? 'GET').toUpperCase();

        if (
          method === 'GET' &&
          url.endsWith('/api/v2/ontologies/northwind/objectTypes')
        ) {
          return jsonResponse({ data: state.objectTypes });
        }

        if (
          method === 'POST' &&
          url.endsWith('/api/v2/ontologies/northwind/objectTypes')
        ) {
          const body = init?.body
            ? (JSON.parse(init.body as string) as Record<string, unknown>)
            : {};
          state.createCalls.push({ body });
          const created = {
            rid: `ri.ontology.main.object-type.${body.apiName as string}`,
            apiName: body.apiName,
            displayName: body.displayName,
            primaryKey: body.primaryKey,
            status: body.status ?? 'ACTIVE',
            visibility: body.visibility ?? 'NORMAL',
          };
          return jsonResponse(created, 201);
        }

        const byRid = url.match(
          /\/api\/v2\/ontologies\/northwind\/objectTypes\/byRid\/([^?]+)/,
        );
        if (byRid && method === 'PUT') {
          const rid = decodeURIComponent(byRid[1]);
          const body = init?.body
            ? (JSON.parse(init.body as string) as Record<string, unknown>)
            : {};
          state.updateCalls.push({ rid, body });
          const idx = state.objectTypes.findIndex((ot) => ot.rid === rid);
          if (idx < 0) return jsonResponse({ errorCode: 'NotFound' }, 404);
          const next = { ...state.objectTypes[idx], ...body };
          state.objectTypes[idx] = next;
          return jsonResponse(next);
        }

        // outgoing links / action types lookups used by the delete impact path
        if (method === 'GET' && url.endsWith('/outgoingLinkTypes')) {
          return jsonResponse({ data: [] });
        }
        if (
          method === 'GET' &&
          url.endsWith('/api/v2/ontologies/northwind/actionTypes')
        ) {
          return jsonResponse({ data: [] });
        }

        return new Response('{}', { status: 200 });
      },
    ),
  );
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
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
      <MemoryRouter initialEntries={['/admin/northwind/objectTypes']}>
        <Routes>
          <Route
            path="/admin/:ontology/objectTypes"
            element={<ObjectTypeAdminPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('ObjectTypeAdminPage — form completeness (US-211/212/264)', () => {
  let state: StubState;

  beforeEach(() => {
    state = makeStub();
    installFetch(state);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('create form sends a single primaryKey and no primaryKeys when one PK field is given', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Employee').length).toBeGreaterThan(0);
    });
    await user.click(
      screen.getByRole('button', { name: /\+ New Object Type/i }),
    );
    await user.type(screen.getByLabelText(/Display Name \*/i), 'Invoice');
    await user.type(screen.getByLabelText(/Primary Key/i), 'invoiceId');

    await user.click(screen.getByRole('button', { name: /^Create$/i }));

    await waitFor(() => expect(state.createCalls.length).toBe(1));
    const body = state.createCalls[0].body;
    expect(body.primaryKey).toBe('invoiceId');
    expect(body.primaryKeys).toBeUndefined();
  });

  it('create form sends a primaryKeys array for a composite key (comma-separated)', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Employee').length).toBeGreaterThan(0);
    });
    await user.click(
      screen.getByRole('button', { name: /\+ New Object Type/i }),
    );
    await user.type(screen.getByLabelText(/Display Name \*/i), 'OrderLine');
    await user.type(
      screen.getByLabelText(/Primary Key/i),
      'orderId, lineNumber',
    );

    await user.click(screen.getByRole('button', { name: /^Create$/i }));

    await waitFor(() => expect(state.createCalls.length).toBe(1));
    const body = state.createCalls[0].body;
    expect(body.primaryKeys).toEqual(['orderId', 'lineNumber']);
    // legacy single key falls back to the first element
    expect(body.primaryKey).toBe('orderId');
  });

  it('create form forwards the Extends parent RID and excludes the type itself', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Employee').length).toBeGreaterThan(0);
    });
    await user.click(
      screen.getByRole('button', { name: /\+ New Object Type/i }),
    );
    await user.type(screen.getByLabelText(/Display Name \*/i), 'Manager');
    await user.type(screen.getByLabelText(/Primary Key/i), 'managerId');

    const extendsSelect = screen.getByLabelText(/Extends/i) as HTMLSelectElement;
    // options: unspecified sentinel + both existing types
    const values = Array.from(extendsSelect.options).map((o) => o.value);
    expect(values).toContain('');
    expect(values).toContain('ri.ontology.main.object-type.emp-1');
    expect(values).toContain('ri.ontology.main.object-type.dept-1');

    await user.selectOptions(
      extendsSelect,
      'ri.ontology.main.object-type.emp-1',
    );
    await user.click(screen.getByRole('button', { name: /^Create$/i }));

    await waitFor(() => expect(state.createCalls.length).toBe(1));
    expect(state.createCalls[0].body.extendsRid).toBe(
      'ri.ontology.main.object-type.emp-1',
    );
  });

  it('create form forwards auditDataAccess when the toggle is enabled', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Employee').length).toBeGreaterThan(0);
    });
    await user.click(
      screen.getByRole('button', { name: /\+ New Object Type/i }),
    );
    await user.type(screen.getByLabelText(/Display Name \*/i), 'Ledger');
    await user.type(screen.getByLabelText(/Primary Key/i), 'ledgerId');

    await user.click(screen.getByLabelText(/Audit data access/i));
    await user.click(screen.getByRole('button', { name: /^Create$/i }));

    await waitFor(() => expect(state.createCalls.length).toBe(1));
    expect(state.createCalls[0].body.auditDataAccess).toBe(true);
  });

  it('edit form preloads auditDataAccess + extendsRid and forwards updated values', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Department').length).toBeGreaterThan(0);
    });
    // After asc sort: Department, Employee. Edit Department (auditDataAccess true).
    const editButtons = screen.getAllByRole('button', { name: /^Edit$/i });
    await user.click(editButtons[0]);

    const auditToggle = (await screen.findByLabelText(
      /Audit data access/i,
    )) as HTMLInputElement;
    expect(auditToggle.checked).toBe(true);

    const extendsSelect = screen.getByLabelText(/Extends/i) as HTMLSelectElement;
    expect(extendsSelect.value).toBe('ri.ontology.main.object-type.emp-1');
    // the type cannot extend itself
    const optionValues = Array.from(extendsSelect.options).map((o) => o.value);
    expect(optionValues).not.toContain('ri.ontology.main.object-type.dept-1');

    // Turn off the audit flag, clear the parent.
    await user.click(auditToggle);
    await user.selectOptions(extendsSelect, '');

    await user.click(screen.getByRole('button', { name: /Save changes/i }));

    await waitFor(() => expect(state.updateCalls.length).toBe(1));
    const body = state.updateCalls[0].body;
    expect(state.updateCalls[0].rid).toBe('ri.ontology.main.object-type.dept-1');
    expect(body.auditDataAccess).toBe(false);
    expect(body.extendsRid).toBe('');
  });
});
