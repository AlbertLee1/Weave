// scenarioRunStream — guarded SSE client for Scenario Runs (VTX-115).
//
// Wraps EventSource with:
//   1. automatic reconnect on transport error
//   2. Last-Event-Id header carried across reconnects so the server can
//      replay from the last delivered event
//   3. a terminal fallback: if reconnect keeps failing, GET
//      /api/vertex/v1/scenarios/{rid}/runs/{runRid} returns the final
//      record so the UI is never stuck "in progress" forever
//
// The actual SSE endpoint and replay support are not mounted in the
// current server/OpenAPI contract. Callers must explicitly opt in before
// this helper opens EventSource; default use throws so UI wiring cannot
// accidentally hit a 404 stream route.

export type RunEvent =
  | { kind: 'progress'; percent: number }
  | { kind: 'log'; line: string }
  | { kind: 'retry'; activityId: string; attempt: number; error: string }
  | { kind: 'completed'; scenarioRunRid: string }
  | { kind: 'failed'; scenarioRunRid: string; error: string };

export interface ScenarioRunStreamOptions {
  scenarioRid: string;
  runRid?: string;
  /** Inject for tests. Default: window.EventSource. */
  EventSourceCtor?: typeof EventSource;
  /** Inject for tests. Default: globalThis.fetch. */
  fetchImpl?: typeof fetch;
  /** Max number of reconnect attempts before falling through to pollFinalState. */
  maxReconnects?: number;
  /** Base backoff in ms; doubled on each retry up to 10s. */
  backoffMs?: number;
  /** Test/future-server escape hatch until /runs/{runRid}/stream is mounted. */
  allowUnimplementedStreamRoute?: boolean;
}

interface Handle {
  /** AsyncIterable of parsed RunEvent. */
  events: AsyncIterable<RunEvent>;
  /** Close the underlying EventSource and end the iteration. */
  close: () => void;
}

interface MinimalEventSource {
  close(): void;
  onmessage: ((ev: MessageEvent) => void) | null;
  onerror: ((ev: Event) => void) | null;
  url: string;
}

// openScenarioRunStream returns an AsyncIterable that yields RunEvent
// objects. The iterable ends after a terminal event (completed/failed)
// or after maxReconnects exhausts and pollFinalState returns.
export function openScenarioRunStream(opts: ScenarioRunStreamOptions): Handle {
  if (!opts.allowUnimplementedStreamRoute) {
    throw new Error('Vertex scenario-run stream route is not mounted; use polling');
  }
  const maxReconnects = opts.maxReconnects ?? 5;
  const backoffMs = opts.backoffMs ?? 1000;
  const EventSourceCtor = opts.EventSourceCtor ?? (globalThis as { EventSource: typeof EventSource }).EventSource;
  const fetchImpl = opts.fetchImpl ?? globalThis.fetch.bind(globalThis);

  let lastEventId = '';
  let closed = false;
  let resolveNext: ((v: IteratorResult<RunEvent>) => void) | null = null;
  const pending: RunEvent[] = [];
  let es: MinimalEventSource | null = null;

  function emit(ev: RunEvent) {
    if (resolveNext) {
      const r = resolveNext;
      resolveNext = null;
      r({ value: ev, done: false });
    } else {
      pending.push(ev);
    }
  }

  function end() {
    closed = true;
    if (es) {
      es.close();
      es = null;
    }
    if (resolveNext) {
      const r = resolveNext;
      resolveNext = null;
      r({ value: undefined as unknown as RunEvent, done: true });
    }
  }

  function connect(attempt: number) {
    if (closed) return;
    const scenarioPath = `/api/vertex/v1/scenarios/${encodeURIComponent(opts.scenarioRid)}`;
    const urlBase = opts.runRid
      ? `${scenarioPath}/runs/${encodeURIComponent(opts.runRid)}/stream`
      : `${scenarioPath}/runs/stream`;
    const url = lastEventId ? `${urlBase}?lastEventId=${encodeURIComponent(lastEventId)}` : urlBase;
    const next = new EventSourceCtor(url) as unknown as MinimalEventSource;
    es = next;
    next.onmessage = (msg) => {
      const messageEvent = msg as MessageEvent & { lastEventId?: string };
      if (messageEvent.lastEventId) lastEventId = messageEvent.lastEventId;
      try {
        const parsed = JSON.parse(messageEvent.data) as RunEvent;
        emit(parsed);
        if (parsed.kind === 'completed' || parsed.kind === 'failed') {
          end();
        }
      } catch {
        /* drop malformed */
      }
    };
    next.onerror = () => {
      if (closed) return;
      next.close();
      es = null;
      if (attempt >= maxReconnects) {
        void pollFinalState(opts, fetchImpl).then((ev) => {
          if (ev) emit(ev);
          end();
        });
        return;
      }
      const delay = Math.min(10_000, backoffMs * Math.pow(2, attempt));
      setTimeout(() => connect(attempt + 1), delay);
    };
  }

  connect(0);

  const events: AsyncIterable<RunEvent> = {
    [Symbol.asyncIterator](): AsyncIterator<RunEvent> {
      return {
        next(): Promise<IteratorResult<RunEvent>> {
          if (pending.length > 0) {
            return Promise.resolve({ value: pending.shift() as RunEvent, done: false });
          }
          if (closed) return Promise.resolve({ value: undefined as unknown as RunEvent, done: true });
          return new Promise<IteratorResult<RunEvent>>((resolve) => {
            resolveNext = resolve;
          });
        },
        return(): Promise<IteratorResult<RunEvent>> {
          end();
          return Promise.resolve({ value: undefined as unknown as RunEvent, done: true });
        },
      };
    },
  };

  return { events, close: end };
}

// pollFinalState hits the rest fallback after maxReconnects exhausts. It
// returns a synthetic completed/failed RunEvent if the server has a
// terminal state, or null when even the REST call fails (caller treats
// that as "stopped" and surfaces an error toast).
export async function pollFinalState(
  opts: ScenarioRunStreamOptions,
  fetchImpl: typeof fetch,
): Promise<RunEvent | null> {
  if (!opts.runRid) return null;
  try {
    const res = await fetchImpl(
      `/api/vertex/v1/scenarios/${encodeURIComponent(opts.scenarioRid)}/runs/${encodeURIComponent(opts.runRid)}`,
    );
    if (!res.ok) return null;
    const body = (await res.json()) as {
      status: 'succeeded' | 'failed';
      runRid?: string;
      scenarioRunRid?: string;
      error?: string;
    };
    const scenarioRunRid = body.runRid ?? body.scenarioRunRid;
    if (!scenarioRunRid) return null;
    if (body.status === 'succeeded') {
      return { kind: 'completed', scenarioRunRid };
    }
    return { kind: 'failed', scenarioRunRid, error: body.error ?? 'failed' };
  } catch {
    return null;
  }
}
