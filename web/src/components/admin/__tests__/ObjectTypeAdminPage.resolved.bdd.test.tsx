import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ObjectTypeAdminPage } from '../ObjectTypeAdminPage';

// US-499 BDD — ObjectType Resolved view + Edit History tabs.
//
// PRD literal acceptance criteria:
//   1) Resolved tab (继承合并)  — surfaces the inheritance-resolved view of
//      an ObjectType where parent properties + outgoing links are merged
//      in with provenance (`inheritedFrom`).
//   2) History tab (时间排序 edits) — lists `action_logs` rows scoped to
//      this ObjectType ordered by time (most recent first).
//
// Two BDD scenarios pin the contract from the SPA side; both go through
// the real Edit modal tab bar so a regression that wires the tab button
// without rendering the corresponding panel fails loudly.

const OBJECT_TYPES = [
  {
    rid: 'ri.ontology.main.object-type.emp-1',
    apiName: 'Employee',
    displayName: 'Employee',
    pluralDisplayName: 'Employees',
    primaryKey: 'employeeId',
    status: 'ACTIVE',
    visibility: 'PROMINENT',
  },
];

const RESOLVED_RESPONSE = {
  apiName: 'Employee',
  displayName: 'Employee',
  status: 'ACTIVE',
  primaryKey: 'employeeId',
  rid: 'ri.ontology.main.object-type.emp-1',
  visibility: 'PROMINENT',
  extendsRid: 'ri.ontology.main.object-type.person-1',
  extendsChain: ['ri.ontology.main.object-type.person-1'],
  properties: {
    employeeId: {
      dataType: { type: 'long' },
      rid: 'ri.ontology.main.property.employeeId',
    },
    fullName: {
      dataType: { type: 'string' },
      rid: 'ri.ontology.main.property.fullName',
      inheritedFrom: 'ri.ontology.main.object-type.person-1',
      displayName: 'Full Name',
    },
    salary: {
      dataType: { type: 'double' },
      rid: 'ri.ontology.main.property.salary',
      displayName: 'Salary',
    },
  },
  outgoingLinkTypes: [
    {
      apiName: 'employeeDepartment',
      displayName: 'Employee → Department',
      rid: 'ri.ontology.main.link-type.dept',
      objectTypeApiName: 'Employee',
      linkedObjectTypeApiName: 'Department',
      cardinality: 'ONE_TO_ONE',
      required: true,
    },
    {
      apiName: 'personEmail',
      displayName: 'Person → Email',
      rid: 'ri.ontology.main.link-type.email',
      objectTypeApiName: 'Person',
      linkedObjectTypeApiName: 'Email',
      cardinality: 'ONE_TO_MANY',
      required: false,
      inheritedFrom: 'ri.ontology.main.object-type.person-1',
    },
  ],
};

const ACTION_LOGS = [
  {
    id: 3,
    actionTypeRid: 'ri.ontology.main.action-type.update-employee',
    userId: 'alice',
    parameters: { x: 1 },
    edits: [{ type: 'MODIFY' }],
    status: 'SUCCESS',
    createdAt: '2026-05-17T12:00:00Z',
  },
  {
    id: 1,
    actionTypeRid: 'ri.ontology.main.action-type.create-employee',
    userId: 'bob',
    parameters: { y: 2 },
    edits: [{ type: 'CREATE' }],
    status: 'SUCCESS',
    createdAt: '2026-05-16T08:30:00Z',
  },
  {
    id: 2,
    actionTypeRid: 'ri.ontology.main.action-type.archive-employee',
    userId: 'carol',
    parameters: {},
    edits: [],
    status: 'FAILED',
    errorMessage: 'permission denied',
    createdAt: '2026-05-16T15:00:00Z',
  },
];

interface StubState {
  objectTypes: typeof OBJECT_TYPES;
  resolvedCalls: number;
  historyCalls: number;
}

