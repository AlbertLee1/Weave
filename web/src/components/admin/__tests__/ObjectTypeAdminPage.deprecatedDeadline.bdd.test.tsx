import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ObjectTypeAdminPage } from '../ObjectTypeAdminPage';

// BDD — setting a deprecation *deadline* on the ObjectType edit form.
//
// The backend UpdateObjectTypeRequest carries `deprecatedDeadline`
// (pkg/oms/admin_handlers.go DeprecatedDeadline *string
// `json:"deprecatedDeadline,omitempty"`). On update the handler parses a
// non-empty value with time.Parse(time.RFC3339, ...) — a parse failure is a
// 400 InvalidParameter:deprecatedDeadline; an empty/null value clears the
// stored deadline. Operators need a control on the edit form to record *when*
// a deprecated ObjectType should be retired; the PUT body must carry that
// value under the `deprecatedDeadline` wire key as a valid RFC3339 string,
// and clearing it must send null / omit so the backend wipes the deadline.

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
    // Stored as RFC3339 (mirrors the backend *time.Time read model).
    deprecatedDeadline: '2026-12-31T00:00:00Z',
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

describe('ObjectTypeAdminPage — deprecation deadline', () => {
  let state: StubState;

  beforeEach(() => {
    state = makeStub();
    installFetch(state);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('preloads the existing deadline and sends an edited deadline as RFC3339 under `deprecatedDeadline`', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Employee').length).toBeGreaterThan(0);
    });

    // Given the operator opens the edit form for a DEPRECATED ObjectType
    await user.click(screen.getByRole('button', { name: /^Edit$/i }));

    // The deprecation-deadline control preloads the existing value, converted
    // from RFC3339 to the datetime-local shape (YYYY-MM-DDTHH:mm).
    const deadlineInput = (await screen.findByLabelText(
      /Deprecation deadline/i,
    )) as HTMLInputElement;
    expect(deadlineInput.type).toBe('datetime-local');
    expect(deadlineInput.value).not.toBe('');
    // The preloaded value must round-trip back to the original instant.
    expect(new Date(deadlineInput.value).toISOString()).toBe(
      '2026-12-31T00:00:00.000Z',
    );

    // When the operator picks a new deadline and saves.
    // (datetime-local inputs don't respond to userEvent.type reliably; set the
    // value directly and fire a change, which is what a date picker emits.)
    fireEvent.change(deadlineInput, { target: { value: '2027-06-15T12:30' } });

    await user.click(screen.getByRole('button', { name: /Save changes/i }));

    // Then the PUT body carries the new deadline under `deprecatedDeadline` as
    // a valid RFC3339 string the backend's time.Parse(time.RFC3339) accepts.
    await waitFor(() => expect(state.updateCalls.length).toBe(1));
    const body = state.updateCalls[0].body;
    const sent = body.deprecatedDeadline;
    expect(typeof sent).toBe('string');
    // RFC3339 round-trip: parsing then re-serializing yields a stable ISO
    // instant (Z / offset, seconds present) — i.e. it is not a bare
    // datetime-local string like "2027-06-15T12:30".
    expect(new Date(sent as string).toISOString()).toBe(
      new Date('2027-06-15T12:30').toISOString(),
    );
    expect(sent).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/);
  });

  it('clears the deadline → PUT body sends null or omits `deprecatedDeadline`', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByText('Employee').length).toBeGreaterThan(0);
    });

    // Given the operator opens the edit form for a type that already has a
    // deadline set.
    await user.click(screen.getByRole('button', { name: /^Edit$/i }));

    const deadlineInput = (await screen.findByLabelText(
      /Deprecation deadline/i,
    )) as HTMLInputElement;
    expect(deadlineInput.value).not.toBe('');

    // When the operator clears the control and saves.
    fireEvent.change(deadlineInput, { target: { value: '' } });

    await user.click(screen.getByRole('button', { name: /Save changes/i }));

    // Then the PUT body either omits `deprecatedDeadline` or sends it as null,
    // both of which the backend treats as "clear the stored deadline".
    await waitFor(() => expect(state.updateCalls.length).toBe(1));
    const body = state.updateCalls[0].body;
    if ('deprecatedDeadline' in body) {
      expect(body.deprecatedDeadline).toBeNull();
    } else {
      expect(body.deprecatedDeadline).toBeUndefined();
    }
  });
});
