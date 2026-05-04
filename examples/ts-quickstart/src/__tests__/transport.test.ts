import { test } from 'node:test';
import assert from 'node:assert/strict';

import { FetchTransport, WeaveHttpError } from '../transport.js';

interface FetchCall {
  url: string;
  init?: RequestInit;
}

function makeFetch(
  responder: (url: string, init?: RequestInit) => { status: number; body: string },
): { fn: typeof fetch; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const fn: typeof fetch = async (input, init) => {
    const url = typeof input === 'string' ? input : input.toString();
    calls.push({ url, init: init as RequestInit | undefined });
    const { status, body } = responder(url, init as RequestInit | undefined);
    return new Response(body, { status });
  };
  return { fn, calls };
}

test('FetchTransport.request resolves on 2xx and parses JSON', async () => {
  const { fn, calls } = makeFetch(() => ({ status: 200, body: '{"ok":true}' }));
  const t = new FetchTransport({ baseUrl: 'http://x', fetch: fn });
  const resp = await t.request<{ ok: boolean }>('/path', { query: { a: 1 } });
  assert.equal(resp.ok, true);
  assert.equal(calls[0]!.url, 'http://x/path?a=1');
});

test('FetchTransport.request raises WeaveHttpError on 4xx with parsed body', async () => {
  const { fn } = makeFetch(() => ({
    status: 400,
    body: JSON.stringify({
      errorCode: 'WEAVE_VALIDATION_FAILED',
      errorName: 'ValidationFailed',
      message: 'bad input',
    }),
  }));
  const t = new FetchTransport({ baseUrl: 'http://x', fetch: fn });
  await assert.rejects(
    () => t.request('/path'),
    (err: unknown) => {
      assert.ok(err instanceof WeaveHttpError);
      assert.equal((err as WeaveHttpError).status, 400);
      assert.equal((err as WeaveHttpError).errorCode, 'WEAVE_VALIDATION_FAILED');
      return true;
    },
  );
});

test('FetchTransport.request strips trailing slashes from baseUrl', async () => {
  const { fn, calls } = makeFetch(() => ({ status: 200, body: '{}' }));
  const t = new FetchTransport({ baseUrl: 'http://x///', fetch: fn });
  await t.request('/api/foo');
  assert.equal(calls[0]!.url, 'http://x/api/foo');
});

test('FetchTransport.request adds Bearer token header when configured', async () => {
  const { fn, calls } = makeFetch(() => ({ status: 200, body: '{}' }));
  const t = new FetchTransport({ baseUrl: 'http://x', token: 's3cret', fetch: fn });
  await t.request('/api/foo');
  const headers = (calls[0]!.init?.headers ?? {}) as Record<string, string>;
  assert.equal(headers['Authorization'], 'Bearer s3cret');
});

test('FetchTransport.request POSTs JSON body with Content-Type', async () => {
  const { fn, calls } = makeFetch(() => ({ status: 200, body: '{}' }));
  const t = new FetchTransport({ baseUrl: 'http://x', fetch: fn });
  await t.request('/api/foo', { method: 'POST', body: { a: 1 } });
  const init = calls[0]!.init!;
  assert.equal(init.method, 'POST');
  assert.equal(init.body, '{"a":1}');
  const headers = init.headers as Record<string, string>;
  assert.equal(headers['Content-Type'], 'application/json');
});
