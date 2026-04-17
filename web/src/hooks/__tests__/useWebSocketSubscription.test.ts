import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useWebSocketSubscription } from '../useWebSocketSubscription';

// ---------------------------------------------------------------------------
// Mock WebSocket
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

  close(_code?: number, _reason?: string) {
    this.closed = true;
    this.readyState = MockWebSocket.CLOSED;
  }

  // Test helpers -------------------------------------------------------

  simulateOpen() {
    this.readyState = MockWebSocket.OPEN;
    this.onopen?.(new Event('open'));
  }

  simulateMessage(data: unknown) {
    const evt = new MessageEvent('message', {
      data: JSON.stringify(data),
    });
    this.onmessage?.(evt);
  }

  simulateClose(code = 1006, reason = '') {
    this.readyState = MockWebSocket.CLOSED;
    this.closed = true;
    this.onclose?.(new CloseEvent('close', { code, reason }));
  }

  simulateError() {
    this.onerror?.(new Event('error'));
  }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('useWebSocketSubscription', () => {
  beforeEach(() => {
    MockWebSocket.instances = [];
    vi.stubGlobal('WebSocket', MockWebSocket);
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('creates a WebSocket connected to the correct URL', () => {
    renderHook(() =>
      useWebSocketSubscription('myOntology', {
        objectType: 'Employee',
        enabled: true,
        onEvent: vi.fn(),
      }),
    );

    expect(MockWebSocket.instances).toHaveLength(1);
    expect(MockWebSocket.instances[0].url).toContain(
      '/api/v2/ontologies/myOntology/subscriptions/ws',
    );
  });

  it('does not create WebSocket when enabled=false', () => {
    renderHook(() =>
      useWebSocketSubscription('myOntology', {
        objectType: 'Employee',
        enabled: false,
        onEvent: vi.fn(),
      }),
    );

    expect(MockWebSocket.instances).toHaveLength(0);
  });

  it('does not create WebSocket when ontology is empty', () => {
    renderHook(() =>
      useWebSocketSubscription('', {
        objectType: 'Employee',
        enabled: true,
        onEvent: vi.fn(),
      }),
    );

    expect(MockWebSocket.instances).toHaveLength(0);
  });

  it('sends subscribe message after receiving welcome', () => {
    renderHook(() =>
      useWebSocketSubscription('myOntology', {
        objectType: 'Employee',
        enabled: true,
        onEvent: vi.fn(),
      }),
    );

    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();

    // Server sends welcome
    ws.simulateMessage({ type: 'welcome', connectionId: 'conn-1' });

    // Hook should send subscribe
    expect(ws.sentMessages).toHaveLength(1);
    const msg = JSON.parse(ws.sentMessages[0]);
    expect(msg.type).toBe('subscribe');
    expect(msg.objectType).toBe('Employee');
  });

  it('sends subscribe message with where and select when provided', () => {
    const whereClause = { type: 'eq', field: 'status', value: 'active' };
    renderHook(() =>
      useWebSocketSubscription('myOntology', {
        objectType: 'Employee',
        where: whereClause,
        select: ['name', 'email'],
        enabled: true,
        onEvent: vi.fn(),
      }),
    );

    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();
    ws.simulateMessage({ type: 'welcome', connectionId: 'conn-1' });

    const msg = JSON.parse(ws.sentMessages[0]);
    expect(msg.type).toBe('subscribe');
    expect(msg.objectType).toBe('Employee');
    expect(msg.where).toEqual(whereClause);
    expect(msg.select).toEqual(['name', 'email']);
  });

  it('calls onEvent for objectChanged messages', () => {
    const onEvent = vi.fn();
    renderHook(() =>
      useWebSocketSubscription('myOntology', {
        objectType: 'Employee',
        enabled: true,
        onEvent,
      }),
    );

    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();
    ws.simulateMessage({ type: 'welcome', connectionId: 'conn-1' });
    ws.simulateMessage({ type: 'subscribed', subscriptionId: 'sub-1' });

    // Simulate objectChanged event
    const changeEvent = {
      type: 'objectChanged',
      subscriptionId: 'sub-1',
      data: {
        state: 'ADDED_OR_UPDATED',
        object: { id: '1', name: 'Alice' },
      },
    };
    ws.simulateMessage(changeEvent);

    expect(onEvent).toHaveBeenCalledTimes(1);
    expect(onEvent).toHaveBeenCalledWith({
      state: 'ADDED_OR_UPDATED',
      object: { id: '1', name: 'Alice' },
    });
  });

  it('calls onEvent for multiple objectChanged messages', () => {
    const onEvent = vi.fn();
    renderHook(() =>
      useWebSocketSubscription('myOntology', {
        objectType: 'Employee',
        enabled: true,
        onEvent,
      }),
    );

    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();
    ws.simulateMessage({ type: 'welcome', connectionId: 'conn-1' });
    ws.simulateMessage({ type: 'subscribed', subscriptionId: 'sub-1' });

    ws.simulateMessage({
      type: 'objectChanged',
      subscriptionId: 'sub-1',
      data: { state: 'ADDED_OR_UPDATED', object: { id: '1' } },
    });
    ws.simulateMessage({
      type: 'objectChanged',
      subscriptionId: 'sub-1',
      data: { state: 'DELETED', object: { id: '2' } },
    });

    expect(onEvent).toHaveBeenCalledTimes(2);
    expect(onEvent).toHaveBeenNthCalledWith(1, {
      state: 'ADDED_OR_UPDATED',
      object: { id: '1' },
    });
    expect(onEvent).toHaveBeenNthCalledWith(2, {
      state: 'DELETED',
      object: { id: '2' },
    });
  });

  it('closes WebSocket on unmount', () => {
    const { unmount } = renderHook(() =>
      useWebSocketSubscription('myOntology', {
        objectType: 'Employee',
        enabled: true,
        onEvent: vi.fn(),
      }),
    );

    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();
    expect(ws.closed).toBe(false);

    unmount();
    expect(ws.closed).toBe(true);
  });

  it('reconnects with exponential backoff on close (1s -> 2s -> 4s -> max 30s)', () => {
    renderHook(() =>
      useWebSocketSubscription('myOntology', {
        objectType: 'Employee',
        enabled: true,
        onEvent: vi.fn(),
      }),
    );

    const firstWS = MockWebSocket.instances[0];
    firstWS.simulateOpen();

    // Abnormal close triggers reconnect after 1s
    firstWS.simulateClose(1006);
    expect(MockWebSocket.instances).toHaveLength(1);

    act(() => { vi.advanceTimersByTime(1000); });
    expect(MockWebSocket.instances).toHaveLength(2);

    // Second close -> 2s backoff
    MockWebSocket.instances[1].simulateClose(1006);
    act(() => { vi.advanceTimersByTime(1999); });
    expect(MockWebSocket.instances).toHaveLength(2);
    act(() => { vi.advanceTimersByTime(1); });
    expect(MockWebSocket.instances).toHaveLength(3);

    // Third close -> 4s backoff
    MockWebSocket.instances[2].simulateClose(1006);
    act(() => { vi.advanceTimersByTime(4000); });
    expect(MockWebSocket.instances).toHaveLength(4);

    // Fourth close -> 8s
    MockWebSocket.instances[3].simulateClose(1006);
    act(() => { vi.advanceTimersByTime(8000); });
    expect(MockWebSocket.instances).toHaveLength(5);

    // Keep closing until we hit the 30s cap
    MockWebSocket.instances[4].simulateClose(1006); // 16s
    act(() => { vi.advanceTimersByTime(16000); });
    expect(MockWebSocket.instances).toHaveLength(6);

    MockWebSocket.instances[5].simulateClose(1006); // would be 32s, capped to 30s
    act(() => { vi.advanceTimersByTime(29999); });
    expect(MockWebSocket.instances).toHaveLength(6);
    act(() => { vi.advanceTimersByTime(1); });
    expect(MockWebSocket.instances).toHaveLength(7);
  });

  it('resets backoff after successful reconnect', () => {
    renderHook(() =>
      useWebSocketSubscription('myOntology', {
        objectType: 'Employee',
        enabled: true,
        onEvent: vi.fn(),
      }),
    );

    // First connect and close
    MockWebSocket.instances[0].simulateOpen();
    MockWebSocket.instances[0].simulateClose(1006);
    act(() => { vi.advanceTimersByTime(1000); });
    expect(MockWebSocket.instances).toHaveLength(2);

    // Successful reconnect resets backoff
    MockWebSocket.instances[1].simulateOpen();

    // Another close — should use 1s again, not 2s
    MockWebSocket.instances[1].simulateClose(1006);
    act(() => { vi.advanceTimersByTime(1000); });
    expect(MockWebSocket.instances).toHaveLength(3);
  });

  it('does not reconnect on normal close (code 1000)', () => {
    renderHook(() =>
      useWebSocketSubscription('myOntology', {
        objectType: 'Employee',
        enabled: true,
        onEvent: vi.fn(),
      }),
    );

    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();

    // Normal close
    ws.simulateClose(1000, 'normal closure');

    act(() => { vi.advanceTimersByTime(60000); });
    expect(MockWebSocket.instances).toHaveLength(1);
  });

  it('re-subscribes after reconnect and welcome', () => {
    renderHook(() =>
      useWebSocketSubscription('myOntology', {
        objectType: 'Employee',
        enabled: true,
        onEvent: vi.fn(),
      }),
    );

    // First connection
    const ws1 = MockWebSocket.instances[0];
    ws1.simulateOpen();
    ws1.simulateMessage({ type: 'welcome', connectionId: 'conn-1' });
    expect(ws1.sentMessages).toHaveLength(1);

    // Disconnect and reconnect
    ws1.simulateClose(1006);
    act(() => { vi.advanceTimersByTime(1000); });
    expect(MockWebSocket.instances).toHaveLength(2);

    const ws2 = MockWebSocket.instances[1];
    ws2.simulateOpen();
    ws2.simulateMessage({ type: 'welcome', connectionId: 'conn-2' });

    // Should have sent subscribe again
    expect(ws2.sentMessages).toHaveLength(1);
    const msg = JSON.parse(ws2.sentMessages[0]);
    expect(msg.type).toBe('subscribe');
    expect(msg.objectType).toBe('Employee');
  });

  it('closes previous WebSocket when params change', () => {
    const { rerender } = renderHook(
      ({ objectType }: { objectType: string }) =>
        useWebSocketSubscription('myOntology', {
          objectType,
          enabled: true,
          onEvent: vi.fn(),
        }),
      { initialProps: { objectType: 'Employee' } },
    );

    const first = MockWebSocket.instances[0];
    first.simulateOpen();
    expect(first.closed).toBe(false);

    rerender({ objectType: 'Order' });

    expect(first.closed).toBe(true);
    expect(MockWebSocket.instances).toHaveLength(2);
  });
});
