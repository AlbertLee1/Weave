import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { createElement } from 'react';
import {
  render,
  screen,
  fireEvent,
  waitFor,
  act,
} from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AuthProvider } from '../../../auth/AuthContext';
import { CommentsTab } from '../CommentsTab';

const server = setupServer();
beforeAll(() => server.listen({ onUnhandledRequest: 'bypass' }));
afterEach(() => {
  server.resetHandlers();
  vi.clearAllMocks();
});
afterAll(() => server.close());

interface MockComment {
  id: string;
  targetRid: string;
  body: string;
  author: string;
  parentId?: string;
  createdAt: string;
  updatedAt: string;
  deletedAt?: string;
}

function meHandler(id: string) {
  return http.get('/api/v2/me', () =>
    HttpResponse.json({
      id,
      email: `${id}@example.test`,
      name: id,
      roles: ['viewer'],
      ontologyRoles: {},
      permissions: ['object.read'],
    }),
  );
}

function listHandler(rows: MockComment[]) {
  return http.get('/api/v2/comments', ({ request }) => {
    const url = new URL(request.url);
    const target = url.searchParams.get('targetRid') ?? '';
    const filtered = rows.filter((r) => r.targetRid === target);
    return HttpResponse.json({
      comments: filtered,
      total: filtered.length,
      limit: 200,
      offset: 0,
    });
  });
}

const targetRid = 'ri.ontology.main.object.emp1';

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

