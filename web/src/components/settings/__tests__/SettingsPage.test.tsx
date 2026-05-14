import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from 'vitest';
import { createElement } from 'react';
import { render, screen, waitFor, act } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { SettingsPage } from '../SettingsPage';

const server = setupServer();
beforeAll(() => server.listen({ onUnhandledRequest: 'bypass' }));
afterEach(() => {
  server.resetHandlers();
  vi.clearAllMocks();
});
afterAll(() => server.close());

interface MockPrefs {
  userId: string;
  theme: string;
  language: string;
  notifications: Record<string, unknown>;
  hotkeys: Record<string, unknown>;
}

function getHandler(prefs: MockPrefs) {
  return http.get('/api/v2/user-preferences', () => HttpResponse.json(prefs));
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
        { initialEntries: ['/settings'] },
        createElement(SettingsPage, null),
      ),
    ),
  );
}

describe('SettingsPage (US-350)', () => {
  it('renders the four sections (theme / language / notifications / hotkeys)', async () => {
    server.use(
      getHandler({
        userId: 'alice',
        theme: 'dark',
        language: 'en',
        notifications: {},
        hotkeys: {},
      }),
    );
    renderPage();
    await waitFor(() => {
      expect(screen.getByTestId('settings-section-theme')).toBeInTheDocument();
    });
    expect(screen.getByTestId('settings-section-language')).toBeInTheDocument();
    expect(
      screen.getByTestId('settings-section-notifications'),
    ).toBeInTheDocument();
    expect(screen.getByTestId('settings-section-hotkeys')).toBeInTheDocument();
  });

  it('reflects the persisted theme as the active radio after the GET resolves', async () => {
    server.use(
      getHandler({
        userId: 'alice',
        theme: 'light',
        language: 'en',
        notifications: {},
        hotkeys: {},
      }),
    );
    renderPage();
    await waitFor(() => {
      expect(screen.getByTestId('settings-theme-light').getAttribute('aria-checked')).toBe('true');
    });
    expect(screen.getByTestId('settings-theme-dark').getAttribute('aria-checked')).toBe('false');
  });

  it('PUTs to /api/v2/user-preferences when the user changes theme', async () => {
    server.use(
      getHandler({
        userId: 'alice',
        theme: '',
        language: '',
        notifications: {},
        hotkeys: {},
      }),
    );
    let received: unknown = null;
    server.use(
      http.put('/api/v2/user-preferences', async ({ request }) => {
        received = await request.json();
        return HttpResponse.json({
          userId: 'alice',
          theme: 'dark',
          language: '',
          notifications: {},
          hotkeys: {},
        });
      }),
    );
    renderPage();
    await waitFor(() => {
      expect(screen.getByTestId('settings-theme-dark')).toBeInTheDocument();
    });
    await act(async () => {
      screen.getByTestId('settings-theme-dark').click();
    });
    await waitFor(() => {
      expect(received).toEqual({ theme: 'dark' });
    });
  });

  it('falls back to a degraded-mode banner when the GET 404s', async () => {
    server.use(
      http.get('/api/v2/user-preferences', () =>
        HttpResponse.json(
          {
            errorCode: 'NOT_FOUND',
            errorName: 'UserPreferencesUnavailable',
            errorInstanceId: 'x',
          },
          { status: 404 },
        ),
      ),
    );
    renderPage();
    await waitFor(() => {
      expect(
        screen.getByTestId('settings-unavailable-banner'),
      ).toBeInTheDocument();
    });
    // Sections still render so the controls remain interactive against
    // localStorage / OS defaults.
    expect(screen.getByTestId('settings-section-theme')).toBeInTheDocument();
  });

  it('uses high-contrast text on unselected theme radios (no text-text-secondary)', async () => {
    server.use(
      getHandler({
        userId: 'alice',
        theme: 'light',
        language: 'en',
        notifications: {},
        hotkeys: {},
      }),
    );
    renderPage();
    // Wait until the persisted theme=light has propagated so we know
    // `dark` is genuinely an unselected radio (not the local default).
    await waitFor(() => {
      expect(
        screen.getByTestId('settings-theme-light').getAttribute('aria-checked'),
      ).toBe('true');
    });
    const darkBtn = screen.getByTestId('settings-theme-dark');
    expect(darkBtn.getAttribute('aria-checked')).toBe('false');
    // text-text-secondary is illegible against the dark surface used in
    // the BG_SECONDARY container — the page must use a higher-contrast
    // foreground for inactive options.
    expect(darkBtn.className).not.toMatch(/text-text-secondary/);
  });

  it('lists every hotkey from the registry inside the hotkeys section', async () => {
    server.use(
      getHandler({
        userId: 'alice',
        theme: '',
        language: '',
        notifications: {},
        hotkeys: {},
      }),
    );
    renderPage();
    await waitFor(() => {
      expect(
        screen.getByTestId('settings-hotkey-commandPalette'),
      ).toBeInTheDocument();
    });
    expect(
      screen.getByTestId('settings-hotkey-showHelp'),
    ).toBeInTheDocument();
    expect(
      screen.getByTestId('settings-hotkey-goDashboard'),
    ).toBeInTheDocument();
  });
});
