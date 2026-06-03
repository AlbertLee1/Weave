import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { LinkTypeAdminPage } from '../LinkTypeAdminPage';

const OBJECT_TYPES = [
  {
    rid: 'ri.ontology.main.object-type.emp',
    apiName: 'Employee',
    displayName: 'Employee',
    primaryKey: 'employeeId',
    status: 'ACTIVE',
    visibility: 'PROMINENT',
  },
  {
    rid: 'ri.ontology.main.object-type.dept',
    apiName: 'Department',
    displayName: 'Department',
    primaryKey: 'departmentId',
    status: 'ACTIVE',
    visibility: 'NORMAL',
  },
  {
    rid: 'ri.ontology.main.object-type.proj',
    apiName: 'Project',
    displayName: 'Project',
    primaryKey: 'projectId',
    status: 'ACTIVE',
    visibility: 'NORMAL',
  },
];

const LINK_TYPES = [
  {
    rid: 'ri.ontology.main.link-type.l1',
    apiName: 'employeeDepartment',
    displayName: 'Employee → Department',
    objectTypeApiName: 'Employee',
    linkedObjectTypeApiName: 'Department',
    cardinality: 'ONE_TO_ONE',
    required: true,
  },
  {
    rid: 'ri.ontology.main.link-type.l2',
    apiName: 'departmentEmployees',
    displayName: 'Department → Employees',
    objectTypeApiName: 'Department',
    linkedObjectTypeApiName: 'Employee',
    cardinality: 'ONE_TO_MANY',
    required: false,
  },
  {
    rid: 'ri.ontology.main.link-type.l3',
    apiName: 'employeeProjects',
    displayName: 'Employee → Projects',
    objectTypeApiName: 'Employee',
    linkedObjectTypeApiName: 'Project',
    cardinality: 'MANY_TO_MANY',
    required: false,
  },
];

const ACTION_TYPES = [
  {
    rid: 'ri.ontology.main.action-type.a1',
    apiName: 'assignEmployeeProject',
    displayName: 'Assign Employee Project',
    status: 'ACTIVE',
    parameters: {},
    rules: [
      {
        type: 'createLink',
        linkTypeApiName: 'employeeProjects',
        sourceObjectPrimaryKey: 'employeeId',
        targetObjectPrimaryKey: 'projectId',
      },
    ],
  },
  {
    rid: 'ri.ontology.main.action-type.a2',
    apiName: 'archiveEmployee',
    displayName: 'Archive Employee',
    status: 'ACTIVE',
    parameters: {},
    rules: [],
  },
];

interface StubState {
  linkTypes: typeof LINK_TYPES;
  createCalls: Array<{ body: unknown }>;
  updateCalls: Array<{ rid: string; body: unknown }>;
  deleteCalls: string[];
}

