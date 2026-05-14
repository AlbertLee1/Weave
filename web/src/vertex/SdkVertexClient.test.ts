import { describe, it, expect, vi } from 'vitest';
import { VertexClient, type RunEvent } from '../../../sdk/typescript/src/vertex';

function mockJSONFetch(body: unknown, init: ResponseInit = {}) {
  return vi.fn(async () =>
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
  return vi.fn(async () =>
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

  it('scenarios.applyToMain POSTs to /apply and returns the new ontologyCommit', async () => {
    const fetchImpl = mockJSONFetch({ ontologyCommit: 'commit-B' });
    const client = new VertexClient({ baseUrl: 'http://x', fetch: fetchImpl as unknown as typeof fetch });
    const r = await client.scenarios.applyToMain({ scenarioRid: 'ri.vertex.main.scenario.s1' });
    expect(r.ontologyCommit).toBe('commit-B');
  });
});
