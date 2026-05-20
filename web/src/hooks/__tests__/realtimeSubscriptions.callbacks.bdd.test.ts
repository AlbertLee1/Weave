import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useObjectSetSubscription } from '../useObjectSetSubscription';
import { useWebSocketSubscription } from '../useWebSocketSubscription';

class CallbackWebSocket {
  static instances: CallbackWebSocket[] = [];

  url: string;
  onmessage: ((evt: MessageEvent) => void) | null = null;
  onopen: ((evt: Event) => void) | null = null;
  onclose: ((evt: CloseEvent) => void) | null = null;
  onerror: ((evt: Event) => void) | null = null;
  sentMessages: string[] = [];

  constructor(url: string) {
    this.url = url;
    CallbackWebSocket.instances.push(this);
  }

  send(data: string) {
    this.sentMessages.push(data);
  }

  close() {}

  simulateMessage(data: unknown) {
    this.onmessage?.(
      new MessageEvent('message', { data: JSON.stringify(data) }),
    );
  }
}

class CallbackEventSource {
  static instances: CallbackEventSource[] = [];

  url: string;
  onmessage: ((evt: MessageEvent) => void) | null = null;
  onopen: (() => void) | null = null;
  onerror: ((evt: Event) => void) | null = null;

  constructor(url: string) {
    this.url = url;
    CallbackEventSource.instances.push(this);
  }

  close() {}

  simulateMessage(data: unknown) {
    this.onmessage?.(
      new MessageEvent('message', { data: JSON.stringify(data) }),
    );
  }
}

describe('BDD realtime subscription callbacks', () => {
  beforeEach(() => {
    CallbackWebSocket.instances = [];
    CallbackEventSource.instances = [];
    vi.stubGlobal('WebSocket', CallbackWebSocket);
    vi.stubGlobal('EventSource', CallbackEventSource);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('Given an active WebSocket subscription, When the onEvent prop changes, Then the existing socket dispatches to the latest callback', () => {
    const firstOnEvent = vi.fn();
    const latestOnEvent = vi.fn();

    const { rerender } = renderHook(
      ({ onEvent }) =>
        useWebSocketSubscription('northwind', {
          objectType: 'Customer',
          enabled: true,
          onEvent,
        }),
      { initialProps: { onEvent: firstOnEvent } },
    );

    const socket = CallbackWebSocket.instances[0];
    rerender({ onEvent: latestOnEvent });

    socket.simulateMessage({
      type: 'objectChanged',
      data: {
        state: 'ADDED_OR_UPDATED',
        object: { customerId: 'ALFKI' },
      },
    });

    expect(firstOnEvent).not.toHaveBeenCalled();
    expect(latestOnEvent).toHaveBeenCalledWith({
      state: 'ADDED_OR_UPDATED',
      object: { customerId: 'ALFKI' },
    });
  });

  it('Given an active ObjectSet SSE subscription, When the onEvent prop changes, Then the existing EventSource dispatches to the latest callback', () => {
    const firstOnEvent = vi.fn();
    const latestOnEvent = vi.fn();

    const { rerender } = renderHook(
      ({ onEvent }) =>
        useObjectSetSubscription('northwind', 'ri.object-set.main.live', {
          enabled: true,
          onEvent,
        }),
      { initialProps: { onEvent: firstOnEvent } },
    );

    const eventSource = CallbackEventSource.instances[0];
    rerender({ onEvent: latestOnEvent });

    eventSource.simulateMessage({
      eventType: 'ADDED_OR_UPDATED',
      object: { __primaryKey: 'ALFKI', __apiName: 'Customer' },
    });

    expect(firstOnEvent).not.toHaveBeenCalled();
    expect(latestOnEvent).toHaveBeenCalledWith({
      eventType: 'ADDED_OR_UPDATED',
      object: { __primaryKey: 'ALFKI', __apiName: 'Customer' },
    });
  });
});
