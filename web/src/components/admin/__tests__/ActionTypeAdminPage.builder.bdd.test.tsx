import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ActionTypeAdminPage } from '../ActionTypeAdminPage';

// Unit-4 BDD: the ActionType admin builder must let an author set the
// completeness fields that the v2 backend already accepts/persists but the
// builder previously dropped:
//   B1 requiresApproval + approvers   (now backend-settable)
//   B2 submissionCriteria             (JSON editor)
//   B8 compensateActionRid + parameterSchema
//
// Given the Ontology Manager ActionType builder
// When  the author fills the new approval / criteria / compensation / schema
//       controls and submits
// Then  the create (and edit) request bodies carry the new wire keys, with
//       JSON parsed and approvers split into a string[].

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
    rid: 'ri.ontology.main.action-type.rollback',
    apiName: 'rollbackEmployee',
    displayName: 'Rollback Employee',
    status: 'ACTIVE',
    parameters: {},
    rules: [],
  },
  {
    rid: 'ri.ontology.main.action-type.archive',
    apiName: 'archiveEmployee',
    displayName: 'Archive Employee',
    status: 'ACTIVE',
    parameters: {},
    rules: [],
    requiresApproval: true,
    approvers: ['role:approver', 'alice'],
    compensateActionRid: 'ri.ontology.main.action-type.rollback',
    submissionCriteria: {
      type: 'parameterMatch',
      value: { parameter: 'status', operator: 'eq', value: 'active' },
    },
    parameterSchema: { type: 'object' },
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

describe('ActionTypeAdminPage builder completeness (Unit-4)', () => {
  let state: StubState;

  beforeEach(() => {
    state = makeStub();
    installFetch(state);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('create body carries requiresApproval + approvers + submissionCriteria + compensateActionRid + parameterSchema', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Archive Employee')).toBeInTheDocument();
    });
    await user.click(screen.getByRole('button', { name: /\+ New Action Type/i }));
    await user.type(
      await screen.findByLabelText(/Display Name \*/i),
      'Promote Employee',
    );

    // B1: approval toggle + approvers.
    await user.click(screen.getByTestId('action-type-requires-approval'));
    const approvers = await screen.findByTestId('action-type-approvers');
    await user.type(approvers, 'role:approver, alice');

    // B2: submission criteria JSON. fireEvent.change avoids userEvent's
    // special-char handling for `{`, `[`, etc. in raw JSON.
    fireEvent.change(screen.getByTestId('action-type-submission-criteria'), {
      target: {
        value:
          '{"type":"parameterMatch","value":{"parameter":"status","operator":"eq","value":"active"}}',
      },
    });

    // B8: compensating action select + parameter schema JSON.
    await user.selectOptions(
      screen.getByTestId('action-type-compensate-select'),
      'ri.ontology.main.action-type.rollback',
    );
    fireEvent.change(screen.getByTestId('action-type-parameter-schema'), {
      target: { value: '{"type":"object"}' },
    });

    await user.click(screen.getByRole('button', { name: /^Create$/i }));

    await waitFor(() => {
      expect(state.createCalls.length).toBe(1);
    });
    const body = state.createCalls[0].body as Record<string, unknown>;
    expect(body.requiresApproval).toBe(true);
    expect(body.approvers).toEqual(['role:approver', 'alice']);
    expect(body.submissionCriteria).toEqual({
      type: 'parameterMatch',
      value: { parameter: 'status', operator: 'eq', value: 'active' },
    });
    expect(body.compensateActionRid).toBe(
      'ri.ontology.main.action-type.rollback',
    );
    expect(body.parameterSchema).toEqual({ type: 'object' });
  });

  it('blocks submit and shows an inline error when submissionCriteria is invalid JSON', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Archive Employee')).toBeInTheDocument();
    });
    await user.click(screen.getByRole('button', { name: /\+ New Action Type/i }));
    await user.type(
      await screen.findByLabelText(/Display Name \*/i),
      'Promote Employee',
    );
    const criteria = screen.getByTestId('action-type-submission-criteria');
    await user.type(criteria, 'not json');
    await user.click(screen.getByRole('button', { name: /^Create$/i }));

    await waitFor(() => {
      expect(screen.getByText(/Invalid JSON/i)).toBeInTheDocument();
    });
    expect(state.createCalls.length).toBe(0);
  });

  it('edit modal pre-populates the new fields and round-trips them on save', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Archive Employee')).toBeInTheDocument();
    });
    const row = screen.getByText('Archive Employee').closest('tr')!;
    await user.click(within(row).getByRole('button', { name: /Edit/i }));

    // Pre-populated approval toggle + approvers.
    const toggle = (await screen.findByTestId(
      'action-type-requires-approval',
    )) as HTMLInputElement;
    expect(toggle.checked).toBe(true);
    const approvers = screen.getByTestId(
      'action-type-approvers',
    ) as HTMLInputElement;
    expect(approvers.value).toBe('role:approver, alice');

    // Pre-populated compensate select.
    const compensate = screen.getByTestId(
      'action-type-compensate-select',
    ) as HTMLSelectElement;
    expect(compensate.value).toBe('ri.ontology.main.action-type.rollback');

    // Pre-populated JSON editors.
    const criteria = screen.getByTestId(
      'action-type-submission-criteria',
    ) as HTMLTextAreaElement;
    expect(criteria.value).toContain('parameterMatch');
    const schema = screen.getByTestId(
      'action-type-parameter-schema',
    ) as HTMLTextAreaElement;
    expect(schema.value).toContain('object');

    await user.click(screen.getByRole('button', { name: /Save changes/i }));
    await waitFor(() => {
      expect(state.updateCalls.length).toBe(1);
    });
    const body = state.updateCalls[0].body as Record<string, unknown>;
    expect(body.requiresApproval).toBe(true);
    expect(body.approvers).toEqual(['role:approver', 'alice']);
    expect(body.compensateActionRid).toBe(
      'ri.ontology.main.action-type.rollback',
    );
    expect(body.submissionCriteria).toMatchObject({ type: 'parameterMatch' });
    expect(body.parameterSchema).toMatchObject({ type: 'object' });
  });
});
