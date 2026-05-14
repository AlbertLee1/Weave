// ScenarioDebugDrawer is the failed-row debug surface (VTX-102): a side
// panel showing the failing scenario run's input snapshot, function logs,
// and the partial edits that were written before the failure.
//
// The data shape mirrors scenario_runs.logs as fetched from the Run API
// (VTX-044). This file is the view layer + a small fetcher hook;
// wiring the actual button trigger lives in the row component which
// belongs to the Scenario History stream.

import { useEffect, useState } from 'react';

export interface ScenarioRunDebug {
  scenarioRunRid: string;
  inputSnapshot: unknown;
  functionLogs: string[];
  partialEdits: Array<{ op: string; objectId?: string; property?: string; newValue?: unknown }>;
}

export interface ScenarioDebugDrawerProps {
  scenarioRunRid: string | null;
  onClose: () => void;
  fetcher?: (rid: string) => Promise<ScenarioRunDebug>;
}

async function defaultFetcher(rid: string): Promise<ScenarioRunDebug> {
  const res = await fetch(`/api/vertex/v1/scenario-runs/${encodeURIComponent(rid)}/debug`);
  if (!res.ok) throw new Error(`debug fetch failed: ${res.status}`);
  return (await res.json()) as ScenarioRunDebug;
}

export function ScenarioDebugDrawer({ scenarioRunRid, onClose, fetcher = defaultFetcher }: ScenarioDebugDrawerProps) {
  const [data, setData] = useState<ScenarioRunDebug | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!scenarioRunRid) {
      setData(null);
      setError(null);
      return;
    }
    let cancelled = false;
    fetcher(scenarioRunRid)
      .then((d) => {
        if (!cancelled) {
          setData(d);
          setError(null);
        }
      })
      .catch((e) => {
        if (!cancelled) setError(String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [scenarioRunRid, fetcher]);

  if (!scenarioRunRid) return null;

  return (
    <aside
      data-testid="scenario-debug-drawer"
      role="dialog"
      aria-label="Scenario debug"
      className="fixed right-0 top-0 z-50 h-full w-[480px] overflow-y-auto border-l border-zinc-200 bg-white p-4 shadow-xl dark:border-zinc-700 dark:bg-zinc-900"
    >
      <header className="mb-3 flex items-center justify-between">
        <h2 className="text-sm font-semibold">Debug — {scenarioRunRid}</h2>
        <button
          type="button"
          data-testid="scenario-debug-close"
          onClick={onClose}
          className="rounded px-2 py-1 text-xs hover:bg-zinc-100 dark:hover:bg-zinc-800"
        >
          Close
        </button>
      </header>
      {error && (
        <div data-testid="scenario-debug-error" className="rounded bg-red-50 p-2 text-xs text-red-700">
          {error}
        </div>
      )}
      {data && (
        <div className="space-y-4 text-xs">
          <section data-testid="scenario-debug-input">
            <h3 className="mb-1 font-semibold">Input snapshot</h3>
            <pre className="overflow-x-auto rounded bg-zinc-50 p-2 dark:bg-zinc-800">
              {JSON.stringify(data.inputSnapshot, null, 2)}
            </pre>
          </section>
          <section data-testid="scenario-debug-logs">
            <h3 className="mb-1 font-semibold">Function logs ({data.functionLogs.length})</h3>
            <pre className="overflow-x-auto whitespace-pre-wrap rounded bg-zinc-50 p-2 dark:bg-zinc-800">
              {data.functionLogs.join('\n')}
            </pre>
          </section>
          <section data-testid="scenario-debug-edits">
            <h3 className="mb-1 font-semibold">Partial edits ({data.partialEdits.length})</h3>
            <ol className="list-decimal pl-5">
              {data.partialEdits.map((e, i) => (
                <li key={i} className="mb-1">
                  <span className="font-mono">{e.op}</span>
                  {e.objectId && <span> · {e.objectId}</span>}
                  {e.property && <span> · {e.property}</span>}
                  {e.newValue !== undefined && (
                    <span className="ml-1 text-zinc-500">= {JSON.stringify(e.newValue)}</span>
                  )}
                </li>
              ))}
            </ol>
          </section>
        </div>
      )}
    </aside>
  );
}