describe('CommentsTab (US-335)', () => {
  beforeEach(() => {
    server.use(meHandler('alice'));
  });

  it('renders empty state when no comments exist', async () => {
    server.use(listHandler([]));
    render(<CommentsTab targetRid={targetRid} />, { wrapper: makeWrapper() });
    await waitFor(() => {
      expect(screen.getByTestId('comments-empty')).toBeInTheDocument();
    });
  });

  it('renders threaded comments with replies', async () => {
    const now = '2026-04-28T12:00:00Z';
    server.use(
      listHandler([
        {
          id: 'c1',
          targetRid,
          body: 'parent comment',
          author: 'alice',
          createdAt: now,
          updatedAt: now,
        },
        {
          id: 'c2',
          targetRid,
          body: 'a reply',
          author: 'bob',
          parentId: 'c1',
          createdAt: now,
          updatedAt: now,
        },
      ]),
    );
    render(<CommentsTab targetRid={targetRid} />, { wrapper: makeWrapper() });
    await waitFor(() => {
      expect(screen.getByTestId('comment-thread-c1')).toBeInTheDocument();
    });
    expect(screen.getByTestId('comment-body-c1')).toHaveTextContent('parent comment');
    expect(screen.getByTestId('comment-reply-c2')).toBeInTheDocument();
    expect(screen.getByTestId('comment-body-c2')).toHaveTextContent('a reply');
  });

  it('renders a tombstone for soft-deleted comments', async () => {
    const ts = '2026-04-28T11:00:00Z';
    server.use(
      listHandler([
        {
          id: 'c1',
          targetRid,
          body: '',
          author: 'alice',
          createdAt: ts,
          updatedAt: ts,
          deletedAt: ts,
        },
      ]),
    );
    render(<CommentsTab targetRid={targetRid} />, { wrapper: makeWrapper() });
    await waitFor(() => {
      expect(screen.getByTestId('comment-body-c1')).toHaveTextContent('[comment deleted]');
    });
    expect(screen.getByTestId('comment-author-c1')).toHaveTextContent('[deleted]');
    // No edit/delete/reply controls on a tombstoned row.
    expect(screen.queryByTestId('comment-edit-button-c1')).not.toBeInTheDocument();
    expect(screen.queryByTestId('comment-reply-button-c1')).not.toBeInTheDocument();
  });

  it('posts a new comment via the create form', async () => {
    server.use(listHandler([]));
    let captured: { body?: unknown } = {};
    server.use(
      http.post('/api/v2/comments', async ({ request }) => {
        captured.body = await request.json();
        return HttpResponse.json({
          id: 'new1',
          targetRid,
          body: (captured.body as { body: string }).body,
          author: 'alice',
          createdAt: '2026-04-28T12:00:00Z',
          updatedAt: '2026-04-28T12:00:00Z',
        });
      }),
    );
    render(<CommentsTab targetRid={targetRid} />, { wrapper: makeWrapper() });
    await waitFor(() => {
      expect(screen.getByTestId('comments-empty')).toBeInTheDocument();
    });
    const input = screen.getByTestId('comment-new-input') as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: 'hello world' } });
    await act(async () => {
      fireEvent.click(screen.getByTestId('comment-new-submit'));
      await new Promise((r) => setTimeout(r, 0));
    });
    await waitFor(() => {
      expect(captured.body).toMatchObject({ targetRid, body: 'hello world' });
    });
  });

  it('expands a reply form and posts a reply scoped to the parent', async () => {
    const ts = '2026-04-28T12:00:00Z';
    server.use(
      listHandler([
        {
          id: 'c1',
          targetRid,
          body: 'parent',
          author: 'alice',
          createdAt: ts,
          updatedAt: ts,
        },
      ]),
    );
    let captured: { body?: unknown } = {};
    server.use(
      http.post('/api/v2/comments', async ({ request }) => {
        captured.body = await request.json();
        return HttpResponse.json({
          id: 'reply1',
          targetRid,
          body: (captured.body as { body: string }).body,
          author: 'alice',
          parentId: 'c1',
          createdAt: ts,
          updatedAt: ts,
        });
      }),
    );
    render(<CommentsTab targetRid={targetRid} />, { wrapper: makeWrapper() });
    await waitFor(() => {
      expect(screen.getByTestId('comment-reply-button-c1')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTestId('comment-reply-button-c1'));
    const replyInput = screen.getByTestId('comment-reply-input-c1') as HTMLTextAreaElement;
    fireEvent.change(replyInput, { target: { value: 'me too' } });
    await act(async () => {
      fireEvent.click(screen.getByTestId('comment-reply-submit-c1'));
      await new Promise((r) => setTimeout(r, 0));
    });
    await waitFor(() => {
      expect(captured.body).toMatchObject({
        targetRid,
        body: 'me too',
        parentId: 'c1',
      });
    });
  });

  it('only shows edit/delete buttons for the authoring user', async () => {
    const ts = '2026-04-28T12:00:00Z';
    server.use(
      listHandler([
        {
          id: 'c1',
          targetRid,
          body: 'mine',
          author: 'alice',
          createdAt: ts,
          updatedAt: ts,
        },
        {
          id: 'c2',
          targetRid,
          body: 'theirs',
          author: 'bob',
          createdAt: ts,
          updatedAt: ts,
        },
      ]),
    );
    render(<CommentsTab targetRid={targetRid} />, { wrapper: makeWrapper() });
    await waitFor(() => {
      expect(screen.getByTestId('comment-thread-c1')).toBeInTheDocument();
    });
    expect(screen.getByTestId('comment-edit-button-c1')).toBeInTheDocument();
    expect(screen.getByTestId('comment-delete-button-c1')).toBeInTheDocument();
    expect(screen.queryByTestId('comment-edit-button-c2')).not.toBeInTheDocument();
    expect(screen.queryByTestId('comment-delete-button-c2')).not.toBeInTheDocument();
  });

  it('edits a comment via inline editor', async () => {
    const ts = '2026-04-28T12:00:00Z';
    const rows: MockComment[] = [
      {
        id: 'c1',
        targetRid,
        body: 'original',
        author: 'alice',
        createdAt: ts,
        updatedAt: ts,
      },
    ];
    server.use(listHandler(rows));
    let updatedBody: string | null = null;
    server.use(
      http.put('/api/v2/comments/:id', async ({ request, params }) => {
        const json = (await request.json()) as { body: string };
        updatedBody = json.body;
        return HttpResponse.json({
          ...rows[0],
          id: String(params.id),
          body: json.body,
        });
      }),
    );
    render(<CommentsTab targetRid={targetRid} />, { wrapper: makeWrapper() });
    await waitFor(() => {
      expect(screen.getByTestId('comment-edit-button-c1')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTestId('comment-edit-button-c1'));
    const editInput = screen.getByTestId('comment-edit-input-c1') as HTMLTextAreaElement;
    expect(editInput.value).toBe('original');
    fireEvent.change(editInput, { target: { value: 'edited body' } });
    await act(async () => {
      fireEvent.click(screen.getByTestId('comment-edit-submit-c1'));
      await new Promise((r) => setTimeout(r, 0));
    });
    await waitFor(() => {
      expect(updatedBody).toBe('edited body');
    });
  });

  it('shows a delete confirmation modal and only deletes after confirm', async () => {
    const ts = '2026-04-28T12:00:00Z';
    server.use(
      listHandler([
        {
          id: 'c1',
          targetRid,
          body: 'going away',
          author: 'alice',
          createdAt: ts,
          updatedAt: ts,
        },
      ]),
    );
    let deleted = false;
    server.use(
      http.delete('/api/v2/comments/:id', () => {
        deleted = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    render(<CommentsTab targetRid={targetRid} />, { wrapper: makeWrapper() });
    await waitFor(() => {
      expect(screen.getByTestId('comment-delete-button-c1')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTestId('comment-delete-button-c1'));
    expect(screen.getByTestId('comment-delete-confirm')).toBeInTheDocument();
    expect(deleted).toBe(false);
    // Cancel first — should NOT delete.
    fireEvent.click(screen.getByTestId('comment-delete-cancel'));
    expect(deleted).toBe(false);
    // Re-open and confirm — should delete.
    fireEvent.click(screen.getByTestId('comment-delete-button-c1'));
    await act(async () => {
      fireEvent.click(screen.getByTestId('comment-delete-submit'));
      await new Promise((r) => setTimeout(r, 0));
    });
    await waitFor(() => {
      expect(deleted).toBe(true);
    });
  });
});
