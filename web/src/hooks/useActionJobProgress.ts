import { useEffect, useRef, useState, useLayoutEffect } from 'react';
import type { ActionJobStatus } from '../api/actions';

// ActionJobProgressEvent mirrors the server's
// subscriptions.ActionJobProgressEvent wire shape. Status is empty for
// in-flight ticks and populated on terminal transitions when the publisher
// tags one. US-318.
export interface ActionJobProgressEvent {
  jobId: string;
  percent: number;
  message?: string;
  status?: ActionJobStatus;
  ontologyApiName?: string;
  actionType?: string;
  errorMessage?: string;
  result?: unknown;
}

export interface ActionJobProgressState {
  // Current percent in [0, 100]. Starts at 0; bumped on every event.
  percent: number;
  // Last human-readable message published by the worker (e.g. "starting",
  // "done", or whatever weave.reportProgress passed). May be empty.
  message: string;
  // Current status if the publisher tagged one. Falls back to 'RUNNING' on
  // any in-flight event without an explicit status. Set to a terminal
  // status by the consumer when the WS feed is silent — see useActionJobProgress.
  status: ActionJobStatus | 'CONNECTING';
  // Connection state of the underlying WebSocket. Useful for surfacing
  // reconnect indicators in the UI without a separate hook.
  connected: boolean;
  // True once at least one progress event has been observed. Lets the UI
  // distinguish "haven't heard from the worker yet" from "worker is at 0%".
  hasProgress: boolean;
}

export interface UseActionJobProgressOptions {
  // ontologyApiName is required by the WS endpoint URL pattern. Passing the
  // active ontology keeps the hub connection scoped to the current page.
  ontologyApiName: string;
  // jobId names the action_jobs row whose progress events the caller wants
  // to receive. The hook stops dispatching once status reaches a terminal
  // value (SUCCEEDED / FAILED / CANCELED).
  jobId: string | null;
  // enabled gates the entire effect. Pass false to defer the connection
  // until a job has been scheduled (e.g. modal still closed).
  enabled?: boolean;
  // onTerminal fires once with the final progress event when the job
  // settles into SUCCEEDED / FAILED / CANCELED. The modal uses this to
  // unmount the cancel button and surface the result.
  onTerminal?: (evt: ActionJobProgressEvent) => void;
}

/**
 * useActionJobProgress opens a WebSocket subscription to a single async
 * action job and exposes the most recent progress tick as React state. The
 * hook auto-reconnects on transient disconnects (1s → 2s → … → 30s) and
 * tears down the connection when the job reaches a terminal state OR when
 * the consumer disables it / unmounts. US-318.
 */
export function useActionJobProgress(
  options: UseActionJobProgressOptions,
): ActionJobProgressState {
  const { ontologyApiName, jobId, enabled = true, onTerminal } = options;

  const initialState: ActionJobProgressState = {
    percent: 0,
    message: '',
    status: 'CONNECTING',
    connected: false,
    hasProgress: false,
  };
  const [state, setState] = useState<ActionJobProgressState>(initialState);

  // Render-phase reset when the (jobId, enabled, ontologyApiName) tuple
  // changes. Using setState during render is the React-blessed alternative
  // to a useEffect+setState that the linter blocks (progress.txt:109).
  const trackingKey = enabled && jobId ? `${ontologyApiName}::${jobId}` : '';
  const [prevKey, setPrevKey] = useState(trackingKey);
  if (trackingKey !== prevKey) {
    setPrevKey(trackingKey);
    setState(initialState);
  }

  const onTerminalRef = useRef(onTerminal);
  // useLayoutEffect keeps the ref in lock-step with the latest callback
  // without writing to it during render (react-hooks/refs).
  useLayoutEffect(() => {
    onTerminalRef.current = onTerminal;
  }, [onTerminal]);

  useEffect(() => {
    if (!enabled || !jobId || !ontologyApiName) {
      return;
    }

    let ws: WebSocket | null = null;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    let backoff = 1000;
    let disposed = false;
    let terminalFired = false;

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = `${protocol}//${window.location.host}/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/subscriptions/ws`;

    const sendSubscribe = (sock: WebSocket) => {
      sock.send(
        JSON.stringify({
          type: 'subscribeActionJob',
          data: { jobId },
        }),
      );
    };

    const connect = () => {
      if (disposed) return;
      ws = new WebSocket(url);

      ws.onopen = () => {
        backoff = 1000;
        setState((prev) => ({ ...prev, connected: true }));
      };

      ws.onmessage = (evt: MessageEvent) => {
        try {
          const msg = JSON.parse(evt.data as string) as {
            type: string;
            data?: ActionJobProgressEvent;
          };
          switch (msg.type) {
            case 'welcome':
              if (ws) sendSubscribe(ws);
              break;
            case 'actionJobProgress':
              if (!msg.data) return;
              {
                const data = msg.data;
                const status: ActionJobStatus =
                  data.status ?? deriveInflightStatus(data.percent);
                setState((prev) => ({
                  percent: typeof data.percent === 'number' ? data.percent : prev.percent,
                  message: data.message ?? '',
                  status,
                  connected: true,
                  hasProgress: true,
                }));
                if (isTerminalStatus(status) && !terminalFired) {
                  terminalFired = true;
                  onTerminalRef.current?.(data);
                  // Politely close the socket — no more events expected.
                  ws?.close(1000);
                }
              }
              break;
          }
        } catch {
          // Ignore malformed frames.
        }
      };

      ws.onclose = (evt: CloseEvent) => {
        if (disposed) return;
        setState((prev) => ({ ...prev, connected: false }));
        if (evt.code === 1000) return; // normal close (terminal or unmount)
        const delay = backoff;
        backoff = Math.min(delay * 2, 30_000);
        reconnectTimer = setTimeout(connect, delay);
      };

      ws.onerror = () => {
        // Always followed by close; reconnect handled there.
      };
    };

    connect();

    return () => {
      disposed = true;
      if (reconnectTimer !== null) clearTimeout(reconnectTimer);
      ws?.close(1000);
    };
  }, [enabled, ontologyApiName, jobId]);

  return state;
}

function isTerminalStatus(s: ActionJobStatus): boolean {
  return s === 'SUCCEEDED' || s === 'FAILED' || s === 'CANCELED';
}

// deriveInflightStatus picks RUNNING for any in-flight progress event that
// arrives without an explicit status. Callers that want to render a more
// nuanced lifecycle (PENDING → RUNNING) should check the persisted GET
// /actions/jobs/{id} via TanStack Query at modal-open time and merge.
function deriveInflightStatus(percent: number): ActionJobStatus {
  if (percent >= 100) return 'SUCCEEDED';
  return 'RUNNING';
}
