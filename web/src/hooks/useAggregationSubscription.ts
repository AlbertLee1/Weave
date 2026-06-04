import { useEffect, useRef, useCallback, useLayoutEffect } from 'react';
import {
  normalizeAggregationResponse,
  type AggregationResponse,
  type WireAggregationResponse,
} from '../api/aggregation';

// A single metric the live aggregation subscription tracks. Mirrors the
// backend `AggMetric` shape (pkg/subscriptions/aggregation.go): `field` is
// required for every type other than `count`, `name` defaults to `type`.
export interface AggregationSubscriptionMetric {
  type: 'count' | 'sum' | 'avg' | 'min' | 'max';
  field?: string;
  name?: string;
}

export type AggregationSubscriptionStatus =
  | 'idle'
  | 'connecting'
  | 'connected'
  | 'reconnecting';

export interface UseAggregationSubscriptionOptions {
  objectType: string;
  metric: AggregationSubscriptionMetric;
  where?: unknown;
  groupBy?: string;
  enabled: boolean;
  onSnapshot: (snapshot: AggregationResponse) => void;
  onStatusChange?: (status: AggregationSubscriptionStatus) => void;
}

/**
 * React hook that subscribes to a live aggregation via WebSocket.
 *
 * Connects to the backend subscriptions WebSocket, sends a
 * `subscribeAggregation` message for the given objectType/metric (with
 * optional where/groupBy), and invokes `onSnapshot` for every
 * `aggregationChanged` message — the server pushes the full current snapshot,
 * so the caller renders the latest result without merging deltas.
 *
 * Only the single-metric / single exact-match groupBy scenario supported by
 * the backend aggregation subscription is handled here; richer aggregation
 * shapes stay on the on-demand HTTP path.
 *
 * Features:
 * - Auto-reconnect with exponential backoff (1s → 2s → 4s → … → max 30s)
 * - Re-subscribes after reconnect
 * - Automatic cleanup on unmount / when disabled / when params change
 */
export function useAggregationSubscription(
  ontology: string,
  options: UseAggregationSubscriptionOptions,
): void {
  const { objectType, metric, where, groupBy, enabled, onSnapshot, onStatusChange } =
    options;

  // Keep callbacks + params in refs so reconnect logic always uses the latest
  // values without forcing the connection effect to re-run on every render.
  const onSnapshotRef = useRef(onSnapshot);
  const onStatusChangeRef = useRef(onStatusChange);
  const metricRef = useRef(metric);
  const whereRef = useRef(where);
  const groupByRef = useRef(groupBy);

  // Track backoff state across reconnections.
  const backoffRef = useRef(1000);

  useLayoutEffect(() => {
    onSnapshotRef.current = onSnapshot;
    onStatusChangeRef.current = onStatusChange;
    metricRef.current = metric;
    whereRef.current = where;
    groupByRef.current = groupBy;
  }, [groupBy, metric, onSnapshot, onStatusChange, where]);

  const buildUrl = useCallback(() => {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    return `${protocol}//${window.location.host}/api/v2/ontologies/${ontology}/subscriptions/ws`;
  }, [ontology]);

  // Re-key the connection on the serialized subscription params so changing the
  // metric / where / groupBy tears down the old socket and opens a fresh one
  // (the backend has no "resubscribe in place" path — a new socket is correct).
  const subKey = JSON.stringify({ objectType, metric, where, groupBy });

  useEffect(() => {
    if (!enabled || !ontology || !objectType) {
      onStatusChangeRef.current?.('idle');
      return;
    }

    let ws: WebSocket | null = null;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    let disposed = false;

    const sendSubscribe = (socket: WebSocket) => {
      const m = metricRef.current;
      const msg: Record<string, unknown> = {
        type: 'subscribeAggregation',
        objectType,
        metric: {
          type: m.type,
          ...(m.type !== 'count' && m.field ? { field: m.field } : {}),
          name: m.name?.trim() || m.type,
        },
      };
      if (whereRef.current) {
        msg.where = whereRef.current;
      }
      if (groupByRef.current) {
        msg.groupBy = groupByRef.current;
      }
      socket.send(JSON.stringify(msg));
    };

    const connect = () => {
      if (disposed) return;

      const url = buildUrl();
      onStatusChangeRef.current?.('connecting');
      ws = new WebSocket(url);

      ws.onopen = () => {
        backoffRef.current = 1000;
        onStatusChangeRef.current?.('connected');
      };

      ws.onmessage = (evt: MessageEvent) => {
        try {
          const msg = JSON.parse(evt.data as string);
          switch (msg.type) {
            case 'welcome':
              if (ws) sendSubscribe(ws);
              break;
            case 'aggregationChanged': {
              // Payload is { state: AggregationResponse } (the full snapshot).
              const state = (msg.data?.state ?? undefined) as
                | WireAggregationResponse
                | undefined;
              onSnapshotRef.current(normalizeAggregationResponse(state));
              break;
            }
            // subscribed, unsubscribed, error, onOutOfDate — no action needed
          }
        } catch {
          // Ignore malformed messages.
        }
      };

      ws.onclose = (evt: CloseEvent) => {
        if (disposed) return;

        // Normal closure (1000) — do not reconnect.
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
      backoffRef.current = 1000;
      onStatusChangeRef.current?.('idle');
    };
    // subKey captures metric/where/groupBy changes; objectType/ontology/buildUrl
    // are explicit so an ontology switch also reconnects.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, ontology, objectType, buildUrl, subKey]);
}
