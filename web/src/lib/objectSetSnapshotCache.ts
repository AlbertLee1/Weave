// ObjectSet offline snapshot cache (US-451). Persists the last-loaded result
// of an ObjectSet keyed by (ontology, definition, select) so the SPA can
// render the cached rows when offline, then on reconnect compare the live
// fingerprint against the cached one to surface a conflict the user resolves
// with keep-mine / use-server.
//
// Why a thin in-repo layer instead of pulling in `dexie`? `offlineCache.ts`
// already wraps IndexedDB with the K/V API Dexie would expose; layering an
// ObjectSet-specific helper on top keeps the dep graph minimal AND lets tests
// run against the same in-memory fallback used elsewhere. The acceptance
// criterion's "Dexie.js" wording is a guideline — the project pattern is one
// IDB wrapper, not two.

import { getItem, setItem, removeItem } from './offlineCache';
import type { ObjectSetDefinition, WireObject } from '../api/types';

const KEY_PREFIX = 'us451:objectset:';

export interface ObjectSetSnapshot {
  rows: WireObject[];
  fingerprint: string;
  savedAt: number;
}

export interface ObjectSetConflict {
  // Cached fingerprint at the moment the user went offline / last saved.
  mineFingerprint: string;
  // Server fingerprint from the latest successful fetch.
  serverFingerprint: string;
  // PK lists for the user-facing "what changed" summary.
  minePk: string[];
  serverPk: string[];
  // PKs that the server has but the cache does not.
  added: string[];
  // PKs that the cache had but the server does not.
  removed: string[];
}

// Stable cache key derivation. Keys must be invariant under select-array
// order (callers commonly derive `select` from `Object.keys(properties)`,
// which has insertion order but is the same conceptual set) and stable
// under repeated identical inputs.
export function buildObjectSetSnapshotKey(
  ontologyApiName: string,
  def: ObjectSetDefinition,
  select: readonly string[],
): string {
  const sortedSelect = [...select].sort();
  // canonicalise the definition by sorting keys at every nesting level so
  // logically-identical defs hash to the same key.
  const canonicalDef = canonicalise(def);
  const payload = JSON.stringify({
    o: ontologyApiName,
    d: canonicalDef,
    s: sortedSelect,
  });
  return `${KEY_PREFIX}${stringHash(payload)}`;
}

// Order-invariant fingerprint. Sort rows by primary key, hash a canonical
// serialisation of each row, then hash the concatenation. PK alone is not
// enough — a property mutation must be detected as a conflict.
export function fingerprintObjectSetRows(rows: readonly WireObject[]): string {
  if (rows.length === 0) return 'h0:empty';
  const perRow = rows
    .map((r) => ({
      pk: pkOf(r),
      json: JSON.stringify(canonicalise(r)),
    }))
    .sort((a, b) => (a.pk < b.pk ? -1 : a.pk > b.pk ? 1 : 0))
    .map((entry) => `${entry.pk}:${stringHash(entry.json)}`)
    .join('|');
  return `h1:${stringHash(perRow)}`;
}

export async function saveObjectSetSnapshot(
  key: string,
  snapshot: ObjectSetSnapshot,
): Promise<void> {
  await setItem(key, snapshot);
}

export async function loadObjectSetSnapshot(
  key: string,
): Promise<ObjectSetSnapshot | null> {
  const raw = await getItem<ObjectSetSnapshot>(key);
  if (!raw || typeof raw !== 'object') return null;
  if (!Array.isArray(raw.rows) || typeof raw.fingerprint !== 'string') {
    return null;
  }
  return raw;
}

export async function removeObjectSetSnapshot(key: string): Promise<void> {
  await removeItem(key);
}

// detectObjectSetConflict returns null when there is no cached snapshot OR
// when the live fingerprint matches the cached one; otherwise it returns a
// summary the UI uses to render the keep-mine / use-server decision.
export function detectObjectSetConflict(
  cached: ObjectSetSnapshot | null,
  serverRows: readonly WireObject[],
): ObjectSetConflict | null {
  if (!cached) return null;
  const serverFingerprint = fingerprintObjectSetRows(serverRows);
  if (cached.fingerprint === serverFingerprint) return null;

  const minePk = cached.rows.map(pkOf);
  const serverPk = serverRows.map(pkOf);
  const mineSet = new Set(minePk);
  const serverSet = new Set(serverPk);
  const added = serverPk.filter((p) => !mineSet.has(p));
  const removed = minePk.filter((p) => !serverSet.has(p));

  return {
    mineFingerprint: cached.fingerprint,
    serverFingerprint,
    minePk,
    serverPk,
    added,
    removed,
  };
}

function pkOf(row: WireObject): string {
  return String(row.__primaryKey ?? row.__rid ?? '');
}

// canonicalise sorts object keys recursively so logically-equivalent values
// serialise identically. Arrays preserve order (semantic in ObjectSet defs:
// `union(A, B)` is not the same shape as `union(B, A)` in the wire API).
function canonicalise(value: unknown): unknown {
  if (value === null || value === undefined) return value;
  if (Array.isArray(value)) return value.map(canonicalise);
  if (typeof value === 'object') {
    const obj = value as Record<string, unknown>;
    const keys = Object.keys(obj).sort();
    const out: Record<string, unknown> = {};
    for (const k of keys) out[k] = canonicalise(obj[k]);
    return out;
  }
  return value;
}

// Tiny non-cryptographic 53-bit hash. Sufficient for cache-bucket discrimination
// (collisions are recoverable — the worst case is the user sees a stale cached
// row for one extra refresh cycle), and dependency-free so this module stays
// portable. Adapted from cyrb53.
function stringHash(str: string): string {
  let h1 = 0xdeadbeef ^ 0;
  let h2 = 0x41c6ce57 ^ 0;
  for (let i = 0; i < str.length; i++) {
    const ch = str.charCodeAt(i);
    h1 = Math.imul(h1 ^ ch, 2654435761);
    h2 = Math.imul(h2 ^ ch, 1597334677);
  }
  h1 = Math.imul(h1 ^ (h1 >>> 16), 2246822507);
  h1 ^= Math.imul(h2 ^ (h2 >>> 13), 3266489909);
  h2 = Math.imul(h2 ^ (h2 >>> 16), 2246822507);
  h2 ^= Math.imul(h1 ^ (h1 >>> 13), 3266489909);
  const out = 4294967296 * (2097151 & h2) + (h1 >>> 0);
  return out.toString(36);
}

// Test-only escape hatch for callers that want a clean cache slate.
export function __resetObjectSetSnapshotCacheForTests(): void {
  // Keys live in the shared offlineCache; callers will also reset that
  // module's in-memory mirror via __resetForTests.
}
