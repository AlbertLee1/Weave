import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { BrowserPage } from '../BrowserPage';

// ---------------------------------------------------------------------------
// Mock EventSource (for SSE fallback path)
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
// Mock useWebSocketSubscription — the hook is unit-tested separately;
// here we only verify BrowserPage integration via the captured onEvent.
// ---------------------------------------------------------------------------

let capturedWsOptions: {
  objectType: string;
  enabled: boolean;
  onEvent: (evt: unknown) => void;
} | null = null;

vi.mock('../../../hooks/useWebSocketSubscription', () => ({
  useWebSocketSubscription: (_ontology: string, options: {
    objectType: string;
    enabled: boolean;
    onEvent: (evt: unknown) => void;
  }) => {
    capturedWsOptions = options;
  },
}));

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
  capturedWsOptions = null;
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
  it('renders a Live toggle in the header', async () => {
    renderBrowserPage();

    await waitFor(() => {
      expect(screen.getByText('Employees')).toBeInTheDocument();
    });

    // Toggle should be present with "Live" label
    expect(screen.getByLabelText(/live/i)).toBeInTheDocument();
  });

  it('shows a pulsing green indicator when Live mode is enabled', async () => {
    renderBrowserPage();

    await waitFor(() => {
      expect(screen.getByText('Employees')).toBeInTheDocument();
    });

    const toggle = screen.getByLabelText(/live/i);

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

  it('enables WebSocket subscription when toggled on', async () => {
    renderBrowserPage();

    await waitFor(() => {
      expect(screen.getByText('Employees')).toBeInTheDocument();
    });

    // Before toggle — WS disabled
    expect(capturedWsOptions?.enabled).toBe(false);

    fireEvent.click(screen.getByLabelText(/live/i));

    // After toggle — WS enabled with correct objectType
    await waitFor(() => {
      expect(capturedWsOptions?.enabled).toBe(true);
    });
    expect(capturedWsOptions?.objectType).toBe('Employee');
  });

  it('invalidates TanStack Query cache on WebSocket objectChanged event', async () => {
    const { queryClient } = renderBrowserPage();

    await waitFor(() => {
      expect(screen.getByText('Employees')).toBeInTheDocument();
    });

    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries');

    fireEvent.click(screen.getByLabelText(/live/i));

    await waitFor(() => {
      expect(capturedWsOptions?.enabled).toBe(true);
    });

    // Simulate a WebSocket objectChanged event via the captured onEvent callback
    capturedWsOptions!.onEvent({
      state: 'ADDED_OR_UPDATED',
      object: { __primaryKey: '3', __apiName: 'Employee', id: '3', name: 'Charlie' },
    });

    await waitFor(() => {
      expect(invalidateSpy).toHaveBeenCalledWith(
        expect.objectContaining({ queryKey: ['objects'] }),
      );
    });

    invalidateSpy.mockRestore();
  });

  it('disables WebSocket subscription when toggled off', async () => {
    renderBrowserPage();

    await waitFor(() => {
      expect(screen.getByText('Employees')).toBeInTheDocument();
    });

    const toggle = screen.getByLabelText(/live/i);

    // Toggle on
    fireEvent.click(toggle);
    await waitFor(() => {
      expect(capturedWsOptions?.enabled).toBe(true);
    });

    // Toggle off
    fireEvent.click(toggle);

    await waitFor(() => {
      expect(screen.queryByTestId('realtime-indicator')).not.toBeInTheDocument();
    });

    expect(capturedWsOptions?.enabled).toBe(false);
  });
});

describe('BrowserPage view-mode toggle', () => {
  it('defaults to table view and hides the map', async () => {
    renderBrowserPage();
    await waitFor(() => {
      expect(screen.getByText('Employees')).toBeInTheDocument();
    });
    const tableBtn = screen.getByTestId('view-mode-table');
    const mapBtn = screen.getByTestId('view-mode-map');
    expect(tableBtn.getAttribute('aria-pressed')).toBe('true');
    expect(mapBtn.getAttribute('aria-pressed')).toBe('false');
    expect(screen.queryByTestId('map-view')).not.toBeInTheDocument();
    expect(screen.queryByTestId('map-view-empty')).not.toBeInTheDocument();
  });

  it('swaps to the map view when the Map button is clicked', async () => {
    renderBrowserPage();
    await waitFor(() => {
      expect(screen.getByText('Employees')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTestId('view-mode-map'));
    // The mock ObjectType exposes only string properties → no geo available.
    await waitFor(() => {
      expect(screen.getByTestId('map-view-empty')).toBeInTheDocument();
    });
    expect(screen.getByTestId('view-mode-map').getAttribute('aria-pressed')).toBe(
      'true',
    );
  });
});
