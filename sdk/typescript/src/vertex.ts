// VertexClient — TypeScript SDK surface for Weave Vertex (VTX-108).
//
// Mirrors the shape of the existing OmsClient module: a constructor that
// takes a base URL + fetch impl, namespaced calls grouped by resource
// (scenarios, graphs), and an AsyncIterable for SSE-streamed Run events.
// The SSE endpoint and JSON schemas are owned by VTX-044 (other stream);
// this module documents the contract and provides a transport that fails
// fast if the server has not implemented it yet.

export interface VertexClientOptions {
  baseUrl?: string;
  /** Inject a fetch implementation. Defaults to globalThis.fetch. */
  fetch?: typeof fetch;
}

export interface ScenarioCreateInput {
  caseStudyRid: string;
  name: string;
  parentOntologyCommit: string;
}

export interface Scenario {
  rid: string;
  caseStudyRid: string;
  name: string;
  status: 'draft' | 'frozen' | 'archived';
  immutable: boolean;
}

export interface ScenarioRunOptions {
  /** If true, returns an AsyncIterable of RunEvent. If false / omitted,
   *  the call resolves with the terminal Run record once /runs completes. */
  streaming?: boolean;
}

export interface ScenarioRunPollOptions {
  intervalMs?: number;
  timeoutMs?: number;
  signal?: AbortSignal;
}

export type RunEvent =
  | { kind: 'progress'; percent: number }
  | { kind: 'log'; line: string }
  | { kind: 'retry'; activityId: string; attempt: number; error: string }
  | { kind: 'completed'; scenarioRunRid: string }
  | { kind: 'failed'; scenarioRunRid: string; error: string };

export interface ScenarioRun {
  scenarioRunRid?: string;
  runRid?: string;
  status: 'pending' | 'running' | 'succeeded' | 'failed' | 'canceled';
  durationMs?: number;
  error?: string;
}

export interface ScenarioRunCheckpoint {
  runRid: string;
  scenarioRid: string;
  status: ScenarioRun['status'];
  completed?: string[];
  lastActivity?: string;
  attemptsById?: Record<string, number>;
  error?: string;
  updatedAt?: string;
}

export interface ScenarioRunRecord {
  rid: string;
  scenarioRid: string;
  status: ScenarioRun['status'];
  error?: string;
  checkpoint?: ScenarioRunCheckpoint;
  startedAt?: string;
  completedAt?: string | null;
  createdAt?: string;
}

export interface ApplyToMainInput {
  scenarioRid: string;
}

export class VertexHttpError extends Error {
  public status: number;
  public path: string;

  constructor(status: number, path: string, body: string) {
    super(`Vertex SDK: ${status} on ${path}: ${body}`);
    this.status = status;
    this.path = path;
  }
}

export class VertexScenarioRunPollingTimeoutError extends Error {
  constructor(timeoutMs: number) {
    super(`Vertex SDK: scenario run polling timed out after ${timeoutMs} ms`);
    this.name = 'VertexScenarioRunPollingTimeoutError';
  }
}

export class VertexScenarioRunPollingAbortedError extends Error {
  constructor() {
    super('Vertex SDK: scenario run polling was aborted');
    this.name = 'VertexScenarioRunPollingAbortedError';
  }
}

export class VertexClient {
  private baseUrl: string;
  private fetchImpl: typeof fetch;

  constructor(opts: VertexClientOptions = {}) {
    this.baseUrl = opts.baseUrl ?? 'http://localhost:9117';
    this.fetchImpl = opts.fetch ?? globalThis.fetch.bind(globalThis);
  }

  get scenarios() {
    return {
      create: (input: ScenarioCreateInput) => this.scenarioCreate(input),
      run: <O extends ScenarioRunOptions>(
        rid: string,
        opts?: O,
      ): O extends { streaming: true }
        ? Promise<AsyncIterable<RunEvent>>
        : Promise<ScenarioRun> => this.scenarioRun(rid, opts) as never,
      getRun: (scenarioRid: string, runRid: string): Promise<ScenarioRunRecord> =>
        this.scenarioGetRun(scenarioRid, runRid),
      waitForRun: (
        scenarioRid: string,
        runRid: string,
        opts?: ScenarioRunPollOptions,
      ): Promise<ScenarioRunRecord> => this.scenarioWaitForRun(scenarioRid, runRid, opts),
      applyToMain: (input: ApplyToMainInput) => this.scenarioApplyToMain(input),
    };
  }

  private async scenarioCreate(input: ScenarioCreateInput): Promise<Scenario> {
    return this.postJSON<Scenario>('/api/vertex/v1/scenarios', input);
  }

  private async scenarioApplyToMain(input: ApplyToMainInput): Promise<{ ontologyCommit: string }> {
    return this.postJSON('/api/vertex/v1/scenarios/' + encodeURIComponent(input.scenarioRid) + '/apply', {});
  }

