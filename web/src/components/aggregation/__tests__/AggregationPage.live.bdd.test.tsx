import { describe, it, expect, beforeAll, afterAll, afterEach, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AggregationPage } from '../AggregationPage';

// ---------------------------------------------------------------------------
// Mock WebSocket — mirrors useWebSocketSubscription.test.ts so the live
// aggregation hook is exercised end-to-end through the rendered page.
// ---------------------------------------------------------------------------

type MessageListener = (evt: MessageEvent) => void;
type CloseListener = (evt: CloseEvent) => void;
type OpenListener = (evt: Event) => void;
type ErrorListener = (evt: Event) => void;

class MockWebSocket {
  static instances: MockWebSocket[] = [];
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;

  url: string;
  readyState: number;
  onmessage: MessageListener | null = null;
  onclose: CloseListener | null = null;
  onopen: OpenListener | null = null;
  onerror: ErrorListener | null = null;
  sentMessages: string[] = [];
  closed = false;

  constructor(url: string) {
    this.url = url;
    this.readyState = MockWebSocket.CONNECTING;
    MockWebSocket.instances.push(this);
  }

  send(data: string) {
    this.sentMessages.push(data);
  }

  close() {
    this.closed = true;
    this.readyState = MockWebSocket.CLOSED;
    // Emit a normal-closure event so the hook does not schedule a reconnect.
    this.onclose?.(new CloseEvent('close', { code: 1000 }));
  }

  simulateOpen() {
    this.readyState = MockWebSocket.OPEN;
    this.onopen?.(new Event('open'));
  }

  simulateMessage(data: unknown) {
    this.onmessage?.(new MessageEvent('message', { data: JSON.stringify(data) }));
  }
}

const objectTypeBody = {
  rid: 'ri.ontology.main.object-type.order',
  apiName: 'order',
  displayName: 'Order',
  primaryKey: 'orderID',
  status: 'ACTIVE',
  visibility: 'PROMINENT',
  properties: {
    orderID: { dataType: { type: 'string' }, rid: 'ri.p.order.id' },
    shipCountry: { dataType: { type: 'string' }, rid: 'ri.p.order.country' },
  },
};

const server = setupServer(
  http.get('/api/v2/ontologies/:ontology/objectTypes/:objectType', () =>
    HttpResponse.json(objectTypeBody),
  ),
  // One-shot aggregate: the initial (non-live) Execute path returns count 5.
  http.post('/api/v2/ontologies/:ontology/objects/:objectType/aggregate', () =>
    HttpResponse.json({
      data: [{ metrics: [{ name: 'count', value: 5 }] }],
      accuracy: 'ACCURATE',
    }),
  ),
);

beforeAll(() => server.listen());
beforeEach(() => {
  MockWebSocket.instances = [];
  vi.stubGlobal('WebSocket', MockWebSocket);
});
afterEach(() => {
  server.resetHandlers();
  vi.unstubAllGlobals();
});
afterAll(() => server.close());

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/aggregation/northwind/order']}>
        <Routes>
          <Route path="/aggregation/:ontology/:objectType" element={<AggregationPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

// Drives the welcome → subscribed → aggregationChanged handshake on the most
// recent mock socket, mirroring the server's emission order.
function pushAggregation(value: number) {
  const ws = MockWebSocket.instances[MockWebSocket.instances.length - 1];
  ws.simulateOpen();
  ws.simulateMessage({ type: 'welcome', connectionId: 'conn-1' });
  ws.simulateMessage({ type: 'subscribed', subscriptionId: 'sub-1' });
  ws.simulateMessage({
    type: 'aggregationChanged',
    subscriptionId: 'sub-1',
    data: { state: { data: [{ metrics: [{ name: 'count', value }] }], accuracy: 'ACCURATE' } },
  });
  return ws;
}

describe('BDD: AggregationPage live updates', () => {
  it('Given the live toggle is off, Then no WebSocket is opened', async () => {
    renderPage();
    await screen.findByTestId('aggregation-execute');
    expect(MockWebSocket.instances).toHaveLength(0);
  });

  it('When Live is enabled and an aggregationChanged event arrives, Then results refresh from the live snapshot', async () => {
    renderPage();
    const toggle = await screen.findByTestId('aggregation-live-toggle');

    // Given the user enables Live
    fireEvent.click(toggle);

    // A subscribe message is sent over the aggregation WS endpoint
    await waitFor(() => expect(MockWebSocket.instances).toHaveLength(1));
    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();
    ws.simulateMessage({ type: 'welcome', connectionId: 'conn-1' });
    await waitFor(() => expect(ws.sentMessages).toHaveLength(1));
    const sub = JSON.parse(ws.sentMessages[0]);
    expect(sub.type).toBe('subscribeAggregation');
    expect(sub.objectType).toBe('order');
    expect(sub.metric).toEqual({ type: 'count', name: 'count' });

    // When an aggregationChanged snapshot arrives with count=7
    ws.simulateMessage({ type: 'subscribed', subscriptionId: 'sub-1' });
    ws.simulateMessage({
      type: 'aggregationChanged',
      subscriptionId: 'sub-1',
      data: { state: { data: [{ metrics: [{ name: 'count', value: 7 }] }], accuracy: 'ACCURATE' } },
    });

    // Then the page renders the live result
    const results = await screen.findByTestId('aggregation-results');
    expect(results.getAttribute('data-bucket-count')).toBe('1');
    await waitFor(() => expect(screen.getByText('7')).toBeInTheDocument());
  });

  it('When a second live snapshot arrives, Then the displayed result updates again', async () => {
    renderPage();
    fireEvent.click(await screen.findByTestId('aggregation-live-toggle'));
    await waitFor(() => expect(MockWebSocket.instances).toHaveLength(1));

    pushAggregation(3);
    await waitFor(() => expect(screen.getByText('3')).toBeInTheDocument());

    const ws = MockWebSocket.instances[0];
    ws.simulateMessage({
      type: 'aggregationChanged',
      subscriptionId: 'sub-1',
      data: { state: { data: [{ metrics: [{ name: 'count', value: 9 }] }], accuracy: 'ACCURATE' } },
    });
    await waitFor(() => expect(screen.getByText('9')).toBeInTheDocument());
  });

  it('When Live is disabled again, Then the WebSocket is closed (no leak)', async () => {
    renderPage();
    const toggle = await screen.findByTestId('aggregation-live-toggle');
    fireEvent.click(toggle);
    await waitFor(() => expect(MockWebSocket.instances).toHaveLength(1));
    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();

    fireEvent.click(toggle);
    await waitFor(() => expect(ws.closed).toBe(true));
  });

  it('When the page unmounts while Live, Then the subscription is cleaned up', async () => {
    const { unmount } = renderPage();
    fireEvent.click(await screen.findByTestId('aggregation-live-toggle'));
    await waitFor(() => expect(MockWebSocket.instances).toHaveLength(1));
    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();

    unmount();
    await waitFor(() => expect(ws.closed).toBe(true));
  });
});
