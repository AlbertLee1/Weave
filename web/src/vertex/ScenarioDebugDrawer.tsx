// ScenarioDebugDrawer is the failed-row debug surface (VTX-102): a side
// panel showing the failing scenario run's input snapshot, function logs,
// and the partial edits that were written before the failure.
//
// The data shape mirrors scenario_runs.logs as fetched from the Run API
// (VTX-044). This file is the view layer + a small fetcher hook;
// wiring the actual button trigger lives in the row component which
// belongs to the Scenario History stream.

import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { ScenarioRunRecord } from '../features/vertex/scenarioPane/scenarioRunAsync';
import '../i18n';

export interface ScenarioRunDebug {
  scenarioRunRid: string;
  inputSnapshot: unknown;
  functionLogs: string[];
  partialEdits: Array<{ op: string; objectId?: string; property?: string; newValue?: unknown }>;
}

export interface ScenarioDebugDrawerProps {
  scenarioRid?: string | null;
  scenarioRunRid: string | null;
  onClose: () => void;
  fetcher?: (input: ScenarioDebugFetchInput) => Promise<ScenarioRunDebug>;
}

export interface ScenarioDebugFetchInput {
  scenarioRid: string | null | undefined;
  scenarioRunRid: string;
}

export class ScenarioRunDebugNotFoundError extends Error {
  constructor() {
    super('Scenario run not found');
    this.name = 'ScenarioRunDebugNotFoundError';
  }
}

function requireScenarioRid(scenarioRid: string | null | undefined): string {
  if (typeof scenarioRid !== 'string' || scenarioRid.trim().length === 0) {
    throw new Error('scenarioRid is required to fetch scenario run debug');
  }
  return scenarioRid.trim();
}

function mapRunRecordToDebug(record: ScenarioRunRecord): ScenarioRunDebug {
  const checkpoint = record.checkpoint;
  const attempts = Object.entries(checkpoint?.attemptsById ?? {});
  const error = record.error ?? checkpoint?.error;
  const functionLogs = [
    `status=${record.status}`,
    checkpoint?.lastActivity ? `lastActivity=${checkpoint.lastActivity}` : null,
    ...attempts.map(([activityId, count]) => `${activityId} attempts=${count}`),
    error ? `error=${error}` : null,
    checkpoint?.updatedAt ? `updatedAt=${checkpoint.updatedAt}` : null,
  ].filter((line): line is string => line !== null);
  return {
    scenarioRunRid: record.rid,
    inputSnapshot: record,
    functionLogs,
    partialEdits: [],
  };
}

async function defaultFetcher(input: ScenarioDebugFetchInput): Promise<ScenarioRunDebug> {
  const scenarioRid = requireScenarioRid(input.scenarioRid);
  const res = await fetch(
    `/api/vertex/v1/scenarios/${encodeURIComponent(scenarioRid)}/runs/${encodeURIComponent(input.scenarioRunRid)}`,
  );
  if (res.status === 404) {
    throw new ScenarioRunDebugNotFoundError();
  }
  if (!res.ok) throw new Error(`scenario run fetch failed: ${res.status}`);
  return mapRunRecordToDebug((await res.json()) as ScenarioRunRecord);
}

export function ScenarioDebugDrawer({
  scenarioRid,
  scenarioRunRid,
  onClose,
  fetcher = defaultFetcher,
}: ScenarioDebugDrawerProps) {
  const { t } = useTranslation();
  const [data, setData] = useState<ScenarioRunDebug | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!scenarioRunRid) {
      setData(null);
      setError(null);
      return;
    }
    let cancelled = false;
    setData(null);
    setError(null);
    fetcher({ scenarioRid, scenarioRunRid })
      .then((d) => {
        if (!cancelled) {
          setData(d);
          setError(null);
        }
      })
      .catch((e) => {
        if (!cancelled) {
          setData(null);
          setError(e instanceof Error ? e.message : String(e));
        }
      });
    return () => {
      cancelled = true;
    };
  }, [scenarioRid, scenarioRunRid, fetcher]);

  if (!scenarioRunRid) return null;

  return (
    <aside
      data-testid="scenario-debug-drawer"
      role="dialog"
      aria-label={t('vertex.debug.ariaLabel')}
      className="fixed right-0 top-0 z-50 h-full w-[480px] overflow-y-auto border-l border-zinc-200 bg-white p-4 shadow-xl dark:border-zinc-700 dark:bg-zinc-900"
    >
      <header className="mb-3 flex items-center justify-between">
        <h2 className="text-sm font-semibold">{t('vertex.debug.title', { rid: scenarioRunRid })}</h2>
        <button
          type="button"
          data-testid="scenario-debug-close"
          onClick={onClose}
          className="rounded px-2 py-1 text-xs hover:bg-zinc-100 dark:hover:bg-zinc-800"
        >
          {t('vertex.debug.close')}
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
            <h3 className="mb-1 font-semibold">{t('vertex.debug.inputSnapshot')}</h3>
            <pre className="overflow-x-auto rounded bg-zinc-50 p-2 dark:bg-zinc-800">
              {JSON.stringify(data.inputSnapshot, null, 2)}
            </pre>
          </section>
          <section data-testid="scenario-debug-logs">
            <h3 className="mb-1 font-semibold">
              {t('vertex.debug.functionLogs', { count: data.functionLogs.length })}
            </h3>
            <pre className="overflow-x-auto whitespace-pre-wrap rounded bg-zinc-50 p-2 dark:bg-zinc-800">
              {data.functionLogs.join('\n')}
            </pre>
          </section>
          <section data-testid="scenario-debug-edits">
            <h3 className="mb-1 font-semibold">
              {t('vertex.debug.partialEdits', { count: data.partialEdits.length })}
            </h3>
            <ol className="list-decimal pl-5">
              {data.partialEdits.length === 0 ? (
                <li>{t('vertex.debug.noPartialEdits')}</li>
              ) : (
                data.partialEdits.map((e, i) => (
                  <li key={i} className="mb-1">
                    <span className="font-mono">{e.op}</span>
                    {e.objectId && <span> · {e.objectId}</span>}
                    {e.property && <span> · {e.property}</span>}
                    {e.newValue !== undefined && (
                      <span className="ml-1 text-zinc-500">= {JSON.stringify(e.newValue)}</span>
                    )}
                  </li>
                ))
              )}
            </ol>
          </section>
        </div>
      )}
    </aside>
  );
}
