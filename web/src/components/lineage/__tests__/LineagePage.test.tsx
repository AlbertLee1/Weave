import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { LineagePage } from '../LineagePage';
import type {
  LineageDirection,
  LineageResponse,
} from '../../../api/lineage';

const ROOT_RID = 'ri.ontology.main.object-type.root';
const PARENT_RID = 'ri.ontology.main.object-type.parent';
const GRANDPARENT_RID = 'ri.ontology.main.object-type.grandparent';

// ReactFlow relies on browser APIs (ResizeObserver, layout measurement)
// that jsdom does not provide. The lineage page test only needs to verify
// our own glue (counts, expand button, detail panel, fetch wiring). Mock
// @xyflow/react with a minimal shell that renders the supplied nodes as
// the wired custom node component — that keeps lineage-node / lineage-node-expand-btn
// reachable while xyflow's own canvas behaviour is covered by their suite.
vi.mock('@xyflow/react', () => {
  return {
    __esModule: true,
    ReactFlowProvider: ({ children }: { children: React.ReactNode }) => (
      <>{children}</>
    ),
    ReactFlow: ({
      nodes,
      nodeTypes,
      children,
    }: {
      nodes: Array<{
        id: string;
        type?: string;
        data?: unknown;
      }>;
      nodeTypes?: Record<
        string,
        React.ComponentType<{ data: unknown; id: string }>
      >;
      children?: React.ReactNode;
    }) => (
      <div data-testid="rf-mock">
        <ul data-testid="rf-mock-nodes">
          {nodes.map((n) => {
            const Comp = nodeTypes?.[n.type ?? 'default'];
            return (
              <li key={n.id} data-testid="rf-mock-node" data-node-id={n.id}>
                {Comp ? <Comp id={n.id} data={n.data} /> : null}
              </li>
            );
          })}
        </ul>
        {children}
      </div>
    ),
    Background: () => null,
    Controls: () => null,
    MiniMap: () => null,
    Handle: () => null,
    Position: { Left: 'left', Right: 'right', Top: 'top', Bottom: 'bottom' },
  };
});

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

interface FetchCall {
  rid: string;
  direction: LineageDirection;
  depth: number;
}

function installFetch(
  responder: (call: FetchCall) => LineageResponse | { status: number; body?: unknown },
) {
  const calls: FetchCall[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(
      async (
        input: RequestInfo | URL,
        init?: RequestInit,
      ): Promise<Response> => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = (init?.method ?? 'GET').toUpperCase();
        const match = url.match(
          /\/api\/v2\/objects\/([^/?]+)\/lineage(?:\?(.*))?$/,
        );
        if (method === 'GET' && match) {
          const rid = decodeURIComponent(match[1]);
          const params = new URLSearchParams(match[2] ?? '');
          const direction =
            (params.get('direction') as LineageDirection) || 'upstream';
          const depth = Number(params.get('depth') ?? '1');
          const call: FetchCall = { rid, direction, depth };
          calls.push(call);
          const got = responder(call);
          if ('status' in got) {
            return jsonResponse(got.body ?? {}, got.status);
          }
          return jsonResponse(got);
        }
        return new Response('{}', { status: 200 });
      },
    ),
  );
  return calls;
}

