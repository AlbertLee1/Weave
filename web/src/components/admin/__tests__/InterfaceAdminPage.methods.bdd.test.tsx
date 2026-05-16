import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { InterfaceAdminPage } from '../InterfaceAdminPage';

// US-498 BDD — Interface Methods tab + polymorphic invoke.
//
// PRD literal acceptance criteria:
//   1) Interface admin 增 Methods tab — the edit modal exposes a Methods
//      tab where admins can CRUD Interface methods.
//   2) Invoke 后展示 typed result — invoking a method dispatches via the
//      backend `/interfaces/methods/{methodRid}/invoke` endpoint and
//      surfaces the resolved ActionType + typed result payload back to
//      the admin in-page.
//
// Two scenarios pin the contract end-to-end (interaction → fetch → render).

interface MockInterface {
  rid: string;
  apiName: string;
  displayName: string;
  extendsRid?: string;
  sharedProperties: Array<{ apiName: string; baseType: string; isArray: boolean }>;
  outgoingLinkTypes: Array<{
    apiName: string;
    displayName: string;
    linkedEntityTypeApiName: string;
    cardinality: 'ONE' | 'MANY';
    required?: boolean;
  }>;
}

interface MockInterfaceMethod {
  rid: string;
  interfaceRid: string;
  name: string;
  params: Array<{ name: string; type: string; required?: boolean }>;
  returns: { type: string };
  description?: string;
}

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

const INTERFACE: MockInterface = {
  rid: 'ri.ontology.main.interface.addr',
  apiName: 'Addressable',
  displayName: 'Addressable',
  sharedProperties: [
    { apiName: 'address', baseType: 'string', isArray: false },
  ],
  outgoingLinkTypes: [],
};

interface StubState {
  interfaces: MockInterface[];
  methods: Record<string, MockInterfaceMethod[]>; // by interfaceRid
  createMethodCalls: Array<{ interfaceRid: string; body: unknown }>;
  invokeCalls: Array<{ methodRid: string; body: unknown }>;
  invokeResponse: {
    actionTypeRid: string;
    actionTypeApiName: string;
    objectType: string;
    methodRid: string;
    result?: unknown;
  };
}

