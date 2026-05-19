import { test, expect } from '@playwright/test';
import { ONTOLOGY, skipWhenBackendDown } from './helpers';

/**
 * US-444 spec 20 — subscribe.
 *
 * Verifies the WebSocket subscribe endpoint (US-380 cursor + replay
 * protocol). The spec opens a raw WebSocket via Playwright's
 * Node runtime, requires the server's welcome frame, sends a `subscribe`
 * envelope for the customer ObjectType, and requires the subscribed
 * acknowledgement. We do NOT assert on object-change events — the seeded
 * baseline is quiet — but the welcome + subscribe acknowledgement prove
 * the route, replay cursor, and subscription registry are wired end-to-end.
 *
 * Playwright's API client does not natively speak ws://. We use the
 * `WebSocket` global available in the Node 20 runtime that Playwright
 * runs under (`@types/node` ships the lib reference) — falling back
 * to a skip when the runtime predates Node 22's stable WebSocket.
 */
test.describe('US-444 — subscribe', () => {
  test('WebSocket subscribe route accepts the upgrade', async ({ request }) => {
    await skipWhenBackendDown(request);

    // Playwright runs on Node 20+; node:ws is the canonical client lib.
    type WSCtor = new (url: string) => {
      addEventListener: (ev: string, cb: (e: MessageEvent) => void) => void;
      removeEventListener: (ev: string, cb: (e: MessageEvent) => void) => void;
      send: (data: string) => void;
      close: () => void;
      readyState: number;
    };
    const WS = (globalThis as unknown as { WebSocket?: WSCtor }).WebSocket;
    test.skip(!WS, 'native WebSocket not available in this Node runtime');

    const wsURL = `ws://localhost:9117/api/v2/ontologies/${ONTOLOGY}/subscriptions/ws`;
    const ws = new (WS as WSCtor)(wsURL);

    const welcome = parseFrame(await readStringFrame(ws, 'welcome frame'));
    expect(welcome.type, 'subscription websocket must send a welcome frame').toBe(
      'welcome',
    );
    expect(welcome.lastEventId, 'welcome frame must carry the replay cursor').toEqual(
      expect.any(Number),
    );

    ws.send(
      JSON.stringify({
        type: 'subscribe',
        data: { objectType: 'customer' },
      }),
    );

    const subscribed = parseFrame(await readStringFrame(ws, 'subscribed frame'));

    ws.close();
    expect(subscribed.type, 'subscription acknowledgement type').toBe('subscribed');
    expect(
      subscribed.subscriptionId,
      'subscription acknowledgement must include subscriptionId',
    ).toEqual(expect.any(String));
  });
});

function readStringFrame(
  ws: {
    addEventListener: (ev: string, cb: (e: MessageEvent) => void) => void;
    removeEventListener: (ev: string, cb: (e: MessageEvent) => void) => void;
  },
  label: string,
): Promise<string> {
  return new Promise((resolve, reject) => {
    let timer: ReturnType<typeof setTimeout>;
    const finish = (err: Error | null, data?: string) => {
      clearTimeout(timer);
      ws.removeEventListener('message', onMessage);
      ws.removeEventListener('error', onError);
      ws.removeEventListener('close', onClose);
      if (err) {
        reject(err);
        return;
      }
      resolve(data ?? '');
    };
    const onMessage = (ev: MessageEvent) => {
      if (typeof ev.data !== 'string') {
        finish(new Error(`${label} payload was not a string`));
        return;
      }
      finish(null, ev.data);
    };
    const onError = () => finish(new Error(`${label} websocket error`));
    const onClose = () =>
      finish(new Error(`${label} websocket closed before frame arrived`));

    timer = setTimeout(
      () => finish(new Error(`${label} did not arrive within 3s`)),
      3000,
    );
    ws.addEventListener('message', onMessage);
    ws.addEventListener('error', onError);
    ws.addEventListener('close', onClose);
  });
}

function parseFrame(text: string): {
  type?: string;
  lastEventId?: number;
  subscriptionId?: string;
} {
  return JSON.parse(text) as {
    type?: string;
    lastEventId?: number;
    subscriptionId?: string;
  };
}
