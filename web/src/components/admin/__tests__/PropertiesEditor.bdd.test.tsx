import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { PropertiesEditor } from '../PropertiesEditor';
import type { ActionType, ObjectType, Property } from '../../../api/types';

// US-496 BDD — Delete-property confirm surfaces blast radius.
//
// PRD literal: "保留属性删除时提示影响范围" (preserve impact warning when
// deleting a property). Two scenarios pin the contract from the SPA side:
//
//   - Given an ObjectType where the property is bound by N ActionType
//     rules, when the admin opens the delete-confirm, then the dialog
//     shows the bound-action count AND lists at least one bound
//     ActionType apiName. Positive control: an unrelated ActionType that
//     binds a different property must NOT inflate the count.
//
//   - Given the property is the current ObjectType title, when the
//     admin opens the delete-confirm, then a title-loss warning is
//     visible — so the admin sees that deleting the property will leave
//     the ObjectType title unset.

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
  {
    rid: 'ri.ontology.main.property.firstName',
    apiName: 'firstName',
    baseType: 'string',
    isArray: false,
    isNullable: false,
    isSearchable: true,
    isSortable: true,
  },
  {
    rid: 'ri.ontology.main.property.lastName',
    apiName: 'lastName',
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
  actionTypes: ActionType[];
  deleteCalls: string[];
}

function makeStub(): StubState {
  return {
    properties: PROPERTIES.map((p) => ({ ...p })),
    objectType: { ...OBJECT_TYPE },
    actionTypes: [],
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
          /\/objectTypes\/byRid\/[^/]+\/properties(\?.*)?$/.test(url)
        ) {
          return jsonResponse({ data: state.properties });
        }
        if (
          method === 'GET' &&
          /\/api\/v2\/ontologies\/northwind\/actionTypes(\?.*)?$/.test(url)
        ) {
          return jsonResponse({ data: state.actionTypes });
        }

        const propMatch = url.match(
          /\/api\/v2\/ontologies\/northwind\/properties\/byRid\/([^?]+)/,
        );
        if (propMatch && method === 'DELETE') {
          state.deleteCalls.push(decodeURIComponent(propMatch[1]));
          state.properties = state.properties.filter(
            (p) => p.rid !== decodeURIComponent(propMatch[1]),
          );
          return new Response('', { status: 200 });
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

describe('US-496 BDD — delete-property impact preview', () => {
  let state: StubState;

  beforeEach(() => {
    state = makeStub();
    installFetch(state);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('Given an ActionType binds the property, When the admin opens delete-confirm, Then the dialog reports the bound count and ActionType apiName', async () => {
    // Given — one ActionType modifies Employee.lastName, another modifies
    // Employee.firstName (must not inflate the count for lastName).
    state.actionTypes = [
      {
        rid: 'ri.ontology.main.action-type.update-employee',
        apiName: 'updateEmployee',
        displayName: 'Update Employee',
        status: 'ACTIVE',
        parameters: {},
        rules: [
          {
            type: 'modifyObject',
            objectType: 'Employee',
            propertyBindings: { lastName: { parameter: 'newLastName' } },
          },
        ],
      },
      {
        rid: 'ri.ontology.main.action-type.touch-firstname',
        apiName: 'touchFirstName',
        displayName: 'Touch First Name',
        status: 'ACTIVE',
        parameters: {},
        rules: [
          {
            type: 'modifyObject',
            objectType: 'Employee',
            propertyBindings: { firstName: { parameter: 'fn' } },
          },
        ],
      },
    ];

    const user = userEvent.setup();
    renderEditor();
    await waitFor(() =>
      expect(screen.getByText('lastName')).toBeInTheDocument(),
    );

    // When — admin opens the delete confirm for lastName.
    const row = screen.getByText('lastName').closest('tr')!;
    await user.click(within(row).getByRole('button', { name: 'Delete' }));
    const confirm = await screen.findByTestId('delete-property-confirm');

    // Then — exactly 1 bound ActionType shown by count; apiName visible.
    await waitFor(() => {
      expect(
        within(confirm).getByTestId('delete-property-impact-actions'),
      ).toBeInTheDocument();
    });
    const impact = within(confirm).getByTestId(
      'delete-property-impact-actions',
    );
    expect(impact.textContent).toMatch(/\b1\b/);
    expect(within(confirm).getByText(/updateEmployee/i)).toBeInTheDocument();
    // Negative control: the unrelated touchFirstName ActionType must
    // NOT appear (would imply impact code reads objectType only and
    // ignores propertyBindings narrowing).
    expect(within(confirm).queryByText(/touchFirstName/i)).toBeNull();

    // The confirm button stays enabled — impact is advisory.
    const confirmBtn = within(confirm).getByRole('button', {
      name: /Delete property/i,
    }) as HTMLButtonElement;
    expect(confirmBtn.disabled).toBe(false);
  });

  it('Given the property is the title property, When delete-confirm opens, Then a title-loss warning is shown', async () => {
    const user = userEvent.setup();
    renderEditor();
    await waitFor(() =>
      expect(screen.getByText('firstName')).toBeInTheDocument(),
    );

    // firstName is the current titleProperty on the ObjectType.
    const row = screen.getByText('firstName').closest('tr')!;
    await user.click(within(row).getByRole('button', { name: 'Delete' }));
    const confirm = await screen.findByTestId('delete-property-confirm');
    expect(
      within(confirm).getByTestId('delete-property-impact-title'),
    ).toBeInTheDocument();
  });
});