  private async scenarioRun(
    rid: string,
    opts?: ScenarioRunOptions,
  ): Promise<ScenarioRun | AsyncIterable<RunEvent>> {
    const path = '/api/vertex/v1/scenarios/' + encodeURIComponent(rid) + '/runs';
    if (!opts?.streaming) {
      return this.postJSON<ScenarioRun>(path, {});
    }
    const res = await this.fetchImpl(this.baseUrl + path, {
      method: 'POST',
      headers: { accept: 'text/event-stream' },
      body: '{}',
    });
    if (!res.ok || !res.body) {
      const text = res.body ? await res.text() : '';
      throw new VertexHttpError(res.status, path, text);
    }
    return sseAsyncIterable(res.body);
  }

  private async scenarioGetRun(scenarioRid: string, runRid: string): Promise<ScenarioRunRecord> {
    return this.getJSON<ScenarioRunRecord>(this.scenarioRunRecordPath(scenarioRid, runRid));
  }

  private async scenarioWaitForRun(
    scenarioRid: string,
    runRid: string,
    opts: ScenarioRunPollOptions = {},
  ): Promise<ScenarioRunRecord> {
    const intervalMs = Math.max(0, opts.intervalMs ?? 1000);
    const timeoutMs = Math.max(0, opts.timeoutMs ?? 60_000);
    const deadline = Date.now() + timeoutMs;
    while (true) {
      throwIfPollingAborted(opts.signal);
      if (Date.now() >= deadline) {
        throw new VertexScenarioRunPollingTimeoutError(timeoutMs);
      }
      const run = await this.scenarioGetRunWithSignal(scenarioRid, runRid, opts.signal);
      if (isTerminalScenarioRunStatus(run.status)) {
        return run;
      }
      const remainingMs = deadline - Date.now();
      if (remainingMs <= 0) {
        throw new VertexScenarioRunPollingTimeoutError(timeoutMs);
      }
      await waitForPollDelay(Math.min(intervalMs, remainingMs), opts.signal);
    }
  }

  private async scenarioGetRunWithSignal(
    scenarioRid: string,
    runRid: string,
    signal?: AbortSignal,
  ): Promise<ScenarioRunRecord> {
    return this.getJSON<ScenarioRunRecord>(this.scenarioRunRecordPath(scenarioRid, runRid), signal);
  }

  private scenarioRunRecordPath(scenarioRid: string, runRid: string): string {
    return (
      '/api/vertex/v1/scenarios/' +
      encodeURIComponent(scenarioRid) +
      '/runs/' +
      encodeURIComponent(runRid)
    );
  }

  private async postJSON<T>(path: string, body: unknown): Promise<T> {
    const res = await this.fetchImpl(this.baseUrl + path, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(body),
    });
    const text = await res.text();
    if (!res.ok) throw new VertexHttpError(res.status, path, text);
    return JSON.parse(text) as T;
  }

  private async getJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
    let res: Response;
    try {
      res = await this.fetchImpl(this.baseUrl + path, {
        method: 'GET',
        signal,
      });
    } catch (err) {
      if (signal?.aborted) {
        throw new VertexScenarioRunPollingAbortedError();
      }
      throw err;
    }
    const text = await res.text();
    if (!res.ok) throw new VertexHttpError(res.status, path, text);
    return JSON.parse(text) as T;
  }
}

function isTerminalScenarioRunStatus(status: ScenarioRun['status']): boolean {
  return status === 'succeeded' || status === 'failed' || status === 'canceled';
}

function throwIfPollingAborted(signal?: AbortSignal): void {
  if (signal?.aborted) {
    throw new VertexScenarioRunPollingAbortedError();
  }
}

function waitForPollDelay(ms: number, signal?: AbortSignal): Promise<void> {
  throwIfPollingAborted(signal);
  if (ms <= 0) return Promise.resolve();
  return new Promise((resolve, reject) => {
    let timeoutId: ReturnType<typeof setTimeout> | null = null;
    const cleanup = () => {
      if (timeoutId !== null) {
        clearTimeout(timeoutId);
        timeoutId = null;
      }
      signal?.removeEventListener('abort', onAbort);
    };
    const onAbort = () => {
      cleanup();
      reject(new VertexScenarioRunPollingAbortedError());
    };
    timeoutId = setTimeout(() => {
      cleanup();
      resolve();
    }, ms);
    signal?.addEventListener('abort', onAbort, { once: true });
  });
}

// sseAsyncIterable parses an SSE response body into a stream of RunEvent.
// Each event block ends in a blank line; data: lines accumulate into the
// event payload. The wire format is locked by VTX-044 — keep this parser
// minimal so it lines up with whatever EventSource shipping in the
// frontend already negotiates.
async function* sseAsyncIterable(body: ReadableStream<Uint8Array>): AsyncIterable<RunEvent> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buf = '';
  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });
    let idx: number;
    while ((idx = buf.indexOf('\n\n')) !== -1) {
      const block = buf.slice(0, idx);
      buf = buf.slice(idx + 2);
      const dataLines = block
        .split('\n')
        .filter((l) => l.startsWith('data:'))
        .map((l) => l.slice(5).trimStart());
      if (dataLines.length === 0) continue;
      try {
        yield JSON.parse(dataLines.join('\n')) as RunEvent;
      } catch {
        // Drop malformed events — the run is best-effort observability.
      }
    }
  }
}
