import { useEffect, useRef, useCallback } from 'react';

// US-501: events carry both the legacy view ({eventType, object}) and
// the US-459 canonical view ({seq, type, rid, properties}) so consumers
// can pick the shape that fits their UI. Both views are populated by
// the server (pkg/oss/subscribe_sse.go sseEventPayload).
export type ObjectSetEventCanonicalType = 'created' | 'modified' | 'deleted';

export interface ObjectSetEvent {
  // Legacy view (US-055..058).
  eventType: 'ADDED_OR_UPDATED' | 'DELETED';
  object: Record<string, unknown>;
  // Canonical view (US-459). Optional only so SDK consumers that fed in
  // a hand-rolled payload don't blow up — server-emitted frames always
  // include all four.
  seq?: number;
  type?: ObjectSetEventCanonicalType;
  rid?: string;
  properties?: Record<string, unknown>;
}

export type ObjectSetSubscriptionStatus =
  | 'idle'
  | 'connecting'
  | 'connected'
  | 'reconnecting';

export interface UseObjectSetSubscriptionOptions {
  enabled: boolean;
  onEvent: (event: ObjectSetEvent) => void;
  /**
   * Optional callback invoked when the SSE connection state changes.
   * Lets consumers render a "live / reconnecting / disconnected"
   * indicator without managing the EventSource lifecycle themselves.
   */
  onStatusChange?: (status: ObjectSetSubscriptionStatus) => void;
}

/**
 * React hook that subscribes to ObjectSet changes via Server-Sent Events.
 *
 * Wraps the browser EventSource API with:
 * - Exponential backoff reconnect (1s → 2s → 4s → … → max 30s)
 * - Last-Event-ID tracking for replay on reconnect
 * - Automatic cleanup on unmount
 */
export function useObjectSetSubscription(
  ontology: string,
  objectSetRid: string,
  options: UseObjectSetSubscriptionOptions,
): void {
  const { enabled, onEvent, onStatusChange } = options;

  // Keep onEvent in a ref so reconnect logic always calls the latest
  // callback without triggering an effect re-run.
  const onEventRef = useRef(onEvent);
  onEventRef.current = onEvent;

  const onStatusChangeRef = useRef(onStatusChange);
  onStatusChangeRef.current = onStatusChange;

  // Track the last event ID for replay on reconnect.
  const lastEventIdRef = useRef<string>('');

  // Track backoff state across reconnections.
  const backoffRef = useRef(1000);

  const buildUrl = useCallback(
    (lastEventId: string) => {
      const base = `/api/v2/ontologies/${ontology}/objectSets/${objectSetRid}/subscribe`;
      if (lastEventId) {
        return `${base}?lastEventId=${encodeURIComponent(lastEventId)}`;
      }
      return base;
    },
    [ontology, objectSetRid],
  );

  useEffect(() => {
    if (!enabled || !ontology || !objectSetRid) {
      onStatusChangeRef.current?.('idle');
      return;
    }

    let es: EventSource | null = null;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    let disposed = false;

    const connect = () => {
      if (disposed) return;

      const url = buildUrl(lastEventIdRef.current);
      onStatusChangeRef.current?.('connecting');
      es = new EventSource(url);

      es.onopen = () => {
        // Reset backoff on successful connection.
        backoffRef.current = 1000;
        onStatusChangeRef.current?.('connected');
      };

      es.onmessage = (evt: MessageEvent) => {
        if (evt.lastEventId) {
          lastEventIdRef.current = evt.lastEventId;
        }
        try {
          const data = JSON.parse(evt.data) as ObjectSetEvent;
          onEventRef.current(data);
        } catch {
          // Ignore malformed messages.
        }
      };

      es.onerror = () => {
        es?.close();
        if (disposed) return;

        onStatusChangeRef.current?.('reconnecting');
        const delay = backoffRef.current;
        backoffRef.current = Math.min(delay * 2, 30_000);
        reconnectTimer = setTimeout(connect, delay);
      };
    };

    connect();

    return () => {
      disposed = true;
      es?.close();
      if (reconnectTimer !== null) {
        clearTimeout(reconnectTimer);
      }
      // Reset state for next mount cycle.
      lastEventIdRef.current = '';
      backoffRef.current = 1000;
      onStatusChangeRef.current?.('idle');
    };
  }, [enabled, ontology, objectSetRid, buildUrl]);
}
