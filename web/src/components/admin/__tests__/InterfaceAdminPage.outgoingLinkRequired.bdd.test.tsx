import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { InterfaceAdminPage } from '../InterfaceAdminPage';

// BDD — Interface OutgoingLinkTypesEditor `required` toggle.
//
// Gap: the OutgoingLinkTypesEditor renders apiName / target / display name /
// cardinality but offered no control to toggle `required`, so operators could
// not declare "implementing object types MUST populate this link". The TS
// type + data layer already carry `required`, the backend passes
// outgoingLinkTypes through as json.RawMessage — only the render layer was
// missing the checkbox.
//
// Scenarios pin the contract end-to-end (interaction → captured request body):
//   1) Given a new Interface with one outgoing link, When the admin ticks the
//      link's Required checkbox and saves, Then the POST /interfaces body
//      carries outgoingLinkTypes[0].required === true.
//   2) Given the admin leaves Required unticked, Then the body carries
//      outgoingLinkTypes[0].required === false (default).

interface StubState {
  createCalls: Array<{
    body: { outgoingLinkTypes?: Array<{ required?: boolean }> };
  }>;
}

function makeStub(): StubState {
  return { createCalls: [] };
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
          return jsonResponse({ data: [] });
        }
        if (
          method === 'GET' &&
          url.endsWith('/api/v2/ontologies/northwind/interfacesAdmin')
        ) {
          return jsonResponse({ data: [] });
        }

        // Interface create.
        if (
          method === 'POST' &&
          url.endsWith('/api/v2/ontologies/northwind/interfaces')
        ) {
          const body = init?.body ? JSON.parse(init.body as string) : {};
          state.createCalls.push({ body });
          return jsonResponse(
            {
              rid: 'ri.ontology.main.interface.new',
              apiName: body.apiName,
              displayName: body.displayName,
              outgoingLinkTypes: body.outgoingLinkTypes ?? [],
            },
            201,
          );
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
      <MemoryRouter initialEntries={['/admin/northwind/interfaces']}>
        <Routes>
          <Route
            path="/admin/:ontology/interfaces"
            element={<InterfaceAdminPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

async function openNewInterfaceAndAddLink(
  user: ReturnType<typeof userEvent.setup>,
) {
  renderPage();
  await waitFor(() => {
    expect(screen.getByTestId('interface-new-btn')).toBeInTheDocument();
  });

  await user.click(screen.getByTestId('interface-new-btn'));

  // Fill in the required display name so the form can submit.
  await user.type(
    await screen.findByTestId('interface-display-name'),
    'Locatable',
  );

  // Add one outgoing link type and give it an apiName (apiName is the
  // filter key — blank rows are dropped on submit).
  await user.click(screen.getByTestId('interface-add-link-type'));
  const apiNameInput = await screen.findByLabelText('Link type 1 api name');
  await user.type(apiNameInput, 'office');
}

describe('BDD — Interface outgoing link `required` toggle', () => {
  let state: StubState;

  beforeEach(() => {
    state = makeStub();
    installFetch(state);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('Given a new Interface with one outgoing link, When the admin ticks Required and saves, Then the create body carries outgoingLinkTypes[0].required === true', async () => {
    const user = userEvent.setup();
    await openNewInterfaceAndAddLink(user);

    // Tick the Required checkbox on row 1.
    const requiredCheckbox = await screen.findByLabelText(
      'Link type 1 required',
    );
    expect(requiredCheckbox).not.toBeChecked();
    await user.click(requiredCheckbox);
    expect(requiredCheckbox).toBeChecked();

    await user.click(screen.getByTestId('interface-create-submit'));

    await waitFor(() => {
      expect(state.createCalls.length).toBe(1);
    });
    const links = state.createCalls[0].body.outgoingLinkTypes ?? [];
    expect(links.length).toBe(1);
    expect(links[0].required).toBe(true);
  });

  it('Given a new Interface with one outgoing link, When the admin leaves Required unticked and saves, Then the create body carries outgoingLinkTypes[0].required === false', async () => {
    const user = userEvent.setup();
    await openNewInterfaceAndAddLink(user);

    // Do NOT tick the checkbox — verify it defaults to unchecked.
    const requiredCheckbox = await screen.findByLabelText(
      'Link type 1 required',
    );
    expect(requiredCheckbox).not.toBeChecked();

    await user.click(screen.getByTestId('interface-create-submit'));

    await waitFor(() => {
      expect(state.createCalls.length).toBe(1);
    });
    const links = state.createCalls[0].body.outgoingLinkTypes ?? [];
    expect(links.length).toBe(1);
    expect(links[0].required).toBe(false);
  });
});
