// Offline cache (US-354). Tiny IndexedDB-backed key/value store mirroring
// localforage's `getItem` / `setItem` / `removeItem` / `clear` / `keys`
// surface so consumers can cache last-known-good snapshots (ontology
// metadata, object lists, dashboard configs) and serve them while the user
// is offline. Falls back to an in-memory map when IndexedDB is unavailable
// (jsdom tests, blocked third-party storage, private browsing in some
// engines) so the same call sites work everywhere without conditionals.
//
// Why not pull in the `localforage` npm package? The shape we need is
// ~80 LOC; their full lib is ~30KB minified plus three driver shims that
// duplicate logic the browser already provides. Keeping it in-repo also
// means the fallback path is testable without adding `fake-indexeddb`.

const DB_NAME = 'weave-offline';
const STORE_NAME = 'kv';
const DB_VERSION = 1;

let dbPromise: Promise<IDBDatabase> | null = null;
let idbAvailable: boolean | null = null;
const memoryFallback = new Map<string, unknown>();

function hasIndexedDB(): boolean {
  if (idbAvailable !== null) return idbAvailable;
  try {
    idbAvailable =
      typeof globalThis !== 'undefined' &&
      typeof (globalThis as { indexedDB?: IDBFactory }).indexedDB !==
        'undefined';
  } catch {
    idbAvailable = false;
  }
  return idbAvailable;
}

function openDB(): Promise<IDBDatabase> {
  if (dbPromise) return dbPromise;
  dbPromise = new Promise<IDBDatabase>((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION);
    req.onupgradeneeded = () => {
      const db = req.result;
      if (!db.objectStoreNames.contains(STORE_NAME)) {
        db.createObjectStore(STORE_NAME);
      }
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error ?? new Error('indexedDB open failed'));
  });
  return dbPromise;
}

async function withStore<T>(
  mode: IDBTransactionMode,
  fn: (store: IDBObjectStore) => IDBRequest<T> | IDBRequest<T[]> | IDBRequest,
): Promise<T> {
  const db = await openDB();
  return new Promise<T>((resolve, reject) => {
    const tx = db.transaction(STORE_NAME, mode);
    const store = tx.objectStore(STORE_NAME);
    const req = fn(store);
    req.onsuccess = () => resolve(req.result as T);
    req.onerror = () => reject(req.error ?? new Error('idb request failed'));
  });
}

export async function getItem<T>(key: string): Promise<T | null> {
  if (!hasIndexedDB()) {
    return (memoryFallback.get(key) as T | undefined) ?? null;
  }
  try {
    const value = await withStore<T | undefined>('readonly', (s) => s.get(key));
    return (value as T | null) ?? null;
  } catch {
    return null;
  }
}

export async function setItem<T>(key: string, value: T): Promise<void> {
  if (!hasIndexedDB()) {
    memoryFallback.set(key, value);
    return;
  }
  try {
    await withStore<IDBValidKey>('readwrite', (s) => s.put(value, key));
  } catch {
    // Quota exceeded / private mode — fall back silently to memory.
    memoryFallback.set(key, value);
  }
}

export async function removeItem(key: string): Promise<void> {
  if (!hasIndexedDB()) {
    memoryFallback.delete(key);
    return;
  }
  try {
    await withStore<undefined>('readwrite', (s) => s.delete(key));
  } catch {
    memoryFallback.delete(key);
  }
}

export async function clear(): Promise<void> {
  memoryFallback.clear();
  if (!hasIndexedDB()) return;
  try {
    await withStore<undefined>('readwrite', (s) => s.clear());
  } catch {
    // ignore — already cleared the in-memory mirror
  }
}

export async function keys(): Promise<string[]> {
  if (!hasIndexedDB()) return Array.from(memoryFallback.keys());
  try {
    const result = await withStore<IDBValidKey[]>('readonly', (s) =>
      s.getAllKeys(),
    );
    return result.map((k) => String(k));
  } catch {
    return [];
  }
}

// Test-only escape hatch so individual tests can reset cached state without
// poking at the indexeddb internals or memoryFallback directly.
export function __resetForTests(): void {
  memoryFallback.clear();
  idbAvailable = null;
  dbPromise = null;
}
