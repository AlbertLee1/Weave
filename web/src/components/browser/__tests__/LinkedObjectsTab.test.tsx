import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { LinkedObjectsTab } from '../LinkedObjectsTab';
import type { LinkType } from '../../../api/types';

// US-497 — LinkProperty CRUD + 边属性编辑.
//
// PRD literal:
//   - "Browser 页 LinkedObjects tab 内嵌边属性表单"
//   - "调用 PUT .../links/{rid}/edges/{pk}/{pk}/properties"
//
// Below contract:
//   - Only MANY_TO_MANY link types expose the inline edit button (forward
//     direction; for now the source PK = the rendered object's PK and the
//     target PK = the linked row's __primaryKey). Cardinality ONE_TO_* must
//     not show an edit affordance, matching the backend's M2M-only edge
//     property column.
//   - Submit posts PUT
//     /api/v2/ontologies/{o}/links/{linkTypeRid}/edges/{sourcePk}/{targetPk}/properties
//     with body { values: { ... } }.
//   - The form pre-renders one field per declared LinkProperty (from
//     GET /links/{rid}/properties); unknown LinkProperty schema (empty list)
//     still shows an info row + Save (server-side validates).

const M2M_LINK: LinkType = {
  rid: 'ri.ontology.main.link-type.membership',
  apiName: 'membership',
  displayName: 'Membership',
  objectTypeApiName: 'User',
  linkedObjectTypeApiName: 'Group',
  cardinality: 'MANY_TO_MANY',
  required: false,
};

