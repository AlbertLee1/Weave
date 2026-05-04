// SubscribeClient — WebSocket subscription wrapper for
// `/api/v2/ontologies/{ontology}/subscriptions/ws` (US-380 cursor + replay).
//
// Transport-pluggable so unit tests can script frame sequences without
// touching a real socket. The default WebSocketTransport uses
// `globalThis.WebSocket` (Node 22+, browsers, Deno).
//
// The wire protocol mirrors the Go server in `pkg/subscriptions/hub.go`:
//
//   Client → Server: { type: "subscribe",   data: SubscribeRequestPayload }
//   Server → Client: { type: "subscribed", subscriptionId }
//   Server → Client: { type: "objectChanged", subscriptionId, cursor, data: ObjectChangeEvent }
//   Server → Client: { type: "welcome",     connectionId, lastEventId }
//   Server → Client: { type: "onOutOfDate", subscriptionId?, lastEventId? }
//
// A connection-level `onOutOfDate` (no subscriptionId) means the saved
// cursor has fallen outside the server's 5-minute window; the iterator
// raises `WeaveOutOfDateError` so the caller can refresh full state.

import type {
  ObjectChangeEvent,
  SubscribeMessage,
  SubscribeRequestPayload,
} from './openapi.js';
import { encodePath } from './transport.js';

export interface SubscribeTransport {
  connect(url: string): Promise<void>;
  send(payload: string): Promise<void>;
  recv(): Promise<string>;
  close(): Promise<void>;
}

export type TransportFactory = () => SubscribeTransport;

export class WeaveOutOfDateError extends Error {
  readonly lastEventId: number;
  constructor(lastEventId: number) {
    super('Weave subscription out of date — refresh full state before resubscribing');
    this.name = 'WeaveOutOfDateError';
    this.lastEventId = lastEventId;
  }
}

export interface SubscribeOptions {
  baseUrl?: string;
  token?: string;
  since?: number;
  transportFactory?: TransportFactory;
}

export interface ChangeEvent extends ObjectChangeEvent {
  cursor: number;
  subscriptionId: string;
}

// WebSocketTransport is the default transport — a thin wrapper around
// the platform's WebSocket constructor. Lazy-checked at use time so
// callers who never subscribe pay no cost when WebSocket is unavailable.
class WebSocketTransport implements SubscribeTransport {
  private ws?: WebSocket;
  private readonly inbox: string[] = [];
  private readonly waiters: Array<{
    resolve: (msg: string) => void;
    reject: (err: unknown) => void;
  }> = [];
  private closed = false;
  private closeError?: unknown;

  async connect(url: string): Promise<void> {
    const ctor: typeof WebSocket | undefined = (globalThis as { WebSocket?: typeof WebSocket })
      .WebSocket;
    if (typeof ctor !== 'function') {
      throw new Error(
        'WebSocketTransport: globalThis.WebSocket is not available — supply opts.transportFactory',
      );
    }
    this.ws = new ctor(url);
    await new Promise<void>((resolve, reject) => {
      const ws = this.ws!;
      const onOpen = (): void => {
        ws.removeEventListener('error', onError as EventListener);
        resolve();
      };
      const onError = (ev: Event): void => {
        ws.removeEventListener('open', onOpen as EventListener);
        reject(new Error('WebSocket connection error: ' + (ev.type ?? 'unknown')));
      };
      ws.addEventListener('open', onOpen as EventListener, { once: true });
      ws.addEventListener('error', onError as EventListener, { once: true });
    });
    this.ws.addEventListener('message', (ev: MessageEvent) => {
      const data = typeof ev.data === 'string' ? ev.data : String(ev.data);
      const next = this.waiters.shift();
      if (next) next.resolve(data);
      else this.inbox.push(data);
    });
    this.ws.addEventListener('close', () => {
      this.closed = true;
      this.flushClose(new Error('WebSocket closed'));
    });
    this.ws.addEventListener('error', (ev: Event) => {
      this.closeError = new Error(
        'WebSocket error: ' + ((ev as ErrorEvent).message ?? ev.type ?? 'unknown'),
      );
      this.flushClose(this.closeError);
    });
  }

