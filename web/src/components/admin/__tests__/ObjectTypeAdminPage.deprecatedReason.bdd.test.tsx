import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ObjectTypeAdminPage } from '../ObjectTypeAdminPage';

// BDD — capturing the deprecation reason on the ObjectType edit form.
// The backend UpdateObjectTypeRequest carries `deprecatedReason`
// (pkg/oms/admin_handlers.go DeprecatedReason `json:"deprecatedReason,omitempty"`,
// persisted at updated.DeprecatedReason = req.DeprecatedReason). Operators need
// a control on the edit form to record *why* an ObjectType was deprecated; the
// PUT body must carry that value under the `deprecatedReason` wire key.

const OBJECT_TYPES = [
  {
    rid: 'ri.ontology.main.object-type.emp-1',
    apiName: 'Employee',
    displayName: 'Employee',
    pluralDisplayName: 'Employees',
    primaryKey: 'employeeId',
    status: 'DEPRECATED',
    visibility: 'PROMINENT',
    icon: 'user',
    deprecatedReason: 'Replaced by Worker',
  },
];

interface StubState {
  objectTypes: typeof OBJECT_TYPES;
  updateCalls: Array<{ rid: string; body: Record<string, unknown> }>;
}

function makeStub(): StubState {
  return {
    objectTypes: OBJECT_TYPES.map((ot) => ({ ...ot })),
    updateCalls: [],
  };
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
          return jsonResponse({ data: state.objectTypes });
        }

        const byRid = url.match(
          /\/api\/v2\/ontologies\/northwind\/objectTypes\/byRid\/([^?]+)/,
        );
        if (byRid && method === 'PUT') {
          const rid = decodeURIComponent(byRid[1]);
          const body = init?.body
            ? (JSON.parse(init.body as string) as Record<string, unknown>)
            : {};
          state.updateCalls.push({ rid, body });
          const idx = state.objectTypes.findIndex((ot) => ot.rid === rid);
          if (idx < 0) return jsonResponse({ errorCode: 'NotFound' }, 404);
          const next = { ...state.objectTypes[idx], ...body };
          state.objectTypes[idx] = next;
          return jsonResponse(next);
        }

        if (method === 'GET' && url.endsWith('/outgoingLinkTypes')) {
          return jsonResponse({ data: [] });
        }
        if (
          method === 'GET' &&
          url.endsWith('/api/v2/ontologies/northwind/actionTypes')
        ) {
          return jsonResponse({ data: [] });
        }

        return new Response('{}', { status: 200 });
      },
    ),
  );
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
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

describe('ObjectTypeAdminPage — deprecation reason', () => {
  let state: StubState;

  beforeEach(() => {
    state = makeStub();
    installFetch(state);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('preloads the existing reason and sends an edited reason under `deprecatedReason`', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Employee').length).toBeGreaterThan(0);
    });

    // Given the operator opens the edit form for a DEPRECATED ObjectType
    await user.click(screen.getByRole('button', { name: /^Edit$/i }));

    // The deprecation-reason control preloads the existing value
    const reasonInput = (await screen.findByLabelText(
      /Deprecation reason/i,
    )) as HTMLTextAreaElement;
    expect(reasonInput.value).toBe('Replaced by Worker');

    // When the operator records a new reason and saves
    await user.clear(reasonInput);
    await user.type(reasonInput, 'Superseded by the Worker object type');

    await user.click(screen.getByRole('button', { name: /Save changes/i }));

    // Then the PUT body carries the new reason under `deprecatedReason`
    await waitFor(() => expect(state.updateCalls.length).toBe(1));
    const body = state.updateCalls[0].body;
    expect(body.deprecatedReason).toBe('Superseded by the Worker object type');
  });
});
