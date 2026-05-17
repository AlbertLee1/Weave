import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { LinkedObjectsTab } from '../LinkedObjectsTab';
import type { LinkType } from '../../../api/types';

// US-497 BDD — Browser LinkedObjects tab embeds an edge-property edit
// form on MANY_TO_MANY links and persists edits via the backend PUT
// endpoint declared in pkg/oms/admin_handlers_link_property.go.
//
// PRD literal:
//   - "Browser 页 LinkedObjects tab 内嵌边属性表单"
//   - "调用 PUT .../links/{rid}/edges/{pk}/{pk}/properties"
//
// Two scenarios — happy path + cardinality gate — pin the user-visible
// contract from the SPA side. The happy path is the load-bearing assertion
// (covers schema fetch, form render, value coercion, PUT payload, and the
// post-save close); the cardinality scenario is the positive control that
// blocks the same wiring from accidentally exposing edit affordances on
// ONE_TO_ONE / ONE_TO_MANY links (backend rejects those edges anyway, but
// the UI must not let users hit that wall).

const M2M_LINK: LinkType = {
  rid: 'ri.ontology.main.link-type.membership',
  apiName: 'membership',
  displayName: 'Membership',
  objectTypeApiName: 'User',
  linkedObjectTypeApiName: 'Group',
  cardinality: 'MANY_TO_MANY',
  required: false,
};

const O2O_LINK: LinkType = {
  rid: 'ri.ontology.main.link-type.profile',
  apiName: 'profile',
  displayName: 'Profile',
  objectTypeApiName: 'User',
  linkedObjectTypeApiName: 'Profile',
  cardinality: 'ONE_TO_ONE',
  required: false,
};

interface StubState {
  linkedObjects: Record<string, Array<Record<string, unknown>>>;
  linkProperties: Record<
    string,
    Array<{
      rid: string;
      linkTypeRid: string;
      apiName: string;
      baseType: string;
      isArray: boolean;
      isNullable: boolean;
    }>
  >;
  putCalls: Array<{
    linkTypeRid: string;
    sourcePk: string;
    targetPk: string;
    body: { values: Record<string, unknown> };
  }>;
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

        const linkedMatch = url.match(
          /\/api\/v2\/ontologies\/[^/]+\/objects\/[^/]+\/[^/]+\/links\/([^/?]+)(\?.*)?$/,
        );
        if (linkedMatch && method === 'GET') {
          const linkApi = decodeURIComponent(linkedMatch[1]);
          return jsonResponse({
            data: state.linkedObjects[linkApi] ?? [],
            totalCount: String(state.linkedObjects[linkApi]?.length ?? 0),
          });
        }

        const lpMatch = url.match(
          /\/api\/v2\/ontologies\/[^/]+\/links\/([^/]+)\/properties(\?.*)?$/,
        );
        if (lpMatch && method === 'GET') {
          const ltRid = decodeURIComponent(lpMatch[1]);
          return jsonResponse({ data: state.linkProperties[ltRid] ?? [] });
        }

        const putMatch = url.match(
          /\/api\/v2\/ontologies\/[^/]+\/links\/([^/]+)\/edges\/([^/]+)\/([^/]+)\/properties(\?.*)?$/,
        );
        if (putMatch && method === 'PUT') {
          const body = init?.body
            ? (JSON.parse(String(init.body)) as {
                values: Record<string, unknown>;
              })
            : { values: {} };
          state.putCalls.push({
            linkTypeRid: decodeURIComponent(putMatch[1]),
            sourcePk: decodeURIComponent(putMatch[2]),
            targetPk: decodeURIComponent(putMatch[3]),
            body,
          });
          return jsonResponse({
            linkTypeRid: decodeURIComponent(putMatch[1]),
            sourceObjectPk: decodeURIComponent(putMatch[2]),
            targetObjectPk: decodeURIComponent(putMatch[3]),
            edgeProperties: body.values,
            createdAt: '2026-05-17T00:00:00Z',
          });
        }

        return new Response('{}', { status: 200 });
      },
    ),
  );
}

function renderTab(linkTypes: LinkType[]) {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchInterval: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={qc}>
      <LinkedObjectsTab
        ontologyApiName="northwind"
        objectType="User"
        primaryKey="u1"
        linkTypes={linkTypes}
      />
    </QueryClientProvider>,
  );
}

function makeStub(): StubState {
  return {
    linkedObjects: {},
    linkProperties: {},
    putCalls: [],
  };
}

describe('US-497 BDD — inline edge property editor', () => {
  let state: StubState;

  beforeEach(() => {
    state = makeStub();
    installFetch(state);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('Given a MANY_TO_MANY link with a declared role LinkProperty, When the admin edits the edge and saves, Then PUT is called with the typed value and the form closes', async () => {
    state.linkedObjects.membership = [
      { __primaryKey: 'g1', name: 'Admins' },
    ];
    state.linkProperties[M2M_LINK.rid] = [
      {
        rid: 'ri.ontology.main.link-property.role',
        linkTypeRid: M2M_LINK.rid,
        apiName: 'role',
        baseType: 'string',
        isArray: false,
        isNullable: false,
      },
    ];
    const user = userEvent.setup();
    renderTab([M2M_LINK]);

    await waitFor(() => {
      expect(screen.getByTestId('linked-objects-table')).toBeInTheDocument();
    });

    // When — admin opens the inline form for g1 and types the role.
    await user.click(screen.getByTestId('edit-edge-properties-g1'));
    const form = await screen.findByTestId('edge-properties-form-g1');
    await user.type(within(form).getByLabelText(/role/i), 'admin');
    await user.click(within(form).getByRole('button', { name: /save/i }));

    // Then — the backend PUT is called once with the correctly shaped body.
    await waitFor(() => {
      expect(state.putCalls).toHaveLength(1);
    });
    expect(state.putCalls[0]).toEqual({
      linkTypeRid: M2M_LINK.rid,
      sourcePk: 'u1',
      targetPk: 'g1',
      body: { values: { role: 'admin' } },
    });

    // And — the form is dismissed after success (the row's Edit button is
    // back on its own).
    await waitFor(() => {
      expect(
        screen.queryByTestId('edge-properties-form-g1'),
      ).not.toBeInTheDocument();
    });
    expect(screen.getByTestId('edit-edge-properties-g1')).toBeInTheDocument();
  });

  it('Given a ONE_TO_ONE link, When the row renders, Then no Edit edge button appears (positive control: M2M still does)', async () => {
    state.linkedObjects.profile = [{ __primaryKey: 'p1', name: 'Profile-1' }];
    state.linkedObjects.membership = [{ __primaryKey: 'g1', name: 'Admins' }];
    state.linkProperties[M2M_LINK.rid] = [];
    renderTab([O2O_LINK, M2M_LINK]);

    // Two tables render — one per link type. Wait for both to load.
    await waitFor(() => {
      expect(screen.getAllByTestId('linked-objects-table')).toHaveLength(2);
    });

    // Positive control — M2M edge edit button must still appear.
    expect(screen.getByTestId('edit-edge-properties-g1')).toBeInTheDocument();
    // Negative — O2O row must NOT show an edit button.
    expect(
      screen.queryByTestId('edit-edge-properties-p1'),
    ).not.toBeInTheDocument();
  });
});
