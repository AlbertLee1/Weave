import { request, ApiRequestError } from './client';

// SqlQueryStatus mirrors the Foundry QueryStatus union returned by
// POST /api/v2/sqlQueries/execute (pkg/sqlqueries/handlers.go). Weave runs
// every query synchronously, so the wire path only ever resolves to the
// terminal "succeeded" / "failed" variants. On "succeeded" the result set
// is inlined: `columns` are the SELECT-list column names and `rows` is one
// array of cell values per result row, positionally aligned with columns.
// A zero-row SELECT still returns columns + an empty rows array. On
// "failed" both columns and rows are null.
export interface SqlQueryStatus {
  type: 'succeeded' | 'failed' | 'running' | 'canceled';
  queryId: string;
  errorMessage?: string;
  failureReason?: string;
  columns?: string[] | null;
  rows?: SqlCell[][] | null;
}

// SqlCell is a single result-grid cell. The engine returns JSON scalars
// (number / string / boolean / null) for most PG types and may return a
// nested array / object for composite or array columns; the UI renders the
// value defensively, so the type is intentionally broad.
export type SqlCell = string | number | boolean | null | unknown;

export interface ExecuteSqlQueryRequest {
  query: string;
  // Accepted by the server for Foundry SDK parity but ignored on this
  // single-machine build (no branch concept for SQL queries).
  fallbackBranchIds?: string[];
}

// executeSqlQuery POSTs a single read-only SELECT to the SQL sandbox
// endpoint and resolves with the QueryStatus.
//
// Two distinct failure surfaces are normalised into a single
// SqlQueryStatus so callers handle one shape:
//
//   1. Validation rejections (non-SELECT, stacked statements, system-table
//      access, empty query) return HTTP 400 with an ApiError body. The
//      client layer throws ApiRequestError for those; we catch it and
//      synthesise a `failed` status carrying the server's failureReason
//      (parameters.code) and reason text so the UI's single error path
//      renders them like an execution failure.
//   2. Execution failures (engine error / timeout / row cap) return HTTP
//      200 with a `failed` QueryStatus envelope — returned as-is.
export async function executeSqlQuery(
  query: string,
): Promise<SqlQueryStatus> {
  try {
    return await request<SqlQueryStatus>('POST', '/api/v2/sqlQueries/execute', {
      query,
    } satisfies ExecuteSqlQueryRequest);
  } catch (err) {
    if (err instanceof ApiRequestError) {
      const reason = err.parameters?.reason;
      // The validation handler uses apierror.NewInvalidParameter(name, …),
      // which puts the generic "INVALID_ARGUMENT" / "INTERNAL" in errorCode
      // and the SPECIFIC reason ("NonSelectQuery", "StackedStatement",
      // "SystemTableAccess", "MissingQuery", "SqlQueryEngineNotConfigured")
      // in errorName. So errorName — not errorCode — is the failureReason
      // the UI should surface, matching the wire `failed` envelope.
      return {
        type: 'failed',
        queryId: '',
        failureReason: err.errorName,
        errorMessage: reason ?? err.errorName,
        columns: null,
        rows: null,
      };
    }
    throw err;
  }
}
