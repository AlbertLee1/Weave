import { test } from 'node:test';
import assert from 'node:assert/strict';

import { ObjectsClient } from '../objects.js';
import { MockHttp } from './mocks.js';

test('ObjectsClient.listObjectTypes hits /api/v2/ontologies/{ont}/objectTypes', async () => {
  const http = new MockHttp([
    { body: { data: [{ apiName: 'Customer', displayName: 'Customer' }] } },
  ]);
  const client = new ObjectsClient(http);
  const types = await client.listObjectTypes('northwind');
  assert.equal(http.calls.length, 1);
  assert.equal(http.calls[0]!.path, '/api/v2/ontologies/northwind/objectTypes');
  assert.deepEqual(types, [{ apiName: 'Customer', displayName: 'Customer' }]);
});

test('ObjectTypeClient.list passes pageSize as query param', async () => {
  const http = new MockHttp([
    { body: { data: [{ __primaryKey: 'ALFKI' }], nextPageToken: 'next' } },
  ]);
  const client = new ObjectsClient(http);
  const customers = client.of('northwind', 'Customer');
  const page = await customers.list({ pageSize: 5, orderBy: 'companyName' });
  const call = http.calls[0]!;
  assert.equal(call.path, '/api/v2/ontologies/northwind/objects/Customer');
  assert.equal(call.opts.method ?? 'GET', 'GET');
  assert.equal(call.opts.query!['pageSize'], 5);
  assert.equal(call.opts.query!['orderBy'], 'companyName');
  assert.equal(page.nextPageToken, 'next');
});

test('ObjectTypeClient.get encodes the primary key', async () => {
  const http = new MockHttp([{ body: { __primaryKey: 'A/B' } }]);
  const client = new ObjectsClient(http);
  const customers = client.of<{ __primaryKey?: string }>('northwind', 'Customer');
  await customers.get('A/B');
  assert.equal(http.calls[0]!.path, '/api/v2/ontologies/northwind/objects/Customer/A%2FB');
});

test('ObjectTypeClient.search posts where clause', async () => {
  const http = new MockHttp([{ body: { data: [] } }]);
  const client = new ObjectsClient(http);
  const customers = client.of('northwind', 'Customer');
  await customers.search({ where: { type: 'eq', field: 'country', value: 'USA' }, pageSize: 10 });
  const call = http.calls[0]!;
  assert.equal(call.path, '/api/v2/ontologies/northwind/objects/Customer/search');
  assert.equal(call.opts.method, 'POST');
  const body = call.opts.body as { where: unknown; pageSize: number };
  assert.deepEqual(body.where, { type: 'eq', field: 'country', value: 'USA' });
  assert.equal(body.pageSize, 10);
});

test('ObjectTypeClient.linkedObjects builds the right path', async () => {
  const http = new MockHttp([{ body: { data: [] } }]);
  const client = new ObjectsClient(http);
  const customers = client.of('northwind', 'Customer');
  await customers.linkedObjects('ALFKI', 'orders');
  assert.equal(
    http.calls[0]!.path,
    '/api/v2/ontologies/northwind/objects/Customer/ALFKI/links/orders',
  );
});

test('ObjectTypeClient.iterate walks pages until nextPageToken is empty', async () => {
  const http = new MockHttp([
    { body: { data: [{ __primaryKey: 'A' }, { __primaryKey: 'B' }], nextPageToken: 'p2' } },
    { body: { data: [{ __primaryKey: 'C' }] } },
  ]);
  const client = new ObjectsClient(http);
  const customers = client.of<{ __primaryKey?: string }>('northwind', 'Customer');
  const seen: string[] = [];
  for await (const row of customers.iterate({ pageSize: 2 })) {
    if (typeof row['__primaryKey'] === 'string') seen.push(row['__primaryKey']);
  }
  assert.deepEqual(seen, ['A', 'B', 'C']);
  assert.equal(http.calls.length, 2);
  assert.equal(http.calls[1]!.opts.query!['pageToken'], 'p2');
});