function makeStub(): StubState {
  return {
    linkTypes: [...LINK_TYPES],
    createCalls: [],
    updateCalls: [],
    deleteCalls: [],
  };
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
        const url = typeof input === 'string' ? input : input.toString();
        const method = (init?.method ?? 'GET').toUpperCase();

        if (
          method === 'GET' &&
          url.endsWith('/api/v2/ontologies/northwind/objectTypes')
        ) {
          return jsonResponse({ data: OBJECT_TYPES });
        }

        if (
          method === 'GET' &&
          url.endsWith('/api/v2/ontologies/northwind/actionTypes')
        ) {
          return jsonResponse({ data: ACTION_TYPES });
        }

        if (
          method === 'GET' &&
          url.endsWith('/api/v2/ontologies/northwind/linkTypes')
        ) {
          return jsonResponse({ data: state.linkTypes });
        }

        if (
          method === 'POST' &&
          url.endsWith('/api/v2/ontologies/northwind/linkTypes')
        ) {
          const body = init?.body ? JSON.parse(init.body as string) : {};
          state.createCalls.push({ body });
          const created = {
            rid: `ri.ontology.main.link-type.${body.apiName}`,
            apiName: body.apiName,
            displayName: body.displayName,
            description: body.description,
            objectTypeApiName: body.objectTypeApiName,
            linkedObjectTypeApiName: body.linkedObjectTypeApiName,
            cardinality: body.cardinality,
            required: body.required ?? false,
          };
          state.linkTypes.push(created);
          return jsonResponse(created, 201);
        }

        const ridMatch = url.match(
          /\/api\/v2\/ontologies\/northwind\/linkTypes\/byRid\/([^?]+)/,
        );
        if (ridMatch && method === 'PUT') {
          const rid = decodeURIComponent(ridMatch[1]);
          const body = init?.body ? JSON.parse(init.body as string) : {};
          state.updateCalls.push({ rid, body });
          const idx = state.linkTypes.findIndex((lt) => lt.rid === rid);
          if (idx < 0) return jsonResponse({ errorCode: 'NotFound' }, 404);
          const next = { ...state.linkTypes[idx], ...body };
          state.linkTypes[idx] = next;
          return jsonResponse(next);
        }
        if (ridMatch && method === 'DELETE') {
          const rid = decodeURIComponent(ridMatch[1]);
          state.deleteCalls.push(rid);
          state.linkTypes = state.linkTypes.filter((lt) => lt.rid !== rid);
          return jsonResponse({}, 200);
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
      <MemoryRouter initialEntries={['/admin/northwind/linkTypes']}>
        <Routes>
          <Route
            path="/admin/:ontology/linkTypes"
            element={<LinkTypeAdminPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('LinkTypeAdminPage', () => {
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

  it('loads and lists all link types with source → target display', async () => {
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Employee → Department')).toBeInTheDocument();
    });
    expect(screen.getByText('Department → Employees')).toBeInTheDocument();
    expect(screen.getByText('Employee → Projects')).toBeInTheDocument();
    // Cardinality badges
    expect(screen.getByText('1 : 1')).toBeInTheDocument();
    expect(screen.getByText('1 : N')).toBeInTheDocument();
    expect(screen.getByText('N : N')).toBeInTheDocument();
  });

  it('filters by cardinality', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Employee → Department')).toBeInTheDocument();
    });
    const select = screen.getByLabelText(
      /Filter by cardinality/i,
    ) as HTMLSelectElement;
    await user.selectOptions(select, 'MANY_TO_MANY');
    await waitFor(() => {
      expect(
        screen.queryByText('Employee → Department'),
      ).not.toBeInTheDocument();
    });
    expect(screen.getByText('Employee → Projects')).toBeInTheDocument();
  });

  it('filters by source object type', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Employee → Department')).toBeInTheDocument();
    });
    const sourceSelect = screen.getByLabelText(
      /Filter by source object type/i,
    ) as HTMLSelectElement;
    await user.selectOptions(sourceSelect, 'Department');
    await waitFor(() => {
      expect(
        screen.queryByText('Employee → Department'),
      ).not.toBeInTheDocument();
    });
    expect(screen.getByText('Department → Employees')).toBeInTheDocument();
  });

  it('filters by search text matching apiName', async () => {
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Employee → Department')).toBeInTheDocument();
    });
    const input = screen.getByLabelText(/Search link types/i) as HTMLInputElement;
    fireEvent.change(input, { target: { value: 'projects' } });
    await waitFor(() => {
      expect(
        screen.queryByText('Employee → Department'),
      ).not.toBeInTheDocument();
    });
    expect(screen.getByText('Employee → Projects')).toBeInTheDocument();
  });

  it('create form auto-generates apiName from displayName', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Employee → Department')).toBeInTheDocument();
    });
    await user.click(screen.getByRole('button', { name: /\+ New Link Type/i }));
    const displayName = await screen.findByLabelText(/Display Name \*/i);
    await user.type(displayName, 'Manages Team');
    const apiNameInput = screen.getByLabelText(/API Name \*/i) as HTMLInputElement;
    expect(apiNameInput.value).toBe('managesTeam');
  });

  it('create hides foreign key input when cardinality is MANY_TO_MANY', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Employee → Department')).toBeInTheDocument();
    });
    await user.click(screen.getByRole('button', { name: /\+ New Link Type/i }));
    // Default cardinality is ONE_TO_MANY → foreign key textarea visible
    expect(
      await screen.findByLabelText(/Foreign key config/i),
    ).toBeInTheDocument();
    const cardinalitySelect = screen.getByRole('combobox', {
      name: /^Cardinality$/i,
    }) as HTMLSelectElement;
    await user.selectOptions(cardinalitySelect, 'MANY_TO_MANY');
    await waitFor(() => {
      expect(
        screen.queryByLabelText(/Foreign key config/i),
      ).not.toBeInTheDocument();
    });
  });

  it('create shows join-table editor only for MANY_TO_MANY (mirrors FK editor)', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Employee → Department')).toBeInTheDocument();
    });
    await user.click(screen.getByRole('button', { name: /\+ New Link Type/i }));
    // Default cardinality is ONE_TO_MANY → join-table editor hidden, FK shown.
    expect(
      await screen.findByLabelText(/Foreign key config/i),
    ).toBeInTheDocument();
    expect(
      screen.queryByLabelText(/Join table config/i),
    ).not.toBeInTheDocument();
    const cardinalitySelect = screen.getByRole('combobox', {
      name: /^Cardinality$/i,
    }) as HTMLSelectElement;
    await user.selectOptions(cardinalitySelect, 'MANY_TO_MANY');
    await waitFor(() => {
      // FK editor hidden, join-table editor shown.
      expect(
        screen.queryByLabelText(/Foreign key config/i),
      ).not.toBeInTheDocument();
    });
    expect(screen.getByLabelText(/Join table config/i)).toBeInTheDocument();
    expect(
      screen.getByTestId('link-type-create-join-table'),
    ).toBeInTheDocument();
  });

  it('create sends joinTableConfig in the POST body for a MANY_TO_MANY link', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Employee → Department')).toBeInTheDocument();
    });
    await user.click(screen.getByRole('button', { name: /\+ New Link Type/i }));
    await user.type(
      screen.getByLabelText(/Display Name \*/i),
      'Project Members',
    );
    await user.selectOptions(
      screen.getByRole('combobox', { name: /^Cardinality$/i }),
      'MANY_TO_MANY',
    );
    const joinTable = screen.getByLabelText(
      /Join table config/i,
    ) as HTMLTextAreaElement;
    fireEvent.change(joinTable, {
      target: {
        value:
          '{"datasetRid":"ds1","sourceColumn":"empId","targetColumn":"projId"}',
      },
    });
    await user.click(screen.getByRole('button', { name: /^Create$/i }));

    await waitFor(() => {
      expect(state.createCalls.length).toBe(1);
    });
    expect(state.createCalls[0].body).toMatchObject({
      cardinality: 'MANY_TO_MANY',
      joinTableConfig: {
        datasetRid: 'ds1',
        sourceColumn: 'empId',
        targetColumn: 'projId',
      },
    });
  });

  it('create omits joinTableConfig when the editor is left empty', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Employee → Department')).toBeInTheDocument();
    });
    await user.click(screen.getByRole('button', { name: /\+ New Link Type/i }));
    await user.type(screen.getByLabelText(/Display Name \*/i), 'No Join Config');
    await user.selectOptions(
      screen.getByRole('combobox', { name: /^Cardinality$/i }),
      'MANY_TO_MANY',
    );
    await user.click(screen.getByRole('button', { name: /^Create$/i }));

    await waitFor(() => {
      expect(state.createCalls.length).toBe(1);
    });
    const body = state.createCalls[0].body as Record<string, unknown>;
    expect(body.joinTableConfig).toBeUndefined();
  });

  it('create rejects invalid JSON in joinTableConfig', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Employee → Department')).toBeInTheDocument();
    });
    await user.click(screen.getByRole('button', { name: /\+ New Link Type/i }));
    await user.type(screen.getByLabelText(/Display Name \*/i), 'With Join');
    await user.selectOptions(
      screen.getByRole('combobox', { name: /^Cardinality$/i }),
      'MANY_TO_MANY',
    );
    const joinTable = screen.getByLabelText(
      /Join table config/i,
    ) as HTMLTextAreaElement;
    fireEvent.change(joinTable, { target: { value: '{not valid' } });
    expect(screen.getByText(/Invalid JSON/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^Create$/i })).toBeDisabled();
  });

  it('create submits the expected payload and closes modal', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Employee → Department')).toBeInTheDocument();
    });
    await user.click(screen.getByRole('button', { name: /\+ New Link Type/i }));
    await user.type(
      screen.getByLabelText(/Display Name \*/i),
      'Project Members',
    );
    await user.selectOptions(
      screen.getByRole('combobox', { name: /^Source object type$/i }),
      'Project',
    );
    await user.selectOptions(
      screen.getByRole('combobox', { name: /^Target object type$/i }),
      'Employee',
    );
    await user.selectOptions(
      screen.getByRole('combobox', { name: /^Cardinality$/i }),
      'MANY_TO_MANY',
    );
    await user.click(screen.getByRole('button', { name: /^Create$/i }));

    await waitFor(() => {
      expect(state.createCalls.length).toBe(1);
    });
    expect(state.createCalls[0].body).toMatchObject({
      apiName: 'projectMembers',
      displayName: 'Project Members',
      objectTypeApiName: 'Project',
      linkedObjectTypeApiName: 'Employee',
      cardinality: 'MANY_TO_MANY',
      required: false,
    });
  });

  it('blocks creating a LinkType with a duplicate apiName', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Employee → Department')).toBeInTheDocument();
    });
    await user.click(screen.getByRole('button', { name: /\+ New Link Type/i }));
    await user.type(screen.getByLabelText(/Display Name \*/i), 'Other');
    const apiNameInput = screen.getByLabelText(/API Name \*/i) as HTMLInputElement;
    await user.clear(apiNameInput);
    await user.type(apiNameInput, 'employeeProjects');
    const submit = screen.getByRole('button', { name: /^Create$/i });
    expect(submit).toBeDisabled();
    expect(
      screen.getByText(/A LinkType with apiName .* already exists/i),
    ).toBeInTheDocument();
  });

  it('create rejects invalid JSON in foreignKeyConfig', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Employee → Department')).toBeInTheDocument();
    });
    await user.click(screen.getByRole('button', { name: /\+ New Link Type/i }));
    await user.type(
      screen.getByLabelText(/Display Name \*/i),
      'With FK',
    );
    const fk = screen.getByLabelText(/Foreign key config/i) as HTMLTextAreaElement;
    fireEvent.change(fk, { target: { value: '{not valid' } });
    expect(screen.getByText(/Invalid JSON/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^Create$/i })).toBeDisabled();
  });

  it('edit form updates displayName and required via PUT', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Employee → Department')).toBeInTheDocument();
    });
    // Rows sorted by displayName asc:
    //   Department → Employees, Employee → Department, Employee → Projects
    const editButtons = screen.getAllByRole('button', { name: /^Edit$/i });
    await user.click(editButtons[1]);

    const displayInput = (await screen.findByLabelText(
      /Display Name \*/i,
    )) as HTMLInputElement;
    expect(displayInput.value).toBe('Employee → Department');
    await user.clear(displayInput);
    await user.type(displayInput, 'Works In');

    await user.click(screen.getByRole('button', { name: /Save changes/i }));

    await waitFor(() => {
      expect(state.updateCalls.length).toBe(1);
    });
    expect(state.updateCalls[0]).toMatchObject({
      rid: 'ri.ontology.main.link-type.l1',
      body: expect.objectContaining({
        displayName: 'Works In',
        required: true,
      }),
    });
  });

  it('delete modal shows impacted ActionType count from rule walking', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Employee → Projects')).toBeInTheDocument();
    });
    const deleteButtons = screen.getAllByRole('button', { name: /^Delete$/i });
    // Rows sorted asc: index 2 = Employee → Projects (employeeProjects)
    await user.click(deleteButtons[2]);

    await waitFor(() => {
      expect(screen.getByTestId('delete-impact-actions')).toHaveTextContent(
        '1 ActionType',
      );
    });
    expect(
      screen.getByTestId('delete-impact-search-around'),
    ).toBeInTheDocument();
  });

  it('delete modal shows zero impact when no actions reference the link', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Employee → Department')).toBeInTheDocument();
    });
    const deleteButtons = screen.getAllByRole('button', { name: /^Delete$/i });
    // Row index 1 = Employee → Department (employeeDepartment) — no rule refs
    await user.click(deleteButtons[1]);
    await waitFor(() => {
      expect(screen.getByTestId('delete-impact-actions')).toHaveTextContent(
        '0 ActionTypes',
      );
    });
  });

  it('VTX-010 create form sends typeClasses when Vertex tag is checked', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Employee → Department')).toBeInTheDocument();
    });
    await user.click(screen.getByRole('button', { name: /\+ New Link Type/i }));
    await user.type(screen.getByLabelText(/Display Name \*/i), 'Routes To');
    await user.selectOptions(
      screen.getByRole('combobox', { name: /^Cardinality$/i }),
      'MANY_TO_MANY',
    );
    await user.click(
      screen.getByRole('checkbox', { name: /vertex:link_bidirectional/i }),
    );
    await user.click(screen.getByRole('button', { name: /^Create$/i }));

    await waitFor(() => {
      expect(state.createCalls.length).toBe(1);
    });
    expect(state.createCalls[0].body).toMatchObject({
      typeClasses: ['vertex:link_bidirectional'],
    });
  });

  it('US-261 create form sends propagateMarkings when the toggle is checked', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Employee → Department')).toBeInTheDocument();
    });
    await user.click(screen.getByRole('button', { name: /\+ New Link Type/i }));
    await user.type(screen.getByLabelText(/Display Name \*/i), 'Owns Asset');
    await user.selectOptions(
      screen.getByRole('combobox', { name: /^Cardinality$/i }),
      'MANY_TO_MANY',
    );
    // Default: propagation off → payload carries propagateMarkings: false.
    const propagateBox = screen.getByRole('checkbox', {
      name: /Propagate markings/i,
    }) as HTMLInputElement;
    expect(propagateBox.checked).toBe(false);
    await user.click(propagateBox);
    await user.click(screen.getByRole('button', { name: /^Create$/i }));

    await waitFor(() => {
      expect(state.createCalls.length).toBe(1);
    });
    expect(state.createCalls[0].body).toMatchObject({
      propagateMarkings: true,
    });
  });

  it('VTX-010 edit form preserves existing typeClasses and toggles tags', async () => {
    const user = userEvent.setup();
    state.linkTypes[0] = {
      ...state.linkTypes[0],
      typeClasses: ['vertex:link_primary_direction'],
    } as (typeof state.linkTypes)[number];
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Employee → Department')).toBeInTheDocument();
    });
    const editButtons = screen.getAllByRole('button', { name: /^Edit$/i });
    // Rows sorted asc: index 1 = Employee → Department (rid l1)
    await user.click(editButtons[1]);

    const primaryBox = (await screen.findByRole('checkbox', {
      name: /vertex:link_primary_direction/i,
    })) as HTMLInputElement;
    expect(primaryBox.checked).toBe(true);
    // Untick primary, tick bidirectional.
    await user.click(primaryBox);
    await user.click(
      screen.getByRole('checkbox', { name: /vertex:link_bidirectional/i }),
    );
    await user.click(screen.getByRole('button', { name: /Save changes/i }));

    await waitFor(() => {
      expect(state.updateCalls.length).toBe(1);
    });
    expect(state.updateCalls[0]).toMatchObject({
      rid: 'ri.ontology.main.link-type.l1',
      body: expect.objectContaining({
        typeClasses: ['vertex:link_bidirectional'],
      }),
    });
  });

  it('US-209 create form sends inverseLinkRid when an inverse link is selected', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Employee → Department')).toBeInTheDocument();
    });
    await user.click(screen.getByRole('button', { name: /\+ New Link Type/i }));
    await user.type(screen.getByLabelText(/Display Name \*/i), 'Routed Via');
    await user.selectOptions(
      screen.getByRole('combobox', { name: /^Cardinality$/i }),
      'MANY_TO_MANY',
    );
    const inverseSelect = screen.getByRole('combobox', {
      name: /^Inverse link$/i,
    }) as HTMLSelectElement;
    // The "(none)" placeholder maps to an empty value.
    expect(inverseSelect.value).toBe('');
    await user.selectOptions(inverseSelect, 'ri.ontology.main.link-type.l2');
    await user.click(screen.getByRole('button', { name: /^Create$/i }));

    await waitFor(() => {
      expect(state.createCalls.length).toBe(1);
    });
    expect(state.createCalls[0].body).toMatchObject({
      inverseLinkRid: 'ri.ontology.main.link-type.l2',
    });
  });

  it('US-209 create form omits inverseLinkRid when "(none)" is selected', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Employee → Department')).toBeInTheDocument();
    });
    await user.click(screen.getByRole('button', { name: /\+ New Link Type/i }));
    await user.type(screen.getByLabelText(/Display Name \*/i), 'No Inverse');
    await user.click(screen.getByRole('button', { name: /^Create$/i }));

    await waitFor(() => {
      expect(state.createCalls.length).toBe(1);
    });
    const body = state.createCalls[0].body as Record<string, unknown>;
    expect(body.inverseLinkRid).toBeUndefined();
  });

  it('US-209 edit form sets inverseLinkRid via PUT when an inverse is chosen', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Employee → Department')).toBeInTheDocument();
    });
    const editButtons = screen.getAllByRole('button', { name: /^Edit$/i });
    // Rows sorted asc: index 1 = Employee → Department (rid l1)
    await user.click(editButtons[1]);

    const inverseSelect = (await screen.findByRole('combobox', {
      name: /^Inverse link$/i,
    })) as HTMLSelectElement;
    expect(inverseSelect.value).toBe('');
    await user.selectOptions(inverseSelect, 'ri.ontology.main.link-type.l2');
    await user.click(screen.getByRole('button', { name: /Save changes/i }));

    await waitFor(() => {
      expect(state.updateCalls.length).toBe(1);
    });
    expect(state.updateCalls[0]).toMatchObject({
      rid: 'ri.ontology.main.link-type.l1',
      body: expect.objectContaining({
        inverseLinkRid: 'ri.ontology.main.link-type.l2',
      }),
    });
  });

  it('US-209 edit form preloads and clears an existing inverseLinkRid', async () => {
    const user = userEvent.setup();
    state.linkTypes[0] = {
      ...state.linkTypes[0],
      inverseLinkRid: 'ri.ontology.main.link-type.l2',
    } as (typeof state.linkTypes)[number];
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Employee → Department')).toBeInTheDocument();
    });
    const editButtons = screen.getAllByRole('button', { name: /^Edit$/i });
    // Rows sorted asc: index 1 = Employee → Department (rid l1)
    await user.click(editButtons[1]);

    const inverseSelect = (await screen.findByRole('combobox', {
      name: /^Inverse link$/i,
    })) as HTMLSelectElement;
    // Preloaded from the LinkType's current inverseLinkRid.
    expect(inverseSelect.value).toBe('ri.ontology.main.link-type.l2');
    // Clear it back to "(none)".
    await user.selectOptions(inverseSelect, '');
    await user.click(screen.getByRole('button', { name: /Save changes/i }));

    await waitFor(() => {
      expect(state.updateCalls.length).toBe(1);
    });
    // Clearing sends an empty string (tri-state: '' = clear the pairing).
    expect(state.updateCalls[0]).toMatchObject({
      rid: 'ri.ontology.main.link-type.l1',
      body: expect.objectContaining({
        inverseLinkRid: '',
      }),
    });
  });

  it('US-209 inverse-link selector excludes the link being edited itself', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Employee → Department')).toBeInTheDocument();
    });
    const editButtons = screen.getAllByRole('button', { name: /^Edit$/i });
    // Rows sorted asc: index 1 = Employee → Department (rid l1)
    await user.click(editButtons[1]);

    const inverseSelect = (await screen.findByRole('combobox', {
      name: /^Inverse link$/i,
    })) as HTMLSelectElement;
    const optionValues = Array.from(inverseSelect.options).map((o) => o.value);
    // The edited link (l1) is excluded; other links + the "(none)" empty
    // option remain.
    expect(optionValues).not.toContain('ri.ontology.main.link-type.l1');
    expect(optionValues).toContain('');
    expect(optionValues).toContain('ri.ontology.main.link-type.l2');
    expect(optionValues).toContain('ri.ontology.main.link-type.l3');
  });

  it('confirms delete and calls the DELETE endpoint', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Employee → Department')).toBeInTheDocument();
    });
    const deleteButtons = screen.getAllByRole('button', { name: /^Delete$/i });
    await user.click(deleteButtons[0]); // first row after asc sort

    await screen.findByText(/Delete Link Type/i);
    const confirm = screen.getAllByRole('button', { name: /^Delete$/i }).at(-1)!;
    await user.click(confirm);

    await waitFor(() => {
      expect(state.deleteCalls.length).toBe(1);
    });
    expect(state.deleteCalls[0]).toBe('ri.ontology.main.link-type.l2');
  });
});
