import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { NotificationCenter } from '../NotificationCenter';

const NOW = new Date('2026-04-18T12:00:00Z');

const notifications = [
  {
    id: 'n1',
    userId: 'dev-user',
    title: 'Inventory low',
    body: 'Widget stock below 10',
    type: 'automate.alert',
    link: '/browser/northwind/Product',
    read: false,
    createdAt: new Date(NOW.getTime() - 5 * 60_000).toISOString(),
  },
  {
    id: 'n2',
    userId: 'dev-user',
    title: 'Pipeline finished',
    body: 'Import job completed with 2 errors',
    type: 'system.info',
    link: '',
    read: true,
    createdAt: new Date(NOW.getTime() - 2 * 60 * 60_000).toISOString(),
  },
];

function setupFetchStub(state: {
  items: typeof notifications;
  failRead?: boolean;
  recordedReadIds?: string[];
  recordedBulkReads?: { types: string[] }[];
}) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString();
      const method = init?.method ?? 'GET';
      if (
        method === 'POST' &&
        url.includes('/api/v2/notifications/read-all')
      ) {
        if (state.failRead) {
          return new Response('{"errorCode":"X","errorName":"fail"}', {
            status: 500,
          });
        }
        // Replicate the backend's ?type= parsing — repeated and/or
        // comma-separated values flatten into a single deduped list.
        const types: string[] = [];
        const seen = new Set<string>();
        const qIndex = url.indexOf('?');
        if (qIndex >= 0) {
          const search = new URLSearchParams(url.slice(qIndex + 1));
          for (const raw of search.getAll('type')) {
            for (const part of raw.split(',')) {
              const t = part.trim();
              if (t && !seen.has(t)) {
                seen.add(t);
                types.push(t);
              }
            }
          }
        }
        state.recordedBulkReads?.push({ types });
        const typeSet = new Set(types);
        let updated = 0;
        state.items = state.items.map((n) => {
          if (n.read) return n;
          if (typeSet.size > 0 && !typeSet.has(n.type)) return n;
          updated += 1;
          state.recordedReadIds?.push(n.id);
          return { ...n, read: true };
        });
        return new Response(JSON.stringify({ updated }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }
      if (method === 'GET' && url.includes('/api/v2/notifications')) {
        return new Response(JSON.stringify({ data: state.items }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }
      const markMatch = url.match(/\/api\/v2\/notifications\/([^/]+)\/read$/);
      if (method === 'POST' && markMatch) {
        if (state.failRead) {
          return new Response('{"errorCode":"X","errorName":"fail"}', { status: 500 });
        }
        const id = decodeURIComponent(markMatch[1]);
        state.recordedReadIds?.push(id);
        state.items = state.items.map((n) =>
          n.id === id ? { ...n, read: true } : n,
        );
        return new Response(null, { status: 204 });
      }
      return new Response('{}', { status: 200 });
    }),
  );
}

