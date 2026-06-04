import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { InterfaceAdminPage } from '../InterfaceAdminPage';

// BDD — Interface secondary metadata fields on shared properties & links.
//
// Gap: the SharedPropertiesEditor rendered apiName / base type / array but
// offered no control for the shared property's `displayName` or
// `description`; the OutgoingLinkTypesEditor rendered apiName / target /
// display name / cardinality / required but no `description`. The TS types
// (InterfaceSharedProperty.displayName?/description?,
// InterfaceOutgoingLinkType.description?) already carry these, and the
// backend passes sharedProperties / outgoingLinkTypes through as
// json.RawMessage — only the render layer was missing the inputs.
//
// Scenarios pin the contract end-to-end (interaction → captured request body):
//   1) Given a new Interface with one shared property and one outgoing link,
//      When the admin fills the shared property displayName/description and the
//      link description and saves, Then the POST /interfaces body carries
//      sharedProperties[0].displayName, sharedProperties[0].description and
//      outgoingLinkTypes[0].description with the typed values.
//   2) Given editing an existing Interface, When the admin edits those same
//      fields and saves, Then the PUT /interfaces/{rid} body carries them.

interface CapturedBody {
  sharedProperties?: Array<{ displayName?: string; description?: string }>;
  outgoingLinkTypes?: Array<{ description?: string }>;
  [k: string]: unknown;
}

interface StubState {
  createCalls: Array<{ body: CapturedBody }>;
  updateCalls: Array<{ body: CapturedBody }>;
  existing: Array<Record<string, unknown>>;
}

function makeStub(existing: Array<Record<string, unknown>> = []): StubState {
  return { createCalls: [], updateCalls: [], existing };
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
          return jsonResponse({ data: state.existing });
        }

        // Interface create.
        if (
          method === 'POST' &&
          url.endsWith('/api/v2/ontologies/northwind/interfaces')
        ) {
          const body: CapturedBody = init?.body
            ? JSON.parse(init.body as string)
            : {};
          state.createCalls.push({ body });
          return jsonResponse(
            {
              rid: 'ri.ontology.main.interface.new',
              apiName: body.apiName,
              displayName: body.displayName,
              sharedProperties: body.sharedProperties ?? [],
              outgoingLinkTypes: body.outgoingLinkTypes ?? [],
            },
            201,
          );
        }

        // Interface update (PUT to /interfaces/{rid}).
        if (
          method === 'PUT' &&
          url.includes('/api/v2/ontologies/northwind/interfaces/')
        ) {
          const body: CapturedBody = init?.body
            ? JSON.parse(init.body as string)
            : {};
          state.updateCalls.push({ body });
          return jsonResponse(
            {
              rid: 'ri.ontology.main.interface.existing',
              apiName: 'Locatable',
              displayName: body.displayName,
              sharedProperties: body.sharedProperties ?? [],
              outgoingLinkTypes: body.outgoingLinkTypes ?? [],
            },
            200,
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

describe('BDD — Interface shared property & link secondary metadata fields', () => {
  let state: StubState;

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('Given a new Interface, When the admin fills shared property displayName/description and link description and saves, Then the create body carries them', async () => {
    state = makeStub();
    installFetch(state);
    const user = userEvent.setup();

    renderPage();
    await waitFor(() => {
      expect(screen.getByTestId('interface-new-btn')).toBeInTheDocument();
    });

    await user.click(screen.getByTestId('interface-new-btn'));

    await user.type(
      await screen.findByTestId('interface-display-name'),
      'Locatable',
    );

    // Add one shared property and fill apiName + secondary metadata.
    await user.click(screen.getByTestId('interface-add-shared-property'));
    await user.type(
      await screen.findByLabelText('Shared property 1 api name'),
      'geohash',
    );
    await user.type(
      await screen.findByLabelText('Shared property 1 display name'),
      'Geohash',
    );
    await user.type(
      await screen.findByLabelText('Shared property 1 description'),
      'Geohash of the entity location',
    );

    // Add one outgoing link and fill apiName + description.
    await user.click(screen.getByTestId('interface-add-link-type'));
    await user.type(
      await screen.findByLabelText('Link type 1 api name'),
      'office',
    );
    await user.type(
      await screen.findByLabelText('Link type 1 description'),
      'Office where this entity resides',
    );

    await user.click(screen.getByTestId('interface-create-submit'));

    await waitFor(() => {
      expect(state.createCalls.length).toBe(1);
    });
    const body = state.createCalls[0].body;
    const sp = body.sharedProperties ?? [];
    const lt = body.outgoingLinkTypes ?? [];
    expect(sp.length).toBe(1);
    expect(sp[0].displayName).toBe('Geohash');
    expect(sp[0].description).toBe('Geohash of the entity location');
    expect(lt.length).toBe(1);
    expect(lt[0].description).toBe('Office where this entity resides');
  });

  it('Given editing an existing Interface, When the admin edits the shared property displayName/description and link description and saves, Then the update body carries them', async () => {
    state = makeStub([
      {
        rid: 'ri.ontology.main.interface.existing',
        apiName: 'Locatable',
        displayName: 'Locatable',
        sharedProperties: [
          {
            apiName: 'geohash',
            baseType: 'string',
            isArray: false,
            displayName: '',
            description: '',
          },
        ],
        outgoingLinkTypes: [
          {
            apiName: 'office',
            displayName: 'Office',
            linkedEntityTypeApiName: 'Office',
            cardinality: 'ONE',
            required: false,
            description: '',
          },
        ],
      },
    ]);
    installFetch(state);
    const user = userEvent.setup();

    renderPage();

    // Open the edit modal for the existing interface.
    const editBtn = await screen.findByTestId('interface-edit-btn');
    await user.click(editBtn);

    // Fill in the secondary metadata fields, which are pre-seeded blank.
    await user.type(
      await screen.findByLabelText('Shared property 1 display name'),
      'Geohash',
    );
    await user.type(
      await screen.findByLabelText('Shared property 1 description'),
      'Updated geohash description',
    );
    await user.type(
      await screen.findByLabelText('Link type 1 description'),
      'Updated link description',
    );

    await user.click(screen.getByTestId('interface-edit-submit'));

    await waitFor(() => {
      expect(state.updateCalls.length).toBe(1);
    });
    const body = state.updateCalls[0].body;
    const sp = body.sharedProperties ?? [];
    const lt = body.outgoingLinkTypes ?? [];
    expect(sp.length).toBe(1);
    expect(sp[0].displayName).toBe('Geohash');
    expect(sp[0].description).toBe('Updated geohash description');
    expect(lt.length).toBe(1);
    expect(lt[0].description).toBe('Updated link description');
  });
});
