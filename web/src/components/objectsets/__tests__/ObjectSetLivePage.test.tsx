import { describe, it, expect, vi, beforeAll, afterAll, afterEach, beforeEach } from 'vitest';
import { render, screen, fireEvent, act, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { setupServer } from 'msw/node';
import { ObjectSetLivePage } from '../ObjectSetLivePage';
import { localStorageKey } from '../../../lib/objectSetBuilder';

const ONTOLOGY = 'test';

// ---------------------------------------------------------------------------
// Mock EventSource (mirrors the one used by useObjectSetSubscription tests)
// ---------------------------------------------------------------------------

type EventSourceListener = (evt: MessageEvent) => void;
type ErrorListener = (evt: Event) => void;

class MockEventSource {
  static instances: MockEventSource[] = [];

  url: string;
  readyState: number;
  onmessage: EventSourceListener | null = null;
  onerror: ErrorListener | null = null;
  onopen: (() => void) | null = null;
  closed = false;

  constructor(url: string) {
    this.url = url;
    this.readyState = 0;
    MockEventSource.instances.push(this);
  }

  close() {
    this.closed = true;
    this.readyState = 2;
  }

  simulateOpen() {
    this.readyState = 1;
    this.onopen?.();
  }

  simulateMessage(data: string, lastEventId?: string) {
    const evt = new MessageEvent('message', {
      data,
      lastEventId: lastEventId ?? '',
    });
    this.onmessage?.(evt);
  }

  simulateError() {
    this.readyState = 2;
    this.onerror?.(new Event('error'));
  }
}

(MockEventSource as unknown as Record<string, number>).CONNECTING = 0;
(MockEventSource as unknown as Record<string, number>).OPEN = 1;
(MockEventSource as unknown as Record<string, number>).CLOSED = 2;

const server = setupServer();
beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

beforeEach(() => {
  MockEventSource.instances = [];
  vi.stubGlobal('EventSource', MockEventSource);
  vi.useFakeTimers();
  window.localStorage.clear();
  const saved = [
    {
      id: 'sa-1',
      name: 'Engineers',
      def: { type: 'base', objectType: 'Employee' },
      createdAt: '2026-01-01T00:00:00.000Z',
      activeVersionId: 'v-1',
      versions: [
        {
          versionId: 'v-1',
          def: { type: 'base', objectType: 'Employee' },
          createdAt: '2026-01-01T00:00:00.000Z',
        },
      ],
    },
  ];
  window.localStorage.setItem(localStorageKey(ONTOLOGY), JSON.stringify(saved));
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  window.localStorage.clear();
});

function renderPage(initialPath = `/objectsets/${ONTOLOGY}/live`) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialPath]}>
        <Routes>
          <Route
            path="/objectsets/:ontology/live"
            element={<ObjectSetLivePage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('ObjectSetLivePage', () => {
  it('renders heading + empty event list + Live toggle disabled without rid', () => {
    renderPage();
    expect(
      screen.getByRole('heading', { name: /object set live/i }),
    ).toBeInTheDocument();
    expect(screen.getByTestId('objectset-live-events-empty')).toBeInTheDocument();
    expect(screen.getByTestId('objectset-live-toggle')).toBeDisabled();
    // Status indicator visible in idle state
    const status = screen.getByTestId('objectset-live-status');
    expect(status).toHaveAttribute('data-state', 'idle');
  });

  it('seeds the rid input from the ?rid= query parameter', () => {
    renderPage(`/objectsets/${ONTOLOGY}/live?rid=ri.objectset.weave.main.tempObjectSet.42`);
    const input = screen.getByTestId('objectset-live-rid-input') as HTMLInputElement;
    expect(input.value).toBe('ri.objectset.weave.main.tempObjectSet.42');
    // With a populated rid the toggle becomes enabled.
    expect(screen.getByTestId('objectset-live-toggle')).not.toBeDisabled();
  });

  it('clicking Go Live opens EventSource and Stop closes it', () => {
    renderPage();
    const input = screen.getByTestId('objectset-live-rid-input') as HTMLInputElement;
    fireEvent.change(input, { target: { value: 'ri.set.1' } });

    const toggle = screen.getByTestId('objectset-live-toggle');
    expect(toggle.textContent).toMatch(/go live/i);
    fireEvent.click(toggle);

    expect(MockEventSource.instances).toHaveLength(1);
    expect(MockEventSource.instances[0].url).toBe(
      `/api/v2/ontologies/${ONTOLOGY}/objectSets/ri.set.1/subscribe`,
    );
    // Status flips to connecting until onopen fires.
    expect(screen.getByTestId('objectset-live-status')).toHaveAttribute(
      'data-state',
      'connecting',
    );

    act(() => {
      MockEventSource.instances[0].simulateOpen();
    });
    expect(screen.getByTestId('objectset-live-status')).toHaveAttribute(
      'data-state',
      'connected',
    );

    // Button now says "Stop" — clicking it should close the EventSource.
    const stopBtn = screen.getByTestId('objectset-live-toggle');
    expect(stopBtn.textContent).toMatch(/stop/i);
    fireEvent.click(stopBtn);

    expect(MockEventSource.instances[0].closed).toBe(true);
    expect(screen.getByTestId('objectset-live-status')).toHaveAttribute(
      'data-state',
      'idle',
    );
  });

  it('renders pushed events using canonical {seq, type, rid, properties} payload', () => {
    renderPage();
    fireEvent.change(screen.getByTestId('objectset-live-rid-input'), {
      target: { value: 'ri.set.1' },
    });
    fireEvent.click(screen.getByTestId('objectset-live-toggle'));

    const es = MockEventSource.instances[0];
    act(() => {
      es.simulateOpen();
      es.simulateMessage(
        JSON.stringify({
          seq: 1,
          type: 'created',
          rid: 'Order:o1',
          properties: { amount: 100 },
          eventType: 'ADDED_OR_UPDATED',
          object: { __apiName: 'Order', __primaryKey: 'o1', amount: 100 },
        }),
        '1',
      );
      es.simulateMessage(
        JSON.stringify({
          seq: 2,
          type: 'deleted',
          rid: 'Order:o2',
          properties: {},
          eventType: 'DELETED',
          object: { __apiName: 'Order', __primaryKey: 'o2' },
        }),
        '2',
      );
    });

    // Two rows rendered, latest first.
    const row1 = screen.getByTestId('objectset-live-event-1');
    expect(row1.textContent).toMatch(/created/i);
    expect(row1.textContent).toMatch(/Order:o1/);
    const row2 = screen.getByTestId('objectset-live-event-2');
    expect(row2.textContent).toMatch(/deleted/i);
    expect(row2.textContent).toMatch(/Order:o2/);

    // Counter reflects events received.
    expect(
      screen.getByTestId('objectset-live-event-count').textContent,
    ).toMatch(/2/);
  });

  it('shows reconnecting indicator on EventSource error', () => {
    renderPage();
    fireEvent.change(screen.getByTestId('objectset-live-rid-input'), {
      target: { value: 'ri.set.1' },
    });
    fireEvent.click(screen.getByTestId('objectset-live-toggle'));

    const es = MockEventSource.instances[0];
    act(() => {
      es.simulateOpen();
    });
    expect(screen.getByTestId('objectset-live-status')).toHaveAttribute(
      'data-state',
      'connected',
    );

    act(() => {
      es.simulateError();
    });
    expect(screen.getByTestId('objectset-live-status')).toHaveAttribute(
      'data-state',
      'reconnecting',
    );
  });

  it('ignores malformed JSON event payloads without crashing the page', () => {
    renderPage();
    fireEvent.change(screen.getByTestId('objectset-live-rid-input'), {
      target: { value: 'ri.set.1' },
    });
    fireEvent.click(screen.getByTestId('objectset-live-toggle'));

    const es = MockEventSource.instances[0];
    act(() => {
      es.simulateOpen();
      es.simulateMessage('not-json{', '1');
    });
    // No rows added, page still renders.
    expect(screen.getByTestId('objectset-live-events-empty')).toBeInTheDocument();
    expect(
      screen.getByTestId('objectset-live-event-count').textContent,
    ).toMatch(/0/);
  });

  it('picks a saved Object Set via createTemporary flow and populates rid', async () => {
    // react-query schedules microtasks via setTimeout; fake timers
    // would freeze the mutation chain. Swap to real timers just for
    // this assertion.
    vi.useRealTimers();
    const { http, HttpResponse } = await import('msw');
    server.use(
      http.post(
        `/api/v2/ontologies/${ONTOLOGY}/objectSets/createTemporary`,
        () => HttpResponse.json({ objectSetRid: 'ri.temp.sa-1' }),
      ),
    );
    renderPage();
    const picker = screen.getByLabelText(/saved object set/i) as HTMLSelectElement;
    fireEvent.change(picker, { target: { value: 'sa-1' } });

    await waitFor(() => {
      const input = screen.getByTestId('objectset-live-rid-input') as HTMLInputElement;
      expect(input.value).toBe('ri.temp.sa-1');
    });
  });
});
