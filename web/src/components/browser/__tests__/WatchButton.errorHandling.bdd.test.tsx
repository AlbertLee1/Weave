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
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AuthProvider } from '../../../auth/AuthContext';
import { useToastStore } from '../../../stores/toastStore';
import { WatchButton } from '../WatchButton';

// BDD: toggling watch must surface a user-visible error toast when the
// underlying create/delete request fails. Previously the mutation fired
// with no onError, so failures were swallowed silently (zero feedback).
const server = setupServer();
beforeAll(() => server.listen({ onUnhandledRequest: 'bypass' }));
afterEach(() => {
  server.resetHandlers();
  vi.clearAllMocks();
  useToastStore.getState().clear();
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

describe('WatchButton error handling (BDD)', () => {
  beforeEach(() => {
    server.use(meHandler());
    useToastStore.getState().clear();
  });

  it('Given a failing create request, When the user clicks Watch, Then an error toast surfaces', async () => {
    server.use(
      http.get('/api/v2/watches/status', () =>
        HttpResponse.json({ targetRid, watching: false }),
      ),
      http.post('/api/v2/watches', () =>
        HttpResponse.json(
          {
            errorCode: 'INTERNAL',
            errorName: 'WatchCreateFailed',
            errorInstanceId: 'x',
            parameters: { reason: 'storage offline' },
          },
          { status: 500 },
        ),
      ),
    );

    render(<WatchButton targetRid={targetRid} />, { wrapper: makeWrapper() });
    await waitFor(() => {
      expect(screen.getByTestId('watch-button')).not.toBeDisabled();
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('watch-button'));
      await new Promise((r) => setTimeout(r, 0));
    });

    await waitFor(() => {
      const toasts = useToastStore.getState().toasts;
      expect(
        toasts.some(
          (t) =>
            t.severity === 'error' && t.message.includes('WatchCreateFailed'),
        ),
      ).toBe(true);
    });
  });

  it('Given a failing delete request, When the user clicks Watching, Then an error toast surfaces', async () => {
    server.use(
      http.get('/api/v2/watches/status', () =>
        HttpResponse.json({ targetRid, watching: true }),
      ),
      http.delete('/api/v2/watches', () =>
        HttpResponse.json(
          {
            errorCode: 'INTERNAL',
            errorName: 'WatchDeleteFailed',
            errorInstanceId: 'x',
            parameters: { reason: 'storage offline' },
          },
          { status: 500 },
        ),
      ),
    );

    render(<WatchButton targetRid={targetRid} />, { wrapper: makeWrapper() });
    await waitFor(() => {
      expect(
        screen.getByTestId('watch-button').getAttribute('data-watching'),
      ).toBe('true');
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('watch-button'));
      await new Promise((r) => setTimeout(r, 0));
    });

    await waitFor(() => {
      const toasts = useToastStore.getState().toasts;
      expect(
        toasts.some(
          (t) =>
            t.severity === 'error' && t.message.includes('WatchDeleteFailed'),
        ),
      ).toBe(true);
    });
  });

  it('Given a successful create request, When the user clicks Watch, Then no error toast surfaces and state flips to watching', async () => {
    const state = { watching: false };
    server.use(
      http.get('/api/v2/watches/status', () =>
        HttpResponse.json({ targetRid, watching: state.watching }),
      ),
      http.post('/api/v2/watches', () => {
        state.watching = true;
        return HttpResponse.json(
          {
            id: 'w1',
            userId: 'alice',
            targetRid,
            createdAt: new Date().toISOString(),
          },
          { status: 201 },
        );
      }),
    );

    render(<WatchButton targetRid={targetRid} />, { wrapper: makeWrapper() });
    await waitFor(() => {
      expect(screen.getByTestId('watch-button')).not.toBeDisabled();
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('watch-button'));
      await new Promise((r) => setTimeout(r, 0));
    });

    await waitFor(() => {
      expect(
        screen.getByTestId('watch-button').getAttribute('data-watching'),
      ).toBe('true');
    });

    const toasts = useToastStore.getState().toasts;
    expect(toasts.some((t) => t.severity === 'error')).toBe(false);
  });
});
