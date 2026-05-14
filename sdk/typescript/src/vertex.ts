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
   *  the call resolves with the terminal Run record once /run completes. */
  streaming?: boolean;
}

export type RunEvent =
  | { kind: 'progress'; percent: number }
  | { kind: 'log'; line: string }
  | { kind: 'retry'; activityId: string; attempt: number; error: string }
  | { kind: 'completed'; scenarioRunRid: string }
  | { kind: 'failed'; scenarioRunRid: string; error: string };

export interface ScenarioRun {
  scenarioRunRid: string;
  status: 'succeeded' | 'failed' | 'canceled';
  durationMs: number;
}

export interface ApplyToMainInput {
  scenarioRid: string;
}

export class VertexHttpError extends Error {
  constructor(public status: number, public path: string, body: string) {
    super(`Vertex SDK: ${status} on ${path}: ${body}`);
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
    const path = '/api/vertex/v1/scenarios/' + encodeURIComponent(rid) + '/run';
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
