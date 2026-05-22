import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { BrowserPage } from '../BrowserPage';

// P2B-003 BDD: Browser live mode provisions a temporary ObjectSet so the
// documented SSE subscription fallback can take over when WebSocket reconnects.
// Operators need a visible failure state when that provisioning step fails.

type MockWebSocketStatus = 'idle' | 'connecting' | 'connected' | 'reconnecting';

let capturedWsOptions: {
  objectType: string;
  enabled: boolean;
  onEvent: (evt: unknown) => void;
  onStatusChange?: (status: MockWebSocketStatus) => void;
} | null = null;

vi.mock('../../../hooks/useWebSocketSubscription', () => ({
  useWebSocketSubscription: (_ontology: string, options: {
    objectType: string;
    enabled: boolean;
    onEvent: (evt: unknown) => void;
    onStatusChange?: (status: MockWebSocketStatus) => void;
  }) => {
    capturedWsOptions = options;
  },
}));

vi.mock('../../../hooks/useObjectTypes', () => ({
  useObjectType: (_ontology: string, apiName: string) => ({
    data: apiName
      ? {
          rid: 'ri.object-type.demo.task',
          apiName,
          displayName: 'Task',
          pluralDisplayName: 'Tasks',
          primaryKey: 'id',
          titleProperty: 'name',
          status: 'ACTIVE',
          visibility: 'NORMAL',
          properties: {
            id: { dataType: { type: 'string' }, rid: 'ri.property.id' },
            name: { dataType: { type: 'string' }, rid: 'ri.property.name' },
          },
        }
      : undefined,
    isLoading: false,
  }),
  useOutgoingLinkTypes: () => ({ data: [], isLoading: false }),
}));

type EventSourceListener = (evt: MessageEvent) => void;

class MockEventSource {
  static instances: MockEventSource[] = [];

  url: string;
  readyState = 0;
  onmessage: EventSourceListener | null = null;
  onerror: ((evt: Event) => void) | null = null;
  onopen: (() => void) | null = null;
  closed = false;

