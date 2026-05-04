// Transport layer — HTTP fetch + WebSocket abstractions used by every
// sub-client. Pluggable so unit tests can inject scripted transports
// without touching real sockets.

import type { APIError } from './openapi.js';

export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE' | 'PATCH';
  body?: unknown;
  query?: Record<string, string | number | boolean | undefined>;
  signal?: AbortSignal;
}

export interface HttpTransport {
  request<T>(path: string, opts?: RequestOptions): Promise<T>;
}

export class WeaveHttpError extends Error {
  readonly status: number;
  readonly body: unknown;
  readonly errorCode?: string;
  readonly errorName?: string;

  constructor(status: number, body: unknown, message?: string) {
    super(message ?? `Weave HTTP ${status}`);
    this.name = 'WeaveHttpError';
    this.status = status;
    this.body = body;
    if (isAPIError(body)) {
      this.errorCode = body.errorCode;
      this.errorName = body.errorName;
    }
  }
}

function isAPIError(b: unknown): b is APIError {
  return (
    typeof b === 'object' &&
    b !== null &&
    'errorCode' in b &&
    typeof (b as { errorCode: unknown }).errorCode === 'string'
  );
}

export interface ClientOptions {
  baseUrl?: string;
  token?: string;
  fetch?: typeof fetch;
  transport?: HttpTransport;
}

// FetchTransport is the default HttpTransport. Wraps the global fetch
// (Node 18+, browsers, Deno, Bun). Strips trailing slashes off baseUrl
// so callers don't have to.
export class FetchTransport implements HttpTransport {
  readonly baseUrl: string;
  readonly token?: string;
  private readonly fetchImpl: typeof fetch;

  constructor(opts: ClientOptions = {}) {
    this.baseUrl = (opts.baseUrl ?? 'http://localhost:9117').replace(/\/+$/, '');
    this.token = opts.token;
    const f = opts.fetch ?? globalThis.fetch;
    if (typeof f !== 'function') {
      throw new Error(
        'FetchTransport: globalThis.fetch is not a function — supply opts.fetch or run on Node 18+',
      );
    }
    this.fetchImpl = f;
  }

  async request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
    const url = this.buildUrl(path, opts.query);
    const headers: Record<string, string> = {
      Accept: 'application/json',
    };
    if (opts.body !== undefined) {
      headers['Content-Type'] = 'application/json';
    }
    if (this.token) {
      headers['Authorization'] = `Bearer ${this.token}`;
    }
    const init: RequestInit = {
      method: opts.method ?? 'GET',
      headers,
      signal: opts.signal,
    };
    if (opts.body !== undefined) {
      init.body = JSON.stringify(opts.body);
    }

    const res = await this.fetchImpl(url, init);
    const text = await res.text();
    let parsed: unknown = text;
    if (text.length > 0) {
      try {
        parsed = JSON.parse(text);
      } catch {
        parsed = text;
      }
    }
    if (!res.ok) {
      throw new WeaveHttpError(res.status, parsed, `Weave ${res.status} ${res.statusText}`);
    }
    return parsed as T;
  }

  private buildUrl(path: string, query?: RequestOptions['query']): string {
    let url = this.baseUrl + (path.startsWith('/') ? path : `/${path}`);
    if (query) {
      const params = new URLSearchParams();
      for (const [k, v] of Object.entries(query)) {
        if (v === undefined || v === null) continue;
        params.set(k, String(v));
      }
      const qs = params.toString();
      if (qs) url += (url.includes('?') ? '&' : '?') + qs;
    }
    return url;
  }
}

export function encodePath(segment: string): string {
  return encodeURIComponent(segment);
}
