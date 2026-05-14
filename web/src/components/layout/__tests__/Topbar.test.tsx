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

function renderTopbarAt(path: string) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, refetchInterval: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Topbar />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('Topbar breadcrumbs', () => {
  beforeEach(() => {
    stubNotifications([]);
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('splits camelCase path segments into spaced words', () => {
    renderTopbarAt('/admin/iotDemo/objectTypes');
    expect(screen.getByText('Object Types')).toBeInTheDocument();
    expect(screen.getByText('Iot Demo')).toBeInTheDocument();
  });
});

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

  // Dogfood Round 3 #1: clicking the bell should flip the shared UI
  // store, not a local hook. Verifying via the store lets the
  // `/notifications` full page (and tests that probe the same state)
  // read drawer state without re-rendering the Topbar component tree.
  it('clicking the bell flips notificationDrawerOpen on the UI store', async () => {
    const { useUIStore } = await import('../../../stores/uiStore');
    useUIStore.getState().closeDrawer();
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    stubNotifications([]);
    renderTopbar();
    expect(useUIStore.getState().notificationDrawerOpen).toBe(false);
    await user.click(screen.getByRole('button', { name: /notifications/i }));
    expect(useUIStore.getState().notificationDrawerOpen).toBe(true);
  });

  // Dogfood Round 3 #3: on `/notifications` the full page already
  // renders the inbox surface, so the Topbar must NOT mount a second
  // NotificationCenter slide panel (which surveyors flagged as a
  // duplicate UI bug).
  it('does not render the notification drawer on /notifications', async () => {
    const { useUIStore } = await import('../../../stores/uiStore');
    useUIStore.getState().closeDrawer();
    stubNotifications([]);
    renderTopbarAt('/notifications');
    expect(screen.queryByTestId('slide-panel')).toBeNull();
  });
});

function installMatchMedia(initialMatches: boolean) {
  const listeners: Array<(ev: MediaQueryListEvent) => void> = [];
  const mql = {
    matches: initialMatches,
    media: '(prefers-color-scheme: dark)',
    onchange: null,
    addEventListener: (_e: string, cb: (ev: MediaQueryListEvent) => void) => {
      listeners.push(cb);
    },
    removeEventListener: (_e: string, cb: (ev: MediaQueryListEvent) => void) => {
      const i = listeners.indexOf(cb);
      if (i >= 0) listeners.splice(i, 1);
    },
    addListener: (cb: (ev: MediaQueryListEvent) => void) => {
      listeners.push(cb);
    },
    removeListener: (cb: (ev: MediaQueryListEvent) => void) => {
      const i = listeners.indexOf(cb);
      if (i >= 0) listeners.splice(i, 1);
    },
    dispatchEvent: () => true,
  };
  const original = window.matchMedia;
  window.matchMedia = vi.fn().mockReturnValue(mql) as unknown as typeof window.matchMedia;
  return {
    fire(matches: boolean) {
      mql.matches = matches;
      for (const cb of [...listeners]) {
        cb({ matches } as MediaQueryListEvent);
      }
    },
    cleanup() {
      if (original) {
        window.matchMedia = original;
      } else {
        // @ts-expect-error allow cleanup in environments without native matchMedia
        delete window.matchMedia;
      }
    },
  };
}

describe('Topbar theme selector', () => {
  beforeEach(() => {
    window.localStorage.clear();
    document.documentElement.classList.remove('dark', 'light');
    stubNotifications([]);
  });

  afterEach(() => {
    window.localStorage.clear();
    document.documentElement.classList.remove('dark', 'light');
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('renders a theme selector trigger button', () => {
    renderTopbar();
    expect(
      screen.getByRole('button', { name: /theme/i }),
    ).toBeInTheDocument();
  });

  it('opens a menu of light / dark / system options', async () => {
    const user = userEvent.setup();
    renderTopbar();
    const trigger = screen.getByRole('button', { name: /theme/i });
    await user.click(trigger);

    expect(
      screen.getByRole('menuitemradio', { name: /light/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole('menuitemradio', { name: /dark/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole('menuitemradio', { name: /system/i }),
    ).toBeInTheDocument();
  });

  it('selecting "light" applies the light class and persists', async () => {
    const user = userEvent.setup();
    renderTopbar();
    expect(document.documentElement.classList.contains('dark')).toBe(true);

    await user.click(screen.getByRole('button', { name: /theme/i }));
    await user.click(screen.getByRole('menuitemradio', { name: /light/i }));

    expect(window.localStorage.getItem('weave:theme')).toBe('light');
    expect(document.documentElement.classList.contains('light')).toBe(true);
    expect(document.documentElement.classList.contains('dark')).toBe(false);
  });

  it('selecting "system" follows prefers-color-scheme', async () => {
    const mm = installMatchMedia(false);
    try {
      const user = userEvent.setup();
      renderTopbar();
      await user.click(screen.getByRole('button', { name: /theme/i }));
      await user.click(screen.getByRole('menuitemradio', { name: /system/i }));

      expect(window.localStorage.getItem('weave:theme')).toBe('system');
      expect(document.documentElement.classList.contains('light')).toBe(true);
    } finally {
      mm.cleanup();
    }
  });

  it('marks the active option with aria-checked', async () => {
    const user = userEvent.setup();
    window.localStorage.setItem('weave:theme', 'light');
    renderTopbar();
    await user.click(screen.getByRole('button', { name: /theme/i }));
    expect(
      screen.getByRole('menuitemradio', { name: /light/i }),
    ).toHaveAttribute('aria-checked', 'true');
    expect(
      screen.getByRole('menuitemradio', { name: /dark/i }),
    ).toHaveAttribute('aria-checked', 'false');
  });
});
