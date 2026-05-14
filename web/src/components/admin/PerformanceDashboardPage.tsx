// US-456: Performance Dashboard. The /metrics endpoint already streams
// prometheus text-format counters and histograms; this page polls every
// 5s, derives QPS / 5xx error rate / avg latency / DB QPS / NATS rates
// across consecutive snapshots, and renders a 60-point sliding window
// sparkline for QPS + latency. We use uplot (already a Quiver dep) rather
// than embedding a Grafana iframe so the page works without external
// infra in dev mode.

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import uPlot from 'uplot';
import 'uplot/dist/uPlot.min.css';
import {
  fetchMetricsSnapshot,
  rateBetween,
  type DerivedRates,
  type MetricsSnapshot,
} from '../../api/metrics';
import { EmptyState } from '../common/EmptyState';

const POLL_INTERVAL_MS = 5000;
const HISTORY_LIMIT = 60;

interface HistoryPoint {
  ts: number;
  rates: DerivedRates;
}

function formatNumber(value: number, digits = 2): string {
  if (!Number.isFinite(value)) return '–';
  if (value === 0) return '0';
  if (Math.abs(value) >= 100) return value.toFixed(0);
  return value.toFixed(digits);
}

function formatPercent(value: number): string {
  if (!Number.isFinite(value)) return '–';
  return `${(value * 100).toFixed(2)}%`;
}

function formatLatency(ms: number): string {
  if (!Number.isFinite(ms)) return '–';
  if (ms >= 1000) return `${(ms / 1000).toFixed(2)} s`;
  return `${ms.toFixed(1)} ms`;
}

interface MetricCardProps {
  label: string;
  value: string;
  hint?: string;
  testId: string;
  intent?: 'default' | 'good' | 'warn' | 'bad';
}

function MetricCard({ label, value, hint, testId, intent = 'default' }: MetricCardProps) {
  const intentStyles: Record<NonNullable<MetricCardProps['intent']>, string> = {
    default: 'border-slate-200 dark:border-slate-700',
    good: 'border-emerald-300 dark:border-emerald-700 bg-emerald-50/50 dark:bg-emerald-950/30',
    warn: 'border-amber-300 dark:border-amber-700 bg-amber-50/50 dark:bg-amber-950/30',
    bad: 'border-rose-300 dark:border-rose-700 bg-rose-50/50 dark:bg-rose-950/30',
  };
  return (
    <div
      data-testid={testId}
      className={`rounded-md border p-4 ${intentStyles[intent]}`}
    >
      <div className="text-xs uppercase tracking-wide text-slate-500 dark:text-slate-400">{label}</div>
      <div className="mt-1 text-2xl font-semibold tabular-nums">{value}</div>
      {hint && <div className="mt-1 text-xs text-slate-500 dark:text-slate-400">{hint}</div>}
    </div>
  );
}

interface SparklineProps {
  history: HistoryPoint[];
  selector: (p: HistoryPoint) => number;
  label: string;
  color: string;
  testId: string;
}

function Sparkline({ history, selector, label, color, testId }: SparklineProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const plotRef = useRef<uPlot | null>(null);

  useEffect(() => {
    if (!containerRef.current) return;
    if (history.length === 0) {
      if (plotRef.current) {
        plotRef.current.destroy();
        plotRef.current = null;
        if (containerRef.current) containerRef.current.innerHTML = '';
      }
      return;
    }
    const xs = history.map((h) => h.ts / 1000);
    const ys = history.map((h) => selector(h));

    if (!plotRef.current) {
      const opts: uPlot.Options = {
        width: containerRef.current.clientWidth || 320,
        height: 80,
        series: [
          {},
          { stroke: color, width: 1.5, label },
        ],
        axes: [
          { show: false },
          { show: false },
        ],
        legend: { show: false },
        cursor: { drag: { x: false, y: false } },
        scales: {
          y: { auto: true },
        },
      };
      plotRef.current = new uPlot(opts, [xs, ys], containerRef.current);
    } else {
      plotRef.current.setData([xs, ys]);
    }
  }, [history, selector, label, color]);

  useEffect(() => {
    return () => {
      if (plotRef.current) {
        plotRef.current.destroy();
        plotRef.current = null;
      }
    };
  }, []);

  return (
    <div className="rounded-md border border-slate-200 dark:border-slate-700 p-3">
      <div className="flex items-center justify-between text-xs text-slate-500 dark:text-slate-400">
        <span>{label}</span>
        <span data-testid={`${testId}-points`}>{history.length} pts</span>
      </div>
      <div ref={containerRef} data-testid={testId} className="mt-2" />
    </div>
  );
}

