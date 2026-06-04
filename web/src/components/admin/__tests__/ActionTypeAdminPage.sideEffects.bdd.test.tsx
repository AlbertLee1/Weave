import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ActionTypeAdminPage } from '../ActionTypeAdminPage';

// BDD: the ActionType admin builder must expose a Side Effects JSON editor.
//
// The v2 backend's UpdateActionTypeRequest already accepts/persists a
// `sideEffects` raw-JSON field (pkg/oms/admin_handlers.go), and the read model
// surfaces it through ToFullMetadataJSON — but the builder previously had no
// control for it, so an operator could never set webhook / notification side
// effects from the Ontology Manager UI.
//
// Given an operator editing an existing ActionType
// When  they type valid JSON into the new Side Effects textarea and save
// Then  the update request body carries `sideEffects` parsed back to an object.
// And   when the JSON is invalid, submit is blocked, an inline error shows,
//       and no update request is dispatched.

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
    rid: 'ri.ontology.main.action-type.archive',
    apiName: 'archiveEmployee',
    displayName: 'Archive Employee',
    status: 'ACTIVE',
    parameters: {},
    rules: [],
    sideEffects: [
      { type: 'webhook', url: 'https://hooks.example.com/archived' },
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
  sideEffects?: unknown;
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

        if (
          method === 'GET' &&
          url.endsWith('/api/v2/ontologies/northwind/objectTypes')
        ) {
          return jsonResponse({ data: OBJECT_TYPES });
        }
        if (
          method === 'GET' &&
          url.endsWith('/api/v2/ontologies/northwind/linkTypes')
        ) {
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

async function openEditModal(user: ReturnType<typeof userEvent.setup>) {
  renderPage();
  await waitFor(() => {
    expect(screen.getByText('Archive Employee')).toBeInTheDocument();
  });
  const row = screen.getByText('Archive Employee').closest('tr')!;
  await user.click(within(row).getByRole('button', { name: /Edit/i }));
  // Wait for the edit form to mount.
  await screen.findByTestId('action-type-edit-form');
}

describe('ActionTypeAdminPage Side Effects editor', () => {
  let state: StubState;

  beforeEach(() => {
    state = makeStub();
    installFetch(state);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('pre-populates the Side Effects editor and round-trips valid JSON on save', async () => {
    const user = userEvent.setup();
    await openEditModal(user);

    // The stored side effects pre-populate the editor.
    const sideEffects = screen.getByTestId(
      'action-type-side-effects',
    ) as HTMLTextAreaElement;
    expect(sideEffects.value).toContain('webhook');

    // Operator edits the side-effects JSON. fireEvent.change avoids
    // userEvent's special-char handling for `{`, `[`, etc. in raw JSON.
    fireEvent.change(sideEffects, {
      target: {
        value:
          '[{"type":"notification","channel":"ops","message":"archived"}]',
      },
    });

    await user.click(screen.getByRole('button', { name: /Save changes/i }));

    await waitFor(() => {
      expect(state.updateCalls.length).toBe(1);
    });
    const body = state.updateCalls[0].body as Record<string, unknown>;
    expect(body.sideEffects).toEqual([
      { type: 'notification', channel: 'ops', message: 'archived' },
    ]);
  });

  it('blocks submit and shows an inline error when Side Effects is invalid JSON', async () => {
    const user = userEvent.setup();
    await openEditModal(user);

    const sideEffects = screen.getByTestId(
      'action-type-side-effects',
    ) as HTMLTextAreaElement;
    fireEvent.change(sideEffects, { target: { value: 'not json {{' } });

    await user.click(screen.getByRole('button', { name: /Save changes/i }));

    await waitFor(() => {
      expect(screen.getByText(/Invalid JSON/i)).toBeInTheDocument();
    });
    expect(state.updateCalls.length).toBe(0);
  });
});
