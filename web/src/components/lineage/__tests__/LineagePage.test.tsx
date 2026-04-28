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
      expect(screen.getByText(ROOT_RID)).toBeInTheDocument();
    });
  });

  it('renders one node per lineage node and one edge per lineage edge', async () => {
    renderPage(ROOT_RID);
    await waitFor(() => {
      const nodes = screen.getAllByTestId('lineage-node');
      expect(nodes).toHaveLength(2);
    });
    expect(screen.getAllByTestId('lineage-edge')).toHaveLength(1);
    const rids = screen
      .getAllByTestId('lineage-node')
      .map((n) => n.getAttribute('data-rid'));
    expect(rids).toContain(ROOT_RID);
    expect(rids).toContain(PARENT_RID);
  });

  it('switching direction triggers a new fetch with that direction', async () => {
    const user = userEvent.setup();
    renderPage(ROOT_RID);
    await waitFor(() => {
      expect(screen.getAllByTestId('lineage-node')).toHaveLength(2);
    });
    const select = screen.getByRole('combobox', { name: /Direction/i });
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
    const depthInput = screen.getByRole('spinbutton', { name: /Depth/i });
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

  it('clicking a non-root node fetches additional lineage from that node and merges it', async () => {
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
    const parent = screen
      .getAllByTestId('lineage-node')
      .find((n) => n.getAttribute('data-rid') === PARENT_RID);
    expect(parent).toBeDefined();
    await user.click(parent!);
    await waitFor(() => {
      const rids = screen
        .getAllByTestId('lineage-node')
        .map((n) => n.getAttribute('data-rid'));
      expect(rids).toContain(GRANDPARENT_RID);
    });
    expect(screen.getAllByTestId('lineage-edge')).toHaveLength(2);
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
      expect(screen.getByText(/truncated/i)).toBeInTheDocument();
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
      expect(screen.getByText(/failed to load/i)).toBeInTheDocument();
    });
  });
});
