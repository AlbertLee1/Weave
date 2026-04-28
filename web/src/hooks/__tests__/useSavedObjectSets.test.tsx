import { describe, it, expect, beforeEach } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { useSavedObjectSets } from '../useObjectSets';
import { localStorageKey } from '../../lib/objectSetBuilder';
import type { ObjectSetDefinition } from '../../api/types';

const ONTOLOGY = 'test-ontology';

const baseA: ObjectSetDefinition = { type: 'base', objectType: 'Employee' };
const baseB: ObjectSetDefinition = { type: 'base', objectType: 'Department' };
const filterC: ObjectSetDefinition = {
  type: 'filter',
  objectSet: { type: 'base', objectType: 'Employee' },
  where: { type: 'eq', field: 'name', value: 'Alice' },
};

beforeEach(() => {
  window.localStorage.clear();
});

describe('useSavedObjectSets versioning', () => {
  it('creates a saved set with one initial version', () => {
    const { result } = renderHook(() => useSavedObjectSets(ONTOLOGY));
    act(() => {
      result.current.save('Active employees', baseA);
    });
    expect(result.current.items).toHaveLength(1);
    const s = result.current.items[0];
    expect(s.versions).toHaveLength(1);
    expect(s.activeVersionId).toBe(s.versions[0].versionId);
    expect(s.versions[0].def).toEqual(baseA);
    expect(s.def).toEqual(baseA);
  });

  it('addVersion appends a version and switches active to it', () => {
    const { result } = renderHook(() => useSavedObjectSets(ONTOLOGY));
    act(() => {
      result.current.save('Q', baseA);
    });
    const id = result.current.items[0].id;
    act(() => {
      result.current.addVersion(id, baseB, 'broaden scope');
    });
    const s = result.current.items[0];
    expect(s.versions).toHaveLength(2);
    expect(s.def).toEqual(baseB);
    expect(s.activeVersionId).toBe(s.versions[0].versionId);
    expect(s.versions[0].def).toEqual(baseB);
    expect(s.versions[0].note).toBe('broaden scope');
    expect(s.versions[1].def).toEqual(baseA);
  });

  it('setActiveVersion switches the active version and updates def', () => {
    const { result } = renderHook(() => useSavedObjectSets(ONTOLOGY));
    act(() => {
      result.current.save('Q', baseA);
    });
    const id = result.current.items[0].id;
    act(() => {
      result.current.addVersion(id, baseB);
    });
    const oldVersionId = result.current.items[0].versions[1].versionId;
    expect(result.current.items[0].def).toEqual(baseB);

    act(() => {
      result.current.setActiveVersion(id, oldVersionId);
    });
    const s = result.current.items[0];
    expect(s.activeVersionId).toBe(oldVersionId);
    expect(s.def).toEqual(baseA);
  });

  it('removeVersion drops a version and re-elects active when needed', () => {
    const { result } = renderHook(() => useSavedObjectSets(ONTOLOGY));
    act(() => {
      result.current.save('Q', baseA);
    });
    const id = result.current.items[0].id;
    act(() => {
      result.current.addVersion(id, baseB);
    });
    act(() => {
      result.current.addVersion(id, filterC);
    });
    expect(result.current.items[0].versions).toHaveLength(3);
    const activeId = result.current.items[0].activeVersionId;

    act(() => {
      result.current.removeVersion(id, activeId);
    });
    const s = result.current.items[0];
    expect(s.versions).toHaveLength(2);
    expect(s.activeVersionId).toBe(s.versions[0].versionId);
    expect(s.def).toEqual(s.versions[0].def);
  });

  it('removeVersion refuses to delete the last remaining version', () => {
    const { result } = renderHook(() => useSavedObjectSets(ONTOLOGY));
    act(() => {
      result.current.save('Q', baseA);
    });
    const id = result.current.items[0].id;
    const onlyVersionId = result.current.items[0].versions[0].versionId;
    let removed: ReturnType<typeof result.current.removeVersion>;
    act(() => {
      removed = result.current.removeVersion(id, onlyVersionId);
    });
    expect(removed!).toBeUndefined();
    expect(result.current.items[0].versions).toHaveLength(1);
  });

  it('migrates legacy entries (no versions field) into a single-version entry', () => {
    const legacy = [
      {
        id: 'legacy-1',
        name: 'Old saved',
        def: baseA,
        createdAt: '2025-01-01T00:00:00.000Z',
      },
    ];
    window.localStorage.setItem(
      localStorageKey(ONTOLOGY),
      JSON.stringify(legacy),
    );

    const { result } = renderHook(() => useSavedObjectSets(ONTOLOGY));
    expect(result.current.items).toHaveLength(1);
    const s = result.current.items[0];
    expect(s.versions).toHaveLength(1);
    expect(s.activeVersionId).toBe(s.versions[0].versionId);
    expect(s.versions[0].def).toEqual(baseA);
    expect(s.versions[0].createdAt).toBe('2025-01-01T00:00:00.000Z');
  });

  it('persists across hook remount with stable version ids', () => {
    const { result, unmount } = renderHook(() => useSavedObjectSets(ONTOLOGY));
    act(() => {
      result.current.save('Q', baseA);
    });
    act(() => {
      result.current.addVersion(result.current.items[0].id, baseB);
    });
    const before = result.current.items[0];
    unmount();

    const { result: result2 } = renderHook(() => useSavedObjectSets(ONTOLOGY));
    const after = result2.current.items[0];
    expect(after.id).toBe(before.id);
    expect(after.versions.map((v) => v.versionId)).toEqual(
      before.versions.map((v) => v.versionId),
    );
    expect(after.activeVersionId).toBe(before.activeVersionId);
  });
});
