import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  Subscription,
  SubscribeClient,
  WeaveOutOfDateError,
  type ChangeEvent,
} from '../subscribe.js';
import { ScriptedTransport } from './mocks.js';

test('Subscription.open handshakes with welcome → subscribed → objectChanged', async () => {
  const transport = new ScriptedTransport([
    { type: 'welcome', connectionId: 'c-1', lastEventId: 0 },
    { type: 'subscribed', subscriptionId: 's-1' },
    {
      type: 'objectChanged',
      subscriptionId: 's-1',
      cursor: 17,
      data: { state: 'ADDED_OR_UPDATED', object: { __primaryKey: 'ALFKI' } },
    },
  ]);
  const sub = await Subscription.open(
    'northwind',
    { objectType: 'Customer' },
    { transportFactory: () => transport },
  );

  assert.equal(transport.sent.length, 1);
  const sent = JSON.parse(transport.sent[0]!) as { type: string; data: { objectType: string } };
  assert.equal(sent.type, 'subscribe');
  assert.equal(sent.data.objectType, 'Customer');

  const next = await sub.next();
  assert.equal(next.done, false);
  const evt = next.value as ChangeEvent;
  assert.equal(evt.state, 'ADDED_OR_UPDATED');
  assert.equal(evt.cursor, 17);
  assert.equal(evt.subscriptionId, 's-1');
  assert.equal(sub.currentCursor, 17);
  await sub.close();
});

test('Subscription throws WeaveOutOfDateError on connection-level onOutOfDate', async () => {
  const transport = new ScriptedTransport([
    { type: 'onOutOfDate', lastEventId: 99 },
  ]);
  await assert.rejects(
    () =>
      Subscription.open(
        'northwind',
        { objectType: 'Customer' },
        { transportFactory: () => transport },
      ),
    (err: unknown) => {
      assert.ok(err instanceof WeaveOutOfDateError);
      assert.equal((err as WeaveOutOfDateError).lastEventId, 99);
      return true;
    },
  );
});

test('Subscription async iterator yields events in order', async () => {
  const transport = new ScriptedTransport([
    { type: 'welcome', lastEventId: 0 },
    { type: 'subscribed', subscriptionId: 's-1' },
    {
      type: 'objectChanged',
      subscriptionId: 's-1',
      cursor: 1,
      data: { state: 'ADDED_OR_UPDATED', object: { __primaryKey: 'A' } },
    },
    {
      type: 'objectChanged',
      subscriptionId: 's-1',
      cursor: 2,
      data: { state: 'DELETED', object: { __primaryKey: 'B' } },
    },
  ]);
  const sub = await Subscription.open(
    'northwind',
    { objectType: 'Customer' },
    { transportFactory: () => transport },
  );

  const collected: ChangeEvent[] = [];
  for (let i = 0; i < 2; i++) {
    const next = await sub.next();
    if (next.done) break;
    collected.push(next.value);
  }
  await sub.close();

  assert.equal(collected.length, 2);
  assert.equal(collected[0]!.state, 'ADDED_OR_UPDATED');
  assert.equal(collected[1]!.state, 'DELETED');
  assert.equal(sub.currentCursor, 2);
});

test('SubscribeClient.objects passes baseUrl + token through', async () => {
  let observedUrl = '';
  const transport = new ScriptedTransport([
    { type: 'welcome' },
    { type: 'subscribed', subscriptionId: 's-1' },
  ]);
  const wrapped = {
    connect: async (url: string): Promise<void> => {
      observedUrl = url;
      await transport.connect(url);
    },
    send: transport.send.bind(transport),
    recv: transport.recv.bind(transport),
    close: transport.close.bind(transport),
  };

  const client = new SubscribeClient('http://localhost:9117', 'sec');
  const sub = await client.objects(
    'northwind',
    { objectType: 'Customer' },
    { transportFactory: () => wrapped, since: 12 },
  );
  await sub.close();

  assert.match(observedUrl, /^ws:\/\/localhost:9117\/api\/v2\/ontologies\/northwind\/subscriptions\/ws/);
  assert.match(observedUrl, /since=12/);
  assert.match(observedUrl, /token=sec/);
});

test('SubscribeClient routes per-call options over default token', async () => {
  let observedUrl = '';
  const transport = new ScriptedTransport([
    { type: 'welcome' },
    { type: 'subscribed', subscriptionId: 's-1' },
  ]);
  const wrapped = {
    connect: async (url: string): Promise<void> => {
      observedUrl = url;
      await transport.connect(url);
    },
    send: transport.send.bind(transport),
    recv: transport.recv.bind(transport),
    close: transport.close.bind(transport),
  };
  const client = new SubscribeClient('http://localhost:9117', 'default-tok');
  const sub = await client.objects(
    'northwind',
    { objectType: 'Order' },
    { transportFactory: () => wrapped, token: 'override-tok' },
  );
  await sub.close();
  assert.match(observedUrl, /token=override-tok/);
});
