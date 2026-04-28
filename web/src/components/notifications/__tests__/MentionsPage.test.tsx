import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { createElement } from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AuthProvider } from '../../../auth/AuthContext';
import { MentionsPage } from '../MentionsPage';

const server = setupServer();
beforeAll(() => server.listen({ onUnhandledRequest: 'bypass' }));
afterEach(() => {
  server.resetHandlers();
  vi.clearAllMocks();
});
afterAll(() => server.close());

const targetRid = 'ri.phonograph2-objects.main.object.emp1';

interface MockComment {
  id: string;
  targetRid: string;
  body: string;
  author: string;
  parentId?: string;
  createdAt: string;
  updatedAt: string;
}

function meHandler() {
  return http.get('/api/v2/me', () =>
    HttpResponse.json({
      id: 'alice',
      email: 'alice@example.test',
      name: 'Alice',
      roles: ['viewer'],
      ontologyRoles: {},
      permissions: ['object.read'],
    }),
  );
}

function notificationsHandler(items: unknown[]) {
  return http.get('/api/v2/notifications', () =>
    HttpResponse.json({ data: items }),
  );
}

function commentsHandler(rows: MockComment[]) {
  return http.get('/api/v2/comments', () =>
    HttpResponse.json({
      comments: rows,
      total: rows.length,
      limit: 200,
      offset: 0,
    }),
  );
}

function renderAt(initialPath: string) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(
    createElement(
      QueryClientProvider,
      { client: qc },
      createElement(
        MemoryRouter,
        { initialEntries: [initialPath] },
        createElement(AuthProvider, null, createElement(MentionsPage, null)),
      ),
    ),
  );
}

describe('MentionsPage (US-340)', () => {
  beforeEach(() => {
    server.use(meHandler());
    Element.prototype.scrollIntoView = vi.fn();
  });

  it('renders the inbox of mention notifications + a no-selection prompt when no rid in URL', async () => {
    const mention = {
      id: 'n1',
      userId: 'alice',
      title: 'bob mentioned you',
      body: 'cc @alice please review',
      type: 'mention',
      link: `/mentions?rid=${targetRid}&commentId=c-42`,
      read: false,
      createdAt: '2026-04-28T12:00:00Z',
    };
    server.use(notificationsHandler([mention]));
    renderAt('/mentions');
    expect(
      await screen.findByTestId('mention-inbox-row-n1'),
    ).toBeInTheDocument();
    expect(screen.getByTestId('mentions-no-selection')).toBeInTheDocument();
  });

  it('renders the source thread + highlights the focused comment when URL has rid + commentId', async () => {
    const mention = {
      id: 'n1',
      userId: 'alice',
      title: 'bob mentioned you',
      body: 'cc @alice please review',
      type: 'mention',
      link: `/mentions?rid=${targetRid}&commentId=c-42`,
      read: true,
      createdAt: '2026-04-28T12:00:00Z',
    };
    const ts = '2026-04-28T11:00:00Z';
    server.use(
      notificationsHandler([mention]),
      commentsHandler([
        {
          id: 'c-1',
          targetRid,
          body: 'first thing',
          author: 'alice',
          createdAt: ts,
          updatedAt: ts,
        },
        {
          id: 'c-42',
          targetRid,
          body: 'cc @alice@example.com please review',
          author: 'bob',
          createdAt: ts,
          updatedAt: ts,
        },
      ]),
    );
    renderAt(`/mentions?rid=${encodeURIComponent(targetRid)}&commentId=c-42`);
    await waitFor(() => {
      expect(screen.getByTestId('comment-row-c-42')).toBeInTheDocument();
    });
    expect(screen.getByTestId('mentions-source-rid')).toHaveTextContent(targetRid);
    expect(
      screen.getByTestId('comment-row-c-42').getAttribute('data-highlight'),
    ).toBe('true');
    expect(
      screen.getByTestId('comment-row-c-1').getAttribute('data-highlight'),
    ).toBe('false');
    expect(
      screen.getByTestId('mention-inbox-row-n1').getAttribute('data-active'),
    ).toBe('true');
  });

  it('marks the targeted notification read on landing when ?id is provided', async () => {
    const mention = {
      id: 'n1',
      userId: 'alice',
      title: 'bob mentioned you',
      body: 'cc @alice please review',
      type: 'mention',
      link: `/mentions?rid=${targetRid}&commentId=c-42`,
      read: false,
      createdAt: '2026-04-28T12:00:00Z',
    };
    server.use(notificationsHandler([mention]), commentsHandler([]));
    let markRequested = false;
    server.use(
      http.post('/api/v2/notifications/:id/read', ({ params }) => {
        if (params.id === 'n1') markRequested = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderAt(
      `/mentions?id=n1&rid=${encodeURIComponent(targetRid)}&commentId=c-42`,
    );
    await waitFor(() => {
      expect(markRequested).toBe(true);
    });
  });

  it('shows an empty state when the user has no mentions', async () => {
    server.use(notificationsHandler([]));
    renderAt('/mentions');
    await waitFor(() => {
      expect(screen.getByTestId('mentions-empty')).toBeInTheDocument();
    });
  });
});
