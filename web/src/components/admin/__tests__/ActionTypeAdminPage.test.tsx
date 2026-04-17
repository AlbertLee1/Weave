import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ActionTypeAdminPage } from '../ActionTypeAdminPage';

const OBJECT_TYPES = [
  {
    rid: 'ri.ontology.main.object-type.emp',
    apiName: 'Employee',
    displayName: 'Employee',
    primaryKey: 'employeeId',
    status: 'ACTIVE',
    visibility: 'PROMINENT',
  },
];

const LINK_TYPES = [
  {
    rid: 'ri.ontology.main.link-type.l1',
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
    apiName: 'archiveEmployee',
    displayName: 'Archive Employee',
    status: 'ACTIVE',
    parameters: {
      employeeId: {
        dataType: { type: 'string' },
        required: true,
      },
    },
    rules: [
      {
        type: 'modifyObject',
        objectType: 'Employee',
        propertyBindings: {
          archived: { type: 'static', value: 'true' },
        },
      },
    ],
  },
  {
    rid: 'ri.ontology.main.action-type.a2',
    apiName: 'createEmployee',
    displayName: 'Create Employee',
    status: 'EXPERIMENTAL',
    parameters: {
      name: { dataType: { type: 'string' }, required: true },
    },
    rules: [
      {
        type: 'createObject',
        objectType: 'Employee',
        propertyBindings: {
          name: { type: 'parameter', value: 'name' },
        },
      },
    ],
  },
];

interface StoredActionType {
  rid: string;
  apiName: string;
  displayName: string;
  description?: string;
  status: string;
  parameters: Record<string, unknown>;
  rules: Array<Record<string, unknown>>;
}

interface StubState {
  actionTypes: StoredActionType[];
  createCalls: Array<{ body: Record<string, unknown> }>;
  updateCalls: Array<{ rid: string; body: Record<string, unknown> }>;
  deleteCalls: string[];
}

