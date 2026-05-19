import { describe, it, expect, vi } from 'vitest';
import { openScenarioRunStream, pollFinalState, type RunEvent } from './scenarioRunStream';

class FakeEventSource {
  static last: FakeEventSource | null = null;
  onmessage: ((ev: MessageEvent) => void) | null = null;
  onerror: ((ev: Event) => void) | null = null;
  url: string;
  closed = false;

  constructor(url: string) {
    this.url = url;
    FakeEventSource.last = this;
  }

  close() {
    this.closed = true;
  }

  pushEvent(data: unknown, lastEventId?: string) {
    if (!this.onmessage) return;
    const ev = new MessageEvent('message', {
      data: JSON.stringify(data),
      lastEventId: lastEventId ?? '',
    });
    this.onmessage(ev);
  }

  pushError() {
    this.onerror?.(new Event('error'));
  }
}

describe('openScenarioRunStream (VTX-115)', () => {
  it('iterates events and terminates after a completed event', async () => {
    const handle = openScenarioRunStream({
      scenarioRid: 'ri.vertex.main.scenario.s1',
      EventSourceCtor: FakeEventSource as unknown as typeof EventSource,
    });
    const es = FakeEventSource.last!;
    es.pushEvent({ kind: 'progress', percent: 25 } satisfies RunEvent, 'evt-1');
    es.pushEvent({ kind: 'completed', scenarioRunRid: 'r1' } satisfies RunEvent, 'evt-2');

    const got: RunEvent[] = [];
    for await (const e of handle.events) got.push(e);
    expect(got.length).toBe(2);
    expect(got[1].kind).toBe('completed');
    expect(es.closed).toBe(true);
  });

  it('reconnects on error and carries Last-Event-Id in the reconnect URL', async () => {
    vi.useFakeTimers();
    const handle = openScenarioRunStream({
      scenarioRid: 'ri.vertex.main.scenario.s1',
      EventSourceCtor: FakeEventSource as unknown as typeof EventSource,
      backoffMs: 10,
    });
    const first = FakeEventSource.last!;
    first.pushEvent({ kind: 'progress', percent: 10 }, 'evt-A');
    first.pushError();
    await vi.advanceTimersByTimeAsync(50);
    const second = FakeEventSource.last!;
    expect(second).not.toBe(first);
    expect(second.url).toContain('lastEventId=evt-A');
    second.pushEvent({ kind: 'completed', scenarioRunRid: 'r1' }, 'evt-B');

    const got: RunEvent[] = [];
    for await (const e of handle.events) got.push(e);
    expect(got.map((e) => e.kind)).toEqual(['progress', 'completed']);
    vi.useRealTimers();
  });

  it('falls back to pollFinalState after maxReconnects exhausts', async () => {
    vi.useFakeTimers();
    const fetchImpl = vi.fn(async () =>
      new Response(JSON.stringify({ status: 'succeeded', runRid: 'r-final' }), { status: 200 }),
    );
    const handle = openScenarioRunStream({
      scenarioRid: 'ri.vertex.main.scenario.s1',
      runRid: 'run-1',
      EventSourceCtor: FakeEventSource as unknown as typeof EventSource,
      fetchImpl: fetchImpl as unknown as typeof fetch,
      maxReconnects: 1,
      backoffMs: 10,
    });

    FakeEventSource.last!.pushError();
    await vi.advanceTimersByTimeAsync(20);
    FakeEventSource.last!.pushError();

    // Pump the microtask queue so pollFinalState resolves.
    await vi.runAllTimersAsync();

    const got: RunEvent[] = [];
    for await (const e of handle.events) got.push(e);
    expect(fetchImpl).toHaveBeenCalledWith(
      `/api/vertex/v1/scenarios/${encodeURIComponent('ri.vertex.main.scenario.s1')}/runs/${encodeURIComponent('run-1')}`,
    );
    expect(got.length).toBe(1);
    expect(got[0].kind).toBe('completed');
    vi.useRealTimers();
  });
});

describe('pollFinalState', () => {
  it('returns null when no runRid is supplied', async () => {
    const r = await pollFinalState(
      { scenarioRid: 's' },
      vi.fn() as unknown as typeof fetch,
    );
    expect(r).toBeNull();
  });

  it('maps a succeeded REST response to a completed event', async () => {
    const fetchImpl = vi.fn(async () =>
      new Response(JSON.stringify({ status: 'succeeded', runRid: 'r-final' }), { status: 200 }),
    );
    const r = await pollFinalState(
      { scenarioRid: 's', runRid: 'j' },
      fetchImpl as unknown as typeof fetch,
    );
    expect(fetchImpl).toHaveBeenCalledWith('/api/vertex/v1/scenarios/s/runs/j');
    expect(r).toEqual({ kind: 'completed', scenarioRunRid: 'r-final' });
  });

  it('maps a failed REST response to a failed event', async () => {
    const fetchImpl = vi.fn(async () =>
      new Response(JSON.stringify({ status: 'failed', runRid: 'r-final', error: 'model failed' }), { status: 200 }),
    );
    const r = await pollFinalState(
      { scenarioRid: 's', runRid: 'j' },
      fetchImpl as unknown as typeof fetch,
    );
    expect(r).toEqual({ kind: 'failed', scenarioRunRid: 'r-final', error: 'model failed' });
  });
});
