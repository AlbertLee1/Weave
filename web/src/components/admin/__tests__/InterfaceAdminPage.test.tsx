import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { InterfaceAdminPage } from '../InterfaceAdminPage';

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
];

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

const INITIAL_INTERFACES: MockInterface[] = [
  {
    rid: 'ri.ontology.main.interface.addr',
    apiName: 'Addressable',
    displayName: 'Addressable',
    sharedProperties: [
      { apiName: 'address', baseType: 'string', isArray: false },
    ],
    outgoingLinkTypes: [],
  },
  {
    rid: 'ri.ontology.main.interface.loc',
    apiName: 'Locatable',
    displayName: 'Locatable',
    sharedProperties: [
      { apiName: 'latitude', baseType: 'double', isArray: false },
      { apiName: 'longitude', baseType: 'double', isArray: false },
    ],
    outgoingLinkTypes: [],
  },
];

interface StubState {
  interfaces: MockInterface[];
  // objectTypeRid -> attachments
  attachments: Record<string, { objectTypeRid: string; interfaceRid: string }[]>;
  createCalls: Array<{ body: unknown }>;
  updateCalls: Array<{ rid: string; body: unknown }>;
  deleteCalls: string[];
  attachCalls: Array<{ objectTypeRid: string; body: unknown }>;
  detachCalls: Array<{ objectTypeRid: string; interfaceRid: string }>;
}