function renderPage(rid: string) {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchInterval: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[`/lineage/${rid}`]}>
        <Routes>
          <Route path="/lineage/:rid" element={<LineagePage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('LineagePage', () => {
  beforeEach(() => {
    // Default: root has one upstream parent.
    installFetch((call) => {
      if (call.rid === ROOT_RID) {
        return {
          root: ROOT_RID,
          direction: call.direction,
          depth: call.depth,
          truncated: false,
          nodes: [
            { rid: ROOT_RID, type: 'object-type' },
            { rid: PARENT_RID, type: 'object-type' },
          ],
          edges: [
            {
              from: PARENT_RID,
              to: ROOT_RID,
              operation: 'pipeline-run',
              timestamp: '2026-04-01T12:00:00Z',
            },
          ],
        };
      }
      return {
        root: call.rid,
        direction: call.direction,
        depth: call.depth,
        truncated: false,
        nodes: [{ rid: call.rid, type: 'object-type' }],
        edges: [],
      };
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('renders the lineage page heading with the root rid', async () => {
    renderPage(ROOT_RID);
    expect(
      screen.getByRole('heading', { name: /Lineage/i }),
    ).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.getByTestId('lineage-root-rid')).toHaveTextContent(
        ROOT_RID,
      );
    });
  });

  it('renders one node per lineage node and reflects the counts', async () => {
    renderPage(ROOT_RID);
    await waitFor(() => {
      expect(screen.getAllByTestId('lineage-node')).toHaveLength(2);
    });
    const counts = screen.getByTestId('lineage-counts');
    expect(counts).toHaveAttribute('data-node-count', '2');
    expect(counts).toHaveAttribute('data-edge-count', '1');
    const rids = screen
      .getAllByTestId('lineage-node')
      .map((n) => n.getAttribute('data-rid'));
    expect(rids).toContain(ROOT_RID);
    expect(rids).toContain(PARENT_RID);
  });

  it('marks the root node and tags non-root nodes as expandable', async () => {
    renderPage(ROOT_RID);
    await waitFor(() => {
      expect(screen.getAllByTestId('lineage-node')).toHaveLength(2);
    });
    const root = screen
      .getAllByTestId('lineage-node')
      .find((n) => n.getAttribute('data-rid') === ROOT_RID);
    expect(root).toBeDefined();
    expect(root).toHaveAttribute('data-root', 'true');
    const parent = screen
      .getAllByTestId('lineage-node')
      .find((n) => n.getAttribute('data-rid') === PARENT_RID);
    expect(parent).toBeDefined();
    expect(parent).toHaveAttribute('data-root', 'false');
    const expandBtns = screen.getAllByTestId('lineage-node-expand-btn');
    expect(expandBtns).toHaveLength(1);
    expect(expandBtns[0]).toHaveAttribute('data-rid', PARENT_RID);
  });

  it('switching direction triggers a new fetch with that direction', async () => {
    const user = userEvent.setup();
    renderPage(ROOT_RID);
    await waitFor(() => {
      expect(screen.getAllByTestId('lineage-node')).toHaveLength(2);
    });
    const select = screen.getByTestId('lineage-direction-select');
    await user.selectOptions(select, 'downstream');
    await waitFor(() => {
      const calls = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls;
      const urls = calls.map(([req]) =>
        typeof req === 'string' ? req : (req as Request).toString(),
      );
      expect(urls.some((u) => u.includes('direction=downstream'))).toBe(true);
    });
  });

  it('changing depth re-fetches with the new depth', async () => {
    const user = userEvent.setup();
    renderPage(ROOT_RID);
    await waitFor(() => {
      expect(screen.getAllByTestId('lineage-node')).toHaveLength(2);
    });
    const depthInput = screen.getByTestId('lineage-depth-input');
    await user.clear(depthInput);
    await user.type(depthInput, '3');
    await waitFor(() => {
      const calls = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls;
      const urls = calls.map(([req]) =>
        typeof req === 'string' ? req : (req as Request).toString(),
      );
      expect(urls.some((u) => u.includes('depth=3'))).toBe(true);
    });
  });

  it('clicking expand on a non-root node fetches additional lineage and merges it', async () => {
    vi.unstubAllGlobals();
    installFetch((call) => {
      if (call.rid === ROOT_RID) {
        return {
          root: ROOT_RID,
          direction: call.direction,
          depth: call.depth,
          truncated: false,
          nodes: [
            { rid: ROOT_RID, type: 'object-type' },
            { rid: PARENT_RID, type: 'object-type' },
          ],
          edges: [
            {
              from: PARENT_RID,
              to: ROOT_RID,
              operation: 'pipeline-run',
              timestamp: '2026-04-01T12:00:00Z',
            },
          ],
        };
      }
      if (call.rid === PARENT_RID) {
        return {
          root: PARENT_RID,
          direction: call.direction,
          depth: call.depth,
          truncated: false,
          nodes: [
            { rid: PARENT_RID, type: 'object-type' },
            { rid: GRANDPARENT_RID, type: 'object-type' },
          ],
          edges: [
            {
              from: GRANDPARENT_RID,
              to: PARENT_RID,
              operation: 'pipeline-run',
              timestamp: '2026-03-01T12:00:00Z',
            },
          ],
        };
      }
      return {
        root: call.rid,
        direction: call.direction,
        depth: call.depth,
        truncated: false,
        nodes: [{ rid: call.rid }],
        edges: [],
      };
    });

    const user = userEvent.setup();
    renderPage(ROOT_RID);
    await waitFor(() => {
      expect(screen.getAllByTestId('lineage-node')).toHaveLength(2);
    });
    const expandBtns = screen.getAllByTestId('lineage-node-expand-btn');
    const parentExpand = expandBtns.find(
      (b) => b.getAttribute('data-rid') === PARENT_RID,
    );
    expect(parentExpand).toBeDefined();
    await user.click(parentExpand!);
    await waitFor(() => {
      const rids = screen
        .getAllByTestId('lineage-node')
        .map((n) => n.getAttribute('data-rid'));
      expect(rids).toContain(GRANDPARENT_RID);
    });
    const counts = screen.getByTestId('lineage-counts');
    expect(counts).toHaveAttribute('data-node-count', '3');
    expect(counts).toHaveAttribute('data-edge-count', '2');
  });

  it('clicking the expand button a second time collapses the contributed nodes', async () => {
    vi.unstubAllGlobals();
    installFetch((call) => {
      if (call.rid === ROOT_RID) {
        return {
          root: ROOT_RID,
          direction: call.direction,
          depth: call.depth,
          truncated: false,
          nodes: [
            { rid: ROOT_RID, type: 'object-type' },
            { rid: PARENT_RID, type: 'object-type' },
          ],
          edges: [
            {
              from: PARENT_RID,
              to: ROOT_RID,
              operation: 'pipeline-run',
              timestamp: '2026-04-01T12:00:00Z',
            },
          ],
        };
      }
      if (call.rid === PARENT_RID) {
        return {
          root: PARENT_RID,
          direction: call.direction,
          depth: call.depth,
          truncated: false,
          nodes: [
            { rid: PARENT_RID, type: 'object-type' },
            { rid: GRANDPARENT_RID, type: 'object-type' },
          ],
          edges: [
            {
              from: GRANDPARENT_RID,
              to: PARENT_RID,
              operation: 'pipeline-run',
              timestamp: '2026-03-01T12:00:00Z',
            },
          ],
        };
      }
      return {
        root: call.rid,
        direction: call.direction,
        depth: call.depth,
        truncated: false,
        nodes: [{ rid: call.rid }],
        edges: [],
      };
    });

    const user = userEvent.setup();
    renderPage(ROOT_RID);
    await waitFor(() => {
      expect(screen.getAllByTestId('lineage-node')).toHaveLength(2);
    });
    const parentExpand = screen
      .getAllByTestId('lineage-node-expand-btn')
      .find((b) => b.getAttribute('data-rid') === PARENT_RID)!;
    await user.click(parentExpand);
    await waitFor(() => {
      expect(screen.getAllByTestId('lineage-node')).toHaveLength(3);
    });
    // After expand, the same button should now say 'collapse'.
    const afterExpand = screen
      .getAllByTestId('lineage-node-expand-btn')
      .find((b) => b.getAttribute('data-rid') === PARENT_RID)!;
    expect(afterExpand).toHaveAttribute('data-expanded', 'true');
    await user.click(afterExpand);
    await waitFor(() => {
      expect(screen.getAllByTestId('lineage-node')).toHaveLength(2);
    });
    const counts = screen.getByTestId('lineage-counts');
    expect(counts).toHaveAttribute('data-node-count', '2');
    expect(counts).toHaveAttribute('data-edge-count', '1');
  });

  it('selecting a node renders the detail panel with property / dataset / transform context', async () => {
    const user = userEvent.setup();
    renderPage(ROOT_RID);
    await waitFor(() => {
      expect(screen.getAllByTestId('lineage-node')).toHaveLength(2);
    });
    expect(screen.queryByTestId('lineage-detail-panel')).toBeNull();
    const parent = screen
      .getAllByTestId('lineage-node')
      .find((n) => n.getAttribute('data-rid') === PARENT_RID)!;
    await user.click(parent);
    await waitFor(() => {
      expect(screen.getByTestId('lineage-detail-panel')).toBeInTheDocument();
    });
    const panel = screen.getByTestId('lineage-detail-panel');
    expect(panel).toHaveAttribute('data-rid', PARENT_RID);
    expect(panel).toHaveAttribute('data-node-type', 'object-type');
    expect(screen.getByTestId('lineage-detail-rid')).toHaveTextContent(
      PARENT_RID,
    );
    expect(screen.getByTestId('lineage-detail-type')).toHaveTextContent(
      'object-type',
    );
    // parent has one outgoing edge to root (operation = pipeline-run).
    expect(screen.getByTestId('lineage-detail-out-count')).toHaveTextContent(
      '1',
    );
    const outEdges = screen.getAllByTestId('lineage-detail-edge');
    expect(outEdges.length).toBeGreaterThanOrEqual(1);
    const outOps = outEdges
      .filter((e) => e.getAttribute('data-edge-direction') === 'out')
      .map((e) => e.getAttribute('data-edge-operation'));
    expect(outOps).toContain('pipeline-run');
  });

  it('renders a truncated indicator when the response is truncated', async () => {
    vi.unstubAllGlobals();
    installFetch((call) => ({
      root: call.rid,
      direction: call.direction,
      depth: call.depth,
      truncated: true,
      nodes: [{ rid: call.rid }],
      edges: [],
    }));
    renderPage(ROOT_RID);
    await waitFor(() => {
      expect(screen.getByTestId('lineage-truncated')).toBeInTheDocument();
    });
  });

  it('shows an empty state when no edges exist', async () => {
    vi.unstubAllGlobals();
    installFetch((call) => ({
      root: call.rid,
      direction: call.direction,
      depth: call.depth,
      truncated: false,
      nodes: [{ rid: call.rid }],
      edges: [],
    }));
    renderPage(ROOT_RID);
    await waitFor(() => {
      expect(screen.getByTestId('lineage-empty')).toBeInTheDocument();
    });
  });

  it('shows an error message when the request fails', async () => {
    vi.unstubAllGlobals();
    installFetch(() => ({ status: 500, body: { errorName: 'boom' } }));
    renderPage(ROOT_RID);
    await waitFor(() => {
      expect(screen.getByTestId('lineage-error')).toBeInTheDocument();
    });
  });
});
