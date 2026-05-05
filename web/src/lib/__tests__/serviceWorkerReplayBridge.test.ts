import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import { attachServiceWorkerReplayBridge } from '../serviceWorker';
import {
  __resetForTests as resetCache,
  clear as clearCache,
} from '../offlineCache';
import {
  enqueueRequest,
  listQueued,
  __resetForTests as resetQueue,
} from '../offlineRequestQueue';
import { __setRequestForTests } from '../queuedRequest';
import type { request as RequestFn } from '../../api/client';

const mockRequest = (impl: (method: string, path: string, body?: unknown) => Promise<unknown>): typeof RequestFn =>
  vi.fn(impl) as unknown as typeof RequestFn;

describe('attachServiceWorkerReplayBridge (US-452)', () => {
  beforeEach(async () => {
    resetCache();
    await clearCache();
    resetQueue();
  });

  afterEach(() => {
    __setRequestForTests(undefined);
  });

  it('drains the queue when a replay-hint message arrives', async () => {
    const requestImpl = mockRequest(async () => undefined);
    __setRequestForTests(requestImpl);
    await enqueueRequest({ method: 'POST', path: '/api/v2/x', body: { v: 1 } });

    const target = new EventTarget();
    const dispose = attachServiceWorkerReplayBridge(target);

    // Mimic the SW's broadcast envelope shape.
    target.dispatchEvent(
      Object.assign(new Event('message'), {
        data: { type: 'weave/offline-replay-hint', method: 'POST', path: '/api/v2/x' },
      }),
    );
    // Drain microtasks: the handler awaits replayQueue, which awaits the executor.
    await new Promise((r) => setTimeout(r, 0));
    await new Promise((r) => setTimeout(r, 0));

    expect(requestImpl).toHaveBeenCalledTimes(1);
    expect(await listQueued()).toEqual([]);
    dispose();
  });

  it('ignores messages that are not replay hints', async () => {
    const requestImpl = mockRequest(async () => undefined);
    __setRequestForTests(requestImpl);
    await enqueueRequest({ method: 'POST', path: '/api/v2/x', body: { v: 1 } });

    const target = new EventTarget();
    const dispose = attachServiceWorkerReplayBridge(target);

    target.dispatchEvent(
      Object.assign(new Event('message'), { data: { type: 'something-else' } }),
    );
    target.dispatchEvent(
      Object.assign(new Event('message'), { data: 'plain string payload' }),
    );
    await new Promise((r) => setTimeout(r, 0));

    expect(requestImpl).not.toHaveBeenCalled();
    expect(await listQueued()).toHaveLength(1);
    dispose();
  });

  it('disposer detaches the listener', async () => {
    const requestImpl = mockRequest(async () => undefined);
    __setRequestForTests(requestImpl);
    await enqueueRequest({ method: 'POST', path: '/api/v2/x', body: { v: 1 } });

    const target = new EventTarget();
    const dispose = attachServiceWorkerReplayBridge(target);
    dispose();

    target.dispatchEvent(
      Object.assign(new Event('message'), {
        data: { type: 'weave/offline-replay-hint' },
      }),
    );
    await new Promise((r) => setTimeout(r, 0));

    expect(requestImpl).not.toHaveBeenCalled();
  });

  it('returns a no-op disposer when no target is supplied', () => {
    const dispose = attachServiceWorkerReplayBridge(null);
    expect(typeof dispose).toBe('function');
    expect(() => dispose()).not.toThrow();
  });
});