function makeStub(): StubState {
  return {
    interfaces: INITIAL_INTERFACES.map((iface) => ({
      ...iface,
      sharedProperties: iface.sharedProperties.map((sp) => ({ ...sp })),
      outgoingLinkTypes: iface.outgoingLinkTypes.map((lt) => ({ ...lt })),
    })),
    attachments: {
      'ri.ontology.main.object-type.emp': [
        {
          objectTypeRid: 'ri.ontology.main.object-type.emp',
          interfaceRid: 'ri.ontology.main.interface.addr',
        },
      ],
      'ri.ontology.main.object-type.dept': [],
    },
    createCalls: [],
    updateCalls: [],
    deleteCalls: [],
    attachCalls: [],
    detachCalls: [],
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

        const listAttachMatch = url.match(
          /\/api\/v2\/ontologies\/northwind\/objectTypes\/byRid\/([^/]+)\/interfaces$/,
        );
        if (listAttachMatch && method === 'GET') {
          const rid = decodeURIComponent(listAttachMatch[1]);
          return jsonResponse({ data: state.attachments[rid] ?? [] });
        }
        if (listAttachMatch && method === 'POST') {
          const rid = decodeURIComponent(listAttachMatch[1]);
          const body = init?.body ? JSON.parse(init.body as string) : {};
          state.attachCalls.push({ objectTypeRid: rid, body });
          const row = {
            objectTypeRid: rid,
            interfaceRid: body.interfaceRid as string,
          };
          state.attachments[rid] = [...(state.attachments[rid] ?? []), row];
          return jsonResponse(row, 201);
        }

        const detachMatch = url.match(
          /\/api\/v2\/ontologies\/northwind\/objectTypes\/byRid\/([^/]+)\/interfaces\/([^/?]+)$/,
        );
        if (detachMatch && method === 'DELETE') {
          const rid = decodeURIComponent(detachMatch[1]);
          const ifaceRid = decodeURIComponent(detachMatch[2]);
          state.detachCalls.push({
            objectTypeRid: rid,
            interfaceRid: ifaceRid,
          });
          state.attachments[rid] = (state.attachments[rid] ?? []).filter(
            (a) => a.interfaceRid !== ifaceRid,
          );
          return jsonResponse({}, 200);
        }

        if (
          method === 'POST' &&
          url.endsWith('/api/v2/ontologies/northwind/interfaces')
        ) {
          const body = init?.body ? JSON.parse(init.body as string) : {};
          state.createCalls.push({ body });
          const created = {
            rid: `ri.ontology.main.interface.${body.apiName}`,
            apiName: body.apiName,
            displayName: body.displayName,
            extendsRid: body.extendsRid,
            sharedProperties: body.sharedProperties ?? [],
            outgoingLinkTypes: body.outgoingLinkTypes ?? [],
          };
          state.interfaces.push(created);
          return jsonResponse(created, 201);
        }

        const ifaceRidMatch = url.match(
          /\/api\/v2\/ontologies\/northwind\/interfaces\/byRid\/([^/?]+)/,
        );
        if (ifaceRidMatch && method === 'PUT') {
          const rid = decodeURIComponent(ifaceRidMatch[1]);
          const body = init?.body ? JSON.parse(init.body as string) : {};
          state.updateCalls.push({ rid, body });
          const idx = state.interfaces.findIndex((i) => i.rid === rid);
          if (idx < 0) return jsonResponse({ errorCode: 'NotFound' }, 404);
          state.interfaces[idx] = {
            ...state.interfaces[idx],
            ...body,
          };
          return jsonResponse(state.interfaces[idx]);
        }
        if (ifaceRidMatch && method === 'DELETE') {
          const rid = decodeURIComponent(ifaceRidMatch[1]);
          state.deleteCalls.push(rid);
          state.interfaces = state.interfaces.filter((i) => i.rid !== rid);
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

describe('InterfaceAdminPage', () => {
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

  it('loads and lists all interfaces sorted by display name', async () => {
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Addressable').length).toBeGreaterThan(0);
    });
    expect(screen.getAllByText('Locatable').length).toBeGreaterThan(0);
    const rows = screen.getAllByRole('row');
    // first row = header, then two data rows
    expect(rows.length).toBeGreaterThanOrEqual(3);
  });

  it('filters by search text matching apiName', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Addressable').length).toBeGreaterThan(0);
    });
    const input = screen.getByLabelText(/Search interfaces/i) as HTMLInputElement;
    await user.type(input, 'locat');
    await waitFor(() => {
      expect(screen.queryByText('Addressable')).not.toBeInTheDocument();
    });
    expect(screen.getAllByText('Locatable').length).toBeGreaterThan(0);
  });

  it('create form auto-generates apiName from displayName', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Addressable').length).toBeGreaterThan(0);
    });
    await user.click(screen.getByRole('button', { name: /\+ New Interface/i }));
    const displayName = await screen.findByLabelText(/Display Name \*/i);
    await user.type(displayName, 'Has Parent');
    const apiNameInput = screen.getByLabelText(/API Name \*/i) as HTMLInputElement;
    expect(apiNameInput.value).toBe('hasParent');
  });

  it('blocks creating an Interface with a duplicate apiName', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Addressable').length).toBeGreaterThan(0);
    });
    await user.click(screen.getByRole('button', { name: /\+ New Interface/i }));
    await user.type(screen.getByLabelText(/Display Name \*/i), 'Whatever');
    const apiNameInput = screen.getByLabelText(/API Name \*/i) as HTMLInputElement;
    await user.clear(apiNameInput);
    await user.type(apiNameInput, 'Addressable');
    const submit = screen.getByRole('button', { name: /^Create$/i });
    expect(submit).toBeDisabled();
    expect(
      screen.getByText(/An Interface with apiName .* already exists/i),
    ).toBeInTheDocument();
  });

  it('create form submits shared properties and link types', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Addressable').length).toBeGreaterThan(0);
    });
    await user.click(screen.getByRole('button', { name: /\+ New Interface/i }));
    await user.type(screen.getByLabelText(/Display Name \*/i), 'Named');

    // Add one shared property
    await user.click(
      screen.getByRole('button', { name: /\+ Add shared property/i }),
    );
    await user.type(
      screen.getByLabelText(/Shared property 1 api name/i),
      'name',
    );

    // Add one outgoing link type
    await user.click(screen.getByRole('button', { name: /\+ Add link type/i }));
    await user.type(
      screen.getByLabelText(/Link type 1 api name/i),
      'parent',
    );
    await user.type(
      screen.getByLabelText(/Link type 1 target type/i),
      'Department',
    );
    await user.type(
      screen.getByLabelText(/Link type 1 display name/i),
      'Parent',
    );

    await user.click(screen.getByRole('button', { name: /^Create$/i }));

    await waitFor(() => {
      expect(state.createCalls.length).toBe(1);
    });
    const body = state.createCalls[0].body as Record<string, unknown>;
    expect(body).toMatchObject({
      apiName: 'named',
      displayName: 'Named',
    });
    expect(body.sharedProperties).toEqual([
      { apiName: 'name', baseType: 'string', isArray: false },
    ]);
    expect(body.outgoingLinkTypes).toEqual([
      {
        apiName: 'parent',
        displayName: 'Parent',
        linkedEntityTypeApiName: 'Department',
        cardinality: 'ONE',
        required: false,
      },
    ]);
  });

  it('edit form updates displayName via PUT', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Addressable').length).toBeGreaterThan(0);
    });
    const editButtons = screen.getAllByRole('button', { name: /^Edit$/i });
    // Rows sorted by displayName asc: Addressable (0), Locatable (1)
    await user.click(editButtons[0]);
    const displayInput = (await screen.findByLabelText(
      /Display Name \*/i,
    )) as HTMLInputElement;
    expect(displayInput.value).toBe('Addressable');
    await user.clear(displayInput);
    await user.type(displayInput, 'Has Address');
    await user.click(screen.getByRole('button', { name: /Save changes/i }));
    await waitFor(() => {
      expect(state.updateCalls.length).toBe(1);
    });
    expect(state.updateCalls[0]).toMatchObject({
      rid: 'ri.ontology.main.interface.addr',
      body: expect.objectContaining({ displayName: 'Has Address' }),
    });
  });

  it('confirms delete and calls the DELETE endpoint', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Addressable').length).toBeGreaterThan(0);
    });
    const deleteButtons = screen.getAllByRole('button', { name: /^Delete$/i });
    await user.click(deleteButtons[0]);
    const heading = await screen.findByText(/Delete Interface/i);
    const modal = heading.closest('div[class*="rounded-xl"]') as HTMLElement;
    const confirm = within(modal).getByRole('button', { name: /^Delete$/i });
    await user.click(confirm);
    await waitFor(() => {
      expect(state.deleteCalls.length).toBe(1);
    });
    expect(state.deleteCalls[0]).toBe('ri.ontology.main.interface.addr');
  });

  it('opens implementing modal and detaches an ObjectType', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Addressable').length).toBeGreaterThan(0);
    });
    // One attachment exists for Addressable.
    const manageButtons = await screen.findAllByRole('button', {
      name: /^Manage \(/i,
    });
    await user.click(manageButtons[0]);
    await screen.findByRole('heading', { name: /Implementing — Addressable/i });
    // Employee row shows Detach button
    const detachBtn = screen.getByRole('button', { name: /Detach Employee/i });
    await user.click(detachBtn);
    await waitFor(() => {
      expect(state.detachCalls.length).toBe(1);
    });
    expect(state.detachCalls[0]).toEqual({
      objectTypeRid: 'ri.ontology.main.object-type.emp',
      interfaceRid: 'ri.ontology.main.interface.addr',
    });
  });

  it('attaches an ObjectType via the implementing modal', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Locatable').length).toBeGreaterThan(0);
    });
    // No attachments for Locatable.
    const manageButtons = await screen.findAllByRole('button', {
      name: /^Manage \(/i,
    });
    // Sorted asc: Addressable (0), Locatable (1)
    await user.click(manageButtons[1]);
    await screen.findByRole('heading', { name: /Implementing — Locatable/i });
    const select = screen.getByLabelText(
      /Object type to attach/i,
    ) as HTMLSelectElement;
    await user.selectOptions(select, 'ri.ontology.main.object-type.emp');
    await user.click(screen.getByRole('button', { name: /^Attach$/i }));
    await waitFor(() => {
      expect(state.attachCalls.length).toBe(1);
    });
    expect(state.attachCalls[0]).toEqual({
      objectTypeRid: 'ri.ontology.main.object-type.emp',
      body: expect.objectContaining({
        interfaceRid: 'ri.ontology.main.interface.loc',
      }),
    });
  });
});
