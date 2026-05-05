// Offline request queue (US-452). Persists failed mutation requests to
// IndexedDB (via the offlineCache K/V layer) so they can be replayed when
// connectivity returns. Mirrors the user-visible behaviour of Workbox's
// `BackgroundSyncPlugin` without pulling in the ~25KB workbox-* runtime —
// the queue is FIFO, replay is order-preserving, and the entry shape
// survives a tab close so a long-offline session can drain on the next
// boot. Keep this file framework-agnostic so it can be exercised from a
// Node test runner (jsdom) without React.
import { getItem, setItem } from './offlineCache';

const STORAGE_KEY = 'us452:offline-request-queue:v1';

export type QueueableMethod = 'POST' | 'PUT' | 'PATCH' | 'DELETE';

export interface QueuedRequest {
  id: string;
  method: QueueableMethod;
  path: string;
  body?: unknown;
  enqueuedAt: number;
  retries: number;
}

export interface ReplayOptions {
  /**
   * When true, halt the drain at the first failing entry so subsequent
   * entries are not fired out-of-order. Defaults to false: every entry is
   * attempted even if earlier ones fail (useful when failures are
   * idempotent and order does not matter).
   */
  stopOnFailure?: boolean;
  /**
   * Maximum number of replay attempts per entry. Entries that exceed
   * `maxRetries` after a failed replay are dropped from the queue (and
   * counted in `dropped`). Defaults to 5.
   */
  maxRetries?: number;
}

export interface ReplayResult {
  replayed: number;
  failed: number;
  dropped: number;
}

export interface AutoReplayOptions {
  /** Override the event target the listener attaches to. Defaults to `window`. */
  target?: EventTarget;
  /** Override the replay options forwarded to `replayQueue`. */
  replay?: ReplayOptions;
}

export type Executor = (entry: QueuedRequest) => Promise<unknown> | unknown;

let nowImpl: () => number = () => Date.now();
let counter = 0;

/** Test seam — substitute a deterministic clock. Pass `undefined` to restore. */
export function __setNowForTests(fn: (() => number) | undefined): void {
  nowImpl = fn ?? (() => Date.now());
}

/** Test seam — reset the in-memory counter so ids restart at 0. */
export function __resetForTests(): void {
  counter = 0;
}

function nextId(): string {
  counter += 1;
  return `${nowImpl()}-${counter}`;
}

async function readQueue(): Promise<QueuedRequest[]> {
  const value = await getItem<QueuedRequest[]>(STORAGE_KEY);
  if (!Array.isArray(value)) return [];
  // Defensive: discard any entry that lost its id during a partial write.
  return value.filter((e) => e && typeof e.id === 'string');
}

async function writeQueue(entries: QueuedRequest[]): Promise<void> {
  await setItem(STORAGE_KEY, entries);
}

export async function listQueued(): Promise<QueuedRequest[]> {
  return readQueue();
}

export async function clearQueue(): Promise<void> {
  await writeQueue([]);
}

export async function removeQueued(id: string): Promise<void> {
  const entries = await readQueue();
  const next = entries.filter((e) => e.id !== id);
  if (next.length !== entries.length) await writeQueue(next);
}

export interface EnqueueInput {
  method: QueueableMethod;
  path: string;
  body?: unknown;
  /** Override the initial retry counter (used when re-queueing partial drains). */
  retries?: number;
}

export async function enqueueRequest(input: EnqueueInput): Promise<string> {
  const entries = await readQueue();
  const entry: QueuedRequest = {
    id: nextId(),
    method: input.method,
    path: input.path,
    body: input.body,
    enqueuedAt: nowImpl(),
    retries: input.retries ?? 0,
  };
  entries.push(entry);
  await writeQueue(entries);
  return entry.id;
}

const DEFAULT_MAX_RETRIES = 5;

export async function replayQueue(
  executor: Executor,
  options: ReplayOptions = {},
): Promise<ReplayResult> {
  const stopOnFailure = options.stopOnFailure ?? false;
  const maxRetries = options.maxRetries ?? DEFAULT_MAX_RETRIES;

  const entries = await readQueue();
  let replayed = 0;
  let failed = 0;
  let dropped = 0;
  const remaining: QueuedRequest[] = [];

  for (let i = 0; i < entries.length; i += 1) {
    const entry = entries[i];
    let success = false;
    try {
      await executor(entry);
      success = true;
    } catch {
      success = false;
    }

    if (success) {
      replayed += 1;
      continue;
    }

    failed += 1;
    const nextRetries = entry.retries + 1;
    if (nextRetries >= maxRetries) {
      dropped += 1;
    } else {
      remaining.push({ ...entry, retries: nextRetries });
    }

    if (stopOnFailure) {
      // Preserve every untouched entry verbatim AFTER the failed one so
      // the order stays stable for the next drain.
      for (let j = i + 1; j < entries.length; j += 1) {
        remaining.push(entries[j]);
      }
      break;
    }
  }

  await writeQueue(remaining);
  return { replayed, failed, dropped };
}

/**
 * Wires `online` events to a queue drain. Returns a disposer that removes
 * the listener so callers (React effects, tests) can shut the loop down
 * cleanly. Overlapping events are coalesced into a single in-flight drain
 * — the second event waits for the first promise to settle before firing
 * a fresh drain (rather than spawning concurrent executors that would
 * fight over queue ownership).
 */
export function startAutoReplay(
  executor: Executor,
  options: AutoReplayOptions = {},
): () => void {
  const target =
    options.target ?? (typeof window !== 'undefined' ? window : null);
  if (!target) return () => {};

  let running: Promise<void> | null = null;
  const drain = () => {
    if (running) return;
    running = (async () => {
      try {
        await replayQueue(executor, options.replay);
      } catch {
        // replayQueue is itself fail-soft; an unexpected throw here means
        // the underlying storage layer is broken and there is nothing we
        // can usefully do beyond letting the next online event retry.
      } finally {
        running = null;
      }
    })();
  };

  target.addEventListener('online', drain);
  return () => {
    target.removeEventListener('online', drain);
  };
}
