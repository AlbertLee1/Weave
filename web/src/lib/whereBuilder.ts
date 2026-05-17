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
  // DOG-004: the backend converter (pkg/oss/where/converter.go)
  // unmarshals containsAnyTerm.value as a string and runs it through
  // a Bleve MatchQuery, which already tokenises on whitespace and ORs the
  // resulting terms. Sending the pre-split array form returns
  // `containsAnyTerm value must be a string` and the UI surfaces
  // `INVALID_ARGUMENT: SearchObjectsFailed`. Normalise to a single
  // space-joined string so the contract matches and multi-word searches
  // such as "OpenAI Codex" behave correctly.
  const normalized = searchText
    .trim()
    .split(/\s+/)
    .filter((t) => t.length > 0)
    .join(' ');
  if (normalized.length === 0) return undefined;
  return {
    type: 'containsAnyTerm',
    field,
    value: normalized,
  };
}
