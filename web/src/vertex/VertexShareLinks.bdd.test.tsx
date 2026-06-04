// VTX-013 BDD — quick share-link create / list / revoke wired to the
// Vertex "Share" TopBar button.
//
// Scenario (Given/When/Then), end-to-end against the three backend
// endpoints (stubbed at the fetch boundary so the contract — verb, path,
// 201/200/204 wire shapes — is exercised exactly as the server defines
// them in pkg/vertex/graphsvc/handler.go):
//
//   POST   /api/vertex/v1/graphs/{rid}/share-links   -> 201 {token,...}
//   GET    /api/vertex/v1/graphs/{rid}/share-links    -> 200 {shareLinks:[…]}
//   DELETE /api/vertex/v1/share-links/{token}         -> 204
//
// The full token is only ever disclosed at create time; the list response
// carries `tokenSuffix` only. The panel surfaces the create-time URL for
// copy, and lists existing links by their suffix with a Revoke button.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react';

import { VertexShareLinkPanel } from './VertexShareLinkPanel';

const RID = 'ri.vertex.main.graph.demo';

interface StubLink {
  tokenSuffix: string;
  graphRid: string;
  createdBy: string;
  createdAt: string;
  revoked: boolean;
}

// In-memory server state the fetch stub mutates so list reflects
// create/revoke side-effects across calls.
let serverLinks: StubLink[];
let nextToken: string;

function jsonResponse(status: number, body: unknown): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: '',
    text: async () => (body === undefined ? '' : JSON.stringify(body)),
    json: async () => body,
  } as unknown as Response;
}

function installFetchStub() {
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString();
    const method = (init?.method ?? 'GET').toUpperCase();

    if (url.endsWith(`/graphs/${RID}/share-links`) && method === 'POST') {
      const token = nextToken;
      serverLinks.push({
        tokenSuffix: token.slice(-8),
        graphRid: RID,
        createdBy: 'alice',
        createdAt: '2026-06-04T00:00:00Z',
        revoked: false,
      });
      return jsonResponse(201, {
        token,
        graphRid: RID,
        createdBy: 'alice',
        createdAt: '2026-06-04T00:00:00Z',
      });
    }

    if (url.endsWith(`/graphs/${RID}/share-links`) && method === 'GET') {
      return jsonResponse(200, { shareLinks: serverLinks });
    }

    const revokeMatch = url.match(/\/share-links\/([^/?#]+)$/);
    if (revokeMatch && method === 'DELETE') {
      const suffix = revokeMatch[1].slice(-8);
      serverLinks = serverLinks.filter((l) => l.tokenSuffix !== suffix);
      return jsonResponse(204, undefined);
    }

    throw new Error(`unexpected fetch: ${method} ${url}`);
  }) as unknown as typeof fetch;
}

const realFetch = globalThis.fetch;

beforeEach(() => {
  serverLinks = [];
  nextToken = 'TOKENabcdefABCDEF12345678';
  installFetchStub();
});

afterEach(() => {
  globalThis.fetch = realFetch;
  vi.restoreAllMocks();
});

describe('VertexShareLinkPanel (VTX-013)', () => {
  it('Given an open panel with no links When it loads Then it lists existing share links (empty state)', async () => {
    render(<VertexShareLinkPanel graphRid={RID} onClose={() => {}} />);
    await waitFor(() => {
      expect(globalThis.fetch).toHaveBeenCalled();
    });
    expect(await screen.findByTestId('vertex-share-empty')).toBeInTheDocument();
  });

  it('Given an open panel When the user creates a share link Then POST is sent and the returned URL is shown', async () => {
    render(<VertexShareLinkPanel graphRid={RID} onClose={() => {}} />);
    await screen.findByTestId('vertex-share-empty');

    fireEvent.click(screen.getByTestId('vertex-share-create'));

    // The full token's URL is rendered for the user to copy.
    const created = await screen.findByTestId('vertex-share-created-url');
    expect(created.textContent).toContain('/share-links/');
    expect(created.textContent).toContain(nextToken);
    expect(created.textContent).toContain('/graph');

    // POST hit the create endpoint exactly.
    const calls = (globalThis.fetch as unknown as ReturnType<typeof vi.fn>).mock.calls;
    const post = calls.find(
      ([, init]) => (init as RequestInit | undefined)?.method?.toUpperCase() === 'POST',
    );
    expect(post).toBeTruthy();
    expect(String(post![0])).toContain(`/graphs/${RID}/share-links`);
  });

  it('Given a created link When the panel reloads the list Then the link appears by suffix', async () => {
    render(<VertexShareLinkPanel graphRid={RID} onClose={() => {}} />);
    await screen.findByTestId('vertex-share-empty');

    fireEvent.click(screen.getByTestId('vertex-share-create'));
    await screen.findByTestId('vertex-share-created-url');

    // The newly minted link shows up in the list, identified by suffix.
    const suffix = nextToken.slice(-8);
    await waitFor(() => {
      expect(screen.getByTestId(`vertex-share-link-${suffix}`)).toBeInTheDocument();
    });
  });

  it('Given an existing link When the user clicks Revoke Then DELETE is sent and the row disappears', async () => {
    // Seed one pre-existing link so the list renders it on open.
    const seedSuffix = 'ZZ778899';
    serverLinks = [
      {
        tokenSuffix: seedSuffix,
        graphRid: RID,
        createdBy: 'alice',
        createdAt: '2026-06-03T00:00:00Z',
        revoked: false,
      },
    ];

    render(<VertexShareLinkPanel graphRid={RID} onClose={() => {}} />);

    const row = await screen.findByTestId(`vertex-share-link-${seedSuffix}`);
    fireEvent.click(within(row).getByTestId('vertex-share-revoke'));

    await waitFor(() => {
      expect(screen.queryByTestId(`vertex-share-link-${seedSuffix}`)).not.toBeInTheDocument();
    });

    const calls = (globalThis.fetch as unknown as ReturnType<typeof vi.fn>).mock.calls;
    const del = calls.find(
      ([, init]) => (init as RequestInit | undefined)?.method?.toUpperCase() === 'DELETE',
    );
    expect(del).toBeTruthy();
    expect(String(del![0])).toContain('/share-links/');
  });
});
