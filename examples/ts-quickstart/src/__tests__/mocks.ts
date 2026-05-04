// Test helpers — a recording HttpTransport and a scripted SubscribeTransport.
// Pure data structures so tests stay deterministic without spinning up a
// real server.

import type { HttpTransport, RequestOptions } from '../transport.js';
import type { SubscribeTransport } from '../subscribe.js';

export interface RecordedCall {
  path: string;
  opts: RequestOptions;
}

export interface MockResponse {
  body?: unknown;
  status?: number;
  // If set, throws this error instead of resolving.
  error?: Error;
}

// MockHttp records every call and replays a scripted queue of responses.
// FIFO: each call pulls the next response in order. If `responses` is
// exhausted, calls fall back to an empty `{}` object so tests stay green
// without micro-managing every list/get response.
export class MockHttp implements HttpTransport {
  readonly calls: RecordedCall[] = [];
  private readonly queue: MockResponse[];

  constructor(queue: MockResponse[] = []) {
    this.queue = queue;
  }

  async request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
    this.calls.push({ path, opts });
    const next = this.queue.shift();
    if (!next) return {} as T;
    if (next.error) throw next.error;
    return (next.body ?? {}) as T;
  }
}

// ScriptedTransport is a SubscribeTransport that emits a fixed list of
// frames in order. Each `recv()` pops one frame; once exhausted, recv()
// rejects to simulate a clean close.
export class ScriptedTransport implements SubscribeTransport {
  readonly sent: string[] = [];
  private readonly inbox: string[];
  private connected = false;

  constructor(frames: Array<unknown | string>) {
    this.inbox = frames.map((f) => (typeof f === 'string' ? f : JSON.stringify(f)));
  }

  async connect(_url: string): Promise<void> {
    this.connected = true;
  }

  async send(payload: string): Promise<void> {
    if (!this.connected) throw new Error('not connected');
    this.sent.push(payload);
  }

  async recv(): Promise<string> {
    if (this.inbox.length === 0) throw new Error('script exhausted');
    return this.inbox.shift()!;
  }

  async close(): Promise<void> {
    this.connected = false;
  }
}