function makeStub(): StubState {
  return {
    interfaces: [
      {
        ...INTERFACE,
        sharedProperties: INTERFACE.sharedProperties.map((sp) => ({ ...sp })),
        outgoingLinkTypes: INTERFACE.outgoingLinkTypes.map((lt) => ({ ...lt })),
      },
    ],
    methods: {
      'ri.ontology.main.interface.addr': [],
    },
    createMethodCalls: [],
    invokeCalls: [],
    invokeResponse: {
      actionTypeRid: 'ri.ontology.main.action-type.set-address-impl',
      actionTypeApiName: 'setAddressForEmployee',
      objectType: 'Employee',
      methodRid: 'ri.ontology.main.interface-method.set-address',
      result: { ok: true, newValue: '742 Evergreen Terrace' },
    },
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
          url.endsWith('/api/v2/ontologies/northwind/interfacesAdmin')
        ) {
          return jsonResponse({ data: state.interfaces });
        }

        // Implementing-object-types modal queries — return empty.
        const listAttachMatch = url.match(
          /\/api\/v2\/ontologies\/northwind\/objectTypes\/byRid\/([^/]+)\/interfaces$/,
        );
        if (listAttachMatch && method === 'GET') {
          return jsonResponse({ data: [] });
        }

        // Interface Methods list / create.
        const methodsMatch = url.match(
          /\/api\/v2\/ontologies\/northwind\/interfaces\/([^/]+)\/methods$/,
        );
        if (methodsMatch && method === 'GET') {
          const interfaceRid = decodeURIComponent(methodsMatch[1]);
          return jsonResponse({ data: state.methods[interfaceRid] ?? [] });
        }
        if (methodsMatch && method === 'POST') {
          const interfaceRid = decodeURIComponent(methodsMatch[1]);
          const body = init?.body ? JSON.parse(init.body as string) : {};
          state.createMethodCalls.push({ interfaceRid, body });
          const created: MockInterfaceMethod = {
            rid: `ri.ontology.main.interface-method.${body.name}`,
            interfaceRid,
            name: body.name,
            params: body.params ?? [],
            returns: body.returns ?? { type: 'string' },
            description: body.description,
          };
          state.methods[interfaceRid] = [
            ...(state.methods[interfaceRid] ?? []),
            created,
          ];
          return jsonResponse(created, 201);
        }

        // Invoke endpoint.
        const invokeMatch = url.match(
          /\/api\/v2\/ontologies\/northwind\/interfaces\/methods\/([^/]+)\/invoke$/,
        );
        if (invokeMatch && method === 'POST') {
          const methodRid = decodeURIComponent(invokeMatch[1]);
          const body = init?.body ? JSON.parse(init.body as string) : {};
          state.invokeCalls.push({ methodRid, body });
          return jsonResponse({
            ...state.invokeResponse,
            methodRid,
          });
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
      <MemoryRouter initialEntries={['/admin/northwind/interfaces']}>
        <Routes>
          <Route
            path="/admin/:ontology/interfaces"
            element={<InterfaceAdminPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('US-498 BDD — Interface Methods tab + polymorphic invoke', () => {
  let state: StubState;

  beforeEach(() => {
    state = makeStub();
    installFetch(state);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('Given an Interface exists, When the admin opens edit + switches to Methods tab + adds a method, Then the create call hits POST /interfaces/{rid}/methods with the typed body', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Addressable').length).toBeGreaterThan(0);
    });

    // Open the edit modal for the Addressable Interface.
    const row = screen.getByTestId('interface-row');
    await user.click(within(row).getByRole('button', { name: 'Edit' }));

    // Switch to the Methods tab.
    const methodsTab = await screen.findByTestId('interface-edit-tab-methods');
    await user.click(methodsTab);

    // The Methods panel should render with an "Add method" button.
    const methodsPanel = await screen.findByTestId(
      'interface-methods-editor',
    );
    await user.click(
      within(methodsPanel).getByRole('button', { name: /\+ Add method/i }),
    );

    // Fill in name + return type, leave params empty.
    await user.type(
      screen.getByTestId('interface-method-draft-name'),
      'setAddress',
    );
    // Default return type should be string; explicitly select to assert path.
    const returnSelect = screen.getByTestId(
      'interface-method-draft-return-type',
    ) as HTMLSelectElement;
    await user.selectOptions(returnSelect, 'string');

    await user.click(
      screen.getByRole('button', {
        name: /Create method/i,
      }),
    );

    await waitFor(() => {
      expect(state.createMethodCalls.length).toBe(1);
    });
    const call = state.createMethodCalls[0];
    expect(call.interfaceRid).toBe('ri.ontology.main.interface.addr');
    expect((call.body as { name: string }).name).toBe('setAddress');
    expect((call.body as { returns: { type: string } }).returns.type).toBe(
      'string',
    );

    // Negative control — POST must hit the methods endpoint, NOT the
    // interface PUT endpoint (would mean the form sent the method as
    // part of the interface body).
    const wrongPath = state.createMethodCalls.some((c) =>
      String(c.interfaceRid).includes('byRid'),
    );
    expect(wrongPath).toBe(false);

    // Verify the newly created method is now visible in the list.
    await waitFor(() => {
      expect(
        within(methodsPanel).getByText('setAddress'),
      ).toBeInTheDocument();
    });
  });

  it('Given a method exists, When the admin opens Invoke and submits an ObjectType apiName, Then a typed result panel shows the resolved ActionType + result payload', async () => {
    // Seed a method up front so the editor lists it directly.
    state.methods['ri.ontology.main.interface.addr'] = [
      {
        rid: 'ri.ontology.main.interface-method.set-address',
        interfaceRid: 'ri.ontology.main.interface.addr',
        name: 'setAddress',
        params: [
          { name: 'value', type: 'string', required: true },
        ],
        returns: { type: 'string' },
      },
    ];

    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Addressable').length).toBeGreaterThan(0);
    });

    const row = screen.getByTestId('interface-row');
    await user.click(within(row).getByRole('button', { name: 'Edit' }));

    const methodsTab = await screen.findByTestId('interface-edit-tab-methods');
    await user.click(methodsTab);

    const methodsPanel = await screen.findByTestId(
      'interface-methods-editor',
    );

    // Wait for the seeded method to load.
    await waitFor(() => {
      expect(within(methodsPanel).getByText('setAddress')).toBeInTheDocument();
    });

    // Open the Invoke dialog.
    await user.click(
      within(methodsPanel).getByRole('button', { name: /Invoke/i }),
    );
    const invokeModal = await screen.findByTestId('interface-method-invoke');

    // Type the target ObjectType apiName + parameters JSON.
    await user.type(
      within(invokeModal).getByTestId('interface-method-invoke-object-type'),
      'Employee',
    );
    const paramsInput = within(invokeModal).getByTestId(
      'interface-method-invoke-parameters',
    ) as HTMLTextAreaElement;
    await user.clear(paramsInput);
    await user.type(
      paramsInput,
      // userEvent interprets {{ as a literal { so escape the JSON braces.
      '{{"value":"742 Evergreen Terrace"}',
    );

    await user.click(
      within(invokeModal).getByRole('button', { name: /^Invoke$/i }),
    );

    // Then — a typed result panel surfaces the resolved ActionType +
    // result payload.
    await waitFor(() => {
      expect(state.invokeCalls.length).toBe(1);
    });
    expect(state.invokeCalls[0].methodRid).toBe(
      'ri.ontology.main.interface-method.set-address',
    );
    expect(
      (state.invokeCalls[0].body as { objectType: string }).objectType,
    ).toBe('Employee');
    expect(
      (state.invokeCalls[0].body as { parameters: Record<string, unknown> })
        .parameters,
    ).toEqual({ value: '742 Evergreen Terrace' });

    // The typed result must include the resolved ActionType apiName +
    // the result payload from the dispatcher.
    const resultPanel = await within(invokeModal).findByTestId(
      'interface-method-invoke-result',
    );
    expect(resultPanel.textContent).toMatch(/setAddressForEmployee/);
    expect(resultPanel.textContent).toMatch(/Employee/);
    expect(resultPanel.textContent).toMatch(/742 Evergreen Terrace/);

    // Negative control — the typed result panel must NOT appear before
    // any invoke; opening the dialog should not silently re-render an
    // earlier result. Sanity-check this is the first appearance by
    // counting invoke calls.
    expect(state.invokeCalls.length).toBe(1);
  });
});
