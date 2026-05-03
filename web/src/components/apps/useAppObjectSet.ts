import { useCallback, useMemo, useState } from 'react';
import { useLoadObjectSet } from '../../hooks/useObjectSets';
import type {
  ObjectSetDefinition,
  OrderBy,
  WhereClause,
} from '../../api/types';
import { substituteVariables } from './layout';
import type { VariableState } from './runtime';

// US-395: Table component runtime binding. The author configures the
// Table with an `objectSet` string (either an `ri.objectSet.…` reference
// or a base ObjectType API name), a `columns` list, plus optional
// pageSize / sort / filter knobs. The runtime hook resolves those props
// against the live variable state (so `{{userId}}` in a filter value
// substitutes at runtime), constructs the canonical `ObjectSetDefinition`
// shape consumed by `useLoadObjectSet`, and exposes paged data plus
// goNext/goPrev/sortBy controls.

export type TableSortDirection = 'asc' | 'desc';

export type TableFilterOp = 'eq' | 'neq' | 'gt' | 'gte' | 'lt' | 'lte' | 'contains';

export const TABLE_FILTER_OPS: TableFilterOp[] = [
  'eq',
  'neq',
  'gt',
  'gte',
  'lt',
  'lte',
  'contains',
];

export const DEFAULT_TABLE_PAGE_SIZE = 25;

export interface TableProps {
  objectSet?: string;
  columns?: string[];
  pageSize?: number | string;
  orderByField?: string;
  orderByDirection?: TableSortDirection;
  filterField?: string;
  filterOp?: TableFilterOp;
  filterValue?: string;
}

// readTableProps tolerates the loose `Record<string, unknown>` props bag
// the editor's component instances carry — every field arrives as
// `unknown` and may legitimately be missing on legacy fixtures.
export function readTableProps(props: Record<string, unknown>): TableProps {
  const objectSet =
    typeof props.objectSet === 'string' ? props.objectSet : undefined;
  const columns = Array.isArray(props.columns)
    ? (props.columns as unknown[]).filter(
        (c): c is string => typeof c === 'string' && c.length > 0,
      )
    : undefined;
  const pageSize =
    typeof props.pageSize === 'number'
      ? props.pageSize
      : typeof props.pageSize === 'string' && props.pageSize.length > 0
        ? props.pageSize
        : undefined;
  const orderByField =
    typeof props.orderByField === 'string' && props.orderByField.length > 0
      ? props.orderByField
      : undefined;
  const orderByDirection =
    props.orderByDirection === 'desc' ? 'desc' : 'asc';
  const filterField =
    typeof props.filterField === 'string' && props.filterField.length > 0
      ? props.filterField
      : undefined;
  const filterOp = TABLE_FILTER_OPS.includes(props.filterOp as TableFilterOp)
    ? (props.filterOp as TableFilterOp)
    : 'eq';
  const filterValue =
    typeof props.filterValue === 'string' ? props.filterValue : undefined;
  return {
    objectSet,
    columns,
    pageSize,
    orderByField,
    orderByDirection,
    filterField,
    filterOp,
    filterValue,
  };
}

export function coercePageSize(raw: number | string | undefined): number {
  if (typeof raw === 'number' && Number.isFinite(raw) && raw > 0) {
    return Math.floor(raw);
  }
  if (typeof raw === 'string' && raw.length > 0) {
    const n = Number(raw);
    if (Number.isFinite(n) && n > 0) return Math.floor(n);
  }
  return DEFAULT_TABLE_PAGE_SIZE;
}

// resolveObjectSetDefinition turns the author's string into a wire-shape
// ObjectSetDefinition. Strings that look like `ri.objectSet.…` are
// treated as saved-set references; everything else is interpreted as a
// base ObjectType API name. Empty/blank strings produce `null`, which
// disables the query (useLoadObjectSet's `enabled` predicate stops the
// fetch when the definition is null).
export function resolveObjectSetDefinition(
  raw: string | undefined,
): ObjectSetDefinition | null {
  if (!raw) return null;
  const trimmed = raw.trim();
  if (!trimmed) return null;
  if (trimmed.startsWith('ri.objectSet.') || trimmed.startsWith('ri.object-set.')) {
    return { type: 'reference', reference: trimmed };
  }
  return { type: 'base', objectType: trimmed };
}

// buildWhereClause maps a (op, field, value) triple into the WhereClause
// shape understood by FilterObjectSet. The op set mirrors the most
// common subset documented in pkg/oss/where (eq/neq/gt/gte/lt/lte +
// contains for string fields). When the value is blank the filter is
// dropped — an "everything" filter produces noisy empty objects in the
// definition that confuse the canonical-JSON cache key.
export function buildWhereClause(
  field: string | undefined,
  op: TableFilterOp,
  value: string | undefined,
): WhereClause | null {
  if (!field || value === undefined || value === '') return null;
  return { type: op, field, value };
}

export interface UseAppObjectSetParams {
  ontologyApiName: string | null;
  props: Record<string, unknown>;
  state: VariableState;
  // sortOverride is the runtime user's column-header click; when set it
  // wins over the author's default `orderByField` / `orderByDirection`.
  sortOverride?: { field: string; direction: TableSortDirection } | null;
}

