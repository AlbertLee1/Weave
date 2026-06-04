import { describe, it, expect, beforeAll, afterAll, afterEach, vi } from 'vitest';
import { createElement } from 'react';
import { render, screen, within, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { SettingsPage } from '../SettingsPage';

// Two sessions: one is the caller's current device, one is a remote login.
const INITIAL_SESSIONS = [
  {
    id: 'sess-current',
    ip: '10.0.0.1',
    user_agent: 'Mozilla/5.0 (Macintosh)',
    created_at: '2026-05-01T08:00:00Z',
    last_seen: '2026-06-03T12:00:00Z',
    current: true,
  },
  {
    id: 'sess-remote',
    ip: '203.0.113.7',
    user_agent: 'Mozilla/5.0 (Windows NT 10.0)',
    created_at: '2026-05-20T09:30:00Z',
    last_seen: '2026-06-02T18:45:00Z',
  },
];

// Server-side session state so DELETE / revoke-others mutate the list the
// next GET returns — exercising the SPA's invalidate-and-refetch loop.
let sessions = [...INITIAL_SESSIONS];

// Spies so each scenario can assert the exact HTTP method/path the SPA fired.
const deleteSpy = vi.fn();
const revokeOthersSpy = vi.fn();

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
  // Diagnostic surfaces are not the subject here; 404 so the System section
  // simply degrades (mirrors the serverInfo BDD test's degraded path).
  http.get('/api/v2/build-info', () =>
    HttpResponse.json({ errorName: 'NotFound' }, { status: 404 }),
  ),
  http.get('/api/v2/build-info/features', () =>
    HttpResponse.json({ features: [] }),
  ),
  http.get('/api/v2/server-info', () =>
    HttpResponse.json({ errorName: 'NotFound' }, { status: 404 }),
  ),
  http.get('/api/v2/server-info/connections', () =>
    HttpResponse.json({ errorName: 'NotFound' }, { status: 404 }),
  ),
  http.get('/api/auth/sessions', () =>
    HttpResponse.json({ sessions }),
  ),
  http.delete('/api/auth/sessions/:sessionID', ({ params }) => {
    deleteSpy(params.sessionID);
    sessions = sessions.filter((s) => s.id !== params.sessionID);
    return new HttpResponse(null, { status: 204 });
  }),
  http.post('/api/auth/sessions/revoke-others', () => {
    revokeOthersSpy();
    sessions = sessions.filter((s) => s.current);
    return HttpResponse.json({ revoked: 1, currentSessionId: 'sess-current' });
  }),
);

beforeAll(() => server.listen({ onUnhandledRequest: 'bypass' }));
afterEach(() => {
  server.resetHandlers();
  sessions = [...INITIAL_SESSIONS];
  deleteSpy.mockClear();
  revokeOthersSpy.mockClear();
});
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

describe('BDD: SettingsPage active session management', () => {
  it('Given the Settings page is open, Then active sessions are listed with the current device marked', async () => {
    renderPage();

    const section = await screen.findByTestId('settings-section-sessions');
    // Both sessions render as rows.
    const current = within(section).getByTestId('session-row-sess-current');
    const remote = within(section).getByTestId('session-row-sess-remote');

    // The remote login surfaces its IP + user agent.
    expect(within(remote).getByText(/203\.0\.113\.7/)).toBeInTheDocument();
    expect(within(remote).getByText(/Windows NT 10\.0/)).toBeInTheDocument();

    // Only the current device carries the "current" marker.
    expect(within(current).getByTestId('session-current-badge')).toBeInTheDocument();
    expect(within(remote).queryByTestId('session-current-badge')).toBeNull();
  });

  it('When Revoke is clicked on a row, Then a DELETE is sent and the row disappears', async () => {
    const user = userEvent.setup();
    renderPage();

    const section = await screen.findByTestId('settings-section-sessions');
    const remote = within(section).getByTestId('session-row-sess-remote');

    await user.click(within(remote).getByTestId('session-revoke-sess-remote'));

    // DELETE fired against the exact session id.
    await waitFor(() => expect(deleteSpy).toHaveBeenCalledWith('sess-remote'));

    // The revoked row is gone from the list.
    await waitFor(() =>
      expect(
        within(section).queryByTestId('session-row-sess-remote'),
      ).toBeNull(),
    );
    // The current device is untouched.
    expect(within(section).getByTestId('session-row-sess-current')).toBeInTheDocument();
  });

  it('When "log out other devices" is clicked, Then revoke-others is POSTed', async () => {
    const user = userEvent.setup();
    renderPage();

    const section = await screen.findByTestId('settings-section-sessions');
    await user.click(within(section).getByTestId('session-revoke-others'));

    await waitFor(() => expect(revokeOthersSpy).toHaveBeenCalledTimes(1));
  });
});
