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
import { ReactionBar } from '../ReactionBar';

// BDD: toggling an emoji reaction must surface a user-visible error toast
// when the underlying create/delete request fails. Previously the four
// create.mutate / remove.mutate calls (togglePill ×2 + onPick ×2) fired with
// no onError, so failures were swallowed silently (zero feedback). Mirrors the
// WatchButton.errorHandling.bdd contract (#216).
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

function failPost() {
  return http.post('/api/v2/reactions', () =>
    HttpResponse.json(
      {
        errorCode: 'INTERNAL',
        errorName: 'ReactionCreateFailed',
        errorInstanceId: 'x',
        parameters: { reason: 'storage offline' },
      },
      { status: 500 },
    ),
  );
}

function failDelete() {
  return http.delete('/api/v2/reactions', () =>
    HttpResponse.json(
      {
        errorCode: 'INTERNAL',
        errorName: 'ReactionDeleteFailed',
        errorInstanceId: 'x',
        parameters: { reason: 'storage offline' },
      },
      { status: 500 },
    ),
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

function errorToasts() {
  return useToastStore.getState().toasts.filter((t) => t.severity === 'error');
}

describe('ReactionBar error handling (BDD)', () => {
  beforeEach(() => {
    server.use(meHandler());
    useToastStore.getState().clear();
  });

  it('Given a failing POST, When the user clicks a not-mine pill (togglePill create), Then an error toast surfaces', async () => {
    server.use(
      http.get('/api/v2/reactions', () =>
        HttpResponse.json({
          targetRid,
          emojis: [{ emoji: '🎉', count: 1, mine: false }],
        }),
      ),
      failPost(),
    );

    render(<ReactionBar targetRid={targetRid} />, { wrapper: makeWrapper() });
    await waitFor(() => {
      expect(screen.getByTestId('reaction-pill-🎉')).not.toBeDisabled();
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('reaction-pill-🎉'));
      await new Promise((r) => setTimeout(r, 0));
    });

    await waitFor(() => {
      expect(
        errorToasts().some((t) => t.message.includes('ReactionCreateFailed')),
      ).toBe(true);
    });
  });

  it('Given a failing DELETE, When the user clicks a mine pill (togglePill remove), Then an error toast surfaces', async () => {
    server.use(
      http.get('/api/v2/reactions', () =>
        HttpResponse.json({
          targetRid,
          emojis: [{ emoji: '👍', count: 1, mine: true }],
        }),
      ),
      failDelete(),
    );

    render(<ReactionBar targetRid={targetRid} />, { wrapper: makeWrapper() });
    await waitFor(() => {
      expect(screen.getByTestId('reaction-pill-👍')).not.toBeDisabled();
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('reaction-pill-👍'));
      await new Promise((r) => setTimeout(r, 0));
    });

    await waitFor(() => {
      expect(
        errorToasts().some((t) => t.message.includes('ReactionDeleteFailed')),
      ).toBe(true);
    });
  });

  it('Given a failing POST, When the user picks a new emoji from the picker (onPick create), Then an error toast surfaces', async () => {
    server.use(
      http.get('/api/v2/reactions', () =>
        HttpResponse.json({ targetRid, emojis: [] }),
      ),
      failPost(),
    );

    render(<ReactionBar targetRid={targetRid} />, { wrapper: makeWrapper() });
    await waitFor(() => {
      expect(screen.getByTestId('reaction-add-button')).not.toBeDisabled();
    });

    fireEvent.click(screen.getByTestId('reaction-add-button'));
    await act(async () => {
      fireEvent.click(screen.getByTestId('reaction-picker-option-🚀'));
      await new Promise((r) => setTimeout(r, 0));
    });

    await waitFor(() => {
      expect(
        errorToasts().some((t) => t.message.includes('ReactionCreateFailed')),
      ).toBe(true);
    });
  });

  it('Given a failing DELETE, When the user re-picks an existing mine emoji from the picker (onPick remove), Then an error toast surfaces', async () => {
    server.use(
      http.get('/api/v2/reactions', () =>
        HttpResponse.json({
          targetRid,
          emojis: [{ emoji: '👍', count: 1, mine: true }],
        }),
      ),
      failDelete(),
    );

    render(<ReactionBar targetRid={targetRid} />, { wrapper: makeWrapper() });
    await waitFor(() => {
      expect(screen.getByTestId('reaction-add-button')).not.toBeDisabled();
    });

    fireEvent.click(screen.getByTestId('reaction-add-button'));
    await act(async () => {
      // 👍 is in DEFAULT_EMOJIS and already mine → onPick takes the remove path.
      fireEvent.click(screen.getByTestId('reaction-picker-option-👍'));
      await new Promise((r) => setTimeout(r, 0));
    });

    await waitFor(() => {
      expect(
        errorToasts().some((t) => t.message.includes('ReactionDeleteFailed')),
      ).toBe(true);
    });
  });

  it('Given a successful POST, When the user clicks a not-mine pill, Then no error toast surfaces', async () => {
    const state = { posted: 0 };
    server.use(
      http.get('/api/v2/reactions', () =>
        HttpResponse.json({
          targetRid,
          emojis: [{ emoji: '🎉', count: 1, mine: false }],
        }),
      ),
      http.post('/api/v2/reactions', async ({ request }) => {
        const body = (await request.json()) as { emoji: string };
        state.posted++;
        return HttpResponse.json(
          {
            id: 'r1',
            userId: 'alice',
            targetRid,
            emoji: body.emoji,
            createdAt: new Date().toISOString(),
          },
          { status: 201 },
        );
      }),
    );

    render(<ReactionBar targetRid={targetRid} />, { wrapper: makeWrapper() });
    await waitFor(() => {
      expect(screen.getByTestId('reaction-pill-🎉')).not.toBeDisabled();
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('reaction-pill-🎉'));
      await new Promise((r) => setTimeout(r, 0));
    });

    await waitFor(() => {
      expect(state.posted).toBe(1);
    });
    expect(errorToasts().length).toBe(0);
  });
});