function makeStub(): StubState {
  return { objectTypes: [...OBJECT_TYPES], resolvedCalls: 0, historyCalls: 0 };
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
          /\/api\/v2\/ontologies\/northwind\/objectTypes(\?.*)?$/.test(url)
        ) {
          return jsonResponse({ data: state.objectTypes });
        }

        if (
          method === 'GET' &&
          /\/api\/v2\/ontologies\/northwind\/objectTypes\/Employee\/resolved(\?.*)?$/.test(
            url,
          )
        ) {
          state.resolvedCalls += 1;
          return jsonResponse(RESOLVED_RESPONSE);
        }

        if (
          method === 'POST' &&
          /\/api\/v2\/ontologies\/northwind\/objectTypes\/Employee\/editsHistory(\?.*)?$/.test(
            url,
          )
        ) {
          state.historyCalls += 1;
          return jsonResponse({ data: ACTION_LOGS });
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
      <MemoryRouter initialEntries={['/admin/northwind/objectTypes']}>
        <Routes>
          <Route
            path="/admin/:ontology/objectTypes"
            element={<ObjectTypeAdminPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

async function openEditModal() {
  const user = userEvent.setup();
  renderPage();
  await waitFor(() => {
    expect(screen.getAllByText('Employee').length).toBeGreaterThan(0);
  });
  // Click Edit on the Employee row.
  const editBtn = screen
    .getAllByTestId('object-type-edit-btn')
    .find((b) => b.getAttribute('data-object-type-api-name') === 'Employee')!;
  await user.click(editBtn);
  return user;
}

describe('US-499 BDD — ObjectType Resolved + History tabs', () => {
  let state: StubState;

  beforeEach(() => {
    state = makeStub();
    installFetch(state);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('Given the admin opens an ObjectType edit modal, When the Resolved tab is selected, Then merged properties + links render with inheritedFrom provenance', async () => {
    const user = await openEditModal();

    // The Resolved tab exists alongside Details / Properties / Bindings.
    const resolvedTab = await screen.findByTestId(
      'object-type-edit-tab-resolved',
    );
    await user.click(resolvedTab);

    // The panel renders the resolved view.
    const panel = await screen.findByTestId('object-type-resolved-panel');
    await waitFor(() => {
      expect(state.resolvedCalls).toBeGreaterThan(0);
    });

    // Properties section: all three resolved properties are listed; the
    // inherited one is marked with provenance.
    await waitFor(() => {
      expect(within(panel).getByText('fullName')).toBeInTheDocument();
    });
    expect(within(panel).getByText('employeeId')).toBeInTheDocument();
    expect(within(panel).getByText('salary')).toBeInTheDocument();

    // Inherited-from badge appears for the fullName property.
    const fullNameRow = within(panel)
      .getByText('fullName')
      .closest('[data-testid="resolved-property-row"]')!;
    expect(
      within(fullNameRow as HTMLElement).getByTestId(
        'resolved-property-inherited-from',
      ),
    ).toBeInTheDocument();

    // The locally-declared salary property has NO inherited-from badge —
    // negative control that rules out "blanket label all rows".
    const salaryRow = within(panel)
      .getByText('salary')
      .closest('[data-testid="resolved-property-row"]')!;
    expect(
      within(salaryRow as HTMLElement).queryByTestId(
        'resolved-property-inherited-from',
      ),
    ).toBeNull();

    // Outgoing links: inherited link gets a badge; locally-declared does not.
    expect(within(panel).getByText('employeeDepartment')).toBeInTheDocument();
    expect(within(panel).getByText('personEmail')).toBeInTheDocument();
    const inheritedLinkRow = within(panel)
      .getByText('personEmail')
      .closest('[data-testid="resolved-link-row"]')!;
    expect(
      within(inheritedLinkRow as HTMLElement).getByTestId(
        'resolved-link-inherited-from',
      ),
    ).toBeInTheDocument();
    const ownLinkRow = within(panel)
      .getByText('employeeDepartment')
      .closest('[data-testid="resolved-link-row"]')!;
    expect(
      within(ownLinkRow as HTMLElement).queryByTestId(
        'resolved-link-inherited-from',
      ),
    ).toBeNull();
  });

  it('Given the admin opens an ObjectType edit modal, When the History tab is selected, Then action_logs render time-sorted (most recent first)', async () => {
    const user = await openEditModal();

    const historyTab = await screen.findByTestId(
      'object-type-edit-tab-history',
    );
    await user.click(historyTab);

    const panel = await screen.findByTestId('object-type-history-panel');
    await waitFor(() => {
      expect(state.historyCalls).toBeGreaterThan(0);
    });

    // All three logs render.
    const rows = await within(panel).findAllByTestId('history-row');
    expect(rows).toHaveLength(3);

    // PRD literal: 时间排序 — descending by createdAt. The 12:00 entry
    // is the most recent; 16-08:30 is the oldest. The middle row at
    // 16-15:00 (id=2) sits between them.
    expect(rows[0].getAttribute('data-action-log-id')).toBe('3');
    expect(rows[1].getAttribute('data-action-log-id')).toBe('2');
    expect(rows[2].getAttribute('data-action-log-id')).toBe('1');

    // Failed status surfaces an error indicator.
    const failedRow = rows[1];
    expect(
      within(failedRow).getByTestId('history-row-status'),
    ).toHaveTextContent(/FAILED/i);
  });
});
