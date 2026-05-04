import { test } from 'node:test';
import assert from 'node:assert/strict';

import { FunctionsClient } from '../functions.js';
import { MockHttp } from './mocks.js';

test('FunctionsClient.list calls /functions', async () => {
  const http = new MockHttp([{ body: { data: [{ apiName: 'topProducts' }] } }]);
  const client = new FunctionsClient(http);
  const list = await client.list('northwind');
  assert.equal(http.calls[0]!.path, '/api/v2/ontologies/northwind/functions');
  assert.equal(list[0]!.apiName, 'topProducts');
});

test('FunctionsClient.execute posts parameters and unwraps {result: ...}', async () => {
  const http = new MockHttp([{ body: { result: { count: 42 } } }]);
  const client = new FunctionsClient(http);
  const got = await client.execute<{ count: number }>('northwind', 'topProducts', { limit: 10 });
  const call = http.calls[0]!;
  assert.equal(call.path, '/api/v2/ontologies/northwind/functions/topProducts/execute');
  assert.equal(call.opts.method, 'POST');
  const body = call.opts.body as { parameters: Record<string, unknown> };
  assert.deepEqual(body.parameters, { limit: 10 });
  assert.deepEqual(got, { count: 42 });
});

test('FunctionsClient.execute returns raw value when server skips the wrapper', async () => {
  const http = new MockHttp([{ body: 'plain-string' }]);
  const client = new FunctionsClient(http);
  const got = await client.execute<string>('northwind', 'echo', {});
  assert.equal(got, 'plain-string');
});

test('FunctionsClient.execute encodes name@version refs', async () => {
  const http = new MockHttp([{ body: { result: null } }]);
  const client = new FunctionsClient(http);
  await client.execute('northwind', 'topProducts@1.2.0', {});
  assert.equal(
    http.calls[0]!.path,
    '/api/v2/ontologies/northwind/functions/topProducts%401.2.0/execute',
  );
});
