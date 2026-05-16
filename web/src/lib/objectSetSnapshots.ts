import type { ObjectSetDefinition } from '../api/types';

// LocalSnapshotEntry mirrors the metadata we persist in localStorage when the
// user freezes a saved ObjectSet via the Snapshots page. The server-side
// snapshot store is the source of truth for the resolved data; this index
// only lets the UI list the snapshots created from this browser and recover
// the original Definition so "restore" can re-open it in the composer.
export interface LocalSnapshotEntry {
  snapshotRid: string;
  ontologyApiName: string;
  objectType: string;
  savedSetId?: string;
  savedSetName?: string;
  def: ObjectSetDefinition;
  createdAt: string;
  totalCount: number;
  definitionHash?: string;
  snapshotAt?: number;
  truncated?: boolean;
}

export function snapshotsLocalStorageKey(ontologyApiName: string): string {
  return `weave.objectset.snapshots.${ontologyApiName}`;
}

export function readLocalSnapshots(ontologyApiName: string): LocalSnapshotEntry[] {
  if (!ontologyApiName || typeof window === 'undefined') return [];
  try {
    const raw = window.localStorage.getItem(snapshotsLocalStorageKey(ontologyApiName));
    if (!raw) return [];
    const parsed = JSON.parse(raw) as unknown;
    if (!Array.isArray(parsed)) return [];
    return parsed.filter(
      (x): x is LocalSnapshotEntry =>
        typeof x === 'object' &&
        x !== null &&
        typeof (x as { snapshotRid?: unknown }).snapshotRid === 'string' &&
        typeof (x as { def?: unknown }).def === 'object',
    );
  } catch {
    return [];
  }
}

export function writeLocalSnapshots(
  ontologyApiName: string,
  entries: LocalSnapshotEntry[],
): void {
  if (!ontologyApiName || typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(
      snapshotsLocalStorageKey(ontologyApiName),
      JSON.stringify(entries),
    );
  } catch {
    // ignore quota / disabled storage errors
  }
}
