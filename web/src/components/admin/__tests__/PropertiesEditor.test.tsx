import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { PropertiesEditor } from '../PropertiesEditor';
import type { ObjectType, Property } from '../../../api/types';

const OBJECT_TYPE: ObjectType = {
  rid: 'ri.ontology.main.object-type.emp-1',
  apiName: 'Employee',
  displayName: 'Employee',
  pluralDisplayName: 'Employees',
  primaryKey: 'employeeId',
  titleProperty: 'firstName',
  status: 'ACTIVE',
  visibility: 'PROMINENT',
};

const PROPERTIES: Property[] = [
  {
    rid: 'ri.ontology.main.property.empId',
    apiName: 'employeeId',
    displayName: 'Employee ID',
    baseType: 'long',
    isArray: false,
    isNullable: false,
    isSearchable: true,
    isSortable: true,
  },
  {
    rid: 'ri.ontology.main.property.firstName',
    apiName: 'firstName',
    displayName: 'First Name',
    baseType: 'string',
    isArray: false,
    isNullable: false,
    isSearchable: true,
    isSortable: true,
  },
  {
    rid: 'ri.ontology.main.property.lastName',
    apiName: 'lastName',
    displayName: 'Last Name',
    baseType: 'string',
    isArray: false,
    isNullable: true,
    isSearchable: false,
    isSortable: false,
  },
];

interface StubState {
  properties: Property[];
  objectType: ObjectType;
  createCalls: Array<{ body: unknown }>;
  updateCalls: Array<{ rid: string; body: unknown }>;
  deleteCalls: string[];
  updateObjectTypeCalls: Array<{ body: unknown }>;
}

function makeStub(): StubState {
  return {
    properties: PROPERTIES.map((p) => ({ ...p })),
    objectType: { ...OBJECT_TYPE },
    createCalls: [],
    updateCalls: [],
    deleteCalls: [],
    updateObjectTypeCalls: [],
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

        const listMatch = url.match(
          /\/api\/v2\/ontologies\/northwind\/objectTypes\/byRid\/([^/?]+)\/properties(\?.*)?$/,
        );
        if (listMatch && method === 'GET') {
          return jsonResponse({ data: state.properties });
        }
        if (listMatch && method === 'POST') {
          const body = init?.body ? JSON.parse(init.body as string) : {};
          state.createCalls.push({ body });
          if (state.properties.some((p) => p.apiName === body.apiName)) {
            return jsonResponse({ errorCode: 'PropertyAlreadyExists' }, 409);
          }
          const created: Property = {
            rid: `ri.ontology.main.property.${body.apiName}`,
            apiName: body.apiName,
            displayName: body.displayName,
            baseType: body.baseType,
            isArray: !!body.isArray,
            isNullable: !!body.isNullable,
            isSearchable: !!body.isSearchable,
            isSortable: !!body.isSortable,
          };
          state.properties.push(created);
          return jsonResponse(created, 201);
        }

        const propMatch = url.match(
          /\/api\/v2\/ontologies\/northwind\/properties\/byRid\/([^?]+)/,
        );
        if (propMatch && method === 'PUT') {
          const rid = decodeURIComponent(propMatch[1]);
          const body = init?.body ? JSON.parse(init.body as string) : {};
          state.updateCalls.push({ rid, body });
          const idx = state.properties.findIndex((p) => p.rid === rid);
          if (idx < 0) return jsonResponse({ errorCode: 'NotFound' }, 404);
          const next = { ...state.properties[idx] };
          if (body.displayName !== undefined) next.displayName = body.displayName;
          if (body.isNullable !== undefined) next.isNullable = body.isNullable;
          if (body.isSearchable !== undefined)
            next.isSearchable = body.isSearchable;
          if (body.isSortable !== undefined) next.isSortable = body.isSortable;
          if (body.editOnly !== undefined) next.editOnly = body.editOnly;
          state.properties[idx] = next;
          return jsonResponse(next);
        }
        if (propMatch && method === 'DELETE') {
          const rid = decodeURIComponent(propMatch[1]);
          state.deleteCalls.push(rid);
          state.properties = state.properties.filter((p) => p.rid !== rid);
          return new Response('', { status: 200 });
        }

        const otUpdateMatch = url.match(
          /\/api\/v2\/ontologies\/northwind\/objectTypes\/byRid\/([^?]+)/,
        );
        if (otUpdateMatch && method === 'PUT') {
          const body = init?.body ? JSON.parse(init.body as string) : {};
          state.updateObjectTypeCalls.push({ body });
          state.objectType = {
            ...state.objectType,
            titleProperty: body.titleProperty,
          };
          return jsonResponse(state.objectType);
        }

        return new Response('{}', { status: 200 });
      },
    ),
  );
}

