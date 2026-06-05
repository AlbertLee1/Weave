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
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { MarketplacePage } from '../MarketplacePage';
import type { InstalledPackage } from '../../../api/packages';
import { useToastStore } from '../../../stores/toastStore';

// a11y BDD — the two self-drawn dialogs in MarketplacePage
// (UninstallConfirmDialog at role="dialog" + PackageDetailsDrawer at
// role="dialog") must mirror the focus management already shipped in
// VertexShareLinkPanel (#229): on open focus moves inside, Escape invokes
// the existing close callback, Tab/Shift+Tab cycle within the dialog (focus
// trap), and on close focus returns to the trigger element. Both dialogs
// must behave independently. These scenarios fail before the focus wiring
// lands (no focus-in, Escape does nothing, Tab escapes the dialog).

const FOCUSABLE_SELECTOR =
  'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])';

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

function useInstalledOnly() {
  server.use(
    http.get('/api/v2/pkg', () => HttpResponse.json({ data: [pkg()] })),
    http.get('/api/v2/pkg/builtin', () => HttpResponse.json({ data: [] })),
  );
}

describe('MarketplacePage dialog focus management (a11y)', () => {
  beforeEach(() => {
    useToastStore.getState().clear();
  });

  // ---- UninstallConfirmDialog ----------------------------------------

  it('Given the Uninstall dialog is opened, When it mounts, Then focus moves inside the dialog', async () => {
    useInstalledOnly();
    const user = userEvent.setup();
    renderPage();

    const trigger = await screen.findByTestId('marketplace-uninstall-northwind');
    await user.click(trigger);

    const dialog = await screen.findByTestId('marketplace-uninstall-dialog');
    await waitFor(() => {
      const active = document.activeElement as HTMLElement | null;
      expect(active).not.toBeNull();
      expect(dialog.contains(active)).toBe(true);
    });
  });

  it('Given the Uninstall dialog is open, When Escape is pressed, Then the dialog closes', async () => {
    useInstalledOnly();
    const user = userEvent.setup();
    renderPage();

    const trigger = await screen.findByTestId('marketplace-uninstall-northwind');
    await user.click(trigger);
    expect(
      await screen.findByTestId('marketplace-uninstall-dialog'),
    ).toBeInTheDocument();

    await user.keyboard('{Escape}');

    await waitFor(() => {
      expect(
        screen.queryByTestId('marketplace-uninstall-dialog'),
      ).not.toBeInTheDocument();
    });
  });

  it('Given the Uninstall dialog is open, When Shift+Tab is pressed on the first element, Then focus wraps to the last element (focus trap)', async () => {
    useInstalledOnly();
    const user = userEvent.setup();
    renderPage();

    const trigger = await screen.findByTestId('marketplace-uninstall-northwind');
    await user.click(trigger);
    const dialog = await screen.findByTestId('marketplace-uninstall-dialog');

    const focusables = Array.from(
      dialog.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR),
    );
    expect(focusables.length).toBeGreaterThan(1);
    const first = focusables[0];
    const last = focusables[focusables.length - 1];

    first.focus();
    expect(document.activeElement).toBe(first);

    await user.tab({ shift: true });
    expect(document.activeElement).toBe(last);

    // And forward Tab from the last wraps back to the first.
    last.focus();
    await user.tab();
    expect(document.activeElement).toBe(first);
  });

  it('Given the Uninstall dialog closes via Escape, Then focus returns to the trigger', async () => {
    useInstalledOnly();
    const user = userEvent.setup();
    renderPage();

    const trigger = await screen.findByTestId('marketplace-uninstall-northwind');
    trigger.focus();
    await user.click(trigger);
    await screen.findByTestId('marketplace-uninstall-dialog');

    await user.keyboard('{Escape}');

    await waitFor(() => {
      expect(
        screen.queryByTestId('marketplace-uninstall-dialog'),
      ).not.toBeInTheDocument();
    });
    expect(document.activeElement).toBe(trigger);
  });

  // ---- PackageDetailsDrawer ------------------------------------------

  it('Given the Details drawer is opened, When it mounts, Then focus moves inside the drawer', async () => {
    useInstalledOnly();
    const user = userEvent.setup();
    renderPage();

    const trigger = await screen.findByTestId('marketplace-details-northwind');
    await user.click(trigger);

    const drawer = await screen.findByTestId('marketplace-details-drawer');
    await waitFor(() => {
      const active = document.activeElement as HTMLElement | null;
      expect(active).not.toBeNull();
      expect(drawer.contains(active)).toBe(true);
    });
  });

  it('Given the Details drawer is open, When Escape is pressed, Then the drawer closes', async () => {
    useInstalledOnly();
    const user = userEvent.setup();
    renderPage();

    const trigger = await screen.findByTestId('marketplace-details-northwind');
    await user.click(trigger);
    expect(
      await screen.findByTestId('marketplace-details-drawer'),
    ).toBeInTheDocument();

    await user.keyboard('{Escape}');

    await waitFor(() => {
      expect(
        screen.queryByTestId('marketplace-details-drawer'),
      ).not.toBeInTheDocument();
    });
  });

  it('Given the Details drawer is open, When Tab cycles past the last element, Then focus wraps to the first (focus trap)', async () => {
    useInstalledOnly();
    const user = userEvent.setup();
    renderPage();

    const trigger = await screen.findByTestId('marketplace-details-northwind');
    await user.click(trigger);
    const drawer = await screen.findByTestId('marketplace-details-drawer');

    const focusables = Array.from(
      drawer.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR),
    );
    expect(focusables.length).toBeGreaterThan(0);
    const first = focusables[0];
    const last = focusables[focusables.length - 1];

    last.focus();
    expect(document.activeElement).toBe(last);

    await user.tab();
    expect(document.activeElement).toBe(first);

    // Shift+Tab from the first wraps to the last.
    first.focus();
    await user.tab({ shift: true });
    expect(document.activeElement).toBe(last);
  });

  it('Given the Details drawer closes via Escape, Then focus returns to the trigger', async () => {
    useInstalledOnly();
    const user = userEvent.setup();
    renderPage();

    const trigger = await screen.findByTestId('marketplace-details-northwind');
    trigger.focus();
    await user.click(trigger);
    await screen.findByTestId('marketplace-details-drawer');

    await user.keyboard('{Escape}');

    await waitFor(() => {
      expect(
        screen.queryByTestId('marketplace-details-drawer'),
      ).not.toBeInTheDocument();
    });
    expect(document.activeElement).toBe(trigger);
  });
});
