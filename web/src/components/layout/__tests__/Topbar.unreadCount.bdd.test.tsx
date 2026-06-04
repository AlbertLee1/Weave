import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Topbar } from '../Topbar';

// BDD scenario (perf): the notification badge must read its number from the
// dedicated O(1) `GET /api/v2/notifications/unread-count` endpoint
// (response shape `{"count": <int>}`), NOT by pulling the entire unread
// list and counting client-side. The list endpoint is partial-index-backed
// and lightweight; loading every unread row just to render a badge defeats
// the "lightweight badge" contract the backend handler documents.

interface FetchCall {
  url: string;
}

function installFetchSpy(count: number) {
  const calls: FetchCall[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();
      calls.push({ url });
      if (url.includes('/api/v2/notifications/unread-count')) {
        return new Response(JSON.stringify({ count }), { status: 200 });
      }
      // The full notification list endpoint — should NOT be hit by the badge.
      if (url.includes('/api/v2/notifications')) {
        return new Response(JSON.stringify({ data: [] }), { status: 200 });
      }
      return new Response('{}', { status: 200 });
    }),
  );
  return calls;
}

function renderTopbar() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, refetchInterval: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <Topbar />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BDD: Topbar notification badge uses the dedicated unread-count endpoint', () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.setSystemTime(new Date('2026-06-04T12:00:00Z'));
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('Given the topbar renders, When unread count is N, Then the badge shows N from /unread-count', async () => {
    const calls = installFetchSpy(7);
    renderTopbar();

    await waitFor(() => {
      expect(screen.getByTestId('notification-badge')).toHaveTextContent('7');
    });

    // The badge request must hit the dedicated O(1) endpoint.
    expect(
      calls.some((c) => c.url.includes('/api/v2/notifications/unread-count')),
    ).toBe(true);

    // And it must NOT pull the full unread list just to compute the badge.
    const listCalls = calls.filter(
      (c) =>
        c.url.includes('/api/v2/notifications') &&
        !c.url.includes('/api/v2/notifications/unread-count'),
    );
    expect(listCalls).toHaveLength(0);
  });

  it('hides the badge when the unread count is zero', async () => {
    installFetchSpy(0);
    renderTopbar();

    await waitFor(() => {
      expect(
        screen.getByRole('link', { name: /notifications/i }),
      ).toBeInTheDocument();
    });
    expect(screen.queryByTestId('notification-badge')).not.toBeInTheDocument();
  });

  it('clamps the badge label to 9+ when the count exceeds 9', async () => {
    installFetchSpy(42);
    renderTopbar();

    await waitFor(() => {
      expect(screen.getByTestId('notification-badge')).toHaveTextContent('9+');
    });
  });
});
