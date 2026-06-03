import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ObjectTypeAdminPage } from '../ObjectTypeAdminPage';

// Regression — ObjectType icon update field-name mismatch.
// The backend UpdateObjectTypeRequest reads/serializes the icon under the JSON
// key `icon` (pkg/oms/admin_handlers.go IconName `json:"icon"`,
// pkg/oms/models.go wire["icon"]). Historically the frontend sent
// `{ iconName: ... }`, which the backend silently dropped, so editing an
// ObjectType's icon did nothing. The Edit form must send the value under the
// `icon` wire key — NOT `iconName`.

const OBJECT_TYPES = [
  {
    rid: 'ri.ontology.main.object-type.emp-1',
    apiName: 'Employee',
    displayName: 'Employee',
    pluralDisplayName: 'Employees',
    primaryKey: 'employeeId',
    status: 'ACTIVE',
    visibility: 'PROMINENT',
    icon: 'user',
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

describe('ObjectTypeAdminPage — icon update field name', () => {
  let state: StubState;

  beforeEach(() => {
    state = makeStub();
    installFetch(state);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('edit form preloads the icon and sends it under the `icon` wire key (not `iconName`)', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Employee').length).toBeGreaterThan(0);
    });

    await user.click(screen.getByRole('button', { name: /^Edit$/i }));

    const iconInput = (await screen.findByLabelText(
      /Free-form icon identifier/i,
    )) as HTMLInputElement;
    // preloaded from objectType.icon
    expect(iconInput.value).toBe('user');

    await user.clear(iconInput);
    await user.type(iconInput, 'briefcase');

    await user.click(screen.getByRole('button', { name: /Save changes/i }));

    await waitFor(() => expect(state.updateCalls.length).toBe(1));
    const body = state.updateCalls[0].body;
    // Load-bearing: the backend reads `icon`, so the PUT body MUST carry it.
    expect(body.icon).toBe('briefcase');
    // The old, broken key must not be present.
    expect(body).not.toHaveProperty('iconName');
  });
});
