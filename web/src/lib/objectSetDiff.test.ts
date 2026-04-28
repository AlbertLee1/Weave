import { describe, it, expect } from 'vitest';
import { diffObjectSets, type ObjectSetDiff } from './objectSetDiff';
import type { WireObject } from '../api/types';

function row(pk: string, props: Record<string, unknown> = {}): WireObject {
  return {
    __rid: `ri.weave.main.object.${pk}`,
    __primaryKey: pk,
    __apiName: 'Employee',
    ...props,
  };
}

describe('diffObjectSets', () => {
  it('returns empty buckets for two empty inputs', () => {
    const result: ObjectSetDiff = diffObjectSets([], []);
    expect(result.onlyInA).toEqual([]);
    expect(result.onlyInB).toEqual([]);
    expect(result.changed).toEqual([]);
  });

  it('classifies primary keys present only in A', () => {
    const a = [row('1', { name: 'Alice' }), row('2', { name: 'Bob' })];
    const b: WireObject[] = [];
    const { onlyInA, onlyInB, changed } = diffObjectSets(a, b);
    expect(onlyInA.map((r) => r.__primaryKey)).toEqual(['1', '2']);
    expect(onlyInB).toEqual([]);
    expect(changed).toEqual([]);
  });

  it('classifies primary keys present only in B', () => {
    const a: WireObject[] = [];
    const b = [row('3', { name: 'Carol' })];
    const { onlyInA, onlyInB, changed } = diffObjectSets(a, b);
    expect(onlyInA).toEqual([]);
    expect(onlyInB.map((r) => r.__primaryKey)).toEqual(['3']);
    expect(changed).toEqual([]);
  });

  it('classifies rows in both with identical properties as unchanged (not surfaced)', () => {
    const a = [row('1', { name: 'Alice', age: 30 })];
    const b = [row('1', { name: 'Alice', age: 30 })];
    const { onlyInA, onlyInB, changed } = diffObjectSets(a, b);
    expect(onlyInA).toEqual([]);
    expect(onlyInB).toEqual([]);
    expect(changed).toEqual([]);
  });

  it('classifies rows in both with different properties as changed', () => {
    const a = [row('1', { name: 'Alice', age: 30 })];
    const b = [row('1', { name: 'Alice', age: 31 })];
    const { onlyInA, onlyInB, changed } = diffObjectSets(a, b);
    expect(onlyInA).toEqual([]);
    expect(onlyInB).toEqual([]);
    expect(changed).toHaveLength(1);
    const entry = changed[0];
    expect(entry.primaryKey).toBe('1');
    expect(entry.fieldChanges).toEqual([
      { field: 'age', valueA: 30, valueB: 31 },
    ]);
  });

  it('reports each differing field as a separate fieldChange', () => {
    const a = [row('1', { name: 'Alice', age: 30, city: 'NYC' })];
    const b = [row('1', { name: 'Alicia', age: 31, city: 'NYC' })];
    const { changed } = diffObjectSets(a, b);
    expect(changed).toHaveLength(1);
    const fields = changed[0].fieldChanges.map((f) => f.field).sort();
    expect(fields).toEqual(['age', 'name']);
  });

  it('detects fields added in B and missing in A', () => {
    const a = [row('1', { name: 'Alice' })];
    const b = [row('1', { name: 'Alice', age: 30 })];
    const { changed } = diffObjectSets(a, b);
    expect(changed).toHaveLength(1);
    expect(changed[0].fieldChanges).toEqual([
      { field: 'age', valueA: undefined, valueB: 30 },
    ]);
  });

  it('detects fields removed from B that existed in A', () => {
    const a = [row('1', { name: 'Alice', age: 30 })];
    const b = [row('1', { name: 'Alice' })];
    const { changed } = diffObjectSets(a, b);
    expect(changed).toHaveLength(1);
    expect(changed[0].fieldChanges).toEqual([
      { field: 'age', valueA: 30, valueB: undefined },
    ]);
  });

  it('ignores envelope keys (__rid, __primaryKey, __apiName) when computing field changes', () => {
    const a = [
      {
        __rid: 'ri.a',
        __primaryKey: '1',
        __apiName: 'Employee',
        name: 'Alice',
      },
    ];
    const b = [
      {
        __rid: 'ri.b',
        __primaryKey: '1',
        __apiName: 'Employee',
        name: 'Alice',
      },
    ];
    const { changed } = diffObjectSets(a, b);
    expect(changed).toEqual([]);
  });

  it('handles a mix of all three categories', () => {
    const a = [
      row('1', { name: 'Alice' }),
      row('2', { name: 'Bob' }),
      row('3', { name: 'Carol' }),
    ];
    const b = [
      row('2', { name: 'Bob' }),
      row('3', { name: 'Carolyn' }),
      row('4', { name: 'Dave' }),
    ];
    const { onlyInA, onlyInB, changed } = diffObjectSets(a, b);
    expect(onlyInA.map((r) => r.__primaryKey)).toEqual(['1']);
    expect(onlyInB.map((r) => r.__primaryKey)).toEqual(['4']);
    expect(changed.map((c) => c.primaryKey)).toEqual(['3']);
  });

  it('treats numeric and string primary keys with the same string form as the same row', () => {
    const a = [{ ...row('1'), __primaryKey: 1 } as WireObject];
    const b = [{ ...row('1'), __primaryKey: '1' } as WireObject];
    const { onlyInA, onlyInB, changed } = diffObjectSets(a, b);
    expect(onlyInA).toEqual([]);
    expect(onlyInB).toEqual([]);
    expect(changed).toEqual([]);
  });

  it('compares structurally for object/array property values', () => {
    const a = [row('1', { tags: ['x', 'y'], meta: { k: 1 } })];
    const b = [row('1', { tags: ['x', 'y'], meta: { k: 1 } })];
    expect(diffObjectSets(a, b).changed).toEqual([]);

    const c = [row('1', { tags: ['x', 'z'] })];
    const d = [row('1', { tags: ['x', 'y'] })];
    const result = diffObjectSets(c, d);
    expect(result.changed).toHaveLength(1);
    expect(result.changed[0].fieldChanges[0].field).toBe('tags');
  });
});
