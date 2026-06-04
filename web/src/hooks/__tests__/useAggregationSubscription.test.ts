import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useAggregationSubscription } from '../useAggregationSubscription';

type MessageListener = (evt: MessageEvent) => void;
type CloseListener = (evt: CloseEvent) => void;
type OpenListener = (evt: Event) => void;
type ErrorListener = (evt: Event) => void;

class MockWebSocket {
  static instances: MockWebSocket[] = [];
  static readonly OPEN = 1;
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
    this.readyState = 0;
    MockWebSocket.instances.push(this);
  }

  send(data: string) {
    this.sentMessages.push(data);
  }

  close() {
    this.closed = true;
    this.readyState = MockWebSocket.CLOSED;
  }

  simulateOpen() {
    this.readyState = MockWebSocket.OPEN;
    this.onopen?.(new Event('open'));
  }

  simulateMessage(data: unknown) {
    this.onmessage?.(new MessageEvent('message', { data: JSON.stringify(data) }));
  }

  simulateClose(code = 1006) {
    this.readyState = MockWebSocket.CLOSED;
    this.closed = true;
    this.onclose?.(new CloseEvent('close', { code }));
  }
}

describe('useAggregationSubscription', () => {
  beforeEach(() => {
    MockWebSocket.instances = [];
    vi.stubGlobal('WebSocket', MockWebSocket);
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('does not open a socket when disabled', () => {
    renderHook(() =>
      useAggregationSubscription('onto', {
        objectType: 'Order',
        metric: { type: 'count' },
        enabled: false,
        onSnapshot: vi.fn(),
      }),
    );
    expect(MockWebSocket.instances).toHaveLength(0);
  });

  it('sends subscribeAggregation with metric/where/groupBy after welcome', () => {
    renderHook(() =>
      useAggregationSubscription('onto', {
        objectType: 'Order',
        metric: { type: 'sum', field: 'total', name: 'revenue' },
        where: { type: 'eq', field: 'status', value: 'open' },
        groupBy: 'country',
        enabled: true,
        onSnapshot: vi.fn(),
      }),
    );
    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();
    ws.simulateMessage({ type: 'welcome', connectionId: 'c1' });

    expect(ws.sentMessages).toHaveLength(1);
    const msg = JSON.parse(ws.sentMessages[0]);
    expect(msg.type).toBe('subscribeAggregation');
    expect(msg.objectType).toBe('Order');
    expect(msg.metric).toEqual({ type: 'sum', field: 'total', name: 'revenue' });
    expect(msg.where).toEqual({ type: 'eq', field: 'status', value: 'open' });
    expect(msg.groupBy).toBe('country');
  });

  it('omits field for count metrics and defaults name to type', () => {
    renderHook(() =>
      useAggregationSubscription('onto', {
        objectType: 'Order',
        metric: { type: 'count' },
        enabled: true,
        onSnapshot: vi.fn(),
      }),
    );
    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();
    ws.simulateMessage({ type: 'welcome', connectionId: 'c1' });
    const msg = JSON.parse(ws.sentMessages[0]);
    expect(msg.metric).toEqual({ type: 'count', name: 'count' });
    expect(msg.metric.field).toBeUndefined();
  });

  it('normalizes aggregationChanged snapshots into the result shape', () => {
    const onSnapshot = vi.fn();
    renderHook(() =>
      useAggregationSubscription('onto', {
        objectType: 'Order',
        metric: { type: 'count' },
        enabled: true,
        onSnapshot,
      }),
    );
    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();
    ws.simulateMessage({ type: 'welcome', connectionId: 'c1' });
    ws.simulateMessage({
      type: 'aggregationChanged',
      subscriptionId: 's1',
      data: {
        state: {
          data: [{ group: { country: 'USA' }, metrics: [{ name: 'count', value: 4 }] }],
          accuracy: 'ACCURATE',
        },
      },
    });

    expect(onSnapshot).toHaveBeenCalledTimes(1);
    expect(onSnapshot).toHaveBeenCalledWith({
      accuracy: 'ACCURATE',
      excludedItems: undefined,
      data: [{ group: { country: 'USA' }, metrics: { count: 4 } }],
    });
  });

  it('reconnects with exponential backoff and re-subscribes', () => {
    renderHook(() =>
      useAggregationSubscription('onto', {
        objectType: 'Order',
        metric: { type: 'count' },
        enabled: true,
        onSnapshot: vi.fn(),
      }),
    );
    const ws1 = MockWebSocket.instances[0];
    ws1.simulateOpen();
    ws1.simulateClose(1006);
    expect(MockWebSocket.instances).toHaveLength(1);
    act(() => { vi.advanceTimersByTime(1000); });
    expect(MockWebSocket.instances).toHaveLength(2);

    const ws2 = MockWebSocket.instances[1];
    ws2.simulateOpen();
    ws2.simulateMessage({ type: 'welcome', connectionId: 'c2' });
    expect(JSON.parse(ws2.sentMessages[0]).type).toBe('subscribeAggregation');
  });

  it('does not reconnect on normal closure (1000)', () => {
    renderHook(() =>
      useAggregationSubscription('onto', {
        objectType: 'Order',
        metric: { type: 'count' },
        enabled: true,
        onSnapshot: vi.fn(),
      }),
    );
    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();
    ws.simulateClose(1000);
    act(() => { vi.advanceTimersByTime(60000); });
    expect(MockWebSocket.instances).toHaveLength(1);
  });

  it('closes the socket on unmount (no leak)', () => {
    const { unmount } = renderHook(() =>
      useAggregationSubscription('onto', {
        objectType: 'Order',
        metric: { type: 'count' },
        enabled: true,
        onSnapshot: vi.fn(),
      }),
    );
    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();
    expect(ws.closed).toBe(false);
    unmount();
    expect(ws.closed).toBe(true);
  });

  it('reconnects with a fresh socket when the metric changes', () => {
    const { rerender } = renderHook(
      ({ field }: { field: string }) =>
        useAggregationSubscription('onto', {
          objectType: 'Order',
          metric: { type: 'sum', field, name: 'm' },
          enabled: true,
          onSnapshot: vi.fn(),
        }),
      { initialProps: { field: 'a' } },
    );
    const first = MockWebSocket.instances[0];
    first.simulateOpen();
    expect(first.closed).toBe(false);

    rerender({ field: 'b' });
    expect(first.closed).toBe(true);
    expect(MockWebSocket.instances).toHaveLength(2);

    const second = MockWebSocket.instances[1];
    second.simulateOpen();
    second.simulateMessage({ type: 'welcome', connectionId: 'c2' });
    expect(JSON.parse(second.sentMessages[0]).metric.field).toBe('b');
  });
});
