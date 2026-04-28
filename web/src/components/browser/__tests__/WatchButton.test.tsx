import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { createElement } from 'react';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AuthProvider } from '../../../auth/AuthContext';
import { WatchButton } from '../WatchButton';

const server = setupServer();
beforeAll(() => server.listen({ onUnhandledRequest: 'bypass' }));
afterEach(() => {
  server.resetHandlers();
  vi.clearAllMocks();
});
afterAll(() => server.close());

const targetRid = 'ri.ontology.main.object.emp1';

function meHandler() {
  return http.get('/api/v2/me', () =>
    HttpResponse.json({
      id: 'alice',
      email: 'alice@example.test',
      name: 'alice',
      roles: ['viewer'],
      ontologyRoles: {},
      permissions: ['object.read'],
    }),
  );
}

function makeWrapper() {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
  return ({ children }: { children: React.ReactNode }) =>
    createElement(
      QueryClientProvider,
      { client },
      createElement(AuthProvider, null, children),
    );
}

describe('WatchButton (US-337)', () => {
  beforeEach(() => {
    server.use(meHandler());
  });

  it('returns nothing when no targetRid is supplied', () => {
    const { container } = render(<WatchButton targetRid={null} />, {
      wrapper: makeWrapper(),
    });
    expect(container.querySelector('[data-testid="watch-button"]')).toBeNull();
  });

  it('hides itself when the watches endpoint is unavailable (degraded mode)', async () => {
    server.use(
      http.get('/api/v2/watches/status', () =>
        HttpResponse.json(
          { errorCode: 'NOT_FOUND', errorName: 'WatchesUnavailable' },
          { status: 404 },
        ),
      ),
    );
    const { container } = render(<WatchButton targetRid={targetRid} />, {
      wrapper: makeWrapper(),
    });
    await waitFor(() => {
      expect(container.querySelector('[data-testid="watch-button"]')).toBeNull();
    });
  });

  it('renders Watch when not currently watching, then POSTs on click', async () => {
    const state = { watching: false, posted: 0 };
    server.use(
      http.get('/api/v2/watches/status', () =>
        HttpResponse.json({ targetRid, watching: state.watching }),
      ),
      http.post('/api/v2/watches', async () => {
        state.watching = true;
        state.posted++;
        return HttpResponse.json(
          { id: 'w1', userId: 'alice', targetRid, createdAt: new Date().toISOString() },
          { status: 201 },
        );
      }),
    );

    render(<WatchButton targetRid={targetRid} />, { wrapper: makeWrapper() });
    await waitFor(() => {
      expect(screen.getByTestId('watch-button')).not.toBeDisabled();
    });
    expect(screen.getByTestId('watch-button').getAttribute('data-watching')).toBe('false');
    expect(screen.getByTestId('watch-button')).toHaveTextContent(/Watch$/);

    await act(async () => {
      fireEvent.click(screen.getByTestId('watch-button'));
      await new Promise((r) => setTimeout(r, 0));
    });

    await waitFor(() => {
      expect(state.posted).toBe(1);
    });
    await waitFor(() => {
      expect(screen.getByTestId('watch-button').getAttribute('data-watching')).toBe('true');
    });
    expect(screen.getByTestId('watch-button')).toHaveTextContent(/Watching/);
  });

  it('renders Watching when already followed, then DELETEs on click', async () => {
    const state = { watching: true, deleted: 0 };
    server.use(
      http.get('/api/v2/watches/status', () =>
        HttpResponse.json({ targetRid, watching: state.watching }),
      ),
      http.delete('/api/v2/watches', () => {
        state.watching = false;
        state.deleted++;
        return new HttpResponse(null, { status: 204 });
      }),
    );

    render(<WatchButton targetRid={targetRid} />, { wrapper: makeWrapper() });
    await waitFor(() => {
      expect(screen.getByTestId('watch-button').getAttribute('data-watching')).toBe('true');
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('watch-button'));
      await new Promise((r) => setTimeout(r, 0));
    });

    await waitFor(() => {
      expect(state.deleted).toBe(1);
    });
    await waitFor(() => {
      expect(screen.getByTestId('watch-button').getAttribute('data-watching')).toBe('false');
    });
    expect(screen.getByTestId('watch-button')).toHaveTextContent(/Watch$/);
  });
});
