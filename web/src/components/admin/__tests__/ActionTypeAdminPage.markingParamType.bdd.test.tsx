import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ActionTypeAdminPage } from '../ActionTypeAdminPage';

// BDD: the ActionType admin builder must let an author declare a `marking`-typed
// parameter. The v2 backend persists `parameters` as opaque json.RawMessage,
// and the runtime ParameterForm already renders a MarkingSelectField for
// `type: 'marking'` (gracefully degrading when the marking catalog is empty),
// but the builder's PARAMETER_TYPES dropdown previously stopped at `media` —
// leaving no way to declare a marking parameter.
//
// Given the Ontology Manager ActionType builder
// When  the author adds a parameter and opens its type dropdown
// Then  a `marking` option is offered, and selecting it makes the create
//       (and edit) request body carry that parameter with type:'marking'.

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

const LINK_TYPES: unknown[] = [];

const ACTION_TYPES = [
  {
    rid: 'ri.ontology.main.action-type.classifyDoc',
    apiName: 'classifyDoc',
    displayName: 'Classify Document',
    status: 'ACTIVE',
    // Existing action whose parameter is already marking-typed, so the edit
    // path can prove the dropdown pre-selects and round-trips `marking`.
    parameters: {
      classification: { dataType: { type: 'marking' }, required: true },
    },
    rules: [],
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
}

function makeStub(): StubState {
  return {
    actionTypes: JSON.parse(JSON.stringify(ACTION_TYPES)),
    createCalls: [],
    updateCalls: [],
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
          state.actionTypes[idx] = {
            ...prev,
            displayName: body.displayName ?? prev.displayName,
            status: body.status ?? prev.status,
          };
          return jsonResponse(state.actionTypes[idx]);
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

describe('ActionTypeAdminPage marking parameter type', () => {
  let state: StubState;

  beforeEach(() => {
    state = makeStub();
    installFetch(state);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('offers a marking option in the parameter type dropdown', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Classify Document')).toBeInTheDocument();
    });
    await user.click(screen.getByRole('button', { name: /\+ New Action Type/i }));
    await user.type(
      await screen.findByLabelText(/Display Name \*/i),
      'Tag Document',
    );

    await user.click(screen.getByTestId('action-type-add-parameter'));

    const typeSelect = await screen.findByLabelText(/Parameter 1 type/i);
    const markingOption = within(typeSelect as HTMLSelectElement).getByRole(
      'option',
      { name: 'marking' },
    );
    expect(markingOption).toBeInTheDocument();
  });

  it('create body carries a marking-typed parameter when marking is selected', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Classify Document')).toBeInTheDocument();
    });
    await user.click(screen.getByRole('button', { name: /\+ New Action Type/i }));
    await user.type(
      await screen.findByLabelText(/Display Name \*/i),
      'Tag Document',
    );

    await user.click(screen.getByTestId('action-type-add-parameter'));
    await user.type(
      await screen.findByLabelText(/Parameter 1 id/i),
      'sensitivity',
    );
    await user.selectOptions(
      await screen.findByLabelText(/Parameter 1 type/i),
      'marking',
    );

    await user.click(screen.getByRole('button', { name: /^Create$/i }));

    await waitFor(() => {
      expect(state.createCalls.length).toBe(1);
    });
    const body = state.createCalls[0].body as Record<string, unknown>;
    const params = body.parameters as Array<{ id: string; type: string }>;
    const sensitivity = params.find((p) => p.id === 'sensitivity');
    expect(sensitivity).toBeDefined();
    expect(sensitivity?.type).toBe('marking');
  });

  it('edit modal pre-selects marking and round-trips it on save', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Classify Document')).toBeInTheDocument();
    });
    const row = screen.getByText('Classify Document').closest('tr')!;
    await user.click(within(row).getByRole('button', { name: /Edit/i }));

    const typeSelect = (await screen.findByLabelText(
      /Parameter 1 type/i,
    )) as HTMLSelectElement;
    expect(typeSelect.value).toBe('marking');

    await user.click(screen.getByRole('button', { name: /Save changes/i }));
    await waitFor(() => {
      expect(state.updateCalls.length).toBe(1);
    });
    const body = state.updateCalls[0].body as Record<string, unknown>;
    const params = body.parameters as Array<{ id: string; type: string }>;
    const classification = params.find((p) => p.id === 'classification');
    expect(classification?.type).toBe('marking');
  });
});