  constructor(url: string) {
    this.url = url;
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
}

(MockEventSource as unknown as Record<string, number>).CONNECTING = 0;
(MockEventSource as unknown as Record<string, number>).OPEN = 1;
(MockEventSource as unknown as Record<string, number>).CLOSED = 2;

let createTemporaryAttempts = 0;
let createTemporaryMode: 'fail' | 'fail-then-success' = 'fail';

const server = setupServer(
  http.get(
    '/api/v2/ontologies/:ontology/objectTypes/byRid/:objectTypeRid/properties',
    () =>
      HttpResponse.json({
        data: [
          {
            rid: 'ri.property.id',
            apiName: 'id',
            baseType: 'string',
            isArray: false,
            isNullable: false,
            isSearchable: true,
            isSortable: true,
          },
          {
            rid: 'ri.property.name',
            apiName: 'name',
            baseType: 'string',
            isArray: false,
            isNullable: false,
            isSearchable: true,
            isSortable: true,
          },
        ],
      }),
  ),
  http.get('/api/v2/ontologies/:ontology/objects/:objectType', () =>
    HttpResponse.json({
      data: [
        {
          __primaryKey: 'task-1',
          __apiName: 'Task',
          id: 'task-1',
          name: 'Open task',
        },
      ],
      totalCount: '1',
    }),
  ),
  http.post(
    '/api/v2/ontologies/:ontology/objectSets/createTemporary',
    () => {
      createTemporaryAttempts += 1;
      if (
        createTemporaryMode === 'fail' ||
        (createTemporaryMode === 'fail-then-success' &&
          createTemporaryAttempts === 1)
      ) {
        return HttpResponse.json(
          {
            errorCode: 'SSESubscribeNotConfigured',
            errorName: 'SSE subscribe not configured',
            errorInstanceId: 'err-live-fallback',
            parameters: { reason: 'NATS unavailable' },
          },
          { status: 500 },
        );
      }
      return HttpResponse.json({
        objectSetRid: 'ri.objectset.retry-success',
      });
    },
  ),
  http.get('/api/v2/saved-searches', () =>
    HttpResponse.json({ savedSearches: [] }),
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
  server.listen({ onUnhandledRequest: 'error' });
});

afterEach(() => {
  createTemporaryAttempts = 0;
  createTemporaryMode = 'fail';
  capturedWsOptions = null;
  MockEventSource.instances = [];
  server.resetHandlers();
});

afterAll(() => {
  server.close();
  vi.unstubAllGlobals();
});

function renderBrowserPage() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/browser/demo/Task']}>
        <Routes>
          <Route path="/browser/:ontology/:objectType" element={<BrowserPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BDD: BrowserPage live SSE fallback failures (P2B-003)', () => {
  it('Given Live mode is enabled and WebSocket reconnects, When ObjectSet provisioning fails, Then the header shows a failed fallback status', async () => {
    renderBrowserPage();

    expect(await screen.findByText('Open task')).toBeInTheDocument();

    fireEvent.click(screen.getByTestId('live-toggle'));

    await waitFor(() => {
      expect(createTemporaryAttempts).toBe(1);
      expect(capturedWsOptions?.enabled).toBe(true);
    });

    act(() => {
      capturedWsOptions!.onStatusChange?.('reconnecting');
    });

    await waitFor(() => {
      const status = screen.getByTestId('live-status');
      expect(status).toHaveTextContent('Fallback failed');
      expect(status).toHaveAttribute(
        'aria-label',
        'Live updates failed to provision SSE fallback: SSESubscribeNotConfigured: SSE subscribe not configured - NATS unavailable',
      );
    });
    expect(screen.queryByTestId('realtime-indicator')).not.toBeInTheDocument();
    expect(MockEventSource.instances).toHaveLength(0);
  });

  it('Given fallback provisioning failed, When Live is turned off and on again, Then the error clears and provisioning is retried', async () => {
    createTemporaryMode = 'fail-then-success';
    renderBrowserPage();

    expect(await screen.findByText('Open task')).toBeInTheDocument();

    fireEvent.click(screen.getByTestId('live-toggle'));
    await waitFor(() => {
      expect(createTemporaryAttempts).toBe(1);
    });

    act(() => {
      capturedWsOptions!.onStatusChange?.('reconnecting');
    });

    await waitFor(() => {
      expect(screen.getByTestId('live-status')).toHaveTextContent(
        'Fallback failed',
      );
    });

    fireEvent.click(screen.getByTestId('live-toggle'));
    await waitFor(() => {
      expect(screen.queryByTestId('live-status')).not.toBeInTheDocument();
      expect(capturedWsOptions?.enabled).toBe(false);
    });

    fireEvent.click(screen.getByTestId('live-toggle'));
    await waitFor(() => {
      expect(createTemporaryAttempts).toBe(2);
      expect(screen.getByTestId('live-status')).toHaveTextContent('Connecting');
    });
    expect(screen.getByTestId('live-status')).not.toHaveTextContent(
      'Fallback failed',
    );

    act(() => {
      capturedWsOptions!.onStatusChange?.('reconnecting');
    });

    await waitFor(() => {
      expect(MockEventSource.instances).toHaveLength(1);
    });
    expect(MockEventSource.instances[0].url).toBe(
      '/api/v2/ontologies/demo/objectSets/ri.objectset.retry-success/subscribe',
    );

    act(() => {
      MockEventSource.instances[0].simulateOpen();
    });

    await waitFor(() => {
      expect(screen.getByTestId('live-status')).toHaveTextContent('Connected');
      expect(screen.getByTestId('live-status')).toHaveAttribute(
        'aria-label',
        'Live updates connected over SSE fallback',
      );
    });
  });
});
