import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { RelationshipGraph } from '../RelationshipGraph';
import type { LinkType, WireObject } from '../../../api/types';

interface FetchSpec {
  outgoingLinkTypes: Record<string, LinkType[]>;
  linkedObjects: Record<string, WireObject[]>;
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function installFetch(spec: FetchSpec) {
  const calls: string[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL): Promise<Response> => {
      const url = typeof input === 'string' ? input : input.toString();
      calls.push(url);
      const olt = url.match(
        /\/api\/v2\/ontologies\/([^/]+)\/objectTypes\/([^/]+)\/outgoingLinkTypes(?:\?|$)/,
      );
      if (olt) {
        const apiName = decodeURIComponent(olt[2]);
        return jsonResponse({ data: spec.outgoingLinkTypes[apiName] ?? [] });
      }
      const lo = url.match(
        /\/api\/v2\/ontologies\/[^/]+\/objects\/([^/]+)\/([^/]+)\/links\/([^/?]+)(?:\?|$)/,
      );
      if (lo) {
        const apiName = decodeURIComponent(lo[1]);
        const pk = decodeURIComponent(lo[2]);
        const linkApi = decodeURIComponent(lo[3]);
        const key = `${apiName}:${pk}:${linkApi}`;
        return jsonResponse({ data: spec.linkedObjects[key] ?? [] });
      }
      return jsonResponse({ data: [] }, 404);
    }),
  );
  return calls;
}

function renderGraph(props: {
  ontologyApiName: string;
  rootObjectType: string;
  rootPrimaryKey: string;
}) {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchInterval: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={qc}>
      <RelationshipGraph {...props} />
    </QueryClientProvider>,
  );
}

const BOB: WireObject = {
  __rid: 'ri.o.bob',
  __apiName: 'Employee',
  __primaryKey: 'bob',
  id: 'bob',
};
const ENG: WireObject = {
  __rid: 'ri.o.eng',
  __apiName: 'Department',
  __primaryKey: 'engineering',
  id: 'engineering',
};

const REPORTS_TO: LinkType = {
  rid: 'ri.lt.reportsTo',
  apiName: 'reportsTo',
  displayName: 'Reports To',
  objectTypeApiName: 'Employee',
  linkedObjectTypeApiName: 'Employee',
  cardinality: 'MANY_TO_MANY',
  required: false,
};
const WORKS_IN: LinkType = {
  rid: 'ri.lt.worksIn',
  apiName: 'worksIn',
  displayName: 'Works In',
  objectTypeApiName: 'Employee',
  linkedObjectTypeApiName: 'Department',
  cardinality: 'MANY_TO_MANY',
  required: false,
};

describe('RelationshipGraph', () => {
  beforeEach(() => {
    installFetch({
      outgoingLinkTypes: {
        Employee: [REPORTS_TO, WORKS_IN],
        Department: [],
      },
      linkedObjects: {
        'Employee:alice:reportsTo': [BOB],
        'Employee:alice:worksIn': [ENG],
      },
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('renders the root node and its directly linked neighbours', async () => {
    renderGraph({
      ontologyApiName: 'main',
      rootObjectType: 'Employee',
      rootPrimaryKey: 'alice',
    });

    await waitFor(() => {
      const nodes = screen.getAllByTestId('relationship-node');
      expect(nodes.length).toBeGreaterThanOrEqual(3);
    });

    const ids = screen
      .getAllByTestId('relationship-node')
      .map((n) => n.getAttribute('data-node-id'));
    expect(ids).toContain('Employee:alice');
    expect(ids).toContain('Employee:bob');
    expect(ids).toContain('Department:engineering');

    const edges = screen.getAllByTestId('relationship-edge');
    expect(edges.length).toBe(2);
  });

  it('marks the root node distinctly from peers', async () => {
    renderGraph({
      ontologyApiName: 'main',
      rootObjectType: 'Employee',
      rootPrimaryKey: 'alice',
    });
    await waitFor(() => {
      expect(screen.getAllByTestId('relationship-node').length).toBeGreaterThanOrEqual(3);
    });

    const root = screen
      .getAllByTestId('relationship-node')
      .find((n) => n.getAttribute('data-node-id') === 'Employee:alice');
    expect(root).toBeDefined();
    expect(root!.getAttribute('data-root')).toBe('true');
  });

  it('clicking a non-root node fetches and merges its neighbours', async () => {
    vi.unstubAllGlobals();
    const CAROL: WireObject = {
      __rid: 'ri.o.carol',
      __apiName: 'Employee',
      __primaryKey: 'carol',
      id: 'carol',
    };
    installFetch({
      outgoingLinkTypes: {
        Employee: [REPORTS_TO],
        Department: [],
      },
      linkedObjects: {
        'Employee:alice:reportsTo': [BOB],
        'Employee:bob:reportsTo': [CAROL],
      },
    });

    renderGraph({
      ontologyApiName: 'main',
      rootObjectType: 'Employee',
      rootPrimaryKey: 'alice',
    });

    await waitFor(() => {
      const ids = screen
        .getAllByTestId('relationship-node')
        .map((n) => n.getAttribute('data-node-id'));
      expect(ids).toContain('Employee:bob');
    });

    const bobNode = screen
      .getAllByTestId('relationship-node')
      .find((n) => n.getAttribute('data-node-id') === 'Employee:bob');
    expect(bobNode).toBeDefined();

    const user = userEvent.setup();
    await user.click(bobNode!);

    await waitFor(() => {
      const ids = screen
        .getAllByTestId('relationship-node')
        .map((n) => n.getAttribute('data-node-id'));
      expect(ids).toContain('Employee:carol');
    });
  });

  it('shows the empty state when the root has no outgoing link types', async () => {
    vi.unstubAllGlobals();
    installFetch({
      outgoingLinkTypes: { Department: [] },
      linkedObjects: {},
    });
    renderGraph({
      ontologyApiName: 'main',
      rootObjectType: 'Department',
      rootPrimaryKey: 'engineering',
    });

    await waitFor(() => {
      expect(screen.getByTestId('relationship-empty')).toBeInTheDocument();
    });
  });

  it('shows an error banner when an expansion request fails', async () => {
    vi.unstubAllGlobals();
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        new Response(
          JSON.stringify({ errorName: 'boom', errorCode: 'INTERNAL' }),
          { status: 500, headers: { 'Content-Type': 'application/json' } },
        ),
      ),
    );
    renderGraph({
      ontologyApiName: 'main',
      rootObjectType: 'Employee',
      rootPrimaryKey: 'alice',
    });

    await waitFor(() => {
      expect(screen.getByTestId('relationship-error')).toBeInTheDocument();
    });
  });
});
