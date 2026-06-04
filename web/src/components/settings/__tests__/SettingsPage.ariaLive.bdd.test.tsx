import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from 'vitest';
import { createElement } from 'react';
import { render, screen, waitFor, act } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse, delay } from 'msw';
import { setupServer } from 'msw/node';
import { SettingsPage } from '../SettingsPage';

// BDD: WAI-ARIA live regions on the Settings page.
//
// Screen-reader users must be told when the page finishes loading and
// when a preference change has been persisted. Both transient banners
// carry role="status"; without aria-live the assistive tech never
// announces them. The reference pattern in the repo (Skeleton.tsx,
// OfflineIndicator.tsx) pairs role="status" with aria-live="polite".

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

describe('SettingsPage aria-live regions (a11y)', () => {
  it('Given the preferences are still loading, Then the loading status announces politely', async () => {
    // GET hangs so isLoading stays true and the loading banner renders.
    // While pending, the loading banner is the only role="status" region
    // (sessions / build-info / server-info queries are likewise pending,
    // and savedFlash is false).
    server.use(
      http.get('/api/v2/user-preferences', async () => {
        await delay('infinite');
        return HttpResponse.json({});
      }),
    );
    renderPage();

    const region = await screen.findByRole('status');
    expect(region.getAttribute('aria-live')).toBe('polite');
  });

  it('Given a preference is saved, Then the saved flash announces politely', async () => {
    server.use(
      getHandler({
        userId: 'alice',
        theme: '',
        language: '',
        notifications: {},
        hotkeys: {},
      }),
    );
    server.use(
      http.put('/api/v2/user-preferences', () =>
        HttpResponse.json({
          userId: 'alice',
          theme: 'dark',
          language: '',
          notifications: {},
          hotkeys: {},
        }),
      ),
    );
    renderPage();

    await waitFor(() => {
      expect(screen.getByTestId('settings-theme-dark')).toBeInTheDocument();
    });
    await act(async () => {
      screen.getByTestId('settings-theme-dark').click();
    });

    const flash = await screen.findByTestId('settings-saved-flash');
    expect(flash.getAttribute('role')).toBe('status');
    expect(flash.getAttribute('aria-live')).toBe('polite');
  });
});