export function PerformanceDashboardPage() {
  const [snapshot, setSnapshot] = useState<MetricsSnapshot | null>(null);
  const [history, setHistory] = useState<HistoryPoint[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [paused, setPaused] = useState(false);
  const prevSnapshotRef = useRef<MetricsSnapshot | null>(null);

  const poll = useCallback(async () => {
    try {
      const next = await fetchMetricsSnapshot();
      setError(null);
      const prev = prevSnapshotRef.current;
      if (prev) {
        const rates = rateBetween(prev, next);
        if (rates) {
          setHistory((h) => {
            const merged = [...h, { ts: next.fetchedAt, rates }];
            if (merged.length > HISTORY_LIMIT) merged.splice(0, merged.length - HISTORY_LIMIT);
            return merged;
          });
        }
      }
      prevSnapshotRef.current = next;
      setSnapshot(next);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'metrics fetch failed');
    }
  }, []);

  useEffect(() => {
    if (paused) return;
    void poll();
    const id = window.setInterval(() => {
      void poll();
    }, POLL_INTERVAL_MS);
    return () => window.clearInterval(id);
  }, [paused, poll]);

  const latest: DerivedRates | null = useMemo(() => {
    if (history.length === 0) return null;
    return history[history.length - 1].rates;
  }, [history]);

  const errorRateIntent: MetricCardProps['intent'] = !latest
    ? 'default'
    : latest.errorRate5xx >= 0.05
    ? 'bad'
    : latest.errorRate5xx >= 0.01
    ? 'warn'
    : latest.errorRate5xx === 0
    ? 'good'
    : 'default';

  return (
    <div className="px-6 py-5 space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Performance Dashboard</h1>
          <p className="text-sm text-slate-500 dark:text-slate-400">
            Live HTTP / DB / NATS rates derived from <code>/metrics</code>. Polls every {POLL_INTERVAL_MS / 1000}s.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            data-testid="perf-toggle-pause"
            className="rounded-md border border-slate-300 dark:border-slate-700 px-3 py-1.5 text-sm hover:bg-slate-100 dark:hover:bg-slate-800"
            onClick={() => setPaused((p) => !p)}
          >
            {paused ? 'Resume' : 'Pause'}
          </button>
          <button
            type="button"
            data-testid="perf-refresh-now"
            className="rounded-md border border-slate-300 dark:border-slate-700 px-3 py-1.5 text-sm hover:bg-slate-100 dark:hover:bg-slate-800"
            onClick={() => void poll()}
          >
            Refresh
          </button>
        </div>
      </div>

      {error && (
        <div
          data-testid="perf-error"
          className="rounded-md border border-rose-300 dark:border-rose-700 bg-rose-50 dark:bg-rose-950/30 px-3 py-2 text-sm text-rose-800 dark:text-rose-300"
        >
          {error}
        </div>
      )}

      {!snapshot && !error && (
        <div data-testid="perf-loading" className="text-sm text-slate-500">
          Waiting for first scrape…
        </div>
      )}

      {snapshot && history.length === 0 && !error && (
        <div data-testid="perf-empty">
          <EmptyState
            title="Waiting for first sample…"
            description={`Metrics will appear within ${POLL_INTERVAL_MS / 1000} s of the next poll.`}
          />
        </div>
      )}

      {latest && (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
          <MetricCard testId="perf-card-qps" label="HTTP QPS" value={formatNumber(latest.qps)} hint="5xx + 2xx + 4xx" />
          <MetricCard
            testId="perf-card-error-rate"
            label="5xx Error Rate"
            value={formatPercent(latest.errorRate5xx)}
            hint=">= 5xx responses"
            intent={errorRateIntent}
          />
          <MetricCard testId="perf-card-latency" label="Avg Latency" value={formatLatency(latest.avgLatencyMs)} hint="histogram sum / count" />
          <MetricCard testId="perf-card-db-qps" label="DB QPS" value={formatNumber(latest.dbQps)} hint="weave_db_queries_total" />
          <MetricCard testId="perf-card-nats-pub" label="NATS Publish/s" value={formatNumber(latest.natsPublishQps)} />
          <MetricCard testId="perf-card-nats-cons" label="NATS Consume/s" value={formatNumber(latest.natsConsumeQps)} />
        </div>
      )}

      {history.length >= 2 && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
          <Sparkline
            testId="perf-sparkline-qps"
            history={history}
            selector={(p) => p.rates.qps}
            label="HTTP QPS"
            color="#2563eb"
          />
          <Sparkline
            testId="perf-sparkline-latency"
            history={history}
            selector={(p) => p.rates.avgLatencyMs}
            label="Avg latency (ms)"
            color="#9333ea"
          />
        </div>
      )}

      {snapshot && (
        <details className="rounded-md border border-slate-200 dark:border-slate-700">
          <summary className="cursor-pointer px-3 py-2 text-sm">Raw counters ({snapshot.rawSamples.length} samples)</summary>
          <div className="max-h-64 overflow-auto px-3 pb-3 text-xs font-mono">
            <ul data-testid="perf-raw-samples">
              {snapshot.rawSamples.slice(0, 200).map((s, i) => (
                <li key={i}>
                  <span className="text-slate-700 dark:text-slate-300">{s.name}</span>
                  {Object.keys(s.labels).length > 0 && (
                    <span className="text-slate-500">
                      {'{'}
                      {Object.entries(s.labels).map(([k, v]) => `${k}="${v}"`).join(',')}
                      {'}'}
                    </span>
                  )}{' '}
                  <span className="text-slate-900 dark:text-slate-100">{s.value}</span>
                </li>
              ))}
            </ul>
          </div>
        </details>
      )}
    </div>
  );
}
