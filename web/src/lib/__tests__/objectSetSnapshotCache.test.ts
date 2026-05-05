import { describe, it, expect, beforeEach } from 'vitest';
import {
  buildObjectSetSnapshotKey,
  fingerprintObjectSetRows,
  saveObjectSetSnapshot,
  loadObjectSetSnapshot,
  removeObjectSetSnapshot,
  detectObjectSetConflict,
  __resetObjectSetSnapshotCacheForTests,
  type ObjectSetSnapshot,
} from '../objectSetSnapshotCache';
import { __resetForTests as resetOfflineCache } from '../offlineCache';
import type { ObjectSetDefinition, WireObject } from '../../api/types';

const baseDef: ObjectSetDefinition = {
  type: 'base',
  objectType: 'Employee',
};

const altDef: ObjectSetDefinition = {
  type: 'base',
  objectType: 'Customer',
};

function row(pk: string | number, fields: Record<string, unknown> = {}): WireObject {
  return {
    __rid: `ri.test.main.object.${pk}`,
    __primaryKey: pk,
    __apiName: 'Employee',
    ...fields,
  };
}

describe('objectSetSnapshotCache (US-451)', () => {
  beforeEach(() => {
    resetOfflineCache();
    __resetObjectSetSnapshotCacheForTests();
  });

  describe('buildObjectSetSnapshotKey', () => {
    it('produces a stable key for identical inputs', () => {
      const k1 = buildObjectSetSnapshotKey('northwind', baseDef, ['name']);
      const k2 = buildObjectSetSnapshotKey('northwind', baseDef, ['name']);
      expect(k1).toBe(k2);
    });

    it('changes when the ontology changes', () => {
      const k1 = buildObjectSetSnapshotKey('northwind', baseDef, ['name']);
      const k2 = buildObjectSetSnapshotKey('chinook', baseDef, ['name']);
      expect(k1).not.toBe(k2);
    });

    it('changes when the objectSet definition changes', () => {
      const k1 = buildObjectSetSnapshotKey('northwind', baseDef, ['name']);
      const k2 = buildObjectSetSnapshotKey('northwind', altDef, ['name']);
      expect(k1).not.toBe(k2);
    });

    it('is invariant under select-array order', () => {
      const k1 = buildObjectSetSnapshotKey('northwind', baseDef, ['a', 'b']);
      const k2 = buildObjectSetSnapshotKey('northwind', baseDef, ['b', 'a']);
      expect(k1).toBe(k2);
    });

    it('always starts with the cache namespace prefix', () => {
      const k = buildObjectSetSnapshotKey('northwind', baseDef, []);
      expect(k.startsWith('us451:objectset:')).toBe(true);
    });
  });

  describe('fingerprintObjectSetRows', () => {
    it('is identical for the same row set in any order', () => {
      const a = [row('1', { name: 'A' }), row('2', { name: 'B' })];
      const b = [row('2', { name: 'B' }), row('1', { name: 'A' })];
      expect(fingerprintObjectSetRows(a)).toBe(fingerprintObjectSetRows(b));
    });

    it('changes when a row property changes', () => {
      const a = [row('1', { name: 'A' })];
      const b = [row('1', { name: 'A2' })];
      expect(fingerprintObjectSetRows(a)).not.toBe(fingerprintObjectSetRows(b));
    });

    it('changes when a row is added', () => {
      const a = [row('1', { name: 'A' })];
      const b = [row('1', { name: 'A' }), row('2', { name: 'B' })];
      expect(fingerprintObjectSetRows(a)).not.toBe(fingerprintObjectSetRows(b));
    });

    it('returns a non-empty string for empty input', () => {
      expect(fingerprintObjectSetRows([])).toBeTruthy();
    });
  });

  describe('save / load / remove round-trip', () => {
    it('persists and reloads a snapshot under a key', async () => {
      const key = buildObjectSetSnapshotKey('northwind', baseDef, ['name']);
      const rows = [row('1', { name: 'A' })];
      const snap: ObjectSetSnapshot = {
        rows,
        fingerprint: fingerprintObjectSetRows(rows),
        savedAt: 1700000000000,
      };
      await saveObjectSetSnapshot(key, snap);

      const got = await loadObjectSetSnapshot(key);
      expect(got).not.toBeNull();
      expect(got?.fingerprint).toBe(snap.fingerprint);
      expect(got?.rows).toEqual(rows);
      expect(got?.savedAt).toBe(1700000000000);
    });

    it('returns null for missing snapshot', async () => {
      const key = buildObjectSetSnapshotKey('northwind', baseDef, []);
      expect(await loadObjectSetSnapshot(key)).toBeNull();
    });

    it('removeObjectSetSnapshot deletes the cached snapshot', async () => {
      const key = buildObjectSetSnapshotKey('northwind', baseDef, []);
      const rows = [row('1')];
      const snap: ObjectSetSnapshot = {
        rows,
        fingerprint: fingerprintObjectSetRows(rows),
        savedAt: 1,
      };
      await saveObjectSetSnapshot(key, snap);
      await removeObjectSetSnapshot(key);
      expect(await loadObjectSetSnapshot(key)).toBeNull();
    });
  });

  describe('detectObjectSetConflict', () => {
    it('returns null when there is no cached snapshot', () => {
      const server = [row('1', { name: 'A' })];
      expect(detectObjectSetConflict(null, server)).toBeNull();
    });

    it('returns null when fingerprints match', () => {
      const rows = [row('1', { name: 'A' })];
      const cached: ObjectSetSnapshot = {
        rows,
        fingerprint: fingerprintObjectSetRows(rows),
        savedAt: 1,
      };
      expect(detectObjectSetConflict(cached, rows)).toBeNull();
    });

    it('reports a conflict when the server fingerprint differs', () => {
      const cachedRows = [row('1', { name: 'A' })];
      const serverRows = [row('1', { name: 'A2' })];
      const cached: ObjectSetSnapshot = {
        rows: cachedRows,
        fingerprint: fingerprintObjectSetRows(cachedRows),
        savedAt: 1,
      };
      const conflict = detectObjectSetConflict(cached, serverRows);
      expect(conflict).not.toBeNull();
      expect(conflict?.minePk).toEqual(['1']);
      expect(conflict?.serverPk).toEqual(['1']);
      expect(conflict?.minePk.sort()).toEqual(['1']);
    });

    it('exposes added/removed PK lists when row sets diverge', () => {
      const cachedRows = [row('1'), row('2')];
      const serverRows = [row('2'), row('3')];
      const cached: ObjectSetSnapshot = {
        rows: cachedRows,
        fingerprint: fingerprintObjectSetRows(cachedRows),
        savedAt: 1,
      };
      const conflict = detectObjectSetConflict(cached, serverRows);
      expect(conflict).not.toBeNull();
      expect(conflict?.added.sort()).toEqual(['3']);
      expect(conflict?.removed.sort()).toEqual(['1']);
    });
  });
});
