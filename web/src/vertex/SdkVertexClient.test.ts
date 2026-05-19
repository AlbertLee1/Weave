import { describe, it, expect, vi } from 'vitest';
import {
  VertexClient,
  VertexScenarioRunPollingAbortedError,
  VertexScenarioRunPollingTimeoutError,
  type RunEvent,
} from '../../../sdk/typescript/src/vertex';

function mockJSONFetch(body: unknown, init: ResponseInit = {}) {
  return vi.fn<typeof fetch>(async () =>
    new Response(JSON.stringify(body), {
      status: 200,
      headers: { 'content-type': 'application/json' },
      ...init,
    }),
  );
}

function mockSSEFetch(events: RunEvent[]) {
  const encoder = new TextEncoder();
  const stream = new ReadableStream<Uint8Array>({
    start(ctl) {
      for (const e of events) {
        ctl.enqueue(encoder.encode(`data: ${JSON.stringify(e)}\n\n`));
      }
      ctl.close();
    },
  });
  return vi.fn<typeof fetch>(async () =>
    new Response(stream, {
      status: 200,
      headers: { 'content-type': 'text/event-stream' },
    }),
  );
}

describe('VertexClient (VTX-108)', () => {
  it('scenarios.create POSTs to /api/vertex/v1/scenarios and returns the Scenario', async () => {
    const fetchImpl = mockJSONFetch({
      rid: 'ri.vertex.main.scenario.s1',
      caseStudyRid: 'ri.vertex.main.case-study.cs1',
      name: 'snowstorm',
      status: 'draft',
      immutable: false,
    });
    const client = new VertexClient({ baseUrl: 'http://x', fetch: fetchImpl as unknown as typeof fetch });
    const s = await client.scenarios.create({
      caseStudyRid: 'ri.vertex.main.case-study.cs1',
      name: 'snowstorm',
      parentOntologyCommit: 'commit-A',
    });
    expect(s.rid).toBe('ri.vertex.main.scenario.s1');
    const call = fetchImpl.mock.calls[0];
    expect(call[0]).toBe('http://x/api/vertex/v1/scenarios');
    expect((call[1] as RequestInit).method).toBe('POST');
  });

  it('scenarios.run returns a ScenarioRun when streaming is not requested', async () => {
    const fetchImpl = mockJSONFetch({
      scenarioRunRid: 'ri.vertex.main.scenario-run.r1',
      status: 'succeeded',
      durationMs: 123,
    });
    const client = new VertexClient({ baseUrl: 'http://x', fetch: fetchImpl as unknown as typeof fetch });
    const r = await client.scenarios.run('ri.vertex.main.scenario.s1');
    expect((r as { status: string }).status).toBe('succeeded');
    expect(fetchImpl.mock.calls[0][0]).toBe(
      'http://x/api/vertex/v1/scenarios/ri.vertex.main.scenario.s1/runs',
    );
  });

  it('scenarios.run returns an AsyncIterable when streaming is requested', async () => {
    const fetchImpl = mockSSEFetch([
      { kind: 'progress', percent: 25 },
      { kind: 'progress', percent: 100 },
      { kind: 'completed', scenarioRunRid: 'ri.vertex.main.scenario-run.r2' },
    ]);
    const client = new VertexClient({ baseUrl: 'http://x', fetch: fetchImpl as unknown as typeof fetch });
    const iter = (await client.scenarios.run('ri.vertex.main.scenario.s1', { streaming: true })) as AsyncIterable<RunEvent>;
    const collected: RunEvent[] = [];
    for await (const ev of iter) collected.push(ev);
    expect(collected.length).toBe(3);
    expect(collected[2].kind).toBe('completed');
  });

  it('scenarios.waitForRun polls the documented GET run route until a terminal failed record', async () => {
    const fetchImpl = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            rid: 'ri.vertex.main.scenario-run.r1',
            scenarioRid: 'ri.vertex.main.scenario.s1',
            status: 'pending',
          }),
          { status: 200 },
        ),
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            rid: 'ri.vertex.main.scenario-run.r1',
            scenarioRid: 'ri.vertex.main.scenario.s1',
            status: 'failed',
            error: 'scoring failed',
            checkpoint: {
              runRid: 'ri.vertex.main.scenario-run.r1',
              scenarioRid: 'ri.vertex.main.scenario.s1',
              status: 'failed',
              attemptsById: { score: 3 },
              error: 'scoring failed',
              updatedAt: '2026-05-20T00:00:00Z',
            },
          }),
          { status: 200 },
        ),
      );
    const client = new VertexClient({ baseUrl: 'http://x', fetch: fetchImpl as unknown as typeof fetch });

    const result = await client.scenarios.waitForRun(
      'ri.vertex.main.scenario.s1',
      'ri.vertex.main.scenario-run.r1',
      { intervalMs: 0, timeoutMs: 1000 },
    );

    expect(fetchImpl.mock.calls.map((call) => call[0])).toEqual([
      'http://x/api/vertex/v1/scenarios/ri.vertex.main.scenario.s1/runs/ri.vertex.main.scenario-run.r1',
      'http://x/api/vertex/v1/scenarios/ri.vertex.main.scenario.s1/runs/ri.vertex.main.scenario-run.r1',
    ]);
    expect((fetchImpl.mock.calls[0][1] as RequestInit).method).toBe('GET');
    expect(result.status).toBe('failed');
    expect(result.error).toBe('scoring failed');
    expect(result.checkpoint?.attemptsById?.score).toBe(3);
  });

  it('scenarios.waitForRun returns canceled terminal records without assuming success', async () => {
    const fetchImpl = mockJSONFetch({
      rid: 'ri.vertex.main.scenario-run.r1',
      scenarioRid: 'ri.vertex.main.scenario.s1',
      status: 'canceled',
      error: 'operator canceled',
      checkpoint: {
        runRid: 'ri.vertex.main.scenario-run.r1',
        scenarioRid: 'ri.vertex.main.scenario.s1',
        status: 'canceled',
        attemptsById: {},
        error: 'operator canceled',
        updatedAt: '2026-05-20T00:00:00Z',
      },
    });
    const client = new VertexClient({ baseUrl: 'http://x', fetch: fetchImpl as unknown as typeof fetch });

    const result = await client.scenarios.waitForRun(
      'ri.vertex.main.scenario.s1',
      'ri.vertex.main.scenario-run.r1',
      { intervalMs: 0 },
    );

    expect(result.status).toBe('canceled');
    expect(result.error).toBe('operator canceled');
  });

  it('scenarios.waitForRun reports timeout and abort without leaking timers', async () => {
    vi.useFakeTimers();
    const fetchImpl = vi.fn<typeof fetch>(async () =>
      new Response(
        JSON.stringify({
          rid: 'ri.vertex.main.scenario-run.r1',
          scenarioRid: 'ri.vertex.main.scenario.s1',
          status: 'running',
        }),
        { status: 200 },
      ),
    );
    const client = new VertexClient({ baseUrl: 'http://x', fetch: fetchImpl as unknown as typeof fetch });

    const timedOut = client.scenarios.waitForRun('ri.vertex.main.scenario.s1', 'ri.vertex.main.scenario-run.r1', {
      intervalMs: 1000,
      timeoutMs: 25,
    });
    const timedOutExpectation = expect(timedOut).rejects.toBeInstanceOf(VertexScenarioRunPollingTimeoutError);
    await vi.advanceTimersByTimeAsync(25);
    await timedOutExpectation;
    expect(vi.getTimerCount()).toBe(0);

    const controller = new AbortController();
    const aborted = client.scenarios.waitForRun('ri.vertex.main.scenario.s1', 'ri.vertex.main.scenario-run.r1', {
      intervalMs: 1000,
      timeoutMs: 60_000,
      signal: controller.signal,
    });
    const abortedExpectation = expect(aborted).rejects.toBeInstanceOf(VertexScenarioRunPollingAbortedError);
    await vi.advanceTimersByTimeAsync(1);
    controller.abort();
    await abortedExpectation;
    expect(vi.getTimerCount()).toBe(0);
    vi.useRealTimers();
  });

  it('scenarios.applyToMain POSTs to /apply and returns the new ontologyCommit', async () => {
    const fetchImpl = mockJSONFetch({ ontologyCommit: 'commit-B' });
    const client = new VertexClient({ baseUrl: 'http://x', fetch: fetchImpl as unknown as typeof fetch });
    const r = await client.scenarios.applyToMain({ scenarioRid: 'ri.vertex.main.scenario.s1' });
    expect(r.ontologyCommit).toBe('commit-B');
  });
});
