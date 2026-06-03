import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ValueTypeAdminPage } from '../ValueTypeAdminPage';

// Unit BDD: bump a ValueType's version from the edit form.
//
// The v2 backend's UpdateValueTypeRequest already accepts `version int`
// (pkg/oms/admin_handlers.go) and applies it only when version > 0
// (existing.Version = req.Version). The admin edit form had no control for
// it, so operators could not bump a value-type version. This scenario covers
// the new optional numeric "Version" input.
//
// Given an operator editing an existing ValueType (current version 3),
// When  they set version = 5 and Save,
// Then  the PUT body carries version = 5.
// And   leaving the field untouched (or blank/0) omits the key so the server's
//       `if req.Version > 0` guard keeps the existing value.

const VALUE_TYPES = [
  {
    rid: 'ri.ontology.main.value-type.email',
    apiName: 'emailAddress',
    displayName: 'Email Address',
    baseType: 'string',
    constraints: { pattern: '^[^@]+@[^@]+$' },
    version: 3,
  },
];

interface StoredValueType {
  rid: string;
  apiName: string;
  displayName: string;
  baseType: string;
  constraints?: unknown;
  version: number;
}

interface StubState {
  valueTypes: StoredValueType[];
  updateCalls: Array<{ rid: string; body: Record<string, unknown> }>;
}

function makeStub(): StubState {
  return {
    valueTypes: JSON.parse(JSON.stringify(VALUE_TYPES)),
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
          url.includes('/api/v2/ontologies/northwind/valueTypesAdmin')
        ) {
          return jsonResponse({ data: state.valueTypes });
        }
        const ridMatch = url.match(
          /\/api\/v2\/ontologies\/northwind\/valueTypes\/byRid\/([^?]+)/,
        );
        if (ridMatch && method === 'PUT') {
          const rid = decodeURIComponent(ridMatch[1]);
          const body = init?.body ? JSON.parse(init.body as string) : {};
          state.updateCalls.push({ rid, body });
          const idx = state.valueTypes.findIndex((v) => v.rid === rid);
          if (idx < 0) return jsonResponse({ errorCode: 'NotFound' }, 404);
          const prev = state.valueTypes[idx];
          state.valueTypes[idx] = {
            ...prev,
            displayName: body.displayName ?? prev.displayName,
            baseType: body.baseType ?? prev.baseType,
            constraints: body.constraints ?? prev.constraints,
            version: body.version > 0 ? body.version : prev.version,
          };
          return jsonResponse(state.valueTypes[idx]);
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
      <MemoryRouter initialEntries={['/admin/northwind/valueTypes']}>
        <Routes>
          <Route
            path="/admin/:ontology/valueTypes"
            element={<ValueTypeAdminPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

async function openEditForm() {
  const user = userEvent.setup();
  renderPage();
  await waitFor(() => {
    expect(screen.getByText('Email Address')).toBeInTheDocument();
  });
  const row = screen.getByText('Email Address').closest('tr')!;
  await user.click(within(row).getByRole('button', { name: /Edit/i }));
  await screen.findByTestId('value-type-edit-form');
  return user;
}

describe('ValueTypeAdminPage version field', () => {
  let state: StubState;

  beforeEach(() => {
    state = makeStub();
    installFetch(state);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('sends version in the update body when the operator bumps it', async () => {
    const user = await openEditForm();

    const versionInput = (await screen.findByTestId(
      'value-type-version',
    )) as HTMLInputElement;

    // Preloads the existing version so operators see the current value.
    expect(versionInput.value).toBe('3');

    await user.clear(versionInput);
    await user.type(versionInput, '5');

    await user.click(screen.getByRole('button', { name: /Save changes/i }));

    await waitFor(() => {
      expect(state.updateCalls.length).toBe(1);
    });
    const body = state.updateCalls[0].body as Record<string, unknown>;
    expect(body.version).toBe(5);
  });

  it('omits version when the field is cleared to blank/0', async () => {
    const user = await openEditForm();

    const versionInput = (await screen.findByTestId(
      'value-type-version',
    )) as HTMLInputElement;

    // Clearing the field means "do not touch the version"; the backend guard
    // is `if req.Version > 0`, so the body must omit (or send <=0) version.
    await user.clear(versionInput);

    await user.click(screen.getByRole('button', { name: /Save changes/i }));

    await waitFor(() => {
      expect(state.updateCalls.length).toBe(1);
    });
    const body = state.updateCalls[0].body as Record<string, unknown>;
    expect('version' in body).toBe(false);
  });
});
