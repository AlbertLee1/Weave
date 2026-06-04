import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MetricsPage } from '../MetricsPage';

// BDD: the time-range tablist (24h / 7d / 30d) in MetricsPage must honour
// the standard WAI-ARIA tabs keyboard contract:
//   - ArrowRight / ArrowLeft move (and activate) the next/previous tab,
//     wrapping at the ends (automatic activation).
//   - Home / End jump to the first / last tab.
//   - roving tabindex: the selected tab is tabIndex 0, the rest -1, and
//     after an arrow key the newly selected tab receives DOM focus.
// Existing mouse-click / window-switch behaviour must be untouched.

const NOW = new Date('2026-04-18T12:00:00Z');

const APPLICATIONS = [
  {
    id: 'app-123',
    name: 'My Cool App',
    description: 'Test app',
    clientId: 'wapp_abc123',
    redirectUris: [],
    scopes: [],
    createdBy: 'user:albert@example.com',
    createdAt: NOW.toISOString(),
    updatedAt: NOW.toISOString(),
  },
];

function makeWindow(window: string, total: number, errors: number) {
  return {
    window,
    since: new Date(NOW.getTime() - 24 * 60 * 60 * 1000).toISOString(),
    until: NOW.toISOString(),
    total,
    errors,
    byStatus: { '2xx': total - errors, '4xx': errors },
    byMethod: { GET: Math.floor(total * 0.7), POST: Math.ceil(total * 0.3) },
    topRoutes: [
      {
        endpoint: '/api/v2/ontologies',
        method: 'GET',
        count: Math.floor(total * 0.5),
        errors: 0,
        p95Ms: 45.2,
      },
    ],
    p50Ms: 42.1,
    p95Ms: 105.8,
    p99Ms: 250.3,
  };
}

function buildUsageResponse(appId: string, clientId: string) {
  return {
    applicationId: appId,
    clientId,
    windows: [
      makeWindow('24h', 150, 5),
      makeWindow('7d', 900, 20),
      makeWindow('30d', 4200, 80),
    ],
  };
}

function setupFetchStub() {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.endsWith('/api/v2/developer/applications')) {
        return new Response(JSON.stringify({ applications: APPLICATIONS }), {
          status: 200,
        });
      }
      const m = url.match(/\/api\/v2\/developer\/applications\/([^/]+)\/usage$/);
      if (m) {
        const id = decodeURIComponent(m[1]);
        const app = APPLICATIONS.find((a) => a.id === id);
        return new Response(
          JSON.stringify(buildUsageResponse(id, app?.clientId ?? 'wapp_unknown')),
          { status: 200 },
        );
      }
      return new Response('{}', { status: 200 });
    }),
  );
}

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, refetchInterval: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <MetricsPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function tabs() {
  return {
    t24: screen.getByRole('tab', { name: /24h/i }),
    t7d: screen.getByRole('tab', { name: /7d/i }),
    t30: screen.getByRole('tab', { name: /30d/i }),
  };
}

describe('BDD: MetricsPage time-range tablist keyboard navigation', () => {
  beforeEach(() => {
    setupFetchStub();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('Given the selected tab is focused, When ArrowRight, Then focus + selection move to the next tab', async () => {
    const user = userEvent.setup();
    renderPage();
    const { t24, t7d } = tabs();

    t24.focus();
    expect(t24).toHaveFocus();

    await user.keyboard('{ArrowRight}');

    expect(t7d).toHaveFocus();
    expect(t7d.getAttribute('aria-selected')).toBe('true');
    expect(t24.getAttribute('aria-selected')).toBe('false');
  });

  it('Given the selected tab is focused, When ArrowLeft, Then focus + selection move to the previous tab', async () => {
    const user = userEvent.setup();
    renderPage();

    // move selection to 7d first
    await user.click(screen.getByRole('tab', { name: /7d/i }));
    const { t24, t7d } = tabs();
    t7d.focus();

    await user.keyboard('{ArrowLeft}');

    expect(t24).toHaveFocus();
    expect(t24.getAttribute('aria-selected')).toBe('true');
  });

  it('ArrowRight wraps from the last tab to the first', async () => {
    const user = userEvent.setup();
    renderPage();

    await user.click(screen.getByRole('tab', { name: /30d/i }));
    const { t24, t30 } = tabs();
    t30.focus();

    await user.keyboard('{ArrowRight}');

    expect(t24).toHaveFocus();
    expect(t24.getAttribute('aria-selected')).toBe('true');
  });

  it('ArrowLeft wraps from the first tab to the last', async () => {
    const user = userEvent.setup();
    renderPage();

    const { t24, t30 } = tabs();
    t24.focus();

    await user.keyboard('{ArrowLeft}');

    expect(t30).toHaveFocus();
    expect(t30.getAttribute('aria-selected')).toBe('true');
  });

  it('Home jumps to the first tab and End jumps to the last tab', async () => {
    const user = userEvent.setup();
    renderPage();

    const { t24, t7d, t30 } = tabs();
    t7d.focus();

    await user.keyboard('{End}');
    expect(t30).toHaveFocus();
    expect(t30.getAttribute('aria-selected')).toBe('true');

    await user.keyboard('{Home}');
    expect(t24).toHaveFocus();
    expect(t24.getAttribute('aria-selected')).toBe('true');
  });

  it('roving tabindex: selected tab has tabIndex 0, the rest -1, and it follows arrow navigation', async () => {
    const user = userEvent.setup();
    renderPage();

    const { t24, t7d, t30 } = tabs();
    expect(t24.getAttribute('tabindex')).toBe('0');
    expect(t7d.getAttribute('tabindex')).toBe('-1');
    expect(t30.getAttribute('tabindex')).toBe('-1');

    t24.focus();
    await user.keyboard('{ArrowRight}');

    expect(t24.getAttribute('tabindex')).toBe('-1');
    expect(t7d.getAttribute('tabindex')).toBe('0');
    expect(t30.getAttribute('tabindex')).toBe('-1');
  });

  it('keyboard navigation switches the displayed totals (data refresh preserved)', async () => {
    const user = userEvent.setup();
    renderPage();

    await waitFor(() => {
      expect(screen.getByText('150')).toBeInTheDocument();
    });

    const { t24 } = tabs();
    t24.focus();
    await user.keyboard('{ArrowRight}');

    await waitFor(() => {
      expect(screen.getByText('900')).toBeInTheDocument();
    });
  });

  it('mouse click still switches the selected tab (no regression)', async () => {
    const user = userEvent.setup();
    renderPage();

    await user.click(screen.getByRole('tab', { name: /30d/i }));
    const { t30 } = tabs();
    expect(t30.getAttribute('aria-selected')).toBe('true');
    await waitFor(() => {
      expect(screen.getByText('4,200')).toBeInTheDocument();
    });
  });
});
