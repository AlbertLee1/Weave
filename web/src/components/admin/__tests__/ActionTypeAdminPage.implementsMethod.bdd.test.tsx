import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ActionTypeAdminPage } from '../ActionTypeAdminPage';

// Unit BDD: bind an ActionType to the interface method it implements.
//
// The v2 backend's Create/UpdateActionTypeRequest already accept
// `implementsMethodRid` (pkg/oms/admin_handlers.go), but the builder had no
// control for it. This scenario covers the new optional "Implements interface
// method" selector.
//
// Given the Ontology Manager ActionType builder, with two interfaces each
//       exposing methods,
// When  the author opens the selector
// Then  it lists every method aggregated across the interfaces, labelled
//       "{interfaceDisplayName}.{methodName}".
// When  the author picks a method and submits a Create
// Then  the POST body carries implementsMethodRid = <method rid>.
// And   leaving the selector at "(none)" omits the key.
// And   the Edit form preloads the action's existing implementsMethodRid and
//       round-trips it on Save.

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

const INTERFACE_TYPES = [
  {
    rid: 'ri.ontology.main.interface.Hireable',
    apiName: 'Hireable',
    displayName: 'Hireable',
  },
  {
    rid: 'ri.ontology.main.interface.Payable',
    apiName: 'Payable',
    displayName: 'Payable',
  },
];

const METHODS_BY_INTERFACE: Record<string, unknown[]> = {
  'ri.ontology.main.interface.Hireable': [
    {
      rid: 'ri.ontology.main.interface-method.hire',
      interfaceRid: 'ri.ontology.main.interface.Hireable',
      name: 'hire',
      params: [],
      returns: { type: 'boolean' },
    },
    {
      rid: 'ri.ontology.main.interface-method.terminate',
      interfaceRid: 'ri.ontology.main.interface.Hireable',
      name: 'terminate',
      params: [],
      returns: { type: 'boolean' },
    },
  ],
  'ri.ontology.main.interface.Payable': [
    {
      rid: 'ri.ontology.main.interface-method.pay',
      interfaceRid: 'ri.ontology.main.interface.Payable',
      name: 'pay',
      params: [],
      returns: { type: 'boolean' },
    },
  ],
};

const ACTION_TYPES = [
  {
    rid: 'ri.ontology.main.action-type.promote',
    apiName: 'promoteEmployee',
    displayName: 'Promote Employee',
    status: 'ACTIVE',
    parameters: {},
    rules: [],
  },
  {
    rid: 'ri.ontology.main.action-type.hireEmployee',
    apiName: 'hireEmployee',
    displayName: 'Hire Employee',
    status: 'ACTIVE',
    parameters: {},
    rules: [],
    // Already bound to the Hireable.hire method.
    implementsMethodRid: 'ri.ontology.main.interface-method.hire',
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
  implementsMethodRid?: string;
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
          url.includes('/api/v2/ontologies/northwind/interfaceTypes')
        ) {
          return jsonResponse({ data: INTERFACE_TYPES });
        }
        const methodsMatch = url.match(
          /\/api\/v2\/ontologies\/northwind\/interfaces\/([^/]+)\/methods/,
        );
        if (method === 'GET' && methodsMatch) {
          const interfaceRid = decodeURIComponent(methodsMatch[1]);
          return jsonResponse({
            data: METHODS_BY_INTERFACE[interfaceRid] ?? [],
          });
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

describe('ActionTypeAdminPage implements interface method', () => {
  let state: StubState;

  beforeEach(() => {
    state = makeStub();
    installFetch(state);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('lists methods aggregated across interfaces and sends implementsMethodRid on Create', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Hire Employee')).toBeInTheDocument();
    });
    await user.click(screen.getByRole('button', { name: /\+ New Action Type/i }));
    await user.type(
      await screen.findByLabelText(/Display Name \*/i),
      'Onboard Employee',
    );

    const select = (await screen.findByTestId(
      'action-type-implements-method-select',
    )) as HTMLSelectElement;

    // The selector aggregates every method across both interfaces and labels
    // them "{interfaceDisplayName}.{methodName}".
    await waitFor(() => {
      const labels = Array.from(select.options).map((o) => o.textContent);
      expect(labels).toContain('Hireable.hire');
      expect(labels).toContain('Hireable.terminate');
      expect(labels).toContain('Payable.pay');
    });

    await user.selectOptions(
      select,
      'ri.ontology.main.interface-method.pay',
    );

    await user.click(screen.getByRole('button', { name: /^Create$/i }));

    await waitFor(() => {
      expect(state.createCalls.length).toBe(1);
    });
    const body = state.createCalls[0].body as Record<string, unknown>;
    expect(body.implementsMethodRid).toBe(
      'ri.ontology.main.interface-method.pay',
    );
  });

  it('omits implementsMethodRid when the selector is left at "(none)"', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Hire Employee')).toBeInTheDocument();
    });
    await user.click(screen.getByRole('button', { name: /\+ New Action Type/i }));
    await user.type(
      await screen.findByLabelText(/Display Name \*/i),
      'Onboard Employee',
    );

    // Leave the selector at its default "(none)".
    await user.click(screen.getByRole('button', { name: /^Create$/i }));

    await waitFor(() => {
      expect(state.createCalls.length).toBe(1);
    });
    const body = state.createCalls[0].body as Record<string, unknown>;
    expect('implementsMethodRid' in body).toBe(false);
  });

  it('preloads the existing implementsMethodRid in the Edit form and round-trips it on Save', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Hire Employee')).toBeInTheDocument();
    });
    const row = screen.getByText('Hire Employee').closest('tr')!;
    await user.click(within(row).getByRole('button', { name: /Edit/i }));

    const select = (await screen.findByTestId(
      'action-type-implements-method-select',
    )) as HTMLSelectElement;
    await waitFor(() => {
      expect(select.value).toBe('ri.ontology.main.interface-method.hire');
    });

    await user.click(screen.getByRole('button', { name: /Save changes/i }));
    await waitFor(() => {
      expect(state.updateCalls.length).toBe(1);
    });
    const body = state.updateCalls[0].body as Record<string, unknown>;
    expect(body.implementsMethodRid).toBe(
      'ri.ontology.main.interface-method.hire',
    );
  });

  it('clears the binding by sending implementsMethodRid="" when Edit picks "(none)"', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Hire Employee')).toBeInTheDocument();
    });
    const row = screen.getByText('Hire Employee').closest('tr')!;
    await user.click(within(row).getByRole('button', { name: /Edit/i }));

    const select = (await screen.findByTestId(
      'action-type-implements-method-select',
    )) as HTMLSelectElement;
    await waitFor(() => {
      expect(select.value).toBe('ri.ontology.main.interface-method.hire');
    });

    // Reset the binding to "(none)".
    await user.selectOptions(select, '');

    await user.click(screen.getByRole('button', { name: /Save changes/i }));
    await waitFor(() => {
      expect(state.updateCalls.length).toBe(1);
    });
    const body = state.updateCalls[0].body as Record<string, unknown>;
    // The server treats "" as "clear the binding" (tri-state pointer), so the
    // edit path must send the empty string rather than omit the key.
    expect(body.implementsMethodRid).toBe('');
  });
});