  async send(payload: string): Promise<void> {
    if (!this.ws) throw new Error('WebSocketTransport: not connected');
    this.ws.send(payload);
  }

  recv(): Promise<string> {
    if (this.inbox.length > 0) {
      return Promise.resolve(this.inbox.shift()!);
    }
    if (this.closed) {
      return Promise.reject(this.closeError ?? new Error('WebSocket closed'));
    }
    return new Promise<string>((resolve, reject) => {
      this.waiters.push({ resolve, reject });
    });
  }

  async close(): Promise<void> {
    this.closed = true;
    this.flushClose(new Error('closed by client'));
    if (this.ws) {
      try {
        this.ws.close();
      } catch {
        // ignore
      }
    }
  }

  private flushClose(err: unknown): void {
    while (this.waiters.length > 0) {
      const w = this.waiters.shift()!;
      w.reject(err);
    }
  }
}

// Subscription is the live async iterator handed to callers. It
// tracks the most recent cursor so a Subscribe restart could resume
// from where it left off.
export class Subscription implements AsyncIterableIterator<ChangeEvent> {
  private cursor: number;
  private subscriptionId = '';
  private closed = false;
  private buffered: ChangeEvent[] = [];
  private firstFrameSent = false;

  constructor(
    private readonly transport: SubscribeTransport,
    private readonly request: SubscribeRequestPayload,
    initialCursor = 0,
  ) {
    this.cursor = initialCursor;
  }

  static async open(
    ontology: string,
    request: SubscribeRequestPayload,
    opts: SubscribeOptions = {},
  ): Promise<Subscription> {
    const factory: TransportFactory =
      opts.transportFactory ?? ((): SubscribeTransport => new WebSocketTransport());
    const transport = factory();
    const url = buildSubscribeUrl(ontology, opts);
    await transport.connect(url);
    const sub = new Subscription(transport, request, opts.since ?? 0);
    await sub.handshake();
    return sub;
  }

  /** Most recent cursor seen on this subscription. */
  get currentCursor(): number {
    return this.cursor;
  }

  /** Subscription id assigned by the server (empty until handshake completes). */
  get id(): string {
    return this.subscriptionId;
  }

  [Symbol.asyncIterator](): AsyncIterableIterator<ChangeEvent> {
    return this;
  }

  async next(): Promise<IteratorResult<ChangeEvent>> {
    if (this.buffered.length > 0) {
      return { value: this.buffered.shift()!, done: false };
    }
    if (this.closed) {
      return { value: undefined as unknown as ChangeEvent, done: true };
    }
    while (!this.closed) {
      const raw = await this.recvOrClose();
      if (raw === null) {
        return { value: undefined as unknown as ChangeEvent, done: true };
      }
      const msg = parseMessage(raw);
      const evt = this.dispatch(msg);
      if (evt) return { value: evt, done: false };
      if (this.closed) {
        return { value: undefined as unknown as ChangeEvent, done: true };
      }
    }
    return { value: undefined as unknown as ChangeEvent, done: true };
  }

  async return(): Promise<IteratorResult<ChangeEvent>> {
    await this.close();
    return { value: undefined as unknown as ChangeEvent, done: true };
  }

  async close(): Promise<void> {
    if (this.closed) return;
    this.closed = true;
    try {
      await this.transport.close();
    } catch {
      // ignore
    }
  }

