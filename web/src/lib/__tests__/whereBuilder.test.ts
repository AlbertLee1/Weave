import { describe, it, expect } from 'vitest';
import { buildWhereClause, buildContainsAnyTermClause } from '../whereBuilder';

describe('buildWhereClause', () => {
  it('returns undefined for empty filters', () => {
    expect(buildWhereClause([])).toBeUndefined();
  });

  it('returns single clause for one filter', () => {
    const result = buildWhereClause([
      { field: 'name', op: 'eq', value: 'Alice' },
    ]);
    expect(result).toEqual({
      type: 'eq',
      field: 'name',
      value: 'Alice',
    });
  });

  it('wraps multiple filters in AND clause', () => {
    const result = buildWhereClause([
      { field: 'name', op: 'eq', value: 'Alice' },
      { field: 'age', op: 'gt', value: 30 },
    ]);
    expect(result).toEqual({
      type: 'and',
      value: [
        { type: 'eq', field: 'name', value: 'Alice' },
        { type: 'gt', field: 'age', value: 30 },
      ],
    });
  });

  it('handles various operators', () => {
    const result = buildWhereClause([
      { field: 'status', op: 'contains', value: 'active' },
    ]);
    expect(result).toEqual({
      type: 'contains',
      field: 'status',
      value: 'active',
    });
  });
});

describe('buildContainsAnyTermClause', () => {
  it('returns undefined for empty search text', () => {
    expect(buildContainsAnyTermClause('name', '')).toBeUndefined();
    expect(buildContainsAnyTermClause('name', '   ')).toBeUndefined();
  });

  it('builds clause with single term', () => {
    const result = buildContainsAnyTermClause('name', 'Alice');
    expect(result).toEqual({
      type: 'containsAnyTerm',
      field: 'name',
      value: ['Alice'],
    });
  });

  it('splits multiple terms by whitespace', () => {
    const result = buildContainsAnyTermClause('name', 'Alice Bob');
    expect(result).toEqual({
      type: 'containsAnyTerm',
      field: 'name',
      value: ['Alice', 'Bob'],
    });
  });
});
