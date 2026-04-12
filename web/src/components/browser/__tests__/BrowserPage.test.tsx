import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { BrowserPage } from '../BrowserPage';

// ---------------------------------------------------------------------------
// Mock EventSource
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
}

(MockEventSource as unknown as Record<string, number>).CONNECTING = 0;
(MockEventSource as unknown as Record<string, number>).OPEN = 1;
(MockEventSource as unknown as Record<string, number>).CLOSED = 2;

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock('../../../hooks/useObjectTypes', () => ({
  useObjectType: (_ontology: string, apiName: string) => ({
    data: apiName
      ? {
          rid: 'ri.ot.test',
          apiName,
          displayName: apiName,
          pluralDisplayName: `${apiName}s`,
          primaryKey: 'id',
          status: 'ACTIVE',
          visibility: 'NORMAL',
          titleProperty: 'name',
          properties: {
            id: { dataType: { type: 'string' }, rid: 'ri.p.id' },
            name: { dataType: { type: 'string' }, rid: 'ri.p.name' },
          },
        }
      : undefined,
    isLoading: false,
  }),
  useOutgoingLinkTypes: () => ({ data: [], isLoading: false }),
}));

// ---------------------------------------------------------------------------
// MSW server
// ---------------------------------------------------------------------------

const server = setupServer(
  // List objects
  http.get('/api/v2/ontologies/:ontology/objects/:objectType', () =>
    HttpResponse.json({
      data: [
        { __primaryKey: '1', __apiName: 'Employee', id: '1', name: 'Alice' },
        { __primaryKey: '2', __apiName: 'Employee', id: '2', name: 'Bob' },
      ],
      totalCount: '2',
    }),
  ),
  // Search objects
  http.post('/api/v2/ontologies/:ontology/objects/:objectType/search', () =>
    HttpResponse.json({
      data: [
        { __primaryKey: '1', __apiName: 'Employee', id: '1', name: 'Alice' },
      ],
      totalCount: '1',
    }),
  ),
  // Create temporary ObjectSet
  http.post(
    '/api/v2/ontologies/:ontology/objectSets/createTemporary',
    () => HttpResponse.json({ objectSetRid: 'ri.objectset.main.test-rid' }),
  ),
);

beforeAll(() => {
  vi.stubGlobal('EventSource', MockEventSource);
  server.listen();
});
afterEach(() => {
  MockEventSource.instances = [];
  server.resetHandlers();
});
afterAll(() => {
  server.close();
  vi.unstubAllGlobals();
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function renderBrowserPage(
  ontology = 'testOntology',
  objectType = 'Employee',
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const utils = render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter
        initialEntries={[`/browser/${ontology}/${objectType}`]}
      >
        <Routes>
          <Route
            path="/browser/:ontology/:objectType"
            element={<BrowserPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { ...utils, queryClient };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('BrowserPage realtime mode', () => {
  it('renders a Realtime toggle in the header', async () => {
    renderBrowserPage();

    await waitFor(() => {
      expect(screen.getByText('Employees')).toBeInTheDocument();
    });

    // Toggle should be present
    expect(screen.getByLabelText(/realtime/i)).toBeInTheDocument();
  });

  it('shows a pulsing green indicator when realtime mode is enabled', async () => {
    renderBrowserPage();

    await waitFor(() => {
      expect(screen.getByText('Employees')).toBeInTheDocument();
    });

    const toggle = screen.getByLabelText(/realtime/i);

    // Before toggle — no green dot
    expect(screen.queryByTestId('realtime-indicator')).not.toBeInTheDocument();

    fireEvent.click(toggle);

    // After toggle — green dot visible
    await waitFor(() => {
      const indicator = screen.getByTestId('realtime-indicator');
      expect(indicator).toBeInTheDocument();
      expect(indicator.className).toMatch(/animate-pulse/);
    });
  });

  it('creates a temporary ObjectSet and subscribes via SSE when toggled on', async () => {
    renderBrowserPage();

    await waitFor(() => {
      expect(screen.getByText('Employees')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByLabelText(/realtime/i));

    // Wait for EventSource to be created (after createTemporary resolves)
    await waitFor(() => {
      expect(MockEventSource.instances.length).toBeGreaterThan(0);
    });

    const es = MockEventSource.instances[0];
    expect(es.url).toContain('/subscribe');
    expect(es.url).toContain('ri.objectset.main.test-rid');
  });

  it('invalidates TanStack Query cache on SSE event', async () => {
    const { queryClient } = renderBrowserPage();

    await waitFor(() => {
      expect(screen.getByText('Employees')).toBeInTheDocument();
    });

    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries');

    fireEvent.click(screen.getByLabelText(/realtime/i));

    await waitFor(() => {
      expect(MockEventSource.instances.length).toBeGreaterThan(0);
    });

    const es = MockEventSource.instances[0];
    es.simulateOpen();
    es.simulateMessage(
      JSON.stringify({
        eventType: 'ADDED_OR_UPDATED',
        object: { __primaryKey: '3', __apiName: 'Employee', id: '3', name: 'Charlie' },
      }),
      '1',
    );

    await waitFor(() => {
      expect(invalidateSpy).toHaveBeenCalledWith(
        expect.objectContaining({ queryKey: ['objects'] }),
      );
    });

    invalidateSpy.mockRestore();
  });

  it('closes EventSource when realtime mode is toggled off', async () => {
    renderBrowserPage();

    await waitFor(() => {
      expect(screen.getByText('Employees')).toBeInTheDocument();
    });

    const toggle = screen.getByLabelText(/realtime/i);

    // Toggle on
    fireEvent.click(toggle);
    await waitFor(() => {
      expect(MockEventSource.instances.length).toBeGreaterThan(0);
    });

    const es = MockEventSource.instances[0];
    expect(es.closed).toBe(false);

    // Toggle off
    fireEvent.click(toggle);

    await waitFor(() => {
      expect(screen.queryByTestId('realtime-indicator')).not.toBeInTheDocument();
    });
  });
});
