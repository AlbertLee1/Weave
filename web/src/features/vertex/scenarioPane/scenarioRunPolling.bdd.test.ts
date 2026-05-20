import { describe, it, expect, vi } from 'vitest';
import {
  pollScenarioRunUntilTerminal,
  ScenarioRunPollingAbortedError,
  ScenarioRunPollingTimeoutError,
} from './scenarioRunAsync';

const scenarioRid = 'ri.vertex.main.scenario.s1';
const runRid = 'ri.vertex.main.scenario-run.r1';

function jsonResponse(body: unknown) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'content-type': 'application/json' },
  });
}

describe('BDD: Vertex scenario-run polling helper (SELF-469)', () => {
  it('Given a pending run, When polling is invoked, Then it calls the documented GET run route until terminal failed state', async () => {
    const fetchImpl = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(jsonResponse({ rid: runRid, scenarioRid, status: 'pending' }))
      .mockResolvedValueOnce(jsonResponse({ rid: runRid, scenarioRid, status: 'running' }))
      .mockResolvedValueOnce(
        jsonResponse({
          rid: runRid,
          scenarioRid,
          status: 'failed',
          error: 'model scorer failed',
          checkpoint: {
            runRid,
            scenarioRid,
            status: 'failed',
            attemptsById: { score: 3 },
            error: 'model scorer failed',
            updatedAt: '2026-05-20T00:00:00Z',
          },
        }),
      );

    const result = await pollScenarioRunUntilTerminal({
      scenarioRid,
      runRid,
      fetchImpl: fetchImpl as unknown as typeof fetch,
      intervalMs: 0,
      timeoutMs: 1000,
    });

    const expectedPath = `/api/vertex/v1/scenarios/${encodeURIComponent(scenarioRid)}/runs/${encodeURIComponent(runRid)}`;
    expect(fetchImpl).toHaveBeenCalledTimes(3);
    expect(fetchImpl.mock.calls.map((call) => call[0])).toEqual([expectedPath, expectedPath, expectedPath]);
    expect(result.status).toBe('failed');
    expect(result.error).toBe('model scorer failed');
    expect(result.checkpoint?.attemptsById?.score).toBe(3);
  });

  it('Given a run transitions to canceled, When polling completes, Then it returns the canceled terminal record', async () => {
    const fetchImpl = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(jsonResponse({ rid: runRid, scenarioRid, status: 'pending' }))
      .mockResolvedValueOnce(
        jsonResponse({
          rid: runRid,
          scenarioRid,
          status: 'canceled',
          error: 'operator canceled',
          checkpoint: {
            runRid,
            scenarioRid,
            status: 'canceled',
            attemptsById: {},
            error: 'operator canceled',
            updatedAt: '2026-05-20T00:00:00Z',
          },
        }),
      );

    const result = await pollScenarioRunUntilTerminal({
      scenarioRid,
      runRid,
      fetchImpl: fetchImpl as unknown as typeof fetch,
      intervalMs: 0,
      timeoutMs: 1000,
    });

    expect(result.status).toBe('canceled');
    expect(result.error).toBe('operator canceled');
    expect(result.checkpoint?.status).toBe('canceled');
  });

  it('Given polling exceeds a timeout, When the helper exits, Then it rejects and clears pending timers', async () => {
    vi.useFakeTimers();
    const fetchImpl = vi.fn<typeof fetch>(async () =>
      jsonResponse({ rid: runRid, scenarioRid, status: 'running' }),
    );

    const pending = pollScenarioRunUntilTerminal({
      scenarioRid,
      runRid,
      fetchImpl: fetchImpl as unknown as typeof fetch,
      intervalMs: 1000,
      timeoutMs: 25,
    });
    const assertion = expect(pending).rejects.toBeInstanceOf(ScenarioRunPollingTimeoutError);

    await vi.advanceTimersByTimeAsync(25);
    await assertion;
    expect(vi.getTimerCount()).toBe(0);
    vi.useRealTimers();
  });

  it('Given an abort signal fires while waiting, When the helper exits, Then it rejects without leaving timers behind', async () => {
    vi.useFakeTimers();
    const controller = new AbortController();
    const fetchImpl = vi.fn<typeof fetch>(async () =>
      jsonResponse({ rid: runRid, scenarioRid, status: 'running' }),
    );

    const pending = pollScenarioRunUntilTerminal({
      scenarioRid,
      runRid,
      fetchImpl: fetchImpl as unknown as typeof fetch,
      intervalMs: 1000,
      timeoutMs: 60_000,
      signal: controller.signal,
    });
    const assertion = expect(pending).rejects.toBeInstanceOf(ScenarioRunPollingAbortedError);

    await vi.advanceTimersByTimeAsync(1);
    controller.abort();
    await assertion;
    expect(vi.getTimerCount()).toBe(0);
    vi.useRealTimers();
  });
});
