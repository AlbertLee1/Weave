import {
  describe,
  it,
  expect,
  beforeAll,
  afterAll,
  afterEach,
} from 'vitest';
import {
  render,
  screen,
  fireEvent,
  waitFor,
  within,
} from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { FunnelDLQAdminPage } from '../FunnelDLQAdminPage';
import { Toaster } from '../../common/Toaster';
import { useToastStore } from '../../../stores/toastStore';
import type { DLQEntry } from '../../../api/funnelDLQ';

// ---------------------------------------------------------------------------
// In-memory DLQ store backing the MSW handlers, mirroring the wire shape of
// cmd/server/admin_funnel_dlq.go. Each test reseeds via afterEach +
// server.resetHandlers so scenarios stay isolated.
// ---------------------------------------------------------------------------

let entries: DLQEntry[] = [];
const replayedIds: string[] = [];
const discardedIds: string[] = [];

function seed(initial: DLQEntry[]) {
  entries = initial.map((e) => ({ ...e, message: { ...e.message } }));
  replayedIds.length = 0;
  discardedIds.length = 0;
}

function makeEntry(
  id: string,
  overrides: Partial<DLQEntry['message']> = {},
): DLQEntry {
  return {
    id,
    subject: `OBJECT_EDITS_DLQ.${id}`,
    message: {
      originalSubject: `OBJECT_EDITS.${id}`,
      reason: `delivery exhausted for ${id}`,
      maxDeliveries: 5,
      streamSequence: 42,
      consumerSequence: 7,
      ...overrides,
    },
  };
}

const server = setupServer(
  http.get('/api/admin/funnel/dlq', () =>
    HttpResponse.json({ entries, size: entries.length }),
  ),

  http.post('/api/admin/funnel/dlq/:id/replay', ({ params }) => {
    const id = String(params.id);
    const entry = entries.find((e) => e.id === id);
    replayedIds.push(id);
    entries = entries.filter((e) => e.id !== id);
    return HttpResponse.json({
      id,
      originalSubject: entry?.message.originalSubject ?? `OBJECT_EDITS.${id}`,
    });
  }),

  http.post('/api/admin/funnel/dlq/:id/discard', ({ params }) => {
    const id = String(params.id);
    discardedIds.push(id);
    entries = entries.filter((e) => e.id !== id);
    return HttpResponse.json({ id, dropped: true });
  }),
);

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchInterval: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/admin/funnel-dlq']}>
        <FunnelDLQAdminPage />
        <Toaster />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => {
  server.resetHandlers();
  useToastStore.getState().clear();
});
afterAll(() => server.close());

describe('FunnelDLQAdminPage BDD', () => {
  // (a) Given the backend returns two entries with a larger depth, When the
  //     page renders, Then both rows + the DLQ depth are displayed.
  it('lists entries and surfaces the DLQ depth', async () => {
    seed([makeEntry('dlq-1'), makeEntry('dlq-2')]);
    // size may exceed entries.length when limit caps the page.
    server.use(
      http.get('/api/admin/funnel/dlq', () =>
        HttpResponse.json({ entries, size: 17 }),
      ),
    );

    renderPage();

    expect(await screen.findByText('dlq-1')).toBeInTheDocument();
    expect(screen.getByText('dlq-2')).toBeInTheDocument();
    // Failure reason + maxDeliveries are exposed for triage.
    expect(
      screen.getByText('delivery exhausted for dlq-1'),
    ).toBeInTheDocument();
    // DLQ depth is rendered from `size`, not the page length.
    const depth = screen.getByTestId('funnel-dlq-depth');
    expect(depth).toHaveTextContent('17');
  });

  // (b) Given the backend returns no entries, Then a friendly empty-state is
  //     shown (queue drained).
  it('shows an empty state when the DLQ is drained', async () => {
    seed([]);

    renderPage();

    expect(await screen.findByTestId('funnel-dlq-empty')).toBeInTheDocument();
    expect(screen.getByTestId('funnel-dlq-depth')).toHaveTextContent('0');
  });

  // (c) Given the DLQ is not configured (HTTP 503), When the page loads, Then a
  //     friendly "not enabled" info state is shown — NOT an error toast.
  it('renders a friendly "not enabled" state on 503 instead of an error toast', async () => {
    seed([]);
    server.use(
      http.get('/api/admin/funnel/dlq', () =>
        HttpResponse.json(
          {
            errorCode: 'SERVICE_UNAVAILABLE',
            errorName: 'FunnelDLQNotConfigured',
            errorInstanceId: 'abc',
          },
          { status: 503 },
        ),
      ),
    );

    renderPage();

    expect(
      await screen.findByTestId('funnel-dlq-not-configured'),
    ).toBeInTheDocument();
    expect(
      screen.getByTestId('funnel-dlq-not-configured'),
    ).toHaveTextContent(/not (enabled|configured)/i);

    // No error toast was pushed for the 503 — it is a benign degraded state.
    expect(screen.queryByTestId('toast')).not.toBeInTheDocument();
    // And no generic error state leaked through.
    expect(screen.queryByTestId('funnel-dlq-error')).not.toBeInTheDocument();
  });

  // (d) Given an entry exists, When the operator clicks Replay, Then a POST is
  //     issued to /replay and the entry disappears once the list refreshes.
  it('replays an entry and refreshes the list', async () => {
    seed([makeEntry('dlq-1')]);

    renderPage();

    await screen.findByText('dlq-1');

    fireEvent.click(screen.getByTestId('funnel-dlq-replay-btn-dlq-1'));

    await waitFor(() => expect(replayedIds).toEqual(['dlq-1']));
    await waitFor(() =>
      expect(screen.queryByText('dlq-1')).not.toBeInTheDocument(),
    );
  });

  // (e) Given an entry exists, When the operator clicks Discard, Then a
  //     confirmation modal appears and only confirming issues the POST.
  it('requires a second confirmation before discarding', async () => {
    seed([makeEntry('dlq-1')]);

    renderPage();

    await screen.findByText('dlq-1');

    fireEvent.click(screen.getByTestId('funnel-dlq-discard-btn-dlq-1'));

    // Modal is shown; no discard has fired yet.
    const dialog = await screen.findByTestId('funnel-dlq-discard-modal');
    expect(discardedIds).toHaveLength(0);

    fireEvent.click(within(dialog).getByTestId('funnel-dlq-discard-confirm'));

    await waitFor(() => expect(discardedIds).toEqual(['dlq-1']));
    await waitFor(() =>
      expect(screen.queryByText('dlq-1')).not.toBeInTheDocument(),
    );
  });

  // (f) Given the replay endpoint fails, When the operator replays, Then an
  //     error toast surfaces the server-provided reason and the row stays.
  it('surfaces an error toast when replay fails', async () => {
    seed([makeEntry('dlq-1')]);
    server.use(
      http.post('/api/admin/funnel/dlq/:id/replay', () =>
        HttpResponse.json(
          {
            errorCode: 'INTERNAL',
            errorName: 'FunnelDLQReplayFailed',
            errorInstanceId: 'xyz',
            parameters: { reason: 'publisher offline' },
          },
          { status: 500 },
        ),
      ),
    );

    renderPage();

    await screen.findByText('dlq-1');
    fireEvent.click(screen.getByTestId('funnel-dlq-replay-btn-dlq-1'));

    expect(
      await screen.findByText(/FunnelDLQReplayFailed/),
    ).toBeInTheDocument();
    expect(screen.getByText(/publisher offline/)).toBeInTheDocument();
    // The row remains because the replay did not succeed.
    expect(screen.getByText('dlq-1')).toBeInTheDocument();
  });
});
