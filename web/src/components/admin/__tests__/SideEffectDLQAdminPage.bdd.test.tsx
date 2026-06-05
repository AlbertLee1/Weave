import {
  describe,
  it,
  expect,
  beforeAll,
  afterAll,
  afterEach,
  beforeEach,
} from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { SideEffectDLQAdminPage } from '../SideEffectDLQAdminPage';
import { Toaster } from '../../common/Toaster';
import { useToastStore } from '../../../stores/toastStore';

// BDD: the Side-Effect DLQ admin page surfaces the backend
// action-side-effect dead-letter-queue ops (cmd/server/admin_side_effect_dlq.go)
// — list / replay / abandon — which previously had zero UI exposure. Each
// scenario asserts an externally observable behavior: what renders, the
// replayStatus badge, and the HTTP method/path the page emits, so the wire
// contract is locked, not just component internals.

interface StoredRow {
  id: number;
  actionLogId: number;
  effectIndex: number;
  effectType: string;
  effectConfig?: unknown;
  outcome: unknown;
  createdAt: string;
  replayStatus: 'pending' | 'replayed' | 'abandoned';
  replayedAt?: string;
  replayCount: number;
}

interface RecordedCall {
  method: string;
  path: string;
}

const SEED: StoredRow[] = [
  {
    id: 101,
    actionLogId: 9001,
    effectIndex: 0,
    effectType: 'webhook',
    effectConfig: { url: 'https://example.test/hook' },
    outcome: { status: 'failed', error: 'connection refused' },
    createdAt: '2026-05-01T10:00:00Z',
    replayStatus: 'pending',
    replayCount: 0,
  },
  {
    id: 102,
    actionLogId: 9002,
    effectIndex: 1,
    effectType: 'webhook',
    outcome: { status: 'success' },
    createdAt: '2026-05-02T11:00:00Z',
    replayStatus: 'replayed',
    replayedAt: '2026-05-03T09:00:00Z',
    replayCount: 1,
  },
];

let rows: StoredRow[];
let calls: RecordedCall[];
// failReplay flips the replay handler to a 409 so the replay-error scenario
// can assert the toast surfaces describeApiError output.
let failReplay = false;

const server = setupServer(
  http.get('/api/admin/side-effect-dlq', () =>
    HttpResponse.json({ entries: rows }),
  ),

  http.post('/api/admin/side-effect-dlq/:id/replay', ({ params }) => {
    const id = Number(params.id);
    calls.push({ method: 'POST', path: `/api/admin/side-effect-dlq/${id}/replay` });
    if (failReplay) {
      return HttpResponse.json(
        {
          errorCode: 'CONFLICT',
          errorName: 'SideEffectDLQNotReplayable',
          errorInstanceId: 'x',
          parameters: { id: String(id), reason: 'still failing' },
        },
        { status: 409 },
      );
    }
    const idx = rows.findIndex((r) => r.id === id);
    if (idx >= 0) {
      rows[idx] = {
        ...rows[idx],
        replayStatus: 'replayed',
        replayCount: rows[idx].replayCount + 1,
        replayedAt: '2026-05-04T09:00:00Z',
      };
    }
    return HttpResponse.json({
      id,
      replayed: true,
      status: 'replayed',
      replayCount: rows[idx]?.replayCount ?? 1,
      outcome: { status: 'success' },
    });
  }),

  http.post('/api/admin/side-effect-dlq/:id/abandon', ({ params }) => {
    const id = Number(params.id);
    calls.push({ method: 'POST', path: `/api/admin/side-effect-dlq/${id}/abandon` });
    const idx = rows.findIndex((r) => r.id === id);
    if (idx >= 0) {
      rows[idx] = { ...rows[idx], replayStatus: 'abandoned' };
    }
    return HttpResponse.json({ id, abandoned: true, status: 'abandoned' });
  }),
);

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => {
  server.resetHandlers();
  useToastStore.getState().clear();
});
afterAll(() => server.close());

