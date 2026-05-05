import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import {
  __resetForTests as resetCache,
  clear as clearCache,
} from '../offlineCache';
import {
  __resetForTests as resetQueue,
  listQueued,
} from '../offlineRequestQueue';
import {
  isOfflineNetworkError,
  queuedRequest,
  __setRequestForTests,
  __setNavigatorOnlineForTests,
} from '../queuedRequest';
import type { request as RequestFn } from '../../api/client';

const mockRequest = (impl: (method: string, path: string, body?: unknown) => Promise<unknown>): typeof RequestFn =>
  vi.fn(impl) as unknown as typeof RequestFn;

describe('queuedRequest (US-452)', () => {
  beforeEach(async () => {
    resetCache();
    await clearCache();
    resetQueue();
    __setNavigatorOnlineForTests(undefined);
  });

  afterEach(() => {
    __setRequestForTests(undefined);
    __setNavigatorOnlineForTests(undefined);
  });

  it('forwards to request() when online and returns its body verbatim', async () => {
    const requestImpl = mockRequest(async () => ({ ok: true, value: 7 }));
    __setRequestForTests(requestImpl);

    const out = await queuedRequest('POST', '/api/v2/x', { v: 1 });
    expect(out).toEqual({ status: 'sent', response: { ok: true, value: 7 } });
    expect(requestImpl).toHaveBeenCalledWith('POST', '/api/v2/x', { v: 1 });
    expect(await listQueued()).toEqual([]);
  });

  it('queues the request when offline and surfaces { status: "queued" }', async () => {
    __setNavigatorOnlineForTests(false);
    const requestImpl = mockRequest(async () => undefined);
    __setRequestForTests(requestImpl);

    const out = await queuedRequest('POST', '/api/v2/x', { v: 1 });
    expect(out.status).toBe('queued');
    expect(requestImpl).not.toHaveBeenCalled();
    const queued = await listQueued();
    expect(queued).toHaveLength(1);
    expect(queued[0].path).toBe('/api/v2/x');
    expect(queued[0].body).toEqual({ v: 1 });
  });

  it('queues when request() throws a TypeError network error', async () => {
    __setNavigatorOnlineForTests(true);
    const requestImpl = mockRequest(async () => {
      throw new TypeError('Failed to fetch');
    });
    __setRequestForTests(requestImpl);

    const out = await queuedRequest('PUT', '/api/v2/y', { v: 2 });
    expect(out.status).toBe('queued');
    expect(requestImpl).toHaveBeenCalledTimes(1);
    const queued = await listQueued();
    expect(queued).toHaveLength(1);
    expect(queued[0].path).toBe('/api/v2/y');
  });

  it('does NOT queue HTTP error responses (4xx/5xx) — only network failures', async () => {
    __setNavigatorOnlineForTests(true);
    const apiErr = Object.assign(new Error('boom'), { name: 'ApiRequestError', statusCode: 422 });
    const requestImpl = mockRequest(async () => {
      throw apiErr;
    });
    __setRequestForTests(requestImpl);

    await expect(queuedRequest('POST', '/api/v2/x', { v: 1 })).rejects.toBe(apiErr);
    expect(await listQueued()).toEqual([]);
  });

  it('rejects GET requests that opted into queueing — only mutations may queue', async () => {
    __setNavigatorOnlineForTests(false);
    await expect(queuedRequest('GET', '/api/v2/things')).rejects.toThrow(
      /only mutation methods/i,
    );
    expect(await listQueued()).toEqual([]);
  });

  it('isOfflineNetworkError recognises typical fetch failures', () => {
    expect(isOfflineNetworkError(new TypeError('Failed to fetch'))).toBe(true);
    expect(isOfflineNetworkError(new TypeError('NetworkError'))).toBe(true);
    expect(isOfflineNetworkError(Object.assign(new Error('x'), { statusCode: 500 }))).toBe(false);
    expect(isOfflineNetworkError(new Error('something else'))).toBe(false);
  });
});
