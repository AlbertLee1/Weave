import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Topbar } from '../Topbar';

const NOW = new Date('2026-04-18T12:00:00Z');

function stubNotifications(items: unknown[]) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.includes('/api/v2/notifications')) {
        return new Response(JSON.stringify({ data: items }), { status: 200 });
      }
      return new Response('{}', { status: 200 });
    }),
  );
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

describe('Topbar notifications', () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.setSystemTime(NOW);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('renders a bell button in the top bar', async () => {
    stubNotifications([]);
    renderTopbar();
    expect(
      screen.getByRole('button', { name: /notifications/i }),
    ).toBeInTheDocument();
  });

  it('shows an unread count badge when there are unread notifications', async () => {
    stubNotifications([
      {
        id: 'n1',
        userId: 'dev-user',
        title: 'A',
        body: '',
        type: '',
        link: '',
        read: false,
        createdAt: NOW.toISOString(),
      },
      {
        id: 'n2',
        userId: 'dev-user',
        title: 'B',
        body: '',
        type: '',
        link: '',
        read: false,
        createdAt: NOW.toISOString(),
      },
    ]);
    renderTopbar();
    await waitFor(() => {
      expect(screen.getByTestId('notification-badge')).toHaveTextContent('2');
    });
  });

  it('hides the badge when there are no unread notifications', async () => {
    stubNotifications([]);
    renderTopbar();
    await waitFor(() => {
      expect(
        screen.getByRole('button', { name: /notifications/i }),
      ).toBeInTheDocument();
    });
    expect(screen.queryByTestId('notification-badge')).not.toBeInTheDocument();
  });

  it('displays 9+ when unread count exceeds 9', async () => {
    stubNotifications(
      Array.from({ length: 15 }, (_, i) => ({
        id: `n${i}`,
        userId: 'dev-user',
        title: 'x',
        body: '',
        type: '',
        link: '',
        read: false,
        createdAt: NOW.toISOString(),
      })),
    );
    renderTopbar();
    await waitFor(() => {
      expect(screen.getByTestId('notification-badge')).toHaveTextContent('9+');
    });
  });

  it('opens the notification panel when the bell is clicked', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    stubNotifications([]);
    renderTopbar();
    const bell = screen.getByRole('button', { name: /notifications/i });
    await user.click(bell);
    expect(screen.getByTestId('slide-panel')).toHaveTextContent(/notifications/i);
  });
});
