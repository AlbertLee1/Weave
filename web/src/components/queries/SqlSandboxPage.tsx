import { useState } from 'react';
import {
  executeSqlQuery,
  type SqlQueryStatus,
  type SqlCell,
} from '../../api/sqlQueries';

// Friendly copy for each known failureReason the server can return. The
// SQL sandbox surfaces validation rejections (NonSelectQuery, etc.) and
// runtime failures (QueryTimeout, MaxRowsExceeded) through the same
// `failed` status, so we map every reason to a human-readable hint here.
const FAILURE_HINTS: Record<string, string> = {
  NonSelectQuery:
    'Only a single read-only SELECT statement is allowed. DML/DDL (INSERT, UPDATE, DELETE, DROP, …) is rejected.',
  StackedStatement:
    'Only one statement is allowed. Remove the extra statement after the semicolon.',
  SystemTableAccess:
    'Queries against pg_* and information_schema.* system tables are not allowed.',
  MissingQuery: 'Enter a SELECT statement to run.',
  QueryTimeout:
    'The query exceeded the sandbox time limit (5s). Narrow the query or add a tighter WHERE clause.',
  MaxRowsExceeded:
    'The query returned more than the 10,000-row sandbox cap. Add a LIMIT or a more selective WHERE clause.',
  ExecutionError: 'The database rejected the query. See the error detail below.',
};

function renderCell(value: SqlCell): string {
  if (value === null || value === undefined) return '';
  if (typeof value === 'object') return JSON.stringify(value);
  return String(value);
}

export function SqlSandboxPage() {
  const [query, setQuery] = useState('SELECT 1');
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState<SqlQueryStatus | null>(null);
  const [unexpectedError, setUnexpectedError] = useState<string | null>(null);

  const canRun = query.trim().length > 0 && !running;

  async function handleRun() {
    if (!canRun) return;
    setRunning(true);
    setUnexpectedError(null);
    setResult(null);
    try {
      const status = await executeSqlQuery(query);
      setResult(status);
    } catch (err) {
      // Network / unexpected (non-ApiError) failures bubble here; the API
      // layer already normalises validation + execution failures into a
      // `failed` status, so this branch is for transport-level problems.
      setUnexpectedError(
        err instanceof Error ? err.message : 'Unexpected error running query',
      );
    } finally {
      setRunning(false);
    }
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    // Cmd/Ctrl+Enter runs the query — the standard SQL-console shortcut.
    if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
      e.preventDefault();
      void handleRun();
    }
  }

  const succeeded = result?.type === 'succeeded';
  const failed = result?.type === 'failed';
  const columns = succeeded ? result?.columns ?? [] : [];
  const rows = succeeded ? result?.rows ?? [] : [];

  return (
    <div
      data-testid="sql-sandbox-page"
      className="flex flex-col h-[calc(100vh-3rem)] bg-bg-primary overflow-hidden"
    >
      <header
        className="px-6 py-4 border-b flex flex-wrap items-center gap-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <h1 className="text-base font-semibold tracking-wide text-text-primary">
          SQL Sandbox
        </h1>
        <p className="text-xs text-text-secondary">
          Run a single read-only SELECT against the datasource. Read-only,
          5s timeout, 10,000-row cap.
        </p>
      </header>

      <div className="flex flex-col gap-3 p-6 overflow-y-auto">
        <label htmlFor="sql-input" className="sr-only">
          SQL query
        </label>
        <textarea
          id="sql-input"
          data-testid="sql-input"
          aria-label="SQL query"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={handleKeyDown}
          spellCheck={false}
          rows={8}
          className="w-full font-mono text-sm rounded-md border bg-bg-secondary text-text-primary p-3 resize-y focus:outline-none focus:ring-1"
          style={{ borderColor: 'rgba(31,41,55,0.7)' }}
          placeholder="SELECT id, name FROM ... LIMIT 100"
        />

        <div className="flex items-center gap-3">
          <button
            type="button"
            data-testid="sql-run"
            onClick={() => void handleRun()}
            disabled={!canRun}
            className="px-4 py-2 rounded-md text-sm font-medium transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            style={{ background: '#F59E0B', color: '#1F2937' }}
          >
            {running ? 'Running…' : 'Run'}
          </button>
          <span className="text-xs text-text-secondary">
            Press Cmd/Ctrl+Enter to run
          </span>
        </div>

        {unexpectedError !== null && (
          <div
            data-testid="sql-transport-error"
            role="alert"
            className="rounded-md border px-4 py-3 text-sm"
            style={{
              borderColor: 'rgba(239,68,68,0.5)',
              background: 'rgba(239,68,68,0.1)',
              color: '#FCA5A5',
            }}
          >
            {unexpectedError}
          </div>
        )}

        {failed && (
          <div
            data-testid="sql-error"
            role="alert"
            className="rounded-md border px-4 py-3 text-sm flex flex-col gap-1"
            style={{
              borderColor: 'rgba(239,68,68,0.5)',
              background: 'rgba(239,68,68,0.1)',
              color: '#FCA5A5',
            }}
          >
            <span className="font-semibold" data-testid="sql-error-reason">
              {result?.failureReason ?? 'QueryFailed'}
            </span>
            {result?.failureReason &&
              FAILURE_HINTS[result.failureReason] !== undefined && (
                <span className="text-text-secondary">
                  {FAILURE_HINTS[result.failureReason]}
                </span>
              )}
            {result?.errorMessage !== undefined &&
              result.errorMessage !== '' && (
                <span
                  className="font-mono text-xs break-all"
                  data-testid="sql-error-detail"
                >
                  {result.errorMessage}
                </span>
              )}
          </div>
        )}

        {succeeded && (
          <div data-testid="sql-result" className="flex flex-col gap-2">
            <div className="text-xs text-text-secondary">
              {rows.length === 0
                ? 'Query succeeded — 0 rows.'
                : `Query succeeded — ${rows.length} row${
                    rows.length === 1 ? '' : 's'
                  }.`}
            </div>
            <div
              className="overflow-auto rounded-md border"
              style={{ borderColor: 'rgba(31,41,55,0.7)' }}
            >
              <table
                data-testid="sql-result-table"
                className="min-w-full text-sm border-collapse"
              >
                <thead>
                  <tr>
                    {columns.map((col, i) => (
                      <th
                        key={`${col}-${i}`}
                        className="text-left px-3 py-2 font-semibold text-text-primary border-b sticky top-0 bg-bg-secondary"
                        style={{ borderColor: 'rgba(31,41,55,0.7)' }}
                      >
                        {col}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {rows.length === 0 ? (
                    <tr>
                      <td
                        colSpan={Math.max(columns.length, 1)}
                        className="px-3 py-4 text-center text-text-secondary"
                        data-testid="sql-result-empty"
                      >
                        No rows returned.
                      </td>
                    </tr>
                  ) : (
                    rows.map((row, ri) => (
                      <tr
                        key={ri}
                        className="odd:bg-transparent even:bg-bg-secondary/40"
                      >
                        {row.map((cell, ci) => (
                          <td
                            key={ci}
                            className="px-3 py-1.5 font-mono text-xs text-text-primary border-b align-top"
                            style={{ borderColor: 'rgba(31,41,55,0.4)' }}
                          >
                            {renderCell(cell)}
                          </td>
                        ))}
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
