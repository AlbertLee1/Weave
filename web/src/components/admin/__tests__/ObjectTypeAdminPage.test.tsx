import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ObjectTypeAdminPage } from '../ObjectTypeAdminPage';

const OBJECT_TYPES = [
  {
    rid: 'ri.ontology.main.object-type.emp-1',
    apiName: 'Employee',
    displayName: 'Employee',
    pluralDisplayName: 'Employees',
    description: 'Company employee',
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
  },
  {
    rid: 'ri.ontology.main.object-type.cust-1',
    apiName: 'Customer',
    displayName: 'Customer',
    pluralDisplayName: 'Customers',
    primaryKey: 'customerId',
    status: 'DEPRECATED',
    visibility: 'HIDDEN',
  },
];

const OUTGOING_LINKS = [
  {
    rid: 'ri.ontology.main.link-type.l1',
    apiName: 'employeeDepartment',
    displayName: 'Employee → Department',
    objectTypeApiName: 'Employee',
    linkedObjectTypeApiName: 'Department',
    cardinality: 'ONE_TO_ONE',
    required: true,
  },
];

const ACTION_TYPES = [
  {
    rid: 'ri.ontology.main.action-type.a1',
    apiName: 'createEmployee',
    displayName: 'Create Employee',
    status: 'ACTIVE',
    parameters: {
      employee: {
        dataType: { type: 'object', objectTypeApiName: 'Employee' },
        required: true,
      },
    },
  },
  {
    rid: 'ri.ontology.main.action-type.a2',
    apiName: 'archiveCustomer',
    displayName: 'Archive Customer',
    status: 'ACTIVE',
    parameters: {
      customer: {
        dataType: { type: 'object', objectTypeApiName: 'Customer' },
        required: true,
      },
    },
  },
];

interface StubState {
  objectTypes: typeof OBJECT_TYPES;
  createCalls: Array<{ body: unknown }>;
  updateCalls: Array<{ rid: string; body: unknown }>;
  deleteCalls: string[];
  forceCreateError?: boolean;
}

function makeStub(): StubState {
  return {
    objectTypes: [...OBJECT_TYPES],
    createCalls: [],
    updateCalls: [],
    deleteCalls: [],
  };
}

