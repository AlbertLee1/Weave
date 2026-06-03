import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { PropertiesEditor } from '../PropertiesEditor';
import type { ObjectType, Property } from '../../../api/types';

// Round 55 BDD — bind a Property to a SharedPropertyType from the
// Add-property form.
//
// PRD: SharedPropertyTypes (reusable property definitions) are listable
// at GET /api/v2/ontologies/{o}/sharedPropertyTypes and the create
// endpoint accepts `sharedPropertyTypeApiName`, validating that the
// property's baseType + isArray match the SPT exactly (mismatch → 400).
//
// Scenarios pin the SPA contract:
//   - Given SPTs exist, the Add form's "Shared property type" selector is
//     populated from listSharedPropertyTypes (plus a "(none)" default).
//   - When an SPT is chosen, baseType + isArray snap to the SPT's values
//     AND both inputs are disabled (so the backend exact-match validation
//     can never 400), and the create POST carries sharedPropertyTypeApiName.
//   - When cleared back to "(none)", baseType/isArray re-enable and the
//     create POST omits sharedPropertyTypeApiName.
//   - If the list endpoint fails, the selector is hidden (graceful
//     degradation) and a plain property can still be created.

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
    rid: 'ri.ontology.main.property.employeeId',
    apiName: 'employeeId',
    baseType: 'long',
    isArray: false,
    isNullable: false,
    isSearchable: true,
    isSortable: true,
  },
];

interface SharedPropertyTypeStub {
  rid: string;
  apiName: string;
  displayName?: string;
  baseType: string;
  isArray: boolean;
}

const SHARED_PROPERTY_TYPES: SharedPropertyTypeStub[] = [
  {
    rid: 'ri.ontology.main.shared-property.email',
    apiName: 'emailAddress',
    displayName: 'Email Address',
    baseType: 'string',
    isArray: false,
  },
  {
    rid: 'ri.ontology.main.shared-property.tags',
    apiName: 'tagList',
    displayName: 'Tag List',
    baseType: 'string',
    isArray: true,
  },
];

interface StubState {
  properties: Property[];
  objectType: ObjectType;
  sharedPropertyTypes: SharedPropertyTypeStub[];
  sptStatus: number;
  createCalls: Array<{ body: Record<string, unknown> }>;
}

