import type { WhereClause } from '../api/types';

export interface FilterCondition {
  field: string;
  op: string;
  value: unknown;
}

export function buildWhereClause(
  filters: FilterCondition[],
): WhereClause | undefined {
  if (filters.length === 0) return undefined;

  const clauses: WhereClause[] = filters.map((f) => ({
    type: f.op,
    field: f.field,
    value: f.value,
  }));

  if (clauses.length === 1) return clauses[0];

  return {
    type: 'and',
    value: clauses,
  };
}

export function buildContainsAnyTermClause(
  field: string,
  searchText: string,
): WhereClause | undefined {
  const terms = searchText
    .trim()
    .split(/\s+/)
    .filter((t) => t.length > 0);
  if (terms.length === 0) return undefined;
  return {
    type: 'containsAnyTerm',
    field,
    value: terms,
  };
}
