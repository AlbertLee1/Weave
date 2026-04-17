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
}) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString();
      const method = init?.method ?? 'GET';
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

  it('disables "Mark all read" when nothing is unread', async () => {
    setupFetchStub({ items: notifications.map((n) => ({ ...n, read: true })) });
    renderPanel(true);
    await waitFor(() => {
      expect(screen.getByText('Inventory low')).toBeInTheDocument();
    });
    const btn = screen.getByRole('button', { name: /mark all read/i });
    expect(btn).toBeDisabled();
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
});
