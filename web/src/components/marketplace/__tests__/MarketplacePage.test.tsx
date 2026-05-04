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
import type {
  BuiltinPackageMetadata,
  InstalledPackage,
} from '../../../api/packages';
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

function builtinListHandler(rows: BuiltinPackageMetadata[]) {
  return http.get('/api/v2/pkg/builtin', () =>
    HttpResponse.json({ data: rows }),
  );
}

function builtin(
  overrides: Partial<BuiltinPackageMetadata> = {},
): BuiltinPackageMetadata {
  return {
    slug: 'northwind',
    name: 'northwind',
    version: '1.0.0',
    ontologyApiName: 'northwind',
    author: 'Weave Examples',
    license: 'MIT',
    description: 'Classic sales-ledger ontology.',
    minWeaveVersion: '0.42.0',
    objectTypeCount: 3,
    linkTypeCount: 1,
    actionTypeCount: 1,
    functionCount: 0,
    migrationCount: 0,
    ...overrides,
  };
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

describe('MarketplacePage built-in catalog (US-414)', () => {
  beforeEach(() => {
    useToastStore.getState().clear();
  });

  it('renders the three built-in example packages on the Built-in tab', async () => {
    server.use(
      listHandler([]),
      builtinListHandler([
        builtin({ slug: 'chinook', name: 'chinook', ontologyApiName: 'chinook' }),
        builtin({ slug: 'iot-demo', name: 'iot-demo', ontologyApiName: 'iotDemo' }),
        builtin({ slug: 'northwind', name: 'northwind' }),
      ]),
    );
    renderPage();
    // Switch to the Built-in tab.
    await waitFor(() => {
      expect(screen.getByTestId('marketplace-tab-builtin')).toBeInTheDocument();
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId('marketplace-tab-builtin'));
    });
    await waitFor(() => {
      expect(
        screen.getByTestId('marketplace-builtin-list'),
      ).toBeInTheDocument();
    });
    expect(
      screen.getByTestId('marketplace-builtin-card-chinook'),
    ).toBeInTheDocument();
    expect(
      screen.getByTestId('marketplace-builtin-card-iot-demo'),
    ).toBeInTheDocument();
    expect(
      screen.getByTestId('marketplace-builtin-card-northwind'),
    ).toBeInTheDocument();
  });

  it('one-click installs a built-in package and surfaces a success toast', async () => {
    const installCalls: string[] = [];
    let installedListCalls = 0;
    server.use(
      http.get('/api/v2/pkg', () => {
        installedListCalls++;
        // Return empty until install fires; afterwards return the imported row.
        if (installedListCalls === 1) {
          return HttpResponse.json({ data: [] });
        }
        return HttpResponse.json({
          data: [
            pkg({
              name: 'northwind',
              version: '1.0.0',
              ontology: 'northwind',
              manifest: { name: 'northwind', version: '1.0.0' },
              migrations: [],
            }),
          ],
        });
      }),
      builtinListHandler([builtin()]),
      http.post('/api/v2/pkg/builtin/:slug/install', ({ params }) => {
        installCalls.push(String(params.slug));
        return HttpResponse.json(
          {
            name: 'northwind',
            version: '1.0.0',
            ontology: 'northwind',
            imported: { objectTypes: 3, linkTypes: 1, actionTypes: 1 },
            migrationsRan: 0,
            migrationsTotal: 0,
            message: 'package installed',
          },
          { status: 201 },
        );
      }),
    );

    renderPage();
    await act(async () => {
      fireEvent.click(screen.getByTestId('marketplace-tab-builtin'));
    });
    await waitFor(() => {
      expect(
        screen.getByTestId('marketplace-builtin-install-northwind'),
      ).toBeInTheDocument();
    });

    await act(async () => {
      fireEvent.click(
        screen.getByTestId('marketplace-builtin-install-northwind'),
      );
    });

    await waitFor(() => {
      expect(installCalls).toEqual(['northwind']);
    });

    // After install we hop back to the Installed tab.
    await waitFor(() => {
      expect(
        screen.getByTestId('marketplace-tab-installed'),
      ).toHaveAttribute('data-active', 'true');
    });

    await waitFor(() => {
      const toasts = useToastStore.getState().toasts;
      expect(
        toasts.some((t) =>
          t.message.toLowerCase().includes('installed'),
        ),
      ).toBe(true);
    });
  });

  it('disables the Install button for built-in packages already present in the registry', async () => {
    server.use(
      listHandler([
        pkg({
          name: 'northwind',
          version: '1.0.0',
          ontology: 'northwind',
          manifest: { name: 'northwind', version: '1.0.0' },
          migrations: [],
        }),
      ]),
      builtinListHandler([builtin()]),
    );
    renderPage();
    await act(async () => {
      fireEvent.click(screen.getByTestId('marketplace-tab-builtin'));
    });
    await waitFor(() => {
      expect(
        screen.getByTestId('marketplace-builtin-install-northwind'),
      ).toBeInTheDocument();
    });
    const btn = screen.getByTestId(
      'marketplace-builtin-install-northwind',
    ) as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
    expect(btn.textContent).toContain('Installed');
    expect(
      screen.getByTestId('marketplace-builtin-already-northwind'),
    ).toBeInTheDocument();
  });

  it('surfaces a toast on install failure', async () => {
    server.use(
      listHandler([]),
      builtinListHandler([builtin()]),
      http.post('/api/v2/pkg/builtin/:slug/install', () =>
        HttpResponse.json(
          {
            errorCode: 'CONFLICT',
            errorName: 'PackageConflict',
            errorInstanceId: 'x',
            parameters: {
              package: 'northwind',
              version: '1.0.0',
              conflicts: '[]',
            },
          },
          { status: 409 },
        ),
      ),
    );
    renderPage();
    await act(async () => {
      fireEvent.click(screen.getByTestId('marketplace-tab-builtin'));
    });
    await waitFor(() => {
      expect(
        screen.getByTestId('marketplace-builtin-install-northwind'),
      ).toBeInTheDocument();
    });
    await act(async () => {
      fireEvent.click(
        screen.getByTestId('marketplace-builtin-install-northwind'),
      );
    });
    await waitFor(() => {
      const toasts = useToastStore.getState().toasts;
      expect(
        toasts.some(
          (t) => t.severity === 'error' && t.message.includes('northwind'),
        ),
      ).toBe(true);
    });
  });

  it('renders the empty state when no built-in packages are embedded', async () => {
    server.use(listHandler([]), builtinListHandler([]));
    renderPage();
    await act(async () => {
      fireEvent.click(screen.getByTestId('marketplace-tab-builtin'));
    });
    await waitFor(() => {
      expect(screen.getByText('No built-in packages')).toBeInTheDocument();
    });
  });
});
