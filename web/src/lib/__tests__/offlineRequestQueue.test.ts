import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import {
  enqueueRequest,
  listQueued,
  clearQueue,
  removeQueued,
  replayQueue,
  startAutoReplay,
  __setNowForTests,
  __resetForTests,
} from '../offlineRequestQueue';
import { __resetForTests as resetCache, clear as clearCache } from '../offlineCache';

describe('offlineRequestQueue (US-452)', () => {
  beforeEach(async () => {
    resetCache();
    await clearCache();
    __resetForTests();
  });

  afterEach(() => {
    __setNowForTests(undefined);
  });

  it('enqueueRequest persists FIFO entries with monotonic ids', async () => {
    __setNowForTests(() => 1000);
    const a = await enqueueRequest({ method: 'POST', path: '/api/v2/x', body: { v: 1 } });
    __setNowForTests(() => 1001);
    const b = await enqueueRequest({ method: 'POST', path: '/api/v2/y', body: { v: 2 } });
    expect(a).not.toBe(b);

    const queued = await listQueued();
    expect(queued).toHaveLength(2);
    expect(queued[0].path).toBe('/api/v2/x');
    expect(queued[1].path).toBe('/api/v2/y');
    expect(queued[0].enqueuedAt).toBe(1000);
    expect(queued[1].enqueuedAt).toBe(1001);
    expect(queued[0].retries).toBe(0);
  });

  it('removeQueued drops a single entry without disturbing siblings', async () => {
    const a = await enqueueRequest({ method: 'POST', path: '/a' });
    const b = await enqueueRequest({ method: 'POST', path: '/b' });
    await removeQueued(a);
    const remaining = await listQueued();
    expect(remaining.map((r) => r.id)).toEqual([b]);
  });

  it('clearQueue empties the persisted queue', async () => {
    await enqueueRequest({ method: 'POST', path: '/a' });
    await enqueueRequest({ method: 'POST', path: '/b' });
    await clearQueue();
    expect(await listQueued()).toEqual([]);
  });

  it('replayQueue invokes executor for every entry in FIFO order and dequeues on success', async () => {
    await enqueueRequest({ method: 'POST', path: '/first', body: 1 });
    await enqueueRequest({ method: 'PUT', path: '/second', body: 2 });

    const seen: string[] = [];
    const executor = vi.fn(async (entry: { path: string }) => {
      seen.push(entry.path);
    });

    const result = await replayQueue(executor);
    expect(result.replayed).toBe(2);
    expect(result.failed).toBe(0);
    expect(seen).toEqual(['/first', '/second']);
    expect(await listQueued()).toEqual([]);
  });

  it('replayQueue keeps entries on failure and increments retries', async () => {
    await enqueueRequest({ method: 'POST', path: '/sticky', body: { x: 1 } });

    const executor = vi.fn(async () => {
      throw new TypeError('Network error');
    });

    const result = await replayQueue(executor);
    expect(result.replayed).toBe(0);
    expect(result.failed).toBe(1);

    const remaining = await listQueued();
    expect(remaining).toHaveLength(1);
    expect(remaining[0].retries).toBe(1);
  });

  it('replayQueue stops at the first failure when stopOnFailure is true to preserve order', async () => {
    const id1 = await enqueueRequest({ method: 'POST', path: '/one' });
    const id2 = await enqueueRequest({ method: 'POST', path: '/two' });

    let calls = 0;
    const executor = vi.fn(async () => {
      calls += 1;
      if (calls === 1) throw new TypeError('Network error');
    });

    const result = await replayQueue(executor, { stopOnFailure: true });
    expect(result.replayed).toBe(0);
    expect(result.failed).toBe(1);
    expect(executor).toHaveBeenCalledTimes(1);

    const remaining = await listQueued();
    expect(remaining.map((r) => r.id)).toEqual([id1, id2]);
  });

  it('replayQueue drops entries that exceed maxRetries even when the executor still fails', async () => {
    const id = await enqueueRequest({
      method: 'POST',
      path: '/doomed',
      retries: 4,
    });
    const executor = vi.fn(async () => {
      throw new TypeError('Network error');
    });

    const result = await replayQueue(executor, { maxRetries: 5 });
    expect(result.failed).toBe(1);
    expect(result.dropped).toBe(1);

    const remaining = await listQueued();
    expect(remaining.map((r) => r.id)).not.toContain(id);
  });

  it('startAutoReplay registers an online listener and disposer removes it', () => {
    const adds: string[] = [];
    const removes: string[] = [];
    const target: Pick<EventTarget, 'addEventListener' | 'removeEventListener'> = {
      addEventListener: ((evt: string) => {
        adds.push(evt);
      }) as EventTarget['addEventListener'],
      removeEventListener: ((evt: string) => {
        removes.push(evt);
      }) as EventTarget['removeEventListener'],
    };
    const dispose = startAutoReplay(async () => undefined, { target: target as EventTarget });
    expect(adds).toEqual(['online']);
    dispose();
    expect(removes).toEqual(['online']);
  });

  it('startAutoReplay drains the queue when the online event fires', async () => {
    const target = new EventTarget();
    await enqueueRequest({ method: 'POST', path: '/will-replay' });

    const executor = vi.fn(async () => undefined);
    const dispose = startAutoReplay(executor, { target });

    // Simulate connectivity restoration.
    target.dispatchEvent(new Event('online'));
    // The handler is async; flush microtasks.
    await new Promise((r) => setTimeout(r, 0));
    expect(executor).toHaveBeenCalledTimes(1);
    expect(await listQueued()).toEqual([]);
    dispose();
  });

  it('startAutoReplay coalesces overlapping online events into a single drain', async () => {
    const target = new EventTarget();
    await enqueueRequest({ method: 'POST', path: '/first' });

    let resolveExec: (() => void) | null = null;
    const executor = vi.fn((): Promise<void> => {
      return new Promise<void>((resolve) => {
        resolveExec = resolve;
      });
    });
    const dispose = startAutoReplay(executor, { target });

    target.dispatchEvent(new Event('online'));
    target.dispatchEvent(new Event('online'));
    await new Promise((r) => setTimeout(r, 0));

    // Even though two events fired, only one executor call is in flight.
    expect(executor).toHaveBeenCalledTimes(1);
    (resolveExec as (() => void) | null)?.();
    await new Promise((r) => setTimeout(r, 0));
    dispose();
  });
});