function installFetch(state: StubState) {
  vi.stubGlobal(
    'fetch',
    vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
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
          if (state.forceCreateError) {
            return jsonResponse(
              {
                errorCode: 'ObjectTypeAlreadyExists',
                errorName: 'Conflict',
                errorInstanceId: 'x',
              },
              409,
            );
          }
          const body = init?.body ? JSON.parse(init.body as string) : {};
          state.createCalls.push({ body });
          const created = {
            rid: `ri.ontology.main.object-type.${body.apiName}`,
            apiName: body.apiName,
            displayName: body.displayName,
            pluralDisplayName: body.pluralDisplayName,
            primaryKey: body.primaryKey,
            status: body.status ?? 'ACTIVE',
            visibility: body.visibility ?? 'NORMAL',
          };
          state.objectTypes.push(created);
          return jsonResponse(created, 201);
        }

        const updateMatch = url.match(
          /\/api\/v2\/ontologies\/northwind\/objectTypes\/byRid\/([^?]+)/,
        );
        if (updateMatch && method === 'PUT') {
          const rid = decodeURIComponent(updateMatch[1]);
          const body = init?.body ? JSON.parse(init.body as string) : {};
          state.updateCalls.push({ rid, body });
          const idx = state.objectTypes.findIndex((ot) => ot.rid === rid);
          if (idx < 0) return jsonResponse({ errorCode: 'NotFound' }, 404);
          const next = { ...state.objectTypes[idx], ...body };
          state.objectTypes[idx] = next;
          return jsonResponse(next);
        }
        if (updateMatch && method === 'DELETE') {
          const rid = decodeURIComponent(updateMatch[1]);
          state.deleteCalls.push(rid);
          state.objectTypes = state.objectTypes.filter((ot) => ot.rid !== rid);
          return new Response('', { status: 204 });
        }

        if (
          method === 'GET' &&
          url.includes('/objectTypes/') &&
          url.endsWith('/outgoingLinkTypes')
        ) {
          return jsonResponse({ data: OUTGOING_LINKS });
        }

        if (
          method === 'GET' &&
          url.endsWith('/api/v2/ontologies/northwind/actionTypes')
        ) {
          return jsonResponse({ data: ACTION_TYPES });
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

describe('ObjectTypeAdminPage', () => {
  let state: StubState;

  beforeEach(() => {
    state = makeStub();
    installFetch(state);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('renders the page heading with the ontology name', async () => {
    renderPage();
    expect(
      screen.getByRole('heading', { name: /Ontology Manager/i }),
    ).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.getByText('northwind')).toBeInTheDocument();
    });
  });

  it('loads and lists object types', async () => {
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Employee').length).toBeGreaterThan(0);
    });
    expect(screen.getAllByText('Department').length).toBeGreaterThan(0);
    expect(screen.getAllByText('Customer').length).toBeGreaterThan(0);
  });

  it('filters by search text', async () => {
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Employee').length).toBeGreaterThan(0);
    });
    const input = screen.getByLabelText(
      /Search object types/i,
    ) as HTMLInputElement;
    fireEvent.change(input, { target: { value: 'depart' } });
    await waitFor(() => {
      expect(screen.queryAllByText('Employee').length).toBe(0);
    });
    expect(screen.getAllByText('Department').length).toBeGreaterThan(0);
  });

  it('filters by status', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Employee').length).toBeGreaterThan(0);
    });
    const select = screen.getByLabelText(/Filter by status/i) as HTMLSelectElement;
    await user.selectOptions(select, 'DEPRECATED');
    await waitFor(() => {
      expect(screen.queryAllByText('Employee').length).toBe(0);
    });
    expect(screen.getAllByText('Customer').length).toBeGreaterThan(0);
  });

  it('sorts by name ascending by default and flips on Z→A', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Employee').length).toBeGreaterThan(0);
    });
    // Ascending: Customer, Department, Employee
    let rows = screen
      .getAllByRole('row')
      .slice(1)
      .map((r) => r.textContent ?? '');
    expect(rows[0]).toMatch(/Customer/);
    expect(rows[2]).toMatch(/Employee/);

    await user.selectOptions(screen.getByLabelText(/Sort by name/i), 'desc');
    rows = screen
      .getAllByRole('row')
      .slice(1)
      .map((r) => r.textContent ?? '');
    expect(rows[0]).toMatch(/Employee/);
    expect(rows[2]).toMatch(/Customer/);
  });

  it('create form auto-generates apiName and pluralDisplayName from displayName', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Employee').length).toBeGreaterThan(0);
    });
    await user.click(screen.getByRole('button', { name: /\+ New Object Type/i }));

    const displayName = await screen.findByLabelText(/Display Name \*/i);
    await user.type(displayName, 'Product Category');

    const apiNameInput = screen.getByLabelText(/API Name \*/i) as HTMLInputElement;
    expect(apiNameInput.value).toBe('productCategory');

    const pluralInput = screen.getByLabelText(
      /Plural Display Name/i,
    ) as HTMLInputElement;
    expect(pluralInput.value).toBe('Product Categories');
  });

  it('create submits the expected payload and closes modal', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Employee').length).toBeGreaterThan(0);
    });
    await user.click(screen.getByRole('button', { name: /\+ New Object Type/i }));
    await user.type(screen.getByLabelText(/Display Name \*/i), 'Invoice');
    await user.type(screen.getByLabelText(/Primary Key \*/i), 'invoiceId');
    await user.click(screen.getByRole('button', { name: /^Create$/i }));

    await waitFor(() => {
      expect(state.createCalls.length).toBe(1);
    });
    expect(state.createCalls[0].body).toMatchObject({
      apiName: 'invoice',
      displayName: 'Invoice',
      primaryKey: 'invoiceId',
      pluralDisplayName: 'Invoices',
      status: 'ACTIVE',
      visibility: 'NORMAL',
    });
  });

  it('blocks creating an ObjectType with a duplicate apiName', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Employee').length).toBeGreaterThan(0);
    });
    await user.click(screen.getByRole('button', { name: /\+ New Object Type/i }));
    await user.type(
      screen.getByLabelText(/Display Name \*/i),
      'Extra Employee',
    );
    const apiNameInput = screen.getByLabelText(/API Name \*/i) as HTMLInputElement;
    await user.clear(apiNameInput);
    await user.type(apiNameInput, 'Employee');
    await user.type(screen.getByLabelText(/Primary Key \*/i), 'id');
    const submit = screen.getByRole('button', { name: /^Create$/i });
    expect(submit).toBeDisabled();
    expect(
      screen.getByText(/An ObjectType with apiName .* already exists/i),
    ).toBeInTheDocument();
  });

  it('edit form loads current values and submits an update', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Employee').length).toBeGreaterThan(0);
    });

    const editButtons = screen.getAllByRole('button', { name: /^Edit$/i });
    await user.click(editButtons[2]); // after sort asc: Customer, Department, Employee

    const displayInput = (await screen.findByLabelText(
      /Display Name \*/i,
    )) as HTMLInputElement;
    expect(displayInput.value).toBe('Employee');
    await user.clear(displayInput);
    await user.type(displayInput, 'Team Member');

    await user.click(screen.getByRole('button', { name: /Save changes/i }));

    await waitFor(() => {
      expect(state.updateCalls.length).toBe(1);
    });
    expect(state.updateCalls[0]).toMatchObject({
      rid: 'ri.ontology.main.object-type.emp-1',
      body: expect.objectContaining({
        displayName: 'Team Member',
        status: 'ACTIVE',
        visibility: 'PROMINENT',
      }),
    });
  });

  it('delete modal shows impacted LinkType and ActionType counts', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Employee').length).toBeGreaterThan(0);
    });
    const deleteButtons = screen.getAllByRole('button', { name: /^Delete$/i });
    await user.click(deleteButtons[2]); // Employee row after asc sort

    await waitFor(() => {
      expect(screen.getByTestId('delete-impact-links')).toHaveTextContent(
        '1 outgoing LinkType',
      );
    });
    expect(screen.getByTestId('delete-impact-actions')).toHaveTextContent(
      '1 ActionType',
    );
  });

  it('confirms delete and calls the DELETE endpoint', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Customer').length).toBeGreaterThan(0);
    });
    const deleteButtons = screen.getAllByRole('button', { name: /^Delete$/i });
    await user.click(deleteButtons[0]); // Customer (first after asc sort)

    await screen.findByText(/Delete Object Type/i);
    const confirm = screen.getAllByRole('button', { name: /^Delete$/i }).at(-1)!;
    await user.click(confirm);

    await waitFor(() => {
      expect(state.deleteCalls.length).toBe(1);
    });
    expect(state.deleteCalls[0]).toBe('ri.ontology.main.object-type.cust-1');
  });

  // US-262: classification dropdown round-trips through Create + Edit modals.

  it('create modal forwards the Classification dropdown value', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Employee').length).toBeGreaterThan(0);
    });
    await user.click(screen.getByRole('button', { name: /\+ New Object Type/i }));
    await user.type(screen.getByLabelText(/Display Name \*/i), 'Invoice');
    await user.type(screen.getByLabelText(/Primary Key \*/i), 'invoiceId');

    const select = screen.getByLabelText(/Classification/i) as HTMLSelectElement;
    // Options include all 5 labels + the unspecified sentinel.
    expect(
      Array.from(select.options).map((o) => o.value),
    ).toEqual(['', 'Public', 'Internal', 'Confidential', 'PII', 'Secret']);
    await user.selectOptions(select, 'Confidential');

    await user.click(screen.getByRole('button', { name: /^Create$/i }));

    await waitFor(() => {
      expect(state.createCalls.length).toBe(1);
    });
    expect(state.createCalls[0].body).toMatchObject({
      classification: 'Confidential',
    });
  });

  it('edit modal preloads classification and forwards the new value', async () => {
    // Seed the Customer row with a PII classification so the edit form sees it.
    state.objectTypes[2] = {
      ...state.objectTypes[2],
      classification: 'PII',
    } as (typeof state.objectTypes)[number] & { classification?: string };

    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Customer').length).toBeGreaterThan(0);
    });
    const editButtons = screen.getAllByRole('button', { name: /^Edit$/i });
    await user.click(editButtons[0]); // Customer after asc sort

    const select = (await screen.findByLabelText(
      /Classification/i,
    )) as HTMLSelectElement;
    expect(select.value).toBe('PII');
    await user.selectOptions(select, 'Secret');

    await user.click(screen.getByRole('button', { name: /Save changes/i }));
    await waitFor(() => {
      expect(state.updateCalls.length).toBe(1);
    });
    expect(state.updateCalls[0]).toMatchObject({
      rid: 'ri.ontology.main.object-type.cust-1',
      body: expect.objectContaining({
        classification: 'Secret',
      }),
    });
  });
});
