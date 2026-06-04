import {
  describe,
  it,
  expect,
  vi,
  beforeAll,
  afterAll,
  afterEach,
  beforeEach,
} from 'vitest';
import { createElement } from 'react';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AuthProvider } from '../../../auth/AuthContext';
import { CommentsTab } from '../CommentsTab';

// BDD coverage for the previously-silent failure modes of the comment
// reply / update / delete mutations. Before this change those three paths
// swallowed errors (fake comment + no error state), so a failed network
// call left the user staring at a draft that "did nothing". Each scenario
// below forces the matching API call to fail and asserts a user-visible
// error surface, then confirms the happy path still clears the draft /
// closes the editor / closes the modal.

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
const ts = '2026-04-28T12:00:00Z';

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

function mineRow(): MockComment {
  return {
    id: 'c1',
    targetRid,
    body: 'parent',
    author: 'alice',
    createdAt: ts,
    updatedAt: ts,
  };
}

describe('CommentsTab error handling (reply/update/delete)', () => {
  beforeEach(() => {
    server.use(meHandler('alice'));
  });

  describe('reply failure', () => {
    it('Given a reply that the server rejects, When the user submits, Then a visible reply error appears and the draft is kept', async () => {
      server.use(listHandler([mineRow()]));
      server.use(
        http.post('/api/v2/comments', () =>
          HttpResponse.json({ error: 'reply boom' }, { status: 500 }),
        ),
      );
      render(<CommentsTab targetRid={targetRid} />, { wrapper: makeWrapper() });
      await waitFor(() => {
        expect(screen.getByTestId('comment-reply-button-c1')).toBeInTheDocument();
      });
      fireEvent.click(screen.getByTestId('comment-reply-button-c1'));
      const replyInput = screen.getByTestId(
        'comment-reply-input-c1',
      ) as HTMLTextAreaElement;
      fireEvent.change(replyInput, { target: { value: 'me too' } });
      await act(async () => {
        fireEvent.click(screen.getByTestId('comment-reply-submit-c1'));
        await new Promise((r) => setTimeout(r, 0));
      });
      // User-visible error surfaces and the reply form / draft are kept so
      // the user can retry without retyping.
      await waitFor(() => {
        expect(screen.getByTestId('comment-reply-error-c1')).toBeInTheDocument();
      });
      expect(
        (screen.getByTestId('comment-reply-input-c1') as HTMLTextAreaElement).value,
      ).toBe('me too');
    });

    it('Given a reply that succeeds, When the user submits, Then the draft clears and the reply form closes', async () => {
      server.use(listHandler([mineRow()]));
      server.use(
        http.post('/api/v2/comments', () =>
          HttpResponse.json({
            id: 'reply1',
            targetRid,
            body: 'me too',
            author: 'alice',
            parentId: 'c1',
            createdAt: ts,
            updatedAt: ts,
          }),
        ),
      );
      render(<CommentsTab targetRid={targetRid} />, { wrapper: makeWrapper() });
      await waitFor(() => {
        expect(screen.getByTestId('comment-reply-button-c1')).toBeInTheDocument();
      });
      fireEvent.click(screen.getByTestId('comment-reply-button-c1'));
      const replyInput = screen.getByTestId(
        'comment-reply-input-c1',
      ) as HTMLTextAreaElement;
      fireEvent.change(replyInput, { target: { value: 'me too' } });
      await act(async () => {
        fireEvent.click(screen.getByTestId('comment-reply-submit-c1'));
        await new Promise((r) => setTimeout(r, 0));
      });
      await waitFor(() => {
        expect(
          screen.queryByTestId('comment-reply-input-c1'),
        ).not.toBeInTheDocument();
      });
      expect(screen.queryByTestId('comment-reply-error-c1')).not.toBeInTheDocument();
    });
  });

  describe('update failure', () => {
    it('Given an edit that the server rejects, When the user saves, Then a visible update error appears and the editor stays open', async () => {
      server.use(listHandler([mineRow()]));
      server.use(
        http.put('/api/v2/comments/:id', () =>
          HttpResponse.json({ error: 'update boom' }, { status: 500 }),
        ),
      );
      render(<CommentsTab targetRid={targetRid} />, { wrapper: makeWrapper() });
      await waitFor(() => {
        expect(screen.getByTestId('comment-edit-button-c1')).toBeInTheDocument();
      });
      fireEvent.click(screen.getByTestId('comment-edit-button-c1'));
      const editInput = screen.getByTestId(
        'comment-edit-input-c1',
      ) as HTMLTextAreaElement;
      fireEvent.change(editInput, { target: { value: 'edited body' } });
      await act(async () => {
        fireEvent.click(screen.getByTestId('comment-edit-submit-c1'));
        await new Promise((r) => setTimeout(r, 0));
      });
      await waitFor(() => {
        expect(screen.getByTestId('comment-edit-error-c1')).toBeInTheDocument();
      });
      // Editor stays open so the user can retry.
      expect(screen.getByTestId('comment-edit-input-c1')).toBeInTheDocument();
    });

    it('Given an edit that succeeds, When the user saves, Then the inline editor closes', async () => {
      const rows = [mineRow()];
      server.use(listHandler(rows));
      server.use(
        http.put('/api/v2/comments/:id', async ({ request, params }) => {
          const json = (await request.json()) as { body: string };
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
      const editInput = screen.getByTestId(
        'comment-edit-input-c1',
      ) as HTMLTextAreaElement;
      fireEvent.change(editInput, { target: { value: 'edited body' } });
      await act(async () => {
        fireEvent.click(screen.getByTestId('comment-edit-submit-c1'));
        await new Promise((r) => setTimeout(r, 0));
      });
      await waitFor(() => {
        expect(
          screen.queryByTestId('comment-edit-input-c1'),
        ).not.toBeInTheDocument();
      });
      expect(screen.queryByTestId('comment-edit-error-c1')).not.toBeInTheDocument();
    });
  });

  describe('delete failure', () => {
    it('Given a delete that the server rejects, When the user confirms, Then a visible error appears inside the still-open modal', async () => {
      server.use(listHandler([mineRow()]));
      server.use(
        http.delete('/api/v2/comments/:id', () =>
          HttpResponse.json({ error: 'delete boom' }, { status: 500 }),
        ),
      );
      render(<CommentsTab targetRid={targetRid} />, { wrapper: makeWrapper() });
      await waitFor(() => {
        expect(screen.getByTestId('comment-delete-button-c1')).toBeInTheDocument();
      });
      fireEvent.click(screen.getByTestId('comment-delete-button-c1'));
      expect(screen.getByTestId('comment-delete-confirm')).toBeInTheDocument();
      await act(async () => {
        fireEvent.click(screen.getByTestId('comment-delete-submit'));
        await new Promise((r) => setTimeout(r, 0));
      });
      // Error shown and the confirm modal stays open so the user can retry
      // or cancel.
      await waitFor(() => {
        expect(screen.getByTestId('comment-delete-error')).toBeInTheDocument();
      });
      expect(screen.getByTestId('comment-delete-confirm')).toBeInTheDocument();
    });

    it('Given a delete that succeeds, When the user confirms, Then the confirm modal closes', async () => {
      server.use(listHandler([mineRow()]));
      server.use(
        http.delete('/api/v2/comments/:id', () => new HttpResponse(null, { status: 204 })),
      );
      render(<CommentsTab targetRid={targetRid} />, { wrapper: makeWrapper() });
      await waitFor(() => {
        expect(screen.getByTestId('comment-delete-button-c1')).toBeInTheDocument();
      });
      fireEvent.click(screen.getByTestId('comment-delete-button-c1'));
      expect(screen.getByTestId('comment-delete-confirm')).toBeInTheDocument();
      await act(async () => {
        fireEvent.click(screen.getByTestId('comment-delete-submit'));
        await new Promise((r) => setTimeout(r, 0));
      });
      await waitFor(() => {
        expect(
          screen.queryByTestId('comment-delete-confirm'),
        ).not.toBeInTheDocument();
      });
      expect(screen.queryByTestId('comment-delete-error')).not.toBeInTheDocument();
    });
  });
});