function renderPanel(open = true, onClose = () => {}) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, refetchInterval: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <NotificationCenter open={open} onClose={onClose} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('NotificationCenter', () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.setSystemTime(NOW);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('does not fetch when closed', () => {
    setupFetchStub({ items: notifications });
    renderPanel(false);
    expect(fetch).not.toHaveBeenCalled();
  });

  it('renders notification title, body and relative time when open', async () => {
    setupFetchStub({ items: notifications });
    renderPanel(true);
    await waitFor(() => {
      expect(screen.getByText('Inventory low')).toBeInTheDocument();
    });
    expect(screen.getByText('Widget stock below 10')).toBeInTheDocument();
    expect(screen.getByText('Pipeline finished')).toBeInTheDocument();
    expect(screen.getByText('5m ago')).toBeInTheDocument();
    expect(screen.getByText('2h ago')).toBeInTheDocument();
  });

  it('shows empty state when there are no notifications', async () => {
    setupFetchStub({ items: [] });
    renderPanel(true);
    await waitFor(() => {
      expect(screen.getByText(/No notifications/i)).toBeInTheDocument();
    });
  });

  it('unread notifications are styled with a dot indicator', async () => {
    setupFetchStub({ items: notifications });
    renderPanel(true);
    await waitFor(() => {
      expect(screen.getByText('Inventory low')).toBeInTheDocument();
    });
    expect(
      screen.getByTestId('notification-item-n1').getAttribute('data-unread'),
    ).toBe('true');
    expect(
      screen.getByTestId('notification-item-n2').getAttribute('data-unread'),
    ).toBe('false');
  });

  it('clicking an unread notification marks it read and navigates when link is set', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    const recordedReadIds: string[] = [];
    setupFetchStub({ items: [...notifications], recordedReadIds });
    renderPanel(true);
    await waitFor(() => {
      expect(screen.getByText('Inventory low')).toBeInTheDocument();
    });

    await user.click(screen.getByTestId('notification-item-n1'));
    await waitFor(() => {
      expect(recordedReadIds).toContain('n1');
    });
  });

  it('"Mark all read" marks every unread notification', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    const recordedReadIds: string[] = [];
    const items = [
      ...notifications,
      {
        id: 'n3',
        userId: 'dev-user',
        title: 'Third',
        body: 'Body',
        type: 'system.info',
        link: '',
        read: false,
        createdAt: new Date(NOW.getTime() - 60_000).toISOString(),
      },
    ];
    setupFetchStub({ items, recordedReadIds });
    renderPanel(true);
    await waitFor(() => {
      expect(screen.getByText('Inventory low')).toBeInTheDocument();
    });

    await user.click(screen.getByRole('button', { name: /mark all read/i }));
    await waitFor(() => {
      expect(recordedReadIds).toContain('n1');
      expect(recordedReadIds).toContain('n3');
    });
    // already-read notification should not be POSTed
    expect(recordedReadIds).not.toContain('n2');
  });

  // Dogfood Round 3 #4: when the active tab has no unread rows the
  // "Mark all read" affordance should disappear entirely rather than
  // render as a disabled button (which surveyors flagged as dead UI).
  it('hides "Mark all read" when nothing is unread', async () => {
    setupFetchStub({ items: notifications.map((n) => ({ ...n, read: true })) });
    renderPanel(true);
    await waitFor(() => {
      expect(screen.getByText('Inventory low')).toBeInTheDocument();
    });
    expect(screen.queryByTestId('notification-mark-all')).toBeNull();
  });

  it('notification with a link renders as an anchor', async () => {
    setupFetchStub({ items: notifications });
    renderPanel(true);
    await waitFor(() => {
      expect(screen.getByText('Inventory low')).toBeInTheDocument();
    });
    const n1 = screen.getByTestId('notification-item-n1');
    expect(n1.tagName).toBe('A');
    expect(n1.getAttribute('href')).toBe('/browser/northwind/Product');
    const n2 = screen.getByTestId('notification-item-n2');
    expect(n2.tagName).not.toBe('A');
  });

  it('mention-typed notification renders an @ badge and links to /mentions deep link (US-340)', async () => {
    const mention = {
      id: 'm1',
      userId: 'dev-user',
      title: 'alice@example.com mentioned you',
      body: 'cc @bob can you review',
      type: 'mention',
      link: '/mentions?rid=ri.phonograph2-objects.main.object.emp1&commentId=c-42',
      read: false,
      createdAt: new Date(NOW.getTime() - 60_000).toISOString(),
    };
    setupFetchStub({ items: [mention] });
    renderPanel(true);
    await waitFor(() => {
      expect(
        screen.getByText('alice@example.com mentioned you'),
      ).toBeInTheDocument();
    });
    expect(
      screen.getByTestId('notification-type-badge-mention'),
    ).toBeInTheDocument();
    const row = screen.getByTestId('notification-item-m1');
    expect(row.getAttribute('data-type')).toBe('mention');
    expect(row.tagName).toBe('A');
    expect(row.getAttribute('href')).toContain('/mentions?');
    expect(row.getAttribute('href')).toContain('commentId=c-42');
  });

  it('non-mention notifications do not render the @ badge', async () => {
    setupFetchStub({ items: notifications });
    renderPanel(true);
    await waitFor(() => {
      expect(screen.getByText('Inventory low')).toBeInTheDocument();
    });
    expect(
      screen.queryByTestId('notification-type-badge-mention'),
    ).not.toBeInTheDocument();
  });

  // US-343: type tabs + bulk-mark-read
  describe('type tabs (US-343)', () => {
    const typedNotifications = [
      {
        id: 'm1',
        userId: 'dev-user',
        title: 'You were mentioned',
        body: 'cc @you',
        type: 'mention',
        link: '/mentions?id=1',
        read: false,
        createdAt: new Date(NOW.getTime() - 60_000).toISOString(),
      },
      {
        id: 'w1',
        userId: 'dev-user',
        title: 'Watched object updated',
        body: 'Customer #42 changed',
        type: 'watch',
        link: '',
        read: false,
        createdAt: new Date(NOW.getTime() - 2 * 60_000).toISOString(),
      },
      {
        id: 'a1',
        userId: 'dev-user',
        title: 'Approval requested',
        body: 'Action requires sign-off',
        type: 'approval',
        link: '',
        read: false,
        createdAt: new Date(NOW.getTime() - 3 * 60_000).toISOString(),
      },
      {
        id: 's1',
        userId: 'dev-user',
        title: 'Pipeline finished',
        body: 'Import job completed',
        type: 'system',
        link: '',
        read: true,
        createdAt: new Date(NOW.getTime() - 4 * 60_000).toISOString(),
      },
      {
        id: 'auto1',
        userId: 'dev-user',
        title: 'Inventory low',
        body: 'Widget < 10',
        type: 'automate.alert',
        link: '/browser/x',
        read: false,
        createdAt: new Date(NOW.getTime() - 5 * 60_000).toISOString(),
      },
    ];

    it('renders the four type tabs plus the All tab', async () => {
      setupFetchStub({ items: typedNotifications });
      renderPanel(true);
      await waitFor(() => {
        expect(screen.getByText('You were mentioned')).toBeInTheDocument();
      });

      for (const key of ['all', 'mention', 'watch', 'approval', 'system']) {
        expect(
          screen.getByTestId(`notification-tab-${key}`),
        ).toBeInTheDocument();
      }
      expect(
        screen
          .getByTestId('notification-tab-all')
          .getAttribute('data-active'),
      ).toBe('true');
    });

    it('per-tab unread counts include only matching rows', async () => {
      setupFetchStub({ items: typedNotifications });
      renderPanel(true);
      await waitFor(() => {
        expect(screen.getByText('You were mentioned')).toBeInTheDocument();
      });

      // 4 unread (m1, w1, a1, auto1)
      expect(screen.getByTestId('notification-tab-count-all')).toHaveTextContent(
        '4',
      );
      expect(
        screen.getByTestId('notification-tab-count-mention'),
      ).toHaveTextContent('1');
      expect(
        screen.getByTestId('notification-tab-count-watch'),
      ).toHaveTextContent('1');
      expect(
        screen.getByTestId('notification-tab-count-approval'),
      ).toHaveTextContent('1');
      // System tab catches both 'system' (read) and 'automate.alert' (unread)
      // — only auto1 is unread → count=1
      expect(
        screen.getByTestId('notification-tab-count-system'),
      ).toHaveTextContent('1');
    });

    it('switching tabs filters the visible rows', async () => {
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
      setupFetchStub({ items: typedNotifications });
      renderPanel(true);
      await waitFor(() => {
        expect(screen.getByText('You were mentioned')).toBeInTheDocument();
      });

      await user.click(screen.getByTestId('notification-tab-mention'));
      expect(screen.getByText('You were mentioned')).toBeInTheDocument();
      expect(screen.queryByText('Watched object updated')).not.toBeInTheDocument();
      expect(screen.queryByText('Approval requested')).not.toBeInTheDocument();

      await user.click(screen.getByTestId('notification-tab-system'));
      // system tab includes the legacy automate.alert as well as the
      // typed `system` entries.
      expect(screen.getByText('Inventory low')).toBeInTheDocument();
      expect(screen.getByText('Pipeline finished')).toBeInTheDocument();
      expect(
        screen.queryByText('You were mentioned'),
      ).not.toBeInTheDocument();
    });

    it('Mark all read on the All tab calls the bulk endpoint without a type filter', async () => {
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
      const recordedBulkReads: { types: string[] }[] = [];
      const recordedReadIds: string[] = [];
      setupFetchStub({
        items: [...typedNotifications],
        recordedBulkReads,
        recordedReadIds,
      });
      renderPanel(true);
      await waitFor(() => {
        expect(screen.getByText('You were mentioned')).toBeInTheDocument();
      });

      await user.click(screen.getByTestId('notification-mark-all'));
      await waitFor(() => {
        expect(recordedBulkReads).toHaveLength(1);
      });
      expect(recordedBulkReads[0].types).toEqual([]);
      // Backend bulk handler marks all 4 unread → recordedReadIds collected
      // by the fetch stub.
      expect(recordedReadIds.sort()).toEqual(
        ['m1', 'w1', 'a1', 'auto1'].sort(),
      );
    });

    it('Mark <type> read on a typed tab scopes the bulk endpoint to that type', async () => {
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
      const recordedBulkReads: { types: string[] }[] = [];
      const recordedReadIds: string[] = [];
      setupFetchStub({
        items: [...typedNotifications],
        recordedBulkReads,
        recordedReadIds,
      });
      renderPanel(true);
      await waitFor(() => {
        expect(screen.getByText('You were mentioned')).toBeInTheDocument();
      });

      await user.click(screen.getByTestId('notification-tab-mention'));
      await user.click(screen.getByTestId('notification-mark-all'));

      await waitFor(() => {
        expect(recordedBulkReads).toHaveLength(1);
      });
      expect(recordedBulkReads[0].types).toEqual(['mention']);
      expect(recordedReadIds).toEqual(['m1']);
    });

    // Dogfood Round 3 #4: same hide-not-disable contract on typed tabs.
    it('Mark all read button is hidden when the active tab has no unread', async () => {
      setupFetchStub({
        items: typedNotifications.map((n) => ({ ...n, read: true })),
      });
      renderPanel(true);
      await waitFor(() => {
        expect(screen.getByText('You were mentioned')).toBeInTheDocument();
      });
      expect(screen.queryByTestId('notification-mark-all')).toBeNull();
    });

    it('typed notifications render type-specific badges', async () => {
      setupFetchStub({ items: typedNotifications });
      renderPanel(true);
      await waitFor(() => {
        expect(screen.getByText('You were mentioned')).toBeInTheDocument();
      });

      expect(
        screen.getByTestId('notification-type-badge-mention'),
      ).toBeInTheDocument();
      expect(
        screen.getByTestId('notification-type-badge-watch'),
      ).toBeInTheDocument();
      expect(
        screen.getByTestId('notification-type-badge-approval'),
      ).toBeInTheDocument();
      expect(
        screen.getByTestId('notification-type-badge-system'),
      ).toBeInTheDocument();
    });
  });
});
