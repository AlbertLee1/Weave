import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MetricsPage } from '../MetricsPage';

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
  {
    id: 'app-456',
    name: 'Other App',
    description: '',
    clientId: 'wapp_def456',
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
      {
        endpoint: '/api/v2/ontologies/{ontology}/objects',
        method: 'POST',
        count: Math.ceil(total * 0.3),
        errors,
        p95Ms: 120.5,
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
    windows: [makeWindow('24h', 150, 5), makeWindow('7d', 900, 20), makeWindow('30d', 4200, 80)],
  };
}

function setupFetchStub(opts: { apps?: boolean; usage?: boolean } = {}) {
  const apps = opts.apps ?? true;
  const usage = opts.usage ?? true;
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.endsWith('/api/v2/developer/applications')) {
        if (!apps) return new Response('{"errorCode":"X","errorName":"fail"}', { status: 500 });
        return new Response(JSON.stringify({ applications: APPLICATIONS }), { status: 200 });
      }
      const m = url.match(/\/api\/v2\/developer\/applications\/([^/]+)\/usage$/);
      if (m) {
        if (!usage) return new Response('{"errorCode":"X","errorName":"fail"}', { status: 500 });
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

describe('MetricsPage', () => {
  beforeEach(() => {
    setupFetchStub();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('renders the metrics page heading', () => {
    renderPage();
    expect(screen.getByText(/API Metrics/i)).toBeInTheDocument();
  });

  it('loads applications into the dropdown', async () => {
    renderPage();
    await waitFor(() => {
      const select = screen.getByLabelText(/Application/i) as HTMLSelectElement;
      expect(select.options.length).toBeGreaterThanOrEqual(2);
    });
  });

  it('renders window tabs (24h / 7d / 30d) and defaults to 24h', async () => {
    renderPage();
    const tab24 = screen.getByRole('tab', { name: /24h/i });
    const tab7d = screen.getByRole('tab', { name: /7d/i });
    const tab30 = screen.getByRole('tab', { name: /30d/i });
    expect(tab24.getAttribute('aria-selected')).toBe('true');
    expect(tab7d.getAttribute('aria-selected')).toBe('false');
    expect(tab30.getAttribute('aria-selected')).toBe('false');
  });

  it('renders KPI cards with total / errors / error rate / P50/P95/P99 after usage load', async () => {
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Total requests')).toBeInTheDocument();
      expect(screen.getByText('150')).toBeInTheDocument();
    });
    expect(screen.getAllByText('Errors').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('Error rate')).toBeInTheDocument();
    expect(screen.getByText('P50 latency')).toBeInTheDocument();
    expect(screen.getByText('P95 latency')).toBeInTheDocument();
    expect(screen.getByText('P99 latency')).toBeInTheDocument();
    expect(screen.getByText('3.33%')).toBeInTheDocument();
  });

  it('renders the endpoint latency table with rows from topRoutes', async () => {
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('/api/v2/ontologies')).toBeInTheDocument();
    });
    expect(screen.getByText('/api/v2/ontologies/{ontology}/objects')).toBeInTheDocument();
  });

  it('switching window tab updates the displayed totals', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('150')).toBeInTheDocument();
    });
    await user.click(screen.getByRole('tab', { name: /7d/i }));
    await waitFor(() => {
      expect(screen.getByText('900')).toBeInTheDocument();
    });
  });

  it('renders bar charts for method and status distributions', async () => {
    renderPage();
    await waitFor(() => {
      expect(screen.getByText(/Request volume by method/i)).toBeInTheDocument();
    });
    expect(screen.getByText(/Response status distribution/i)).toBeInTheDocument();
    expect(screen.getAllByTestId('bar-chart').length).toBeGreaterThanOrEqual(2);
    expect(screen.getAllByText('GET').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('2xx')).toBeInTheDocument();
  });

  it('shows empty state when no applications are registered', async () => {
    vi.unstubAllGlobals();
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        new Response(JSON.stringify({ applications: [] }), { status: 200 }),
      ),
    );
    renderPage();
    await waitFor(() => {
      expect(screen.getByText(/No applications yet/i)).toBeInTheDocument();
    });
  });

  // Dogfood report #6: previously the empty state was a one-liner pointing
  // to an unspecified "Developer Console". Make sure the new state gives
  // the user actionable next steps: a curl snippet and a link to the API
  // Playground.
  it('empty state includes a curl snippet and Playground link', async () => {
    vi.unstubAllGlobals();
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        new Response(JSON.stringify({ applications: [] }), { status: 200 }),
      ),
    );
    renderPage();
    await waitFor(() => {
      expect(
        screen.getByTestId('metrics-empty-applications'),
      ).toBeInTheDocument();
    });
    expect(
      screen.getByText(/POST .*\/api\/v2\/developer\/applications/),
    ).toBeInTheDocument();
    const link = screen.getByTestId('metrics-empty-playground-link');
    expect(link).toHaveAttribute('href', '/developer/playground');
  });

  it('surfaces an error when usage fails', async () => {
    vi.unstubAllGlobals();
    setupFetchStub({ usage: false });
    renderPage();
    await waitFor(() => {
      expect(screen.getByText(/Failed to load usage/i)).toBeInTheDocument();
    });
  });
});
