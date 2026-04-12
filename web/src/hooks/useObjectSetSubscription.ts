import { useEffect, useRef, useCallback } from 'react';

export interface ObjectSetEvent {
  eventType: 'ADDED_OR_UPDATED' | 'DELETED';
  object: Record<string, unknown>;
}

export interface UseObjectSetSubscriptionOptions {
  enabled: boolean;
  onEvent: (event: ObjectSetEvent) => void;
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
  const { enabled, onEvent } = options;

  // Keep onEvent in a ref so reconnect logic always calls the latest
  // callback without triggering an effect re-run.
  const onEventRef = useRef(onEvent);
  onEventRef.current = onEvent;

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
      return;
    }

    let es: EventSource | null = null;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    let disposed = false;

    const connect = () => {
      if (disposed) return;

      const url = buildUrl(lastEventIdRef.current);
      es = new EventSource(url);

      es.onopen = () => {
        // Reset backoff on successful connection.
        backoffRef.current = 1000;
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
    };
  }, [enabled, ontology, objectSetRid, buildUrl]);
}