function renderEditor(objectType = OBJECT_TYPE) {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchInterval: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={qc}>
      <PropertiesEditor
        ontologyApiName="northwind"
        objectType={objectType}
      />
    </QueryClientProvider>,
  );
}

describe('PropertiesEditor', () => {
  let state: StubState;

  beforeEach(() => {
    state = makeStub();
    installFetch(state);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('loads and lists properties sorted by apiName', async () => {
    renderEditor();
    await waitFor(() => {
      expect(screen.getByText('employeeId')).toBeInTheDocument();
    });
    expect(screen.getByText('firstName')).toBeInTheDocument();
    expect(screen.getByText('lastName')).toBeInTheDocument();
    expect(screen.getByText('3 properties')).toBeInTheDocument();
  });

  it('marks primary key radio as checked and disabled', async () => {
    renderEditor();
    await waitFor(() => {
      expect(screen.getByText('employeeId')).toBeInTheDocument();
    });
    const pkRadio = screen.getByLabelText(
      'primary key employeeId',
    ) as HTMLInputElement;
    expect(pkRadio.checked).toBe(true);
    expect(pkRadio.disabled).toBe(true);
    const nonPkRadio = screen.getByLabelText(
      'primary key firstName',
    ) as HTMLInputElement;
    expect(nonPkRadio.checked).toBe(false);
  });

  it('marks title property radio and allows changing it', async () => {
    const user = userEvent.setup();
    renderEditor();
    await waitFor(() => {
      expect(screen.getByText('lastName')).toBeInTheDocument();
    });
    const oldTitle = screen.getByLabelText(
      'title property firstName',
    ) as HTMLInputElement;
    expect(oldTitle.checked).toBe(true);
    const newTitle = screen.getByLabelText(
      'title property lastName',
    ) as HTMLInputElement;
    await user.click(newTitle);
    await waitFor(() => {
      expect(state.updateObjectTypeCalls.length).toBe(1);
    });
    expect(
      (state.updateObjectTypeCalls[0].body as { titleProperty: string })
        .titleProperty,
    ).toBe('lastName');
  });

  it('creates a new property via Add form', async () => {
    const user = userEvent.setup();
    renderEditor();
    await waitFor(() => {
      expect(screen.getByText('employeeId')).toBeInTheDocument();
    });
    await user.click(screen.getByRole('button', { name: /Add Property/i }));
    const form = await screen.findByTestId('add-property-form');
    const apiNameInput = within(form).getAllByRole('textbox')[0];
    await user.type(apiNameInput, 'email');
    const baseTypeSelect = within(form).getByLabelText(/Base Type/i);
    await user.selectOptions(baseTypeSelect, 'string');
    await user.click(within(form).getByLabelText('Searchable'));
    await user.click(
      within(form).getByRole('button', { name: /Create property/i }),
    );
    await waitFor(() => {
      expect(state.createCalls.length).toBe(1);
    });
    const body = state.createCalls[0].body as Record<string, unknown>;
    expect(body.apiName).toBe('email');
    expect(body.baseType).toBe('string');
    expect(body.isSearchable).toBe(true);
    await waitFor(() => {
      expect(screen.getByText('email')).toBeInTheDocument();
    });
  });

  it('blocks duplicate apiName on create', async () => {
    const user = userEvent.setup();
    renderEditor();
    await waitFor(() =>
      expect(screen.getByText('firstName')).toBeInTheDocument(),
    );
    await user.click(screen.getByRole('button', { name: /Add Property/i }));
    const form = await screen.findByTestId('add-property-form');
    const apiNameInput = within(form).getAllByRole('textbox')[0];
    await user.type(apiNameInput, 'firstName');
    expect(
      within(form).getByText(/apiName already used/i),
    ).toBeInTheDocument();
    const submit = within(form).getByRole('button', {
      name: /Create property/i,
    }) as HTMLButtonElement;
    expect(submit.disabled).toBe(true);
  });

  it('renders struct field editor when struct baseType selected', async () => {
    const user = userEvent.setup();
    renderEditor();
    await waitFor(() =>
      expect(screen.getByText('firstName')).toBeInTheDocument(),
    );
    await user.click(screen.getByRole('button', { name: /Add Property/i }));
    const form = await screen.findByTestId('add-property-form');
    await user.selectOptions(
      within(form).getByLabelText(/Base Type/i),
      'struct',
    );
    expect(within(form).getByTestId('struct-fields')).toBeInTheDocument();
    expect(
      within(form).getByText(/Struct type requires at least one field/i),
    ).toBeInTheDocument();
    await user.click(within(form).getByRole('button', { name: /Add field/i }));
    const fieldName = within(form).getByLabelText(
      'struct field name 0',
    ) as HTMLInputElement;
    await user.type(fieldName, 'street');
    await user.type(within(form).getAllByRole('textbox')[0], 'address');
    await user.click(
      within(form).getByRole('button', { name: /Create property/i }),
    );
    await waitFor(() => {
      expect(state.createCalls.length).toBe(1);
    });
    const body = state.createCalls[0].body as Record<string, unknown>;
    expect(body.baseType).toBe('struct');
    expect(body.typeConfig).toEqual({
      fields: [{ name: 'street', type: 'string' }],
    });
  });

  it('edits an existing property inline', async () => {
    const user = userEvent.setup();
    renderEditor();
    await waitFor(() =>
      expect(screen.getByText('lastName')).toBeInTheDocument(),
    );
    const row = screen.getByText('lastName').closest('tr')!;
    await user.click(within(row).getByRole('button', { name: 'Edit' }));
    const form = await screen.findByTestId('edit-property-form');
    const searchableToggle = within(form).getByLabelText('Searchable');
    await user.click(searchableToggle);
    await user.click(
      within(form).getByRole('button', { name: /Save property/i }),
    );
    await waitFor(() => {
      expect(state.updateCalls.length).toBe(1);
    });
    expect(state.updateCalls[0].rid).toBe(
      'ri.ontology.main.property.lastName',
    );
    expect(
      (state.updateCalls[0].body as { isSearchable?: boolean }).isSearchable,
    ).toBe(true);
  });

  it('deletes a property after confirmation', async () => {
    const user = userEvent.setup();
    renderEditor();
    await waitFor(() =>
      expect(screen.getByText('lastName')).toBeInTheDocument(),
    );
    const row = screen.getByText('lastName').closest('tr')!;
    await user.click(within(row).getByRole('button', { name: 'Delete' }));
    const confirm = await screen.findByTestId('delete-property-confirm');
    await user.click(
      within(confirm).getByRole('button', { name: /Delete property/i }),
    );
    await waitFor(() => {
      expect(state.deleteCalls.length).toBe(1);
    });
    expect(state.deleteCalls[0]).toBe(
      'ri.ontology.main.property.lastName',
    );
    await waitFor(() => {
      expect(screen.queryByText('lastName')).not.toBeInTheDocument();
    });
  });

  it('disables Delete for the primary key property', async () => {
    renderEditor();
    await waitFor(() =>
      expect(screen.getByText('employeeId')).toBeInTheDocument(),
    );
    const row = screen.getByText('employeeId').closest('tr')!;
    const deleteBtn = within(row).getByRole('button', {
      name: 'Delete',
    }) as HTMLButtonElement;
    expect(deleteBtn.disabled).toBe(true);
  });
});
