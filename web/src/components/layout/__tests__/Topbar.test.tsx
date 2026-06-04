import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Topbar } from '../Topbar';

const NOW = new Date('2026-04-18T12:00:00Z');

// The badge now reads its number from the dedicated O(1)
// `/notifications/unread-count` endpoint ({"count": <int>}) rather than
// counting the full unread list. We still accept an `items` array for the
// existing call sites and derive the count from its length so these
// scenarios keep expressing intent in terms of "N unread notifications".
function stubNotifications(items: unknown[]) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.includes('/api/v2/notifications/unread-count')) {
        return new Response(JSON.stringify({ count: items.length }), {
          status: 200,
        });
      }
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

  it('renders a bell link in the top bar that points to /notifications', async () => {
    stubNotifications([]);
    renderTopbar();
    const bell = screen.getByRole('link', { name: /notifications/i });
    expect(bell).toBeInTheDocument();
    // Dogfood Round 3 (revisit): the bell is the single entry into the
    // /notifications full page — no Topbar drawer in the way.
    expect(bell).toHaveAttribute('href', '/notifications');
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
        screen.getByRole('link', { name: /notifications/i }),
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

  // Dogfood Round 3 (revisit): the Topbar drawer was removed entirely
  // in favour of the /notifications full page. The bell link must NOT
  // mount a slide-panel on any route — that was the surveyors' "always
  // open" / "duplicate UI" complaint at root.
  it('never mounts a notification slide-panel drawer', async () => {
    stubNotifications([]);
    renderTopbar();
    expect(screen.queryByTestId('slide-panel')).toBeNull();
  });

  it('never mounts the drawer on /notifications either', async () => {
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
