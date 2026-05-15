// ScenarioRetryPane visualises Scenario Run activity retries (VTX-101).
// The SSE wiring that produces these events is owned by VTX-044 (other
// stream); this component is the pure view layer and is exercised by
// ScenarioRetryPane.test.tsx with synthetic events.

import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import '../i18n';

export interface ScenarioRetryEvent {
  activityId: string;
  attempt: number;
  error: string;
  stack?: string;
  occurredAt: string;
}

export interface ScenarioRetryPaneProps {
  events: ScenarioRetryEvent[];
}

interface PerActivity {
  activityId: string;
  retries: ScenarioRetryEvent[];
}

function groupByActivity(events: ScenarioRetryEvent[]): PerActivity[] {
  const map = new Map<string, ScenarioRetryEvent[]>();
  for (const e of events) {
    const xs = map.get(e.activityId) ?? [];
    xs.push(e);
    map.set(e.activityId, xs);
  }
  return Array.from(map.entries())
    .map(([activityId, retries]) => ({
      activityId,
      retries: [...retries].sort((a, b) => a.attempt - b.attempt),
    }))
    .sort((a, b) => a.activityId.localeCompare(b.activityId));
}

export function ScenarioRetryPane({ events }: ScenarioRetryPaneProps) {
  const { t } = useTranslation();
  const grouped = useMemo(() => groupByActivity(events), [events]);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  if (grouped.length === 0) {
    return (
      <div data-testid="scenario-retry-empty" className="text-sm text-zinc-500 italic">
        {t('vertex.retry.empty')}
      </div>
    );
  }

  const toggle = (key: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  return (
    <div data-testid="scenario-retry-pane" className="space-y-3">
      {grouped.map(({ activityId, retries }) => {
        const key = activityId;
        const open = expanded.has(key);
        return (
          <div
            key={activityId}
            data-testid={`scenario-retry-row-${activityId}`}
            className="rounded border border-zinc-200 dark:border-zinc-700"
          >
            <button
              type="button"
              onClick={() => toggle(key)}
              data-testid={`scenario-retry-toggle-${activityId}`}
              className="flex w-full items-center justify-between px-3 py-2 text-left"
              aria-expanded={open}
            >
              <span className="font-mono text-sm">{activityId}</span>
              <span
                data-testid={`scenario-retry-counter-${activityId}`}
                className="rounded bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-900 dark:bg-amber-900/40 dark:text-amber-200"
              >
                {t('vertex.retry.counter', { count: retries.length })}
              </span>
            </button>
            {open && (
              <ol
                data-testid={`scenario-retry-stacks-${activityId}`}
                className="border-t border-zinc-200 px-3 py-2 text-xs dark:border-zinc-700"
              >
                {retries.map((r) => (
                  <li key={r.attempt} className="mb-2 last:mb-0">
                    <div className="font-medium">{t('vertex.retry.attempt', { num: r.attempt })}</div>
                    <div className="text-zinc-600 dark:text-zinc-400">{r.error}</div>
                    {r.stack && (
                      <pre className="mt-1 overflow-x-auto whitespace-pre rounded bg-zinc-50 p-2 text-[11px] dark:bg-zinc-900">
                        {r.stack}
                      </pre>
                    )}
                  </li>
                ))}
              </ol>
            )}
          </div>
        );
      })}
    </div>
  );
}
