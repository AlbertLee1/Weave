import {
  describe,
  it,
  expect,
  vi,
  beforeAll,
  afterAll,
  beforeEach,
  afterEach,
} from 'vitest';
import { createElement } from 'react';
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { MarketplacePage } from '../MarketplacePage';
import type { InstalledPackage } from '../../../api/packages';
import { useToastStore } from '../../../stores/toastStore';

const server = setupServer();
beforeAll(() => server.listen({ onUnhandledRequest: 'bypass' }));
afterEach(() => {
  server.resetHandlers();
  vi.clearAllMocks();
  useToastStore.getState().clear();
});
afterAll(() => server.close());

function pkg(overrides: Partial<InstalledPackage> = {}): InstalledPackage {
  return {
    id: 1,
    name: 'northwind',
    version: '1.0.0',
    ontology: 'northwind',
    manifest: {
      name: 'northwind',
      version: '1.0.0',
      author: 'Weave Team',
      license: 'MIT',
      description: 'Northwind sample ontology',
      dependencies: { core: '^1.0.0' },
    },
    migrations: ['000001_init.up.sql'],
    enabled: true,
    installedBy: 'alice',
    installedAt: '2026-05-01T00:00:00Z',
    updatedAt: '2026-05-01T00:00:00Z',
    ...overrides,
  };
}

function listHandler(packages: InstalledPackage[]) {
  return http.get('/api/v2/pkg', () => HttpResponse.json({ data: packages }));
}

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(
    createElement(
      QueryClientProvider,
      { client: qc },
      createElement(
        MemoryRouter,
        { initialEntries: ['/marketplace'] },
        createElement(MarketplacePage, null),
      ),
    ),
  );
}

describe('MarketplacePage (US-413)', () => {
  beforeEach(() => {
    useToastStore.getState().clear();
  });

  it('renders the empty state when no packages are installed', async () => {
    server.use(listHandler([]));
    renderPage();
    await waitFor(() => {
      expect(
        screen.getByText('No packages installed'),
      ).toBeInTheDocument();
    });
    expect(screen.queryByTestId('marketplace-list')).not.toBeInTheDocument();
  });

  it('lists installed packages with version, dependencies, and status badge', async () => {
    server.use(listHandler([pkg()]));
    renderPage();
    await waitFor(() => {
      expect(screen.getByTestId('marketplace-list')).toBeInTheDocument();
    });
    const card = screen.getByTestId('marketplace-card-northwind');
    expect(card).toHaveAttribute('data-enabled', 'true');
    expect(card.textContent).toContain('northwind');
    expect(card.textContent).toContain('v1.0.0');
    expect(card.textContent).toContain('Northwind sample ontology');
    expect(screen.getByTestId('marketplace-status-northwind').textContent).toBe(
      'Enabled',
    );
    const deps = screen.getByTestId('marketplace-deps-northwind');
    expect(deps.textContent).toContain('core@^1.0.0');
  });

  it('toggles enabled flag via the per-row checkbox and pushes a success toast', async () => {
    const enableCalls: Array<{ name: string; body: { enabled: boolean } }> = [];
    server.use(
      listHandler([pkg()]),
      http.post('/api/v2/pkg/:name/enabled', async ({ params, request }) => {
        const body = (await request.json()) as { enabled: boolean };
        enableCalls.push({ name: String(params.name), body });
        return HttpResponse.json({ name: String(params.name), enabled: body.enabled });
      }),
    );
    renderPage();
    await waitFor(() => {
      expect(screen.getByTestId('marketplace-toggle-northwind')).toBeInTheDocument();
    });

    const toggle = screen.getByTestId(
      'marketplace-toggle-northwind',
    ) as HTMLInputElement;
    expect(toggle.checked).toBe(true);
    await act(async () => {
      fireEvent.click(toggle);
    });

    await waitFor(() => {
      expect(enableCalls).toHaveLength(1);
    });
    expect(enableCalls[0]).toEqual({
      name: 'northwind',
      body: { enabled: false },
    });
    await waitFor(() => {
      const toasts = useToastStore.getState().toasts;
      expect(toasts.some((t) => t.message.includes('disabled'))).toBe(true);
    });
  });

  it('uninstalls a package after the confirmation dialog and surfaces a toast', async () => {
    let firstCall = true;
    const deleteCalls: string[] = [];
    server.use(
      http.get('/api/v2/pkg', () => {
        if (firstCall) {
          firstCall = false;
          return HttpResponse.json({ data: [pkg()] });
        }
        return HttpResponse.json({ data: [] });
      }),
      http.delete('/api/v2/pkg/:name', ({ params }) => {
        deleteCalls.push(String(params.name));
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderPage();
    await waitFor(() => {
      expect(
        screen.getByTestId('marketplace-uninstall-northwind'),
      ).toBeInTheDocument();
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('marketplace-uninstall-northwind'));
    });

    expect(
      screen.getByTestId('marketplace-uninstall-dialog'),
    ).toBeInTheDocument();

    await act(async () => {
      fireEvent.click(screen.getByTestId('marketplace-uninstall-confirm'));
    });

    await waitFor(() => {
      expect(deleteCalls).toEqual(['northwind']);
    });

    await waitFor(() => {
      expect(
        screen.queryByTestId('marketplace-uninstall-dialog'),
      ).not.toBeInTheDocument();
    });

    await waitFor(() => {
      const toasts = useToastStore.getState().toasts;
      expect(toasts.some((t) => t.message.includes('uninstalled'))).toBe(true);
    });
  });

  it('cancels uninstall without firing the delete', async () => {
    const deleteCalls: string[] = [];
    server.use(
      listHandler([pkg()]),
      http.delete('/api/v2/pkg/:name', ({ params }) => {
        deleteCalls.push(String(params.name));
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderPage();
    await waitFor(() => {
      expect(
        screen.getByTestId('marketplace-uninstall-northwind'),
      ).toBeInTheDocument();
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId('marketplace-uninstall-northwind'));
    });
    expect(
      screen.getByTestId('marketplace-uninstall-dialog'),
    ).toBeInTheDocument();
    await act(async () => {
      fireEvent.click(screen.getByTestId('marketplace-uninstall-cancel'));
    });
    expect(
      screen.queryByTestId('marketplace-uninstall-dialog'),
    ).not.toBeInTheDocument();
    expect(deleteCalls).toEqual([]);
  });

  it('renders an error message when the list endpoint fails', async () => {
    server.use(
      http.get('/api/v2/pkg', () =>
        HttpResponse.json(
          {
            errorCode: 'INTERNAL',
            errorName: 'ListInstalledPackagesFailed',
            errorInstanceId: 'x',
          },
          { status: 500 },
        ),
      ),
    );
    renderPage();
    await waitFor(() => {
      expect(screen.getByTestId('marketplace-error')).toBeInTheDocument();
    });
  });

  it('renders a disabled package with the correct badge and toggle state', async () => {
    server.use(listHandler([pkg({ enabled: false })]));
    renderPage();
    await waitFor(() => {
      expect(screen.getByTestId('marketplace-card-northwind')).toBeInTheDocument();
    });
    expect(
      screen.getByTestId('marketplace-card-northwind'),
    ).toHaveAttribute('data-enabled', 'false');
    expect(
      screen.getByTestId('marketplace-status-northwind').textContent,
    ).toBe('Disabled');
    const toggle = screen.getByTestId(
      'marketplace-toggle-northwind',
    ) as HTMLInputElement;
    expect(toggle.checked).toBe(false);
  });
});
