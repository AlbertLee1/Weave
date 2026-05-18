import { useEffect, useRef, useCallback } from 'react';

export interface WebSocketChangeEvent {
  state: 'ADDED_OR_UPDATED' | 'DELETED';
  object: Record<string, unknown>;
}

export type WebSocketSubscriptionStatus =
  | 'idle'
  | 'connecting'
  | 'connected'
  | 'reconnecting';

export interface UseWebSocketSubscriptionOptions {
  objectType: string;
  where?: unknown;
  select?: string[];
  enabled: boolean;
  onEvent: (event: WebSocketChangeEvent) => void;
  onStatusChange?: (status: WebSocketSubscriptionStatus) => void;
}

/**
 * React hook that subscribes to object changes via WebSocket.
 *
 * Connects to the backend WebSocket endpoint, sends a subscribe message
 * for the given objectType (with optional where/select filters), and
 * invokes onEvent for each objectChanged message.
 *
 * Features:
 * - Auto-reconnect with exponential backoff (1s → 2s → 4s → … → max 30s)
 * - Re-subscribes after reconnect
 * - Automatic cleanup on unmount
 */
export function useWebSocketSubscription(
  ontology: string,
  options: UseWebSocketSubscriptionOptions,
): void {
  const { objectType, where, select, enabled, onEvent, onStatusChange } = options;

  // Keep onEvent in a ref so reconnect logic always calls the latest
  // callback without triggering an effect re-run.
  const onEventRef = useRef(onEvent);
  onEventRef.current = onEvent;

  const onStatusChangeRef = useRef(onStatusChange);
  onStatusChangeRef.current = onStatusChange;

  // Keep subscription params in refs for re-subscribe after reconnect.
  const objectTypeRef = useRef(objectType);
  objectTypeRef.current = objectType;
  const whereRef = useRef(where);
  whereRef.current = where;
  const selectRef = useRef(select);
  selectRef.current = select;

  // Track backoff state across reconnections.
  const backoffRef = useRef(1000);

  const buildUrl = useCallback(() => {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    return `${protocol}//${window.location.host}/api/v2/ontologies/${ontology}/subscriptions/ws`;
  }, [ontology]);

  useEffect(() => {
    if (!enabled || !ontology || !objectType) {
      onStatusChangeRef.current?.('idle');
      return;
    }

    let ws: WebSocket | null = null;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    let disposed = false;

    const sendSubscribe = (socket: WebSocket) => {
      const msg: Record<string, unknown> = {
        type: 'subscribe',
        objectType: objectTypeRef.current,
      };
      if (whereRef.current) {
        msg.where = whereRef.current;
      }
      if (selectRef.current && selectRef.current.length > 0) {
        msg.select = selectRef.current;
      }
      socket.send(JSON.stringify(msg));
    };

    const connect = () => {
      if (disposed) return;

      const url = buildUrl();
      onStatusChangeRef.current?.('connecting');
      ws = new WebSocket(url);

      ws.onopen = () => {
        // Reset backoff on successful connection.
        backoffRef.current = 1000;
        onStatusChangeRef.current?.('connected');
      };

      ws.onmessage = (evt: MessageEvent) => {
        try {
          const msg = JSON.parse(evt.data as string);
          switch (msg.type) {
            case 'welcome':
              // After welcome, send subscribe request
              if (ws) sendSubscribe(ws);
              break;
            case 'objectChanged':
              // Dispatch change event to caller
              if (msg.data) {
                onEventRef.current(msg.data as WebSocketChangeEvent);
              }
              break;
            // subscribed, unsubscribed, error, onOutOfDate — no action needed
          }
        } catch {
          // Ignore malformed messages.
        }
      };

      ws.onclose = (evt: CloseEvent) => {
        if (disposed) return;

        // Normal closure (1000) — do not reconnect
        if (evt.code === 1000) {
          onStatusChangeRef.current?.('idle');
          return;
        }

        onStatusChangeRef.current?.('reconnecting');
        const delay = backoffRef.current;
        backoffRef.current = Math.min(delay * 2, 30_000);
        reconnectTimer = setTimeout(connect, delay);
      };

      ws.onerror = () => {
        // Error is always followed by close, so reconnect is handled there.
      };
    };

    connect();

    return () => {
      disposed = true;
      if (reconnectTimer !== null) {
        clearTimeout(reconnectTimer);
      }
      ws?.close();
      // Reset state for next mount cycle.
      backoffRef.current = 1000;
      onStatusChangeRef.current?.('idle');
    };
  }, [enabled, ontology, objectType, buildUrl]);
}
