import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { createElement } from 'react';
import { render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { SettingsPage } from '../SettingsPage';

const server = setupServer(
  http.get('/api/v2/user-preferences', () =>
    HttpResponse.json({
      userId: 'alice',
      theme: 'dark',
      language: 'en',
      notifications: {},
      hotkeys: {},
    }),
  ),
  http.get('/api/v2/build-info', () =>
    HttpResponse.json({
      version: '1.2.3',
      commit: 'abc1234',
      goVersion: 'go1.23.0',
      buildTime: '2026-05-01T08:00:00Z',
    }),
  ),
  http.get('/api/v2/build-info/features', () =>
    HttpResponse.json({
      features: [
        { name: 'aip', enabled: true },
        { name: 'vertex', enabled: false, reason: 'experimental' },
      ],
    }),
  ),
);

beforeAll(() => server.listen({ onUnhandledRequest: 'bypass' }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

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

describe('BDD: SettingsPage System / Build Info section (build-info parity)', () => {
  it('Given the server build-info, When Settings loads, Then version/commit/goVersion and feature flags render', async () => {
    renderPage();

    const section = await screen.findByTestId('settings-section-system');
    expect(within(section).getByText('1.2.3')).toBeInTheDocument();
    expect(within(section).getByText('abc1234')).toBeInTheDocument();
    expect(within(section).getByText('go1.23.0')).toBeInTheDocument();

    // Feature flags surface with their enabled state.
    const aip = within(section).getByTestId('settings-feature-aip');
    expect(aip).toHaveAttribute('data-enabled', 'true');
    const vertex = within(section).getByTestId('settings-feature-vertex');
    expect(vertex).toHaveAttribute('data-enabled', 'false');
  });

  it('Given build-info is unavailable (404), Then the section degrades without crashing the page', async () => {
    server.use(
      http.get('/api/v2/build-info', () =>
        HttpResponse.json({ errorName: 'NotFound' }, { status: 404 }),
      ),
      http.get('/api/v2/build-info/features', () =>
        HttpResponse.json({ errorName: 'NotFound' }, { status: 404 }),
      ),
    );
    renderPage();

    // The rest of the page still renders.
    await waitFor(() =>
      expect(screen.getByTestId('settings-section-theme')).toBeInTheDocument(),
    );
    // No build values are shown.
    expect(screen.queryByText('go1.23.0')).toBeNull();
  });
});
