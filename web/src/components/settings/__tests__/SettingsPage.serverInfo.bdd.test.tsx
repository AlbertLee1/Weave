import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { createElement } from 'react';
import { render, screen, within } from '@testing-library/react';
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
    HttpResponse.json({ features: [] }),
  ),
  http.get('/api/v2/server-info', () =>
    HttpResponse.json({
      startedAt: '2026-05-01T00:00:00Z',
      uptimeSeconds: 93784, // 1d 02h 03m
      goroutineCount: 42,
      memoryAllocBytes: 10485760,
      memorySysBytes: 20971520,
      gcCycles: 7,
    }),
  ),
  http.get('/api/v2/server-info/connections', () =>
    HttpResponse.json({
      postgres: {
        acquiredConns: 2,
        idleConns: 3,
        totalConns: 5,
        maxConns: 10,
        newConnsCount: 12,
      },
      nats: {
        status: 'connected',
        serverUrl: 'nats://localhost:4222',
        inMsgs: 100,
        outMsgs: 90,
        reconnects: 0,
      },
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

describe('BDD: SettingsPage System runtime status (server-info parity)', () => {
  it('Given server-info + connections, Then uptime, NATS status and PG pool render', async () => {
    renderPage();

    const section = await screen.findByTestId('settings-section-system');
    // Uptime is humanised from seconds.
    expect(within(section).getByTestId('server-uptime')).toHaveTextContent(/1d/);
    // NATS connection status surfaces.
    const nats = within(section).getByTestId('server-nats-status');
    expect(nats).toHaveAttribute('data-status', 'connected');
    // PG pool usage surfaces (acquired / max).
    expect(within(section).getByTestId('server-pg-pool')).toHaveTextContent('5');
  });

  it('Given server-info is unavailable (404), Then runtime rows degrade but build-info still shows', async () => {
    server.use(
      http.get('/api/v2/server-info', () =>
        HttpResponse.json({ errorName: 'NotFound' }, { status: 404 }),
      ),
      http.get('/api/v2/server-info/connections', () =>
        HttpResponse.json({ errorName: 'NotFound' }, { status: 404 }),
      ),
    );
    renderPage();

    const section = await screen.findByTestId('settings-section-system');
    expect(within(section).getByText('1.2.3')).toBeInTheDocument();
    expect(within(section).queryByTestId('server-uptime')).toBeNull();
  });
});