  // handshake reads the welcome envelope (which may immediately surface
  // a connection-level onOutOfDate) and pushes the SubscribeRequest.
  private async handshake(): Promise<void> {
    const welcome = await this.recvOrClose();
    if (welcome !== null) {
      const msg = parseMessage(welcome);
      if (msg.type === 'onOutOfDate' && !msg.subscriptionId) {
        await this.close();
        throw new WeaveOutOfDateError(msg.lastEventId ?? 0);
      }
      if (msg.type === 'error') {
        await this.close();
        throw new Error('Weave subscribe handshake error: ' + (msg.error ?? 'unknown'));
      }
      // welcome / replay events are buffered; first event surfaces on next().
      const evt = this.dispatch(msg);
      if (evt) this.buffered.push(evt);
    }
    const payload = JSON.stringify({ type: 'subscribe', data: this.request });
    await this.transport.send(payload);
  }

  private dispatch(msg: SubscribeMessage): ChangeEvent | undefined {
    switch (msg.type) {
      case 'welcome': {
        if (typeof msg.lastEventId === 'number' && !this.firstFrameSent) {
          this.firstFrameSent = true;
        }
        return undefined;
      }
      case 'subscribed': {
        if (msg.subscriptionId) this.subscriptionId = msg.subscriptionId;
        return undefined;
      }
      case 'objectChanged': {
        const data = msg.data as ObjectChangeEvent | undefined;
        if (!data) return undefined;
        const cursor = msg.cursor ?? 0;
        if (cursor > this.cursor) this.cursor = cursor;
        return {
          state: data.state,
          object: data.object,
          cursor,
          subscriptionId: msg.subscriptionId ?? this.subscriptionId,
        };
      }
      case 'onOutOfDate': {
        // Connection-level onOutOfDate has no subscriptionId — fatal.
        // Subscription-level onOutOfDate (with subscriptionId) is a soft
        // signal that the server dropped a frame; surfaced as a typed
        // error so the caller can decide between resync and abort.
        const err = new WeaveOutOfDateError(msg.lastEventId ?? 0);
        this.closed = true;
        throw err;
      }
      case 'error': {
        throw new Error('Weave subscribe error: ' + (msg.error ?? 'unknown'));
      }
      default:
        return undefined;
    }
  }

  private async recvOrClose(): Promise<string | null> {
    try {
      return await this.transport.recv();
    } catch (err) {
      if (this.closed) return null;
      this.closed = true;
      throw err;
    }
  }
}

export class SubscribeClient {
  constructor(
    private readonly defaultBaseUrl: string,
    private readonly defaultToken?: string,
  ) {}

  /**
   * Open a live subscription to an ObjectType.
   *
   * @example
   *   const sub = await client.subscribe.objects('northwind', { objectType: 'Customer' });
   *   for await (const evt of sub) {
   *     console.log(evt.state, evt.object['__primaryKey']);
   *   }
   */
  async objects(
    ontology: string,
    request: SubscribeRequestPayload,
    opts: SubscribeOptions = {},
  ): Promise<Subscription> {
    const merged: SubscribeOptions = {
      baseUrl: opts.baseUrl ?? this.defaultBaseUrl,
      token: opts.token ?? this.defaultToken,
      since: opts.since,
      ...(opts.transportFactory ? { transportFactory: opts.transportFactory } : {}),
    };
    return Subscription.open(ontology, request, merged);
  }
}

function parseMessage(raw: string): SubscribeMessage {
  try {
    const m = JSON.parse(raw) as SubscribeMessage;
    if (typeof m !== 'object' || m === null || typeof m.type !== 'string') {
      throw new Error('not an envelope');
    }
    return m;
  } catch (err) {
    throw new Error('Weave subscribe: malformed frame: ' + (err as Error).message);
  }
}

function buildSubscribeUrl(ontology: string, opts: SubscribeOptions): string {
  const base = (opts.baseUrl ?? 'http://localhost:9117').replace(/\/+$/, '');
  let url = base + `/api/v2/ontologies/${encodePath(ontology)}/subscriptions/ws`;
  url = url.replace(/^http:/, 'ws:').replace(/^https:/, 'wss:');
  const params = new URLSearchParams();
  if (opts.since && opts.since > 0) params.set('since', String(opts.since));
  if (opts.token) params.set('token', opts.token);
  const qs = params.toString();
  if (qs) url += '?' + qs;
  return url;
}
