import { test } from 'node:test';
import assert from 'node:assert/strict';

import { ActionsClient } from '../actions.js';
import { MockHttp } from './mocks.js';

test('ActionsClient.list pulls /actionTypes', async () => {
  const http = new MockHttp([
    { body: { data: [{ apiName: 'createOrder' }] } },
  ]);
  const client = new ActionsClient(http);
  const types = await client.list('northwind');
  assert.equal(http.calls[0]!.path, '/api/v2/ontologies/northwind/actionTypes');
  assert.equal(types[0]!.apiName, 'createOrder');
});

test('ActionsClient.apply posts parameters and returnEdits option', async () => {
  const http = new MockHttp([{ body: { edits: [] } }]);
  const client = new ActionsClient(http);
  await client.apply('northwind', 'createOrder', { customerId: 'ALFKI' }, { returnEdits: 'CHANGES' });
  const call = http.calls[0]!;
  assert.equal(call.path, '/api/v2/ontologies/northwind/actions/createOrder/apply');
  assert.equal(call.opts.method, 'POST');
  const body = call.opts.body as { parameters: Record<string, unknown>; options?: { returnEdits?: string } };
  assert.deepEqual(body.parameters, { customerId: 'ALFKI' });
  assert.equal(body.options?.returnEdits, 'CHANGES');
});

test('ActionsClient.applyBatch posts a request list', async () => {
  const http = new MockHttp([{ body: { results: [] } }]);
  const client = new ActionsClient(http);
  await client.applyBatch('northwind', 'createOrder', [
    { parameters: { customerId: 'ALFKI' } },
    { parameters: { customerId: 'BERGS' } },
  ]);
  const body = http.calls[0]!.opts.body as { requests: Array<{ parameters: Record<string, unknown> }> };
  assert.equal(body.requests.length, 2);
  assert.equal(body.requests[1]!.parameters['customerId'], 'BERGS');
});

test('ActionsClient.apply propagates branch as query', async () => {
  const http = new MockHttp([{ body: {} }]);
  const client = new ActionsClient(http);
  await client.apply('northwind', 'createOrder', {}, { branch: 'feature-x' });
  assert.equal(http.calls[0]!.opts.query!['branch'], 'feature-x');
});
