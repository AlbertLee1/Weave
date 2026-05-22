import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from 'vitest';
import { act, render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { BrowserPage } from '../BrowserPage';
import { useTimeTravelStore } from '../../../stores/timeTravelStore';

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
  onStatusChange?: (status: 'idle' | 'connecting' | 'connected' | 'reconnecting') => void;
} | null = null;

vi.mock('../../../hooks/useWebSocketSubscription', () => ({
  useWebSocketSubscription: (_ontology: string, options: {
    objectType: string;
    enabled: boolean;
    onEvent: (evt: unknown) => void;
    onStatusChange?: (status: 'idle' | 'connecting' | 'connected' | 'reconnecting') => void;
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
          rid: apiName === 'Delivery' ? 'ri.ot.delivery' : 'ri.ot.test',
          apiName,
          displayName: apiName,
          pluralDisplayName: `${apiName}s`,
          primaryKey: 'id',
          status: 'ACTIVE',
          visibility: 'NORMAL',
          titleProperty: 'name',
          properties:
            apiName === 'Delivery'
              ? {
                  id: { dataType: { type: 'string' }, rid: 'ri.p.id' },
                  summary: { dataType: { type: 'string' }, rid: 'ri.p.summary' },
                  deliveredDate: {
                    dataType: { type: 'date' },
                    rid: 'ri.p.deliveredDate',
                  },
                }
              : {
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
  http.get(
    '/api/v2/ontologies/:ontology/objectTypes/byRid/:objectTypeRid/properties',
    ({ params }) => {
      if (params.objectTypeRid === 'ri.ot.delivery') {
        return HttpResponse.json({
          data: [
            {
              rid: 'ri.p.id',
              apiName: 'id',
              baseType: 'string',
              isArray: false,
              isNullable: false,
              isSearchable: true,
              isSortable: true,
            },
            {
              rid: 'ri.p.summary',
              apiName: 'summary',
              baseType: 'string',
              isArray: false,
              isNullable: true,
              isSearchable: true,
              isSortable: false,
            },
            {
              rid: 'ri.p.deliveredDate',
              apiName: 'deliveredDate',
              baseType: 'date',
              isArray: false,
              isNullable: true,
              isSearchable: false,
              isSortable: true,
            },
          ],
        });
      }
      return HttpResponse.json({
        data: [
          {
            rid: 'ri.p.id',
            apiName: 'id',
            baseType: 'string',
            isArray: false,
            isNullable: false,
            isSearchable: true,
            isSortable: true,
          },
          {
            rid: 'ri.p.name',
            apiName: 'name',
            baseType: 'string',
            isArray: false,
            isNullable: true,
            isSearchable: true,
            isSortable: true,
          },
        ],
      });
    },
  ),
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
  http.get('/api/v2/datasets/:ontology/history', () =>
    HttpResponse.json({ data: [] }),
  ),
  http.get('/api/v2/ontologies/:ontology/actionTypes', () =>
    HttpResponse.json({ data: [] }),
  ),
);

beforeAll(() => {
  vi.stubGlobal('EventSource', MockEventSource);
  server.listen();
});
afterEach(() => {
  MockEventSource.instances = [];
  capturedWsOptions = null;
  useTimeTravelStore.setState({ selections: {} });
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
    expect(screen.getByTestId('live-toggle')).toBeInTheDocument();
  });

  it('shows connected status only after WebSocket reports connected', async () => {
    renderBrowserPage();

    await waitFor(() => {
      expect(screen.getByText('Employees')).toBeInTheDocument();
    });

    const toggle = screen.getByTestId('live-toggle');

    // Before toggle — no connected transport indicator
    expect(screen.queryByTestId('realtime-indicator')).not.toBeInTheDocument();

    fireEvent.click(toggle);

    await waitFor(() => {
      expect(screen.getByTestId('live-status')).toHaveTextContent('Connecting');
    });
    expect(screen.queryByTestId('realtime-indicator')).not.toBeInTheDocument();

    act(() => {
      capturedWsOptions!.onStatusChange?.('connected');
    });

    // After transport opens — connected status and green dot visible
    await waitFor(() => {
      expect(screen.getByTestId('live-status')).toHaveTextContent('Connected');
      const indicator = screen.getByTestId('realtime-indicator');
      expect(indicator).toBeInTheDocument();
      expect(indicator).toHaveAttribute(
        'aria-label',
        'Live updates connected over WebSocket',
      );
      expect(indicator.className).toMatch(/animate-pulse/);
    });
  });

  it('shows reconnecting status when the active WebSocket transport reconnects', async () => {
    renderBrowserPage();

    await waitFor(() => {
      expect(screen.getByText('Employees')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('live-toggle'));

    await waitFor(() => {
      expect(capturedWsOptions?.enabled).toBe(true);
      expect(capturedWsOptions?.onStatusChange).toBeDefined();
    });

    act(() => {
      capturedWsOptions!.onStatusChange?.('connected');
    });

    await waitFor(() => {
      expect(screen.getByTestId('live-status')).toHaveTextContent('Connected');
    });

    act(() => {
      capturedWsOptions!.onStatusChange?.('reconnecting');
    });

    await waitFor(() => {
      const status = screen.getByTestId('live-status');
      expect(status).toHaveTextContent('Reconnecting');
      expect(status).toHaveAttribute(
        'aria-label',
        'Live updates reconnecting over WebSocket',
      );
    });
    expect(screen.queryByTestId('realtime-indicator')).not.toBeInTheDocument();
  });

  it('shows connected status when the SSE fallback opens', async () => {
    renderBrowserPage();

    await waitFor(() => {
      expect(screen.getByText('Employees')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('live-toggle'));

    await waitFor(() => {
      expect(capturedWsOptions?.enabled).toBe(true);
      expect(capturedWsOptions?.onStatusChange).toBeDefined();
    });

    act(() => {
      capturedWsOptions!.onStatusChange?.('reconnecting');
    });

    await waitFor(() => {
      expect(MockEventSource.instances).toHaveLength(1);
    });

    act(() => {
      MockEventSource.instances[0].simulateOpen();
    });

    await waitFor(() => {
      const status = screen.getByTestId('live-status');
      expect(status).toHaveTextContent('Connected');
      expect(status).toHaveAttribute(
        'aria-label',
        'Live updates connected over SSE fallback',
      );
      expect(screen.getByTestId('realtime-indicator')).toHaveAttribute(
        'aria-label',
        'Live updates connected over SSE fallback',
      );
    });
  });

  it('explains that Live is unavailable while Time Travel is active', async () => {
    useTimeTravelStore.getState().setAsOf('testOntology', 'tx-abc');

    renderBrowserPage();

    await waitFor(() => {
      expect(screen.getByText('Employees')).toBeInTheDocument();
    });

    expect(screen.getByTestId('live-toggle')).toBeDisabled();
    expect(screen.getByTestId('live-status')).toHaveTextContent('Unavailable');
    expect(screen.getByTestId('live-status')).toHaveAttribute(
      'aria-label',
      'Live updates unavailable while Time Travel is active',
    );
  });

  it('blocks export while Time Travel is active and restores it without resetting query state', async () => {
    useTimeTravelStore.getState().setAsOf('testOntology', 'tx-abc');

    renderBrowserPage();

    await waitFor(() => {
      expect(screen.getByText('Employees')).toBeInTheDocument();
    });

    const exportButton = screen.getByTestId('export-button');
    expect(exportButton).toBeDisabled();
    expect(exportButton).toHaveAttribute(
      'title',
      'Exports are unavailable while Time Travel is active.',
    );

    const searchInput = screen.getByTestId('search-input');
    fireEvent.change(searchInput, { target: { value: 'Alice' } });
    fireEvent.keyDown(searchInput, { key: 'Enter' });

    await waitFor(() => {
      expect(searchInput).toHaveValue('Alice');
    });

    act(() => {
      useTimeTravelStore.getState().clearAsOf('testOntology');
    });

    await waitFor(() => {
      expect(screen.getByTestId('export-button')).toBeEnabled();
    });
    expect(screen.getByTestId('search-input')).toHaveValue('Alice');

    fireEvent.click(screen.getByTestId('export-button'));
    expect(screen.getByTestId('export-csv')).toBeInTheDocument();
    expect(screen.getByTestId('export-json')).toBeInTheDocument();
    expect(screen.getByTestId('export-xlsx')).toBeInTheDocument();
  });

  it('enables WebSocket subscription when toggled on', async () => {
    renderBrowserPage();

    await waitFor(() => {
      expect(screen.getByText('Employees')).toBeInTheDocument();
    });

    // Before toggle — WS disabled
    expect(capturedWsOptions?.enabled).toBe(false);

    fireEvent.click(screen.getByTestId('live-toggle'));

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

    fireEvent.click(screen.getByTestId('live-toggle'));

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

  it('falls back to ObjectSet SSE when the WebSocket transport is reconnecting', async () => {
    const { queryClient } = renderBrowserPage();

    await waitFor(() => {
      expect(screen.getByText('Employees')).toBeInTheDocument();
    });

    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries');

    fireEvent.click(screen.getByTestId('live-toggle'));

    await waitFor(() => {
      expect(capturedWsOptions?.enabled).toBe(true);
      expect(capturedWsOptions?.onStatusChange).toBeDefined();
    });
    expect(MockEventSource.instances).toHaveLength(0);

    act(() => {
      capturedWsOptions!.onStatusChange?.('reconnecting');
    });

    await waitFor(() => {
      expect(MockEventSource.instances).toHaveLength(1);
    });
    expect(MockEventSource.instances[0].url).toBe(
      '/api/v2/ontologies/testOntology/objectSets/ri.objectset.main.test-rid/subscribe',
    );

    MockEventSource.instances[0].simulateMessage(
      JSON.stringify({
        eventType: 'ADDED_OR_UPDATED',
        object: { __primaryKey: '3', __apiName: 'Employee', id: '3' },
      }),
      '7',
    );

    await waitFor(() => {
      expect(invalidateSpy).toHaveBeenCalledWith(
        expect.objectContaining({ queryKey: ['objects'] }),
      );
    });

    invalidateSpy.mockRestore();
  });

  it('keeps ObjectSet SSE disabled while the WebSocket transport is connected', async () => {
    renderBrowserPage();

    await waitFor(() => {
      expect(screen.getByText('Employees')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('live-toggle'));

    await waitFor(() => {
      expect(capturedWsOptions?.enabled).toBe(true);
      expect(capturedWsOptions?.onStatusChange).toBeDefined();
    });

    act(() => {
      capturedWsOptions!.onStatusChange?.('connected');
    });

    await waitFor(() => {
      expect(screen.getByTestId('live-status')).toHaveTextContent('Connected');
    });
    expect(MockEventSource.instances).toHaveLength(0);
  });

  it('disables WebSocket subscription when toggled off', async () => {
    renderBrowserPage();

    await waitFor(() => {
      expect(screen.getByText('Employees')).toBeInTheDocument();
    });

    const toggle = screen.getByTestId('live-toggle');

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

describe('BrowserPage facets', () => {
  function setup(searchHandler: Parameters<typeof http.post>[1]) {
    server.use(
      http.post(
        '/api/v2/ontologies/:ontology/objects/:objectType/search',
        searchHandler,
      ),
    );
    return renderBrowserPage();
  }

  it('falls back to compact object type properties for facet fields when detailed metadata is unavailable', async () => {
    let capturedBody: unknown = null;
    server.use(
      http.get(
        '/api/v2/ontologies/:ontology/objectTypes/byRid/:objectTypeRid/properties',
        () => HttpResponse.json({ message: 'unavailable' }, { status: 500 }),
      ),
    );
    setup(async ({ request }) => {
      capturedBody = await request.json();
      return HttpResponse.json({
        data: [
          { __primaryKey: '1', __apiName: 'Employee', id: '1', name: 'Alice' },
        ],
        totalCount: '1',
        facets: {
          name: [
            { value: 'Alice', count: 3 },
            { value: 'Bob', count: 2 },
          ],
        },
      });
    });

    // Trigger a search by typing in the search input
    const input = await screen.findByTestId('search-input');
    fireEvent.change(input, { target: { value: 'al' } });
    fireEvent.keyDown(input, { key: 'Enter' });

    // Wait for the panel to render
    await waitFor(() => {
      expect(screen.getByTestId('facets-panel')).toBeInTheDocument();
    });

    // Body should include `facets: ["name"]` (id is the primary key, so excluded)
    expect(capturedBody).toMatchObject({ facets: ['name'] });

    // Bucket values + counts visible
    expect(screen.getByLabelText('name: Alice')).toBeInTheDocument();
    expect(screen.getByText('Bob')).toBeInTheDocument();
    expect(screen.getByText('3')).toBeInTheDocument();
  });

  it('prefers detailed searchable property metadata for facet fields', async () => {
    let capturedBody: unknown = null;
    let detailedPropertiesLoaded = false;
    server.use(
      http.get(
        '/api/v2/ontologies/:ontology/objectTypes/byRid/:objectTypeRid/properties',
        ({ params }) => {
          if (params.objectTypeRid !== 'ri.ot.delivery') {
            return HttpResponse.json({ data: [] });
          }
          detailedPropertiesLoaded = true;
          return HttpResponse.json({
            data: [
              {
                rid: 'ri.p.id',
                apiName: 'id',
                baseType: 'string',
                isArray: false,
                isNullable: false,
                isSearchable: true,
                isSortable: true,
              },
              {
                rid: 'ri.p.summary',
                apiName: 'summary',
                baseType: 'string',
                isArray: false,
                isNullable: true,
                isSearchable: false,
                isSortable: false,
              },
              {
                rid: 'ri.p.deliveredDate',
                apiName: 'deliveredDate',
                baseType: 'date',
                isArray: false,
                isNullable: true,
                isSearchable: true,
                isSortable: true,
              },
            ],
          });
        },
      ),
    );
    server.use(
      http.post(
        '/api/v2/ontologies/:ontology/objects/:objectType/search',
        async ({ request }) => {
          capturedBody = await request.json();
          return HttpResponse.json({
            data: [
              {
                __primaryKey: 'delivery-1',
                __apiName: 'Delivery',
                id: 'delivery-1',
                summary: 'Dock received',
                deliveredDate: '2026-05-19',
              },
            ],
            totalCount: '1',
            facets: {
              deliveredDate: [{ value: '2026-05-19', count: 1 }],
            },
          });
        },
      ),
    );

    renderBrowserPage('testOntology', 'Delivery');

    await waitFor(() => {
      expect(detailedPropertiesLoaded).toBe(true);
    });
    const input = await screen.findByTestId('search-input');
    fireEvent.change(input, { target: { value: 'dock' } });
    fireEvent.keyDown(input, { key: 'Enter' });

    await waitFor(() => {
      expect(capturedBody).not.toBeNull();
    });

    expect(capturedBody).toMatchObject({ facets: ['deliveredDate'] });
  });

  it('AND-merges a clicked facet into the where clause and re-fires search', async () => {
    const requestBodies: Array<Record<string, unknown>> = [];
    setup(async ({ request }) => {
      const body = (await request.json()) as Record<string, unknown>;
      requestBodies.push(body);
      return HttpResponse.json({
        data: [
          { __primaryKey: '1', __apiName: 'Employee', id: '1', name: 'Alice' },
        ],
        totalCount: '1',
        facets: {
          name: [
            { value: 'Alice', count: 3 },
            { value: 'Bob', count: 1 },
          ],
        },
      });
    });

    const input = await screen.findByTestId('search-input');
    fireEvent.change(input, { target: { value: 'al' } });
    fireEvent.keyDown(input, { key: 'Enter' });

    await waitFor(() =>
      expect(screen.getByTestId('facets-panel')).toBeInTheDocument(),
    );

    const beforeCount = requestBodies.length;
    fireEvent.click(screen.getByLabelText('name: Alice'));

    await waitFor(() => {
      expect(requestBodies.length).toBeGreaterThan(beforeCount);
    });

    const lastBody = requestBodies[requestBodies.length - 1];
    // The where clause should now include an `and` of the prior text-search and
    // the facet `eq` clause for name=Alice.
    expect(lastBody).toMatchObject({
      where: {
        type: 'and',
        value: expect.arrayContaining([
          expect.objectContaining({ type: 'eq', field: 'name', value: 'Alice' }),
        ]),
      },
    });
  });
});

describe('BrowserPage saved searches', () => {
  it('loads a startsWith filter with a readable chip label and forwards the saved WhereClause', async () => {
    const requestBodies: Array<Record<string, unknown>> = [];
    server.use(
      http.get('/api/v2/saved-searches', () =>
        HttpResponse.json({
          savedSearches: [
            {
              id: 'saved-prefix',
              name: 'A names',
              ontology: 'testOntology',
              objectType: 'Employee',
              createdBy: 'test-user',
              createdAt: '2026-05-19T00:00:00Z',
              updatedAt: '2026-05-19T00:00:00Z',
              definition: {
                filters: [{ field: 'name', op: 'startsWith', value: 'Al' }],
              },
            },
          ],
        }),
      ),
      http.post(
        '/api/v2/ontologies/:ontology/objects/:objectType/search',
        async ({ request }) => {
          requestBodies.push((await request.json()) as Record<string, unknown>);
          return HttpResponse.json({
            data: [
              { __primaryKey: '1', __apiName: 'Employee', id: '1', name: 'Alice' },
            ],
            totalCount: '1',
          });
        },
      ),
    );

    renderBrowserPage();

    fireEvent.click(await screen.findByTestId('saved-search-load-saved-prefix'));

    await waitFor(() => {
      expect(screen.getByText('name starts with:')).toBeInTheDocument();
      expect(screen.getByText('Al')).toBeInTheDocument();
    });

    await waitFor(() => {
      expect(requestBodies).toContainEqual(
        expect.objectContaining({
          where: { type: 'startsWith', field: 'name', value: 'Al' },
        }),
      );
    });
  });
});

describe('BrowserPage sortability', () => {
  it('uses detailed property metadata before sending Browser orderBy requests', async () => {
    const orderByValues: Array<string | null> = [];
    server.use(
      http.get(
        '/api/v2/ontologies/:ontology/objects/:objectType',
        ({ request }) => {
          orderByValues.push(new URL(request.url).searchParams.get('orderBy'));
          return HttpResponse.json({
            data: [
              {
                __primaryKey: 'delivery-1',
                __apiName: 'Delivery',
                id: 'delivery-1',
                summary: 'Dock received',
                deliveredDate: '2026-05-19',
              },
            ],
            totalCount: '1',
          });
        },
      ),
    );

    renderBrowserPage('testOntology', 'Delivery');

    await waitFor(() => {
      expect(screen.getByText('Deliverys')).toBeInTheDocument();
    });
    await screen.findByText('summary');

    fireEvent.click(screen.getByText('summary'));
    await waitFor(() => {
      expect(orderByValues).toContain(null);
    });
    expect(orderByValues).not.toContain('summary:asc');

    await waitFor(() => {
      fireEvent.click(screen.getByText('deliveredDate'));
      expect(orderByValues).toContain('deliveredDate:asc');
    });
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
