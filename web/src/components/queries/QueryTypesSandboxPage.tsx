import { useEffect, useMemo, useState } from 'react';
import { useParams } from 'react-router';
import { FormProvider, useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import {
  useQueryTypes,
  useExecuteQueryType,
} from '../../hooks/useQueryTypes';
import type {
  ActionParameterV2,
  QueryType,
  QueryTypeParameter,
} from '../../api/types';
import { ParameterForm } from '../actions/ParameterForm';
import {
  buildParameterDefaults,
  buildParameterZodSchema,
} from '../actions/parameterSchema';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';
import { ApiRequestError } from '../../api/client';

// parseQueryTypeParameters coerces the loose `unknown` parameters column
// into the typed array shape the sandbox renders. Defensive against:
// (1) malformed JSON (storage column is JSONB so the wire shape isn't
//     guaranteed); (2) wrapper objects like { params: [...] } that older
//     fixtures use; (3) entries missing `id`.
export function parseQueryTypeParameters(raw: unknown): QueryTypeParameter[] {
  let value: unknown = raw;
  if (typeof value === 'string') {
    try {
      value = JSON.parse(value);
    } catch {
      return [];
    }
  }
  if (value && typeof value === 'object' && !Array.isArray(value)) {
    const wrapped = (value as Record<string, unknown>).params;
    if (Array.isArray(wrapped)) value = wrapped;
  }
  if (!Array.isArray(value)) return [];
  const out: QueryTypeParameter[] = [];
  for (const entry of value) {
    if (!entry || typeof entry !== 'object') continue;
    const obj = entry as Record<string, unknown>;
    const id = typeof obj.id === 'string' ? obj.id : '';
    if (!id) continue;
    out.push({
      id,
      type: typeof obj.type === 'string' ? obj.type : 'string',
      required: obj.required === true,
      description:
        typeof obj.description === 'string' ? obj.description : undefined,
    });
  }
  return out;
}

// Adapter from the QueryType wire shape into the ActionParameterV2 record
// the shared ParameterForm understands. ParameterForm dispatches on
// `dataType.type` so we lift the QueryType's flat `type` field into the
// nested shape it expects. Keeps the form renderer single-purpose.
export function queryParamsToActionParams(
  params: QueryTypeParameter[],
): Record<string, ActionParameterV2> {
  const out: Record<string, ActionParameterV2> = {};
  for (const p of params) {
    out[p.id] = {
      dataType: { type: p.type },
      required: p.required === true,
      description: p.description,
    };
  }
  return out;
}

interface ResultTableViewProps {
  result: Record<string, unknown>;
}

// ResultTableView normalises the heterogeneous shape of query results into
// a two-column key/value table or, when `value` is an array of records,
// into a per-record table with auto-derived columns. Falls back to a
// single-row k/v rendering when neither pattern fits.
function ResultTableView({ result }: ResultTableViewProps) {
  const rows = useMemo(() => deriveTableRows(result), [result]);
  const columns = useMemo(() => {
    const seen = new Set<string>();
    for (const row of rows) {
      for (const key of Object.keys(row)) seen.add(key);
    }
    return Array.from(seen);
  }, [rows]);

  if (rows.length === 0 || columns.length === 0) {
    return (
      <div
        data-testid="query-result-table-empty"
        className="text-xs text-text-secondary py-4"
      >
        No tabular result. Switch to JSON view for the full payload.
      </div>
    );
  }
  return (
    <table
      data-testid="query-result-table"
      className="w-full text-xs font-mono border-collapse"
    >
      <thead>
        <tr>
          {columns.map((c) => (
            <th
              key={c}
              data-testid={`query-result-column-${c}`}
              className="text-left px-2 py-1 border-b border-border text-text-secondary"
            >
              {c}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((row, i) => (
          <tr key={i} data-testid="query-result-row">
            {columns.map((c) => (
              <td
                key={c}
                data-testid={`query-result-cell-${c}`}
                className="px-2 py-1 border-b border-border text-text-primary align-top"
              >
                {formatCell(row[c])}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function deriveTableRows(result: Record<string, unknown>): Array<Record<string, unknown>> {
  // Foundry-style functions usually wrap their payload in `{ value: ... }`.
  const value = (result.value ?? result) as unknown;
  if (Array.isArray(value)) {
    return value
      .filter((v): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v))
      .map((v) => v);
  }
  if (value && typeof value === 'object') {
    // If the inner object has exactly one array-valued property, treat that
    // as the row collection (e.g. `{ customers: [...], totalCount: 3 }`).
    const obj = value as Record<string, unknown>;
    const arrayKeys = Object.keys(obj).filter((k) => Array.isArray(obj[k]));
    if (arrayKeys.length === 1) {
      const items = obj[arrayKeys[0]] as unknown[];
      return items
        .filter((v): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v))
        .map((v) => v);
    }
  }
  return [];
}

function formatCell(v: unknown): string {
  if (v === null || v === undefined) return '';
  if (typeof v === 'object') return JSON.stringify(v);
  return String(v);
}

export function QueryTypesSandboxPage() {
  const { ontology } = useParams<{ ontology: string }>();
  const queryTypesQuery = useQueryTypes(ontology ?? '');
  const executeMutation = useExecuteQueryType(ontology ?? '');

  const [selectedApiName, setSelectedApiName] = useState<string | null>(null);
  const [result, setResult] = useState<Record<string, unknown> | null>(null);
  const [resultView, setResultView] = useState<'table' | 'json'>('table');
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  const selectedQueryType = useMemo<QueryType | null>(() => {
    if (!selectedApiName) return null;
    return (
      queryTypesQuery.data?.find((q) => q.apiName === selectedApiName) ?? null
    );
  }, [queryTypesQuery.data, selectedApiName]);

  const parameters = useMemo(
    () => parseQueryTypeParameters(selectedQueryType?.parameters),
    [selectedQueryType],
  );

  const actionParams = useMemo(
    () => queryParamsToActionParams(parameters),
    [parameters],
  );

  const formSchema = useMemo(
    () => buildParameterZodSchema(actionParams),
    [actionParams],
  );
  const formDefaults = useMemo(
    () => buildParameterDefaults(actionParams),
    [actionParams],
  );

  const form = useForm<Record<string, unknown>>({
    resolver: zodResolver(formSchema),
    defaultValues: formDefaults,
    mode: 'onBlur',
  });

  // Reset the form whenever the selected QueryType changes so the new
  // schema governs the inputs.
  useEffect(() => {
    form.reset(formDefaults);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [formDefaults]);

  function handleSelect(apiName: string) {
    setSelectedApiName(apiName);
    setResult(null);
    setErrorMessage(null);
  }

  function handleExecute(values: Record<string, unknown>) {
    if (!selectedQueryType) return;
    setErrorMessage(null);
    setResult(null);
    // Strip undefined entries — optional fields collapse to absent so the
    // wire payload doesn't carry explicit-null.
    const cleaned: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(values)) {
      if (v !== undefined) cleaned[k] = v;
    }
    executeMutation.mutate(
      { queryApiName: selectedQueryType.apiName, parameters: cleaned },
      {
        onSuccess: (data) => setResult(data),
        onError: (err) => {
          if (err instanceof ApiRequestError) {
            setErrorMessage(
              err.parameters?.error ?? err.errorName ?? err.message,
            );
          } else if (err instanceof Error) {
            setErrorMessage(err.message);
          } else {
            setErrorMessage('Query execution failed.');
          }
        },
      },
    );
  }

  if (!ontology) {
    return (
      <div
        data-testid="query-types-sandbox-no-ontology"
        className="p-6 text-sm text-text-secondary"
      >
        Select an ontology to open the QueryTypes sandbox.
      </div>
    );
  }

  if (queryTypesQuery.isLoading) {
    return (
      <div
        data-testid="query-types-sandbox-loading"
        className="p-6 flex items-center justify-center"
      >
        <LoadingSpinner />
      </div>
    );
  }

  if (queryTypesQuery.isError) {
    return (
      <div data-testid="query-types-sandbox-error" className="p-6">
        <EmptyState
          title="Failed to load QueryTypes"
          description={(queryTypesQuery.error as Error)?.message ?? 'Unknown error'}
        />
      </div>
    );
  }

  const list = queryTypesQuery.data ?? [];
  const queryTypeCount = list.length;

  return (
    <div
      data-testid="query-types-sandbox-page"
      data-ontology-api-name={ontology}
      className="flex h-full flex-col"
    >
      <header className="px-6 py-4 border-b border-border flex items-baseline justify-between">
        <div>
          <h1 className="text-lg font-semibold text-text-primary">
            QueryTypes Sandbox
          </h1>
          <p className="text-xs text-text-secondary">
            Parameterised reads against{' '}
            <span className="font-mono">{ontology}</span>
          </p>
        </div>
        <div
          data-testid="query-types-count"
          className="text-xs text-text-secondary"
        >
          {queryTypeCount} QueryType{queryTypeCount === 1 ? '' : 's'}
        </div>
      </header>
      <div className="flex-1 grid grid-cols-12 gap-4 px-6 py-4 overflow-hidden">
        {/* Left rail: QueryType list */}
        <aside className="col-span-3 overflow-y-auto border border-border rounded">
          {queryTypeCount === 0 ? (
            <div data-testid="query-types-sandbox-empty" className="p-4">
              <EmptyState
                title="No QueryTypes defined"
                description="Define a QueryType via the admin API to enable the sandbox."
              />
            </div>
          ) : (
            <ul data-testid="query-types-list" className="divide-y divide-border">
              {list.map((qt) => {
                const isSelected = qt.apiName === selectedApiName;
                return (
                  <li
                    key={qt.rid}
                    data-testid="query-type-row"
                    data-query-api-name={qt.apiName}
                    data-query-selected={isSelected ? 'true' : 'false'}
                  >
                    <button
                      type="button"
                      data-testid={`query-type-select-${qt.apiName}`}
                      onClick={() => handleSelect(qt.apiName)}
                      className={`w-full text-left px-3 py-2 transition-colors ${
                        isSelected
                          ? 'bg-bg-tertiary text-text-primary'
                          : 'text-text-secondary hover:bg-bg-tertiary hover:text-text-primary'
                      }`}
                    >
                      <div className="text-sm font-medium">
                        {qt.displayName || qt.apiName}
                      </div>
                      <div className="text-[10px] uppercase tracking-wider text-text-secondary">
                        {qt.apiName} · {qt.status}
                      </div>
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
        </aside>

        {/* Right pane: parameter form + result */}
        <main className="col-span-9 flex flex-col gap-4 overflow-y-auto">
          {!selectedQueryType ? (
            <div
              data-testid="query-type-detail-empty"
              className="flex-1 flex items-center justify-center text-sm text-text-secondary"
            >
              Pick a QueryType from the list to execute it.
            </div>
          ) : (
            <section
              data-testid="query-type-detail"
              data-query-api-name={selectedQueryType.apiName}
              className="flex flex-col gap-4"
            >
              <div>
                <h2
                  data-testid="query-type-display-name"
                  className="text-base font-semibold text-text-primary"
                >
                  {selectedQueryType.displayName || selectedQueryType.apiName}
                </h2>
                {selectedQueryType.description && (
                  <p className="text-xs text-text-secondary mt-1">
                    {selectedQueryType.description}
                  </p>
                )}
              </div>

              <FormProvider {...form}>
                <form
                  data-testid="query-type-parameter-form"
                  onSubmit={form.handleSubmit(handleExecute)}
                  className="flex flex-col gap-4"
                >
                  <ParameterForm parameters={actionParams} />
                  <div className="flex items-center gap-3">
                    <button
                      type="submit"
                      data-testid="query-type-execute-button"
                      disabled={executeMutation.isPending}
                      className="bg-accent-cyan text-bg-primary text-sm font-medium px-4 py-2 rounded disabled:opacity-50"
                    >
                      {executeMutation.isPending ? 'Running…' : 'Run query'}
                    </button>
                    {executeMutation.isPending && (
                      <span
                        data-testid="query-type-running"
                        className="text-xs text-text-secondary"
                      >
                        Executing query…
                      </span>
                    )}
                  </div>
                </form>
              </FormProvider>

              {errorMessage && (
                <div
                  data-testid="query-result-error"
                  role="alert"
                  className="border border-accent-error text-accent-error text-xs font-mono px-3 py-2 rounded"
                >
                  {errorMessage}
                </div>
              )}

              {result && (
                <section
                  data-testid="query-result-panel"
                  className="border border-border rounded"
                >
                  <div
                    role="tablist"
                    aria-label="Result view"
                    className="flex border-b border-border text-xs"
                  >
                    <button
                      type="button"
                      role="tab"
                      aria-selected={resultView === 'table'}
                      data-testid="query-result-tab-table"
                      data-active={resultView === 'table' ? 'true' : 'false'}
                      onClick={() => setResultView('table')}
                      className={`px-3 py-2 ${
                        resultView === 'table'
                          ? 'text-text-primary border-b-2 border-accent-cyan'
                          : 'text-text-secondary'
                      }`}
                    >
                      Table
                    </button>
                    <button
                      type="button"
                      role="tab"
                      aria-selected={resultView === 'json'}
                      data-testid="query-result-tab-json"
                      data-active={resultView === 'json' ? 'true' : 'false'}
                      onClick={() => setResultView('json')}
                      className={`px-3 py-2 ${
                        resultView === 'json'
                          ? 'text-text-primary border-b-2 border-accent-cyan'
                          : 'text-text-secondary'
                      }`}
                    >
                      JSON
                    </button>
                  </div>
                  <div className="p-3">
                    {resultView === 'table' ? (
                      <ResultTableView result={result} />
                    ) : (
                      <pre
                        data-testid="query-result-json"
                        className="text-xs font-mono text-text-primary whitespace-pre-wrap break-all max-h-96 overflow-auto"
                      >
                        {JSON.stringify(result, null, 2)}
                      </pre>
                    )}
                  </div>
                </section>
              )}
            </section>
          )}
        </main>
      </div>
    </div>
  );
}
