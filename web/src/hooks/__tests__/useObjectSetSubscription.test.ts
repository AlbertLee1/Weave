import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useObjectSetSubscription } from '../useObjectSetSubscription';

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
    this.readyState = 0; // CONNECTING
    MockEventSource.instances.push(this);
  }

  close() {
    this.closed = true;
    this.readyState = 2; // CLOSED
  }

  // Test helpers -------------------------------------------------------

  /** Simulate successful connection */
  simulateOpen() {
    this.readyState = 1; // OPEN
    this.onopen?.();
  }

  /** Simulate an incoming SSE message */
  simulateMessage(data: string, lastEventId?: string) {
    const evt = new MessageEvent('message', {
      data,
      lastEventId: lastEventId ?? '',
    });
    this.onmessage?.(evt);
  }

  /** Simulate an error (triggers reconnect logic) */
  simulateError() {
    this.readyState = 2;
    this.onerror?.(new Event('error'));
  }
}

// Expose constants that real EventSource has
(MockEventSource as unknown as Record<string, number>).CONNECTING = 0;
(MockEventSource as unknown as Record<string, number>).OPEN = 1;
(MockEventSource as unknown as Record<string, number>).CLOSED = 2;

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('useObjectSetSubscription', () => {
  beforeEach(() => {
    MockEventSource.instances = [];
    vi.stubGlobal('EventSource', MockEventSource);
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('creates an EventSource connected to the correct URL', () => {
    renderHook(() =>
      useObjectSetSubscription('myOntology', 'rid:abc', {
        enabled: true,
        onEvent: vi.fn(),
      }),
    );

    expect(MockEventSource.instances).toHaveLength(1);
    expect(MockEventSource.instances[0].url).toBe(
      '/api/v2/ontologies/myOntology/objectSets/rid:abc/subscribe',
    );
  });

  it('does not create EventSource when enabled=false', () => {
    renderHook(() =>
      useObjectSetSubscription('myOntology', 'rid:abc', {
        enabled: false,
        onEvent: vi.fn(),
      }),
    );

    expect(MockEventSource.instances).toHaveLength(0);
  });

  it('calls onEvent for each incoming message', () => {
    const onEvent = vi.fn();
    renderHook(() =>
      useObjectSetSubscription('myOntology', 'rid:abc', {
        enabled: true,
        onEvent,
      }),
    );

    const es = MockEventSource.instances[0];
    es.simulateOpen();

    const events = [
      { eventType: 'ADDED_OR_UPDATED', object: { __primaryKey: '1', __apiName: 'Order' } },
      { eventType: 'ADDED_OR_UPDATED', object: { __primaryKey: '2', __apiName: 'Order' } },
      { eventType: 'DELETED', object: { __primaryKey: '3', __apiName: 'Order' } },
    ];

    for (const evt of events) {
      es.simulateMessage(JSON.stringify(evt));
    }

    expect(onEvent).toHaveBeenCalledTimes(3);
    expect(onEvent).toHaveBeenNthCalledWith(1, events[0]);
    expect(onEvent).toHaveBeenNthCalledWith(2, events[1]);
    expect(onEvent).toHaveBeenNthCalledWith(3, events[2]);
  });

  it('closes EventSource on unmount', () => {
    const { unmount } = renderHook(() =>
      useObjectSetSubscription('myOntology', 'rid:abc', {
        enabled: true,
        onEvent: vi.fn(),
      }),
    );

    const es = MockEventSource.instances[0];
    expect(es.closed).toBe(false);

    unmount();
    expect(es.closed).toBe(true);
  });

  it('reconnects with exponential backoff on error (1s -> 2s -> 4s -> max 30s)', () => {
    renderHook(() =>
      useObjectSetSubscription('myOntology', 'rid:abc', {
        enabled: true,
        onEvent: vi.fn(),
      }),
    );

    const firstES = MockEventSource.instances[0];
    firstES.simulateOpen();

    // Error triggers reconnect after 1s
    firstES.simulateError();
    expect(MockEventSource.instances).toHaveLength(1); // not yet reconnected

    act(() => { vi.advanceTimersByTime(1000); });
    expect(MockEventSource.instances).toHaveLength(2);

    // Second error -> 2s backoff
    MockEventSource.instances[1].simulateError();
    act(() => { vi.advanceTimersByTime(1999); });
    expect(MockEventSource.instances).toHaveLength(2); // still waiting
    act(() => { vi.advanceTimersByTime(1); });
    expect(MockEventSource.instances).toHaveLength(3);

    // Third error -> 4s backoff
    MockEventSource.instances[2].simulateError();
    act(() => { vi.advanceTimersByTime(4000); });
    expect(MockEventSource.instances).toHaveLength(4);

    // Fourth error -> 8s
    MockEventSource.instances[3].simulateError();
    act(() => { vi.advanceTimersByTime(8000); });
    expect(MockEventSource.instances).toHaveLength(5);

    // Keep erroring until we hit the 30s cap
    MockEventSource.instances[4].simulateError(); // 16s
    act(() => { vi.advanceTimersByTime(16000); });
    expect(MockEventSource.instances).toHaveLength(6);

    MockEventSource.instances[5].simulateError(); // would be 32s, capped to 30s
    act(() => { vi.advanceTimersByTime(29999); });
    expect(MockEventSource.instances).toHaveLength(6); // not yet
    act(() => { vi.advanceTimersByTime(1); });
    expect(MockEventSource.instances).toHaveLength(7);
  });

  it('resets backoff after successful reconnect', () => {
    renderHook(() =>
      useObjectSetSubscription('myOntology', 'rid:abc', {
        enabled: true,
        onEvent: vi.fn(),
      }),
    );

    // Error and reconnect
    MockEventSource.instances[0].simulateOpen();
    MockEventSource.instances[0].simulateError();
    act(() => { vi.advanceTimersByTime(1000); });
    expect(MockEventSource.instances).toHaveLength(2);

    // Successful open resets backoff
    MockEventSource.instances[1].simulateOpen();

    // Another error — should use 1s again, not 2s
    MockEventSource.instances[1].simulateError();
    act(() => { vi.advanceTimersByTime(1000); });
    expect(MockEventSource.instances).toHaveLength(3);
  });

  it('attaches Last-Event-ID on reconnect by tracking lastEventId', () => {
    renderHook(() =>
      useObjectSetSubscription('myOntology', 'rid:abc', {
        enabled: true,
        onEvent: vi.fn(),
      }),
    );

    const es = MockEventSource.instances[0];
    es.simulateOpen();
    es.simulateMessage('{"eventType":"ADDED_OR_UPDATED","object":{}}', '42');

    // Error and reconnect
    es.simulateError();
    act(() => { vi.advanceTimersByTime(1000); });

    const reconnected = MockEventSource.instances[1];
    // The URL should include the last event ID for replay
    // EventSource doesn't support custom headers natively, so
    // we pass it as a query parameter that the server can read
    expect(reconnected.url).toContain('lastEventId=42');
  });

  it('closes previous EventSource before creating a new one when params change', () => {
    const { rerender } = renderHook(
      ({ rid }: { rid: string }) =>
        useObjectSetSubscription('myOntology', rid, {
          enabled: true,
          onEvent: vi.fn(),
        }),
      { initialProps: { rid: 'rid:abc' } },
    );

    const first = MockEventSource.instances[0];
    expect(first.closed).toBe(false);

    rerender({ rid: 'rid:def' });

    expect(first.closed).toBe(true);
    expect(MockEventSource.instances).toHaveLength(2);
    expect(MockEventSource.instances[1].url).toContain('rid:def');
  });

  it('does not create EventSource when ontology is empty', () => {
    renderHook(() =>
      useObjectSetSubscription('', 'rid:abc', {
        enabled: true,
        onEvent: vi.fn(),
      }),
    );

    expect(MockEventSource.instances).toHaveLength(0);
  });

  it('does not create EventSource when rid is empty', () => {
    renderHook(() =>
      useObjectSetSubscription('myOntology', '', {
        enabled: true,
        onEvent: vi.fn(),
      }),
    );

    expect(MockEventSource.instances).toHaveLength(0);
  });
});