beforeEach(() => {
  rows = JSON.parse(JSON.stringify(SEED)) as StoredRow[];
  calls = [];
  failReplay = false;
});

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/admin/side-effect-dlq']}>
        <SideEffectDLQAdminPage />
        <Toaster />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BDD: SideEffectDLQAdminPage', () => {
  it('renders the DLQ entries with a replayStatus badge', async () => {
    renderPage();

    expect(
      await screen.findByRole('heading', { level: 1, name: /Side-Effect DLQ/i }),
    ).toBeInTheDocument();

    const pendingRow = await screen.findByTestId('side-effect-dlq-row-101');
    expect(within(pendingRow).getByText('101')).toBeInTheDocument();
    expect(within(pendingRow).getByText('9001')).toBeInTheDocument();
    expect(within(pendingRow).getByText('webhook')).toBeInTheDocument();
    const badge = within(pendingRow).getByTestId('side-effect-dlq-status-badge');
    expect(badge).toHaveTextContent(/pending/i);

    const replayedRow = screen.getByTestId('side-effect-dlq-row-102');
    expect(
      within(replayedRow).getByTestId('side-effect-dlq-status-badge'),
    ).toHaveTextContent(/replayed/i);
  });

  it('shows an empty state when there are no entries', async () => {
    rows = [];
    renderPage();
    expect(await screen.findByTestId('side-effect-dlq-empty')).toBeInTheDocument();
  });

  it('replays a pending row via POST and refreshes the list', async () => {
    const user = userEvent.setup();
    renderPage();
    const pendingRow = await screen.findByTestId('side-effect-dlq-row-101');

    await user.click(
      within(pendingRow).getByTestId('side-effect-dlq-replay-btn'),
    );

    await waitFor(() => {
      expect(
        calls.find((c) => c.path === '/api/admin/side-effect-dlq/101/replay'),
      ).toBeTruthy();
    });
    const post = calls.find(
      (c) => c.path === '/api/admin/side-effect-dlq/101/replay',
    )!;
    expect(post.method).toBe('POST');

    // List refreshes; row 101 now badges as replayed.
    await waitFor(() => {
      expect(
        within(screen.getByTestId('side-effect-dlq-row-101')).getByTestId(
          'side-effect-dlq-status-badge',
        ),
      ).toHaveTextContent(/replayed/i);
    });
  });

  it('abandons a pending row only after the confirmation modal', async () => {
    const user = userEvent.setup();
    renderPage();
    const pendingRow = await screen.findByTestId('side-effect-dlq-row-101');

    // Clicking Abandon opens a confirmation modal — no request fired yet.
    await user.click(
      within(pendingRow).getByTestId('side-effect-dlq-abandon-btn'),
    );
    await screen.findByTestId('side-effect-dlq-abandon-modal');
    expect(
      calls.find((c) => c.path.endsWith('/abandon')),
    ).toBeUndefined();

    // Confirm → POST abandon.
    await user.click(screen.getByTestId('side-effect-dlq-abandon-confirm'));
    await waitFor(() => {
      expect(
        calls.find((c) => c.path === '/api/admin/side-effect-dlq/101/abandon'),
      ).toBeTruthy();
    });

    // List refreshes; row 101 badges as abandoned.
    await waitFor(() => {
      expect(
        within(screen.getByTestId('side-effect-dlq-row-101')).getByTestId(
          'side-effect-dlq-status-badge',
        ),
      ).toHaveTextContent(/abandoned/i);
    });
  });

  it('disables replay/abandon for non-pending rows', async () => {
    renderPage();
    const replayedRow = await screen.findByTestId('side-effect-dlq-row-102');

    expect(
      within(replayedRow).getByTestId('side-effect-dlq-replay-btn'),
    ).toBeDisabled();
    expect(
      within(replayedRow).getByTestId('side-effect-dlq-abandon-btn'),
    ).toBeDisabled();
  });

  it('surfaces a toast when replay fails', async () => {
    failReplay = true;
    const user = userEvent.setup();
    renderPage();
    const pendingRow = await screen.findByTestId('side-effect-dlq-row-101');

    await user.click(
      within(pendingRow).getByTestId('side-effect-dlq-replay-btn'),
    );

    const toaster = await screen.findByTestId('toaster');
    expect(
      within(toaster).getByText(/SideEffectDLQNotReplayable/i),
    ).toBeInTheDocument();
  });
});