function makeStub(): StubState {
  return {
    properties: PROPERTIES.map((p) => ({ ...p })),
    objectType: { ...OBJECT_TYPE },
    sharedPropertyTypes: SHARED_PROPERTY_TYPES.map((s) => ({ ...s })),
    sptStatus: 200,
    createCalls: [],
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
          /\/api\/v2\/ontologies\/northwind\/sharedPropertyTypes(\?.*)?$/.test(
            url,
          )
        ) {
          if (state.sptStatus !== 200) {
            return jsonResponse(
              { errorCode: 'ListSharedPropertyTypesFailed' },
              state.sptStatus,
            );
          }
          return jsonResponse({ data: state.sharedPropertyTypes });
        }

        const listMatch = url.match(
          /\/api\/v2\/ontologies\/northwind\/objectTypes\/byRid\/([^/?]+)\/properties(\?.*)?$/,
        );
        if (listMatch && method === 'GET') {
          return jsonResponse({ data: state.properties });
        }
        if (listMatch && method === 'POST') {
          const body = init?.body ? JSON.parse(init.body as string) : {};
          state.createCalls.push({ body });
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

        if (
          /\/api\/v2\/ontologies\/northwind\/actionTypes(\?.*)?$/.test(url) &&
          method === 'GET'
        ) {
          return jsonResponse({ data: [] });
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
      <PropertiesEditor ontologyApiName="northwind" objectType={objectType} />
    </QueryClientProvider>,
  );
}

async function openAddForm(user: ReturnType<typeof userEvent.setup>) {
  await waitFor(() =>
    expect(screen.getByText('employeeId')).toBeInTheDocument(),
  );
  await user.click(screen.getByRole('button', { name: /Add Property/i }));
  return screen.findByTestId('add-property-form');
}

describe('Round 55 BDD — bind Property to SharedPropertyType', () => {
  let state: StubState;

  beforeEach(() => {
    state = makeStub();
    installFetch(state);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('Given SPTs exist, When the admin opens Add, Then the selector lists them with a "(none)" default', async () => {
    const user = userEvent.setup();
    renderEditor();
    const form = await openAddForm(user);

    const select = await within(form).findByLabelText(/Shared property type/i);
    // (none) default + the two stubbed SPTs.
    await waitFor(() =>
      expect(
        within(select as HTMLElement).getByRole('option', {
          name: /emailAddress/i,
        }),
      ).toBeInTheDocument(),
    );
    expect(
      within(select as HTMLElement).getByRole('option', { name: /tagList/i }),
    ).toBeInTheDocument();
    const noneOpt = within(select as HTMLElement).getByRole('option', {
      name: /none/i,
    }) as HTMLOptionElement;
    expect(noneOpt.selected).toBe(true);
  });

  it('Given an SPT is selected, Then baseType+isArray snap to its values, lock, and the create POST carries sharedPropertyTypeApiName', async () => {
    const user = userEvent.setup();
    renderEditor();
    const form = await openAddForm(user);

    const apiNameInput = within(form).getAllByRole('textbox')[0];
    await user.type(apiNameInput, 'workEmail');

    const select = await within(form).findByLabelText(/Shared property type/i);
    // tagList is string + isArray=true — distinguishes both locked fields
    // from their defaults (string + isArray=false).
    await user.selectOptions(select as HTMLElement, 'tagList');

    const baseTypeSelect = within(form).getByLabelText(
      /Base Type/i,
    ) as HTMLSelectElement;
    await waitFor(() => expect(baseTypeSelect.value).toBe('string'));
    expect(baseTypeSelect.disabled).toBe(true);

    const arrayToggle = within(form).getByRole('checkbox', {
      name: /Array/i,
    }) as HTMLInputElement;
    expect(arrayToggle.checked).toBe(true);
    expect(arrayToggle.disabled).toBe(true);

    await user.click(
      within(form).getByRole('button', { name: /Create property/i }),
    );
    await waitFor(() => expect(state.createCalls.length).toBe(1));
    const body = state.createCalls[0].body;
    expect(body.sharedPropertyTypeApiName).toBe('tagList');
    expect(body.baseType).toBe('string');
    expect(body.isArray).toBe(true);
  });

  it('Given an SPT was selected then cleared to "(none)", Then baseType/isArray re-enable and the create POST omits sharedPropertyTypeApiName', async () => {
    const user = userEvent.setup();
    renderEditor();
    const form = await openAddForm(user);

    const apiNameInput = within(form).getAllByRole('textbox')[0];
    await user.type(apiNameInput, 'plainProp');

    const select = await within(form).findByLabelText(/Shared property type/i);
    await user.selectOptions(select as HTMLElement, 'emailAddress');

    const baseTypeSelect = within(form).getByLabelText(
      /Base Type/i,
    ) as HTMLSelectElement;
    await waitFor(() => expect(baseTypeSelect.disabled).toBe(true));

    // Clear back to (none).
    await user.selectOptions(select as HTMLElement, '');
    await waitFor(() => expect(baseTypeSelect.disabled).toBe(false));
    const arrayToggle = within(form).getByRole('checkbox', {
      name: /Array/i,
    }) as HTMLInputElement;
    expect(arrayToggle.disabled).toBe(false);

    // Pick a distinctive baseType to prove the field is editable again.
    await user.selectOptions(baseTypeSelect, 'integer');
    await user.click(
      within(form).getByRole('button', { name: /Create property/i }),
    );
    await waitFor(() => expect(state.createCalls.length).toBe(1));
    const body = state.createCalls[0].body;
    expect('sharedPropertyTypeApiName' in body).toBe(false);
    expect(body.baseType).toBe('integer');
  });

  it('Given the SPT list endpoint fails, Then the selector is hidden and a plain property still creates', async () => {
    state.sptStatus = 500;
    const user = userEvent.setup();
    renderEditor();
    const form = await openAddForm(user);

    // Selector hidden — give the failed query a tick to settle.
    await waitFor(() =>
      expect(
        within(form).queryByLabelText(/Shared property type/i),
      ).toBeNull(),
    );

    const apiNameInput = within(form).getAllByRole('textbox')[0];
    await user.type(apiNameInput, 'fallbackProp');
    await user.click(
      within(form).getByRole('button', { name: /Create property/i }),
    );
    await waitFor(() => expect(state.createCalls.length).toBe(1));
    expect('sharedPropertyTypeApiName' in state.createCalls[0].body).toBe(
      false,
    );
  });
});
