import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ObjectTypeAdminPage } from '../ObjectTypeAdminPage';

// BDD — VTX-077: configuring an ObjectType as a Vertex Timeline event from the
// admin edit form. The backend UpdateObjectTypeRequest carries isEvent /
// eventStartProp / eventEndProp (pkg/oms/admin_handlers.go). Operators need an
// "is event" toggle that, when on, reveals start/end property pickers; the PUT
// body must carry the three fields so the Timeline can render the type.

const OBJECT_TYPES = [
  {
    rid: 'ri.ontology.main.object-type.flight-delay',
    apiName: 'FlightDelay',
    displayName: 'Flight Delay',
    pluralDisplayName: 'Flight Delays',
    primaryKey: 'id',
    status: 'ACTIVE',
    visibility: 'PROMINENT',
    properties: {
      id: { dataType: { type: 'string' }, rid: 'ri.prop.id' },
      delayStart: { dataType: { type: 'timestamp' }, rid: 'ri.prop.start' },
      delayEnd: { dataType: { type: 'timestamp' }, rid: 'ri.prop.end' },
    },
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

describe('ObjectTypeAdminPage — timeline event fields (VTX-077)', () => {
  let state: StubState;

  beforeEach(() => {
    state = makeStub();
    installFetch(state);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('toggles isEvent, picks start/end props, and sends all three in the PUT body', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Flight Delay').length).toBeGreaterThan(0);
    });

    // Given the operator opens the edit form
    await user.click(screen.getByRole('button', { name: /^Edit$/i }));

    // The "is event" toggle is present and starts off
    const toggle = (await screen.findByTestId(
      'object-type-edit-is-event',
    )) as HTMLInputElement;
    expect(toggle.checked).toBe(false);

    // When the operator marks it as an event
    await user.click(toggle);

    // The start/end property pickers appear and offer the type's properties
    const startSelect = (await screen.findByTestId(
      'object-type-edit-event-start-prop',
    )) as HTMLSelectElement;
    const endSelect = (await screen.findByTestId(
      'object-type-edit-event-end-prop',
    )) as HTMLSelectElement;

    await user.selectOptions(startSelect, 'delayStart');
    await user.selectOptions(endSelect, 'delayEnd');

    // And saves
    await user.click(screen.getByRole('button', { name: /Save changes/i }));

    // Then the PUT body carries all three event fields
    await waitFor(() => expect(state.updateCalls.length).toBe(1));
    const body = state.updateCalls[0].body;
    expect(body.isEvent).toBe(true);
    expect(body.eventStartProp).toBe('delayStart');
    expect(body.eventEndProp).toBe('delayEnd');
  });
});
