// Queued mutation request helper (US-452). The thin layer that sits on
// top of `request()`: it tries the network first, falls back to the
// persistent offline queue when (a) `navigator.onLine` is false at call
// time, or (b) the request throws a recognisable network-level error
// (TypeError "Failed to fetch", "NetworkError", abort). HTTP responses
// — including 4xx / 5xx — are NOT retried here; an `ApiRequestError`
// surfaces verbatim to the caller because the server actually answered
// and a replay would just hit the same validation failure.
//
// Only mutation methods (POST/PUT/PATCH/DELETE) may be queued. GETs are
// rejected outright because their semantics are "fetch fresh data" — a
// replayed GET whose response is discarded does nothing useful. The SPA
// surfaces stale GETs through `useOfflineObjectSet` (US-451) instead.
import type { QueueableMethod } from './offlineRequestQueue';
import { enqueueRequest } from './offlineRequestQueue';
import { request as defaultRequest } from '../api/client';

const QUEUEABLE: ReadonlySet<QueueableMethod> = new Set<QueueableMethod>([
  'POST',
  'PUT',
  'PATCH',
  'DELETE',
]);

export type QueuedRequestResult<T> =
  | { status: 'sent'; response: T }
  | { status: 'queued'; id: string };

let requestImpl: typeof defaultRequest = defaultRequest;
let onlineOverride: boolean | undefined;

/** Test seam — substitute the underlying request implementation. */
export function __setRequestForTests(fn: typeof defaultRequest | undefined): void {
  requestImpl = fn ?? defaultRequest;
}

/**
 * Test seam — pin `navigator.onLine`. Pass `undefined` to fall back to
 * the real navigator (or `true` when navigator is missing, e.g. SSR).
 */
export function __setNavigatorOnlineForTests(value: boolean | undefined): void {
  onlineOverride = value;
}

function isOnline(): boolean {
  if (onlineOverride !== undefined) return onlineOverride;
  if (typeof navigator === 'undefined') return true;
  // navigator.onLine is unreliable in some headless browsers but is the
  // canonical browser hint; treat undefined / non-boolean as "online" to
  // avoid surprise queueing in environments that don't implement it.
  return navigator.onLine !== false;
}

/**
 * isOfflineNetworkError returns true when the thrown value looks like a
 * connectivity failure rather than an API error. The contract is
 * intentionally narrow: a `TypeError` from `fetch()` (the standard way
 * the browser signals "couldn't reach the network") plus the explicit
 * `AbortError` / `NetworkError` names that Node's undici and some
 * service workers raise. Anything with a numeric `statusCode` (i.e. an
 * `ApiRequestError`) means the server answered and is NOT a network
 * failure.
 */
export function isOfflineNetworkError(err: unknown): boolean {
  if (!err || typeof err !== 'object') return false;
  if ('statusCode' in err && typeof (err as { statusCode?: unknown }).statusCode === 'number') {
    return false;
  }
  if (err instanceof TypeError) return true;
  const name = (err as { name?: unknown }).name;
  return name === 'NetworkError' || name === 'AbortError';
}

function assertQueueable(method: string): asserts method is QueueableMethod {
  if (!QUEUEABLE.has(method as QueueableMethod)) {
    throw new Error(
      `queuedRequest: only mutation methods (POST/PUT/PATCH/DELETE) may be queued, got ${method}`,
    );
  }
}

/**
 * queuedRequest tries the network, then falls back to the persistent
 * offline queue. Returns a discriminated union so callers can render an
 * "queued for replay" toast vs. consuming the response body.
 */
export async function queuedRequest<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<QueuedRequestResult<T>> {
  assertQueueable(method);

  if (!isOnline()) {
    const id = await enqueueRequest({ method, path, body });
    return { status: 'queued', id };
  }

  try {
    const response = await requestImpl<T>(method, path, body);
    return { status: 'sent', response };
  } catch (err) {
    if (isOfflineNetworkError(err)) {
      const id = await enqueueRequest({ method, path, body });
      return { status: 'queued', id };
    }
    throw err;
  }
}

/**
 * Replay executor that drives every queued entry back through the
 * canonical `request()` so authentication, branch scoping, and error
 * envelopes stay identical to a fresh call. Pass this as the executor
 * for `startAutoReplay` from boot wiring.
 */
export async function executeQueuedEntry(entry: {
  method: string;
  path: string;
  body?: unknown;
}): Promise<unknown> {
  return requestImpl(entry.method, entry.path, entry.body);
}