const O2M_LINK: LinkType = {
  rid: 'ri.ontology.main.link-type.owns',
  apiName: 'owns',
  displayName: 'Owns',
  objectTypeApiName: 'User',
  linkedObjectTypeApiName: 'Asset',
  cardinality: 'ONE_TO_MANY',
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
      displayName?: string;
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

        // GET linked objects.
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

        // GET link properties (schema).
        const lpMatch = url.match(
          /\/api\/v2\/ontologies\/[^/]+\/links\/([^/]+)\/properties(\?.*)?$/,
        );
        if (lpMatch && method === 'GET') {
          const ltRid = decodeURIComponent(lpMatch[1]);
          return jsonResponse({
            data: state.linkProperties[ltRid] ?? [],
          });
        }

        // PUT edge properties.
        const putMatch = url.match(
          /\/api\/v2\/ontologies\/[^/]+\/links\/([^/]+)\/edges\/([^/]+)\/([^/]+)\/properties(\?.*)?$/,
        );
        if (putMatch && method === 'PUT') {
          const body = init?.body
            ? (JSON.parse(String(init.body)) as {
                values: Record<string, unknown>;
              })
            : { values: {} };
          const call = {
            linkTypeRid: decodeURIComponent(putMatch[1]),
            sourcePk: decodeURIComponent(putMatch[2]),
            targetPk: decodeURIComponent(putMatch[3]),
            body,
          };
          state.putCalls.push(call);
          return jsonResponse({
            linkTypeRid: call.linkTypeRid,
            sourceObjectPk: call.sourcePk,
            targetObjectPk: call.targetPk,
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

describe('US-497 LinkedObjectsTab — edge property edit form', () => {
  let state: StubState;

  beforeEach(() => {
    state = makeStub();
    installFetch(state);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('does not render Edit edge button for non-M2M links', async () => {
    state.linkedObjects.owns = [{ __primaryKey: 'asset-1', name: 'Laptop' }];
    renderTab([O2M_LINK]);

    await waitFor(() => {
      expect(screen.getByTestId('linked-objects-table')).toBeInTheDocument();
    });
    expect(
      screen.queryByTestId('edit-edge-properties-asset-1'),
    ).not.toBeInTheDocument();
  });

  it('renders Edit edge button for M2M link rows', async () => {
    state.linkedObjects.membership = [
      { __primaryKey: 'g1', name: 'Admins' },
      { __primaryKey: 'g2', name: 'Editors' },
    ];
    state.linkProperties[M2M_LINK.rid] = [
      {
        rid: 'ri.ontology.main.link-property.role',
        linkTypeRid: M2M_LINK.rid,
        apiName: 'role',
        baseType: 'string',
        isArray: false,
        isNullable: true,
      },
    ];
    renderTab([M2M_LINK]);

    await waitFor(() => {
      expect(screen.getByTestId('linked-objects-table')).toBeInTheDocument();
    });
    expect(screen.getByTestId('edit-edge-properties-g1')).toBeInTheDocument();
    expect(screen.getByTestId('edit-edge-properties-g2')).toBeInTheDocument();
  });

  it('opens an inline form populated from the LinkProperty schema and submits PUT', async () => {
    state.linkedObjects.membership = [{ __primaryKey: 'g1', name: 'Admins' }];
    state.linkProperties[M2M_LINK.rid] = [
      {
        rid: 'ri.ontology.main.link-property.role',
        linkTypeRid: M2M_LINK.rid,
        apiName: 'role',
        baseType: 'string',
        isArray: false,
        isNullable: false,
      },
      {
        rid: 'ri.ontology.main.link-property.weight',
        linkTypeRid: M2M_LINK.rid,
        apiName: 'weight',
        baseType: 'long',
        isArray: false,
        isNullable: true,
      },
    ];
    const user = userEvent.setup();
    renderTab([M2M_LINK]);

    await waitFor(() => {
      expect(screen.getByTestId('linked-objects-table')).toBeInTheDocument();
    });

    await user.click(screen.getByTestId('edit-edge-properties-g1'));
    const form = await screen.findByTestId('edge-properties-form-g1');
    // Both declared properties surface as input fields.
    const roleInput = within(form).getByLabelText(/role/i);
    const weightInput = within(form).getByLabelText(/weight/i);
    await user.type(roleInput, 'admin');
    await user.type(weightInput, '7');

    await user.click(within(form).getByRole('button', { name: /save/i }));

    await waitFor(() => {
      expect(state.putCalls).toHaveLength(1);
    });
    expect(state.putCalls[0]).toMatchObject({
      linkTypeRid: M2M_LINK.rid,
      sourcePk: 'u1',
      targetPk: 'g1',
    });
    // long is coerced to a number before being sent.
    expect(state.putCalls[0].body.values).toEqual({
      role: 'admin',
      weight: 7,
    });
  });

  it('omits empty optional values from the PUT payload so nullable fields stay unset', async () => {
    state.linkedObjects.membership = [{ __primaryKey: 'g1', name: 'Admins' }];
    state.linkProperties[M2M_LINK.rid] = [
      {
        rid: 'ri.ontology.main.link-property.role',
        linkTypeRid: M2M_LINK.rid,
        apiName: 'role',
        baseType: 'string',
        isArray: false,
        isNullable: true,
      },
      {
        rid: 'ri.ontology.main.link-property.note',
        linkTypeRid: M2M_LINK.rid,
        apiName: 'note',
        baseType: 'string',
        isArray: false,
        isNullable: true,
      },
    ];
    const user = userEvent.setup();
    renderTab([M2M_LINK]);

    await waitFor(() => {
      expect(screen.getByTestId('linked-objects-table')).toBeInTheDocument();
    });

    await user.click(screen.getByTestId('edit-edge-properties-g1'));
    const form = await screen.findByTestId('edge-properties-form-g1');
    // Only fill role; leave note blank — blank optional must not become "".
    await user.type(within(form).getByLabelText(/role/i), 'admin');
    await user.click(within(form).getByRole('button', { name: /save/i }));

    await waitFor(() => {
      expect(state.putCalls).toHaveLength(1);
    });
    expect(state.putCalls[0].body.values).toEqual({ role: 'admin' });
  });
});