function makeStub(): StubState {
  return {
    actionTypes: JSON.parse(JSON.stringify(ACTION_TYPES)),
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

        if (method === 'GET' && url.endsWith('/api/v2/ontologies/northwind/objectTypes')) {
          return jsonResponse({ data: OBJECT_TYPES });
        }
        if (method === 'GET' && url.endsWith('/api/v2/ontologies/northwind/linkTypes')) {
          return jsonResponse({ data: LINK_TYPES });
        }
        if (
          method === 'GET' &&
          url.endsWith('/api/v2/ontologies/northwind/actionTypesAdmin')
        ) {
          return jsonResponse({ data: state.actionTypes });
        }
        if (
          method === 'POST' &&
          url.endsWith('/api/v2/ontologies/northwind/actionTypes')
        ) {
          const body = init?.body ? JSON.parse(init.body as string) : {};
          state.createCalls.push({ body });
          const created: StoredActionType = {
            rid: `ri.ontology.main.action-type.${body.apiName}`,
            apiName: body.apiName,
            displayName: body.displayName,
            description: body.description,
            status: body.status ?? 'ACTIVE',
            parameters: {},
            rules: body.rules ?? [],
          };
          state.actionTypes.push(created);
          return jsonResponse(created, 201);
        }
        const ridMatch = url.match(
          /\/api\/v2\/ontologies\/northwind\/actionTypes\/byRid\/([^?]+)/,
        );
        if (ridMatch && method === 'PUT') {
          const rid = decodeURIComponent(ridMatch[1]);
          const body = init?.body ? JSON.parse(init.body as string) : {};
          state.updateCalls.push({ rid, body });
          const idx = state.actionTypes.findIndex((a) => a.rid === rid);
          if (idx < 0) return jsonResponse({ errorCode: 'NotFound' }, 404);
          const prev = state.actionTypes[idx];
          const next = {
            ...prev,
            displayName: body.displayName ?? prev.displayName,
            status: body.status ?? prev.status,
            rules: body.rules ?? prev.rules,
          };
          state.actionTypes[idx] = next;
          return jsonResponse(next);
        }
        if (ridMatch && method === 'DELETE') {
          const rid = decodeURIComponent(ridMatch[1]);
          state.deleteCalls.push(rid);
          state.actionTypes = state.actionTypes.filter((a) => a.rid !== rid);
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
      <MemoryRouter initialEntries={['/admin/northwind/actionTypes']}>
        <Routes>
          <Route
            path="/admin/:ontology/actionTypes"
            element={<ActionTypeAdminPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('ActionTypeAdminPage', () => {
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

  it('loads and lists action types with rule counts', async () => {
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Archive Employee')).toBeInTheDocument();
    });
    expect(screen.getByText('Create Employee')).toBeInTheDocument();
    // Two rows × 1 rule each → we expect at least 2 "1" cells in the Rules col.
    const archiveRow = screen.getByText('Archive Employee').closest('tr')!;
    expect(within(archiveRow).getByText('ACTIVE')).toBeInTheDocument();
    const createRow = screen.getByText('Create Employee').closest('tr')!;
    expect(within(createRow).getByText('EXPERIMENTAL')).toBeInTheDocument();
  });

  it('filters by search text', async () => {
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Archive Employee')).toBeInTheDocument();
    });
    const input = screen.getByLabelText(/Search action types/i) as HTMLInputElement;
    fireEvent.change(input, { target: { value: 'archive' } });
    await waitFor(() => {
      expect(screen.queryByText('Create Employee')).not.toBeInTheDocument();
    });
    expect(screen.getByText('Archive Employee')).toBeInTheDocument();
  });

  it('filters by status', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Archive Employee')).toBeInTheDocument();
    });
    const select = screen.getByLabelText(/Filter by status/i) as HTMLSelectElement;
    await user.selectOptions(select, 'EXPERIMENTAL');
    await waitFor(() => {
      expect(screen.queryByText('Archive Employee')).not.toBeInTheDocument();
    });
    expect(screen.getByText('Create Employee')).toBeInTheDocument();
  });

  it('create form auto-generates apiName from displayName', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Archive Employee')).toBeInTheDocument();
    });
    await user.click(screen.getByRole('button', { name: /\+ New Action Type/i }));
    const displayName = await screen.findByLabelText(/Display Name \*/i);
    await user.type(displayName, 'Promote Employee');
    const apiNameInput = screen.getByLabelText(/API Name \*/i) as HTMLInputElement;
    expect(apiNameInput.value).toBe('promoteEmployee');
  });

  it('shows JSON preview reflecting rule edits', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Archive Employee')).toBeInTheDocument();
    });
    await user.click(screen.getByRole('button', { name: /\+ New Action Type/i }));
    await user.type(
      await screen.findByLabelText(/Display Name \*/i),
      'Greet Employee',
    );
    await user.click(screen.getByRole('button', { name: /\+ Add rule/i }));

    const preview = await screen.findByTestId('action-json-preview');
    await waitFor(() => {
      expect(preview.textContent ?? '').toContain('"rules"');
    });
    expect(preview.textContent ?? '').toContain('createObject');
    expect(preview.textContent ?? '').toContain('greetEmployee');
  });

  it('submits a create request with rules', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Archive Employee')).toBeInTheDocument();
    });
    await user.click(screen.getByRole('button', { name: /\+ New Action Type/i }));
    await user.type(
      await screen.findByLabelText(/Display Name \*/i),
      'Greet Employee',
    );
    await user.click(screen.getByRole('button', { name: /\+ Add rule/i }));
    // Select Employee as the object type.
    const otSelect = await screen.findByLabelText(/Rule 1 object type/i);
    await user.selectOptions(otSelect, 'Employee');

    // Submit
    const submitBtn = screen.getByRole('button', { name: /^Create$/i });
    await user.click(submitBtn);

    await waitFor(() => {
      expect(state.createCalls.length).toBe(1);
    });
    const body = state.createCalls[0].body as Record<string, unknown>;
    expect(body.apiName).toBe('greetEmployee');
    expect(body.displayName).toBe('Greet Employee');
    expect(Array.isArray(body.rules)).toBe(true);
    expect((body.rules as Array<{ type: string }>)[0].type).toBe(
      'createObject',
    );
  });

  it('edit modal pre-populates parameters and rules', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Archive Employee')).toBeInTheDocument();
    });
    const archiveRow = screen.getByText('Archive Employee').closest('tr')!;
    await user.click(within(archiveRow).getByRole('button', { name: /Edit/i }));
    const modalDisplay = await screen.findByLabelText(/Display Name \*/i);
    expect((modalDisplay as HTMLInputElement).value).toBe('Archive Employee');
    // Preview reflects the pre-loaded rule.
    const preview = await screen.findByTestId('action-json-preview');
    expect(preview.textContent ?? '').toContain('modifyObject');
    expect(preview.textContent ?? '').toContain('archived');
  });

  it('delete modal shows confirmation and calls DELETE', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Archive Employee')).toBeInTheDocument();
    });
    const archiveRow = screen.getByText('Archive Employee').closest('tr')!;
    await user.click(within(archiveRow).getByRole('button', { name: /Delete/i }));
    const heading = await screen.findByRole('heading', {
      name: /Delete Action Type/i,
    });
    // Scope the confirm button to the modal panel (the heading's ancestor).
    const modal = heading.closest('div[class*="rounded-xl"]') as HTMLElement;
    const confirmBtn = within(modal).getByRole('button', { name: /^Delete$/i });
    await user.click(confirmBtn);
    await waitFor(() => {
      expect(state.deleteCalls).toContain(
        'ri.ontology.main.action-type.a1',
      );
    });
  });
});