export interface UseAppObjectSetResult {
  enabled: boolean;
  resolved: TableProps;
  definition: ObjectSetDefinition | null;
  orderBy: OrderBy | undefined;
  data: Record<string, unknown>[];
  totalCount: number | undefined;
  pageIndex: number;
  pageSize: number;
  hasNextPage: boolean;
  hasPrevPage: boolean;
  goNext: () => void;
  goPrev: () => void;
  reset: () => void;
  loading: boolean;
  error: unknown;
}

// useAppObjectSet is the per-Table runtime data hook. The PRD AC names a
// `useObjectSet` hook; this is its App-Editor flavour, layered on top of
// the existing TanStack-Query-backed `useLoadObjectSet`. The hook owns
// the pageToken stack so prev navigation works without re-fetching all
// preceding pages.
export function useAppObjectSet(
  params: UseAppObjectSetParams,
): UseAppObjectSetResult {
  const { ontologyApiName, props, state, sortOverride } = params;

  const resolved = useMemo(() => readTableProps(props), [props]);
  const pageSize = coercePageSize(resolved.pageSize);

  // Substitute variable refs in the author's strings BEFORE building the
  // wire-shape definition so the cache key reflects the resolved values.
  // Non-string state values get stringified (toString on number/bool) so
  // the substituted output is always a string.
  const substState = useMemo(() => {
    const out: Record<string, string | number | boolean> = {};
    for (const [k, v] of Object.entries(state)) {
      out[k] = v as string | number | boolean;
    }
    return out;
  }, [state]);

  const objectSetRef = resolved.objectSet
    ? substituteVariables(resolved.objectSet, substState)
    : '';
  const filterValueResolved =
    resolved.filterValue !== undefined
      ? substituteVariables(resolved.filterValue, substState)
      : undefined;

  const baseDefinition = useMemo(
    () => resolveObjectSetDefinition(objectSetRef),
    [objectSetRef],
  );

  const definition = useMemo<ObjectSetDefinition | null>(() => {
    if (!baseDefinition) return null;
    const where = buildWhereClause(
      resolved.filterField,
      resolved.filterOp ?? 'eq',
      filterValueResolved,
    );
    if (!where) return baseDefinition;
    return { type: 'filter', objectSet: baseDefinition, where };
  }, [
    baseDefinition,
    resolved.filterField,
    resolved.filterOp,
    filterValueResolved,
  ]);

  const sortField =
    sortOverride?.field ?? resolved.orderByField ?? undefined;
  const sortDirection =
    sortOverride?.direction ?? resolved.orderByDirection ?? 'asc';

  const orderBy = useMemo<OrderBy | undefined>(() => {
    if (!sortField) return undefined;
    return { fields: [{ field: sortField, direction: sortDirection }] };
  }, [sortField, sortDirection]);

  // pageTokenStack[0] is the token that produced the current page (empty
  // string for page 0). Pushing a new entry advances; popping retreats.
  const [pageTokenStack, setPageTokenStack] = useState<string[]>(['']);
  const stableDefinitionKey = useMemo(
    () => (definition ? JSON.stringify(definition) : ''),
    [definition],
  );

  // Definition change → pagination resets so the user doesn't end up on
  // an out-of-range page after a filter narrows the result set. The
  // documented React "previous prop" reset pattern (see the React docs
  // on storing information from previous renders) is preferred over
  // useEffect here: cursor tokens are server-allocated and only valid
  // for the definition they were minted against, so we must reset
  // before the next render commits — useEffect would let the stale
  // pageToken hit the network on the very next fetch.
  const [prevDefinitionKey, setPrevDefinitionKey] = useState<string>(
    stableDefinitionKey,
  );
  if (prevDefinitionKey !== stableDefinitionKey) {
    setPrevDefinitionKey(stableDefinitionKey);
    setPageTokenStack(['']);
  }

  const currentToken = pageTokenStack[pageTokenStack.length - 1] ?? '';

  const enabled = ontologyApiName !== null && definition !== null;

  const query = useLoadObjectSet({
    ontologyApiName: ontologyApiName ?? '',
    objectSet: definition,
    select: resolved.columns ?? [],
    pageSize,
    pageToken: currentToken === '' ? undefined : currentToken,
    orderBy,
    enabled: enabled && (resolved.columns ?? []).length > 0,
  });

  const data = query.data?.data ?? [];
  const nextPageToken = query.data?.nextPageToken;
  const totalCount =
    typeof query.data?.totalCount === 'string'
      ? Number(query.data.totalCount)
      : undefined;

  const goNext = useCallback(() => {
    if (!nextPageToken) return;
    setPageTokenStack((prev) => [...prev, nextPageToken]);
  }, [nextPageToken]);

  const goPrev = useCallback(() => {
    setPageTokenStack((prev) => (prev.length <= 1 ? prev : prev.slice(0, -1)));
  }, []);

  const reset = useCallback(() => {
    setPageTokenStack(['']);
  }, []);

  return {
    enabled,
    resolved,
    definition,
    orderBy,
    data,
    totalCount: Number.isFinite(totalCount) ? totalCount : undefined,
    pageIndex: pageTokenStack.length - 1,
    pageSize,
    hasNextPage: Boolean(nextPageToken),
    hasPrevPage: pageTokenStack.length > 1,
    goNext,
    goPrev,
    reset,
    loading: query.isLoading || query.isFetching,
    error: query.error,
  };
}
