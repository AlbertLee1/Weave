// VertexClient — TypeScript SDK surface for Weave Vertex (VTX-108).
//
// Mirrors the shape of the existing OmsClient module: a constructor that
// takes a base URL + fetch impl and namespaced calls grouped by resource
// (scenarios, graphs). Scenario runs follow the mounted start-and-poll
// contract: POST /runs accepts a run and GET /runs/{runRid} returns the
// persisted lifecycle record.

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

export interface ScenarioRunOptions extends ScenarioRunPollOptions {
  /** Scenario-run streaming is not mounted yet; true rejects without a request. */
  streaming?: boolean;
}

export interface ScenarioRunPollOptions {
  intervalMs?: number;
  timeoutMs?: number;
  signal?: AbortSignal;
}

export interface ScenarioRunStartResponse {
  runRid: string;
  status: 'pending';
}

export interface ScenarioRunCheckpoint {
  runRid: string;
  scenarioRid: string;
  status: ScenarioRunLifecycleStatus;
  completed?: string[];
  lastActivity?: string;
  attemptsById?: Record<string, number>;
  error?: string;
  updatedAt?: string;
}

export interface ScenarioRunRecord {
  rid: string;
  scenarioRid: string;
  status: ScenarioRunLifecycleStatus;
  error?: string;
  checkpoint?: ScenarioRunCheckpoint;
  startedAt?: string;
  completedAt?: string | null;
  createdAt?: string;
}

export type ScenarioRunLifecycleStatus =
  | 'pending'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'canceled';

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
      startRun: (rid: string): Promise<ScenarioRunStartResponse> =>
        this.scenarioStartRun(rid),
      run: (rid: string, opts?: ScenarioRunOptions): Promise<ScenarioRunRecord> =>
        this.scenarioRun(rid, opts),
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
  ): Promise<ScenarioRunRecord> {
    if (opts?.streaming) {
      throw new Error('Vertex SDK: scenario-run streaming is not mounted; use polling waitForRun');
    }
    const accepted = await this.scenarioStartRun(rid);
    if (!accepted.runRid) {
      throw new Error('Vertex SDK: scenario run start response is missing runRid');
    }
    return this.scenarioWaitForRun(rid, accepted.runRid, opts);
  }

  private async scenarioStartRun(rid: string): Promise<ScenarioRunStartResponse> {
    const path = '/api/vertex/v1/scenarios/' + encodeURIComponent(rid) + '/runs';
    return this.postJSON<ScenarioRunStartResponse>(path, {});
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

function isTerminalScenarioRunStatus(status: ScenarioRunLifecycleStatus): boolean {
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
