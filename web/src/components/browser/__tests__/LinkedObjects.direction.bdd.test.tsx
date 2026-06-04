import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { LinkedObjectsTab } from '../LinkedObjectsTab';
import type { LinkType } from '../../../api/types';

// BDD — Object detail's LinkedObjects tab must let the user choose the
// traversal direction (forward / reverse) so that incoming/reverse links
// become discoverable.
//
// Backend contract:
//   - pkg/oss/handlers.go:651 reads `?direction=` off the linkedObjects
//     query and pkg/oss/service.go:62-65 walks the link source->target
//     ("forward", the default) or target->source ("reverse").
//
// The SPA previously never sent `?direction=`, so the tab could only ever
// surface forward links. These scenarios pin the user-visible contract:
// the default load uses forward traversal, and flipping the toggle to
// reverse re-fetches with `direction=reverse`.

const LINK: LinkType = {
  rid: 'ri.ontology.main.link-type.membership',
  apiName: 'membership',
  displayName: 'Membership',
  objectTypeApiName: 'User',
  linkedObjectTypeApiName: 'Group',
  cardinality: 'MANY_TO_MANY',
  required: false,
};

interface CapturedCall {
  linkApi: string;
  direction: string | null;
}

interface StubState {
  linkedObjects: Record<string, Array<Record<string, unknown>>>;
  linkedCalls: CapturedCall[];
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
          const parsed = new URL(url, 'http://localhost');
          state.linkedCalls.push({
            linkApi,
            direction: parsed.searchParams.get('direction'),
          });
          return jsonResponse({
            data: state.linkedObjects[linkApi] ?? [],
            totalCount: String(state.linkedObjects[linkApi]?.length ?? 0),
          });
        }

        // Edge-property schema lookups (unused here, but the tab may probe).
        const lpMatch = url.match(
          /\/api\/v2\/ontologies\/[^/]+\/links\/([^/]+)\/properties(\?.*)?$/,
        );
        if (lpMatch && method === 'GET') {
          return jsonResponse({ data: [] });
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

describe('BDD — linked objects forward/reverse direction', () => {
  let state: StubState;

  beforeEach(() => {
    state = { linkedObjects: { membership: [{ __primaryKey: 'g1' }] }, linkedCalls: [] };
    installFetch(state);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('Given the LinkedObjects view opens, Then the default load does NOT request reverse traversal', async () => {
    renderTab([LINK]);

    await waitFor(() => {
      expect(state.linkedCalls.length).toBeGreaterThan(0);
    });

    const first = state.linkedCalls[0];
    expect(first.linkApi).toBe('membership');
    // Default must be forward — either no direction param, or an explicit
    // "forward" value, but never "reverse".
    expect(first.direction === null || first.direction === 'forward').toBe(true);
  });

  it('Given the LinkedObjects view, When the user switches to reverse, Then listLinkedObjects re-fetches with direction=reverse', async () => {
    const user = userEvent.setup();
    renderTab([LINK]);

    await waitFor(() => {
      expect(state.linkedCalls.length).toBeGreaterThan(0);
    });
    const callsBefore = state.linkedCalls.length;

    // When — flip the direction toggle to reverse for this link group.
    const reverseToggle = await screen.findByTestId(
      'link-direction-reverse-membership',
    );
    await user.click(reverseToggle);

    // Then — a fresh request goes out carrying direction=reverse.
    await waitFor(() => {
      expect(
        state.linkedCalls.some((c) => c.direction === 'reverse'),
      ).toBe(true);
    });
    expect(state.linkedCalls.length).toBeGreaterThan(callsBefore);
  });
});
