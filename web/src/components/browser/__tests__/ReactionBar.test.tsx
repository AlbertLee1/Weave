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
import { ReactionBar } from '../ReactionBar';

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

describe('ReactionBar (US-342)', () => {
  beforeEach(() => {
    server.use(meHandler());
  });

  it('renders nothing when no targetRid is supplied', () => {
    const { container } = render(<ReactionBar targetRid={null} />, {
      wrapper: makeWrapper(),
    });
    expect(
      container.querySelector('[data-testid="reaction-bar"]'),
    ).toBeNull();
  });

  it('hides itself when the reactions endpoint is unavailable (degraded mode)', async () => {
    server.use(
      http.get('/api/v2/reactions', () =>
        HttpResponse.json(
          { errorCode: 'NOT_FOUND', errorName: 'ReactionsUnavailable' },
          { status: 404 },
        ),
      ),
    );
    const { container } = render(<ReactionBar targetRid={targetRid} />, {
      wrapper: makeWrapper(),
    });
    await waitFor(() => {
      expect(
        container.querySelector('[data-testid="reaction-bar"]'),
      ).toBeNull();
    });
  });

  it('renders aggregate counts and pressed state for the caller', async () => {
    server.use(
      http.get('/api/v2/reactions', () =>
        HttpResponse.json({
          targetRid,
          emojis: [
            { emoji: '👍', count: 3, mine: true },
            { emoji: '🎉', count: 1, mine: false },
          ],
        }),
      ),
    );
    render(<ReactionBar targetRid={targetRid} />, { wrapper: makeWrapper() });
    await waitFor(() => {
      expect(screen.getByTestId('reaction-pill-👍')).toBeInTheDocument();
    });
    expect(screen.getByTestId('reaction-count-👍')).toHaveTextContent('3');
    expect(
      screen.getByTestId('reaction-pill-👍').getAttribute('data-mine'),
    ).toBe('true');
    expect(screen.getByTestId('reaction-count-🎉')).toHaveTextContent('1');
    expect(
      screen.getByTestId('reaction-pill-🎉').getAttribute('data-mine'),
    ).toBe('false');
  });

  it('clicking a not-mine pill POSTs the reaction', async () => {
    const state = { posted: 0, lastEmoji: '' };
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
        state.lastEmoji = body.emoji;
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
    expect(state.lastEmoji).toBe('🎉');
  });

  it('clicking a mine=true pill DELETEs the reaction', async () => {
    const state = { deleted: 0, lastEmoji: '' };
    server.use(
      http.get('/api/v2/reactions', () =>
        HttpResponse.json({
          targetRid,
          emojis: [{ emoji: '👍', count: 1, mine: true }],
        }),
      ),
      http.delete('/api/v2/reactions', ({ request }) => {
        const url = new URL(request.url);
        state.deleted++;
        state.lastEmoji = url.searchParams.get('emoji') ?? '';
        return new HttpResponse(null, { status: 204 });
      }),
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
      expect(state.deleted).toBe(1);
    });
    expect(state.lastEmoji).toBe('👍');
  });

  it('opens the picker on + button and POSTs the chosen default emoji', async () => {
    const state = { posted: 0, lastEmoji: '' };
    server.use(
      http.get('/api/v2/reactions', () =>
        HttpResponse.json({ targetRid, emojis: [] }),
      ),
      http.post('/api/v2/reactions', async ({ request }) => {
        const body = (await request.json()) as { emoji: string };
        state.posted++;
        state.lastEmoji = body.emoji;
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
      expect(screen.getByTestId('reaction-add-button')).not.toBeDisabled();
    });
    fireEvent.click(screen.getByTestId('reaction-add-button'));
    expect(screen.getByTestId('reaction-picker')).toBeInTheDocument();
    await act(async () => {
      fireEvent.click(screen.getByTestId('reaction-picker-option-🚀'));
      await new Promise((r) => setTimeout(r, 0));
    });
    await waitFor(() => {
      expect(state.posted).toBe(1);
    });
    expect(state.lastEmoji).toBe('🚀');
    await waitFor(() => {
      expect(screen.queryByTestId('reaction-picker')).toBeNull();
    });
  });

  it('the picker custom-input submits the typed emoji', async () => {
    const state = { posted: 0, lastEmoji: '' };
    server.use(
      http.get('/api/v2/reactions', () =>
        HttpResponse.json({ targetRid, emojis: [] }),
      ),
      http.post('/api/v2/reactions', async ({ request }) => {
        const body = (await request.json()) as { emoji: string };
        state.posted++;
        state.lastEmoji = body.emoji;
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
      expect(screen.getByTestId('reaction-add-button')).not.toBeDisabled();
    });
    fireEvent.click(screen.getByTestId('reaction-add-button'));
    fireEvent.change(screen.getByTestId('reaction-picker-custom-input'), {
      target: { value: '✨' },
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId('reaction-picker-custom-submit'));
      await new Promise((r) => setTimeout(r, 0));
    });
    await waitFor(() => {
      expect(state.posted).toBe(1);
    });
    expect(state.lastEmoji).toBe('✨');
  });
});
