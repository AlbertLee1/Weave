import { useMemo, useState } from 'react';
import { Link } from 'react-router';
import type { RouteStat, UsageSummary } from '../../api/developer';
import {
  useApplicationUsage,
  useApplications,
} from '../../hooks/useDeveloper';
import { LoadingSpinner } from '../common/LoadingSpinner';

const WINDOW_LABELS = ['24h', '7d', '30d'] as const;
type WindowLabel = (typeof WINDOW_LABELS)[number];

const METHOD_COLORS: Record<string, string> = {
  GET: '#14B8A6',
  POST: '#F59E0B',
  PUT: '#3B82F6',
  PATCH: '#8B5CF6',
  DELETE: '#EF4444',
};

const STATUS_COLORS: Record<string, string> = {
  '2xx': '#14B8A6',
  '3xx': '#3B82F6',
  '4xx': '#F59E0B',
  '5xx': '#EF4444',
  other: '#6B7280',
};

export function MetricsPage() {
  const {
    data: apps,
    isLoading: loadingApps,
    error: appsError,
  } = useApplications();

  const [selectedAppId, setSelectedAppId] = useState<string | null>(null);
  const [selectedWindow, setSelectedWindow] = useState<WindowLabel>('24h');

  const effectiveAppId = selectedAppId ?? apps?.[0]?.id ?? null;

  const {
    data: usage,
    isLoading: loadingUsage,
    error: usageError,
  } = useApplicationUsage(effectiveAppId);

  const window: UsageSummary | undefined = useMemo(
    () => usage?.windows.find((w) => w.window === selectedWindow),
    [usage, selectedWindow],
  );

  return (
    <div className="flex flex-col h-[calc(100vh-3rem)] bg-bg-primary overflow-y-auto">
      <header
        className="px-6 py-4 border-b flex flex-wrap items-center gap-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <h1 className="text-base font-semibold tracking-wide text-text-primary">
          API Metrics
        </h1>
        <div className="flex items-center gap-2 text-xs text-text-secondary">
          <label htmlFor="metrics-app-select" className="uppercase tracking-widest">
            Application
          </label>
          <select
            id="metrics-app-select"
            className="px-2 py-1 rounded bg-bg-tertiary text-text-primary text-xs outline-none border border-transparent focus:border-accent-cyan/40 min-w-[12rem]"
            value={effectiveAppId ?? ''}
            onChange={(e) => setSelectedAppId(e.target.value || null)}
            disabled={loadingApps || !apps || apps.length === 0}
          >
            {loadingApps && <option value="">Loading…</option>}
            {!loadingApps && (!apps || apps.length === 0) && (
              <option value="">No applications registered</option>
            )}
            {apps?.map((app) => (
              <option key={app.id} value={app.id}>
                {app.name} ({app.clientId})
              </option>
            ))}
          </select>
        </div>
        <WindowTabs selected={selectedWindow} onSelect={setSelectedWindow} />
      </header>

      <div className="flex-1 px-6 py-4">
        {appsError && (
          <p className="text-sm text-accent-error">
            Failed to load applications: {(appsError as Error).message}
          </p>
        )}
        {!effectiveAppId && !loadingApps && !appsError && (
          <EmptyApplications />
        )}
        {effectiveAppId && loadingUsage && (
          <div className="flex items-center justify-center py-20">
            <LoadingSpinner size="lg" />
          </div>
        )}
        {effectiveAppId && usageError && (
          <p className="text-sm text-accent-error">
            Failed to load usage: {(usageError as Error).message}
          </p>
        )}
        {effectiveAppId && !loadingUsage && !usageError && window && (
          <MetricsBody window={window} />
        )}
      </div>
    </div>
  );
}

function WindowTabs({
  selected,
  onSelect,
}: {
  selected: WindowLabel;
  onSelect: (w: WindowLabel) => void;
}) {
  return (
    <div
      role="tablist"
      aria-label="Time range"
      className="inline-flex rounded border overflow-hidden"
      style={{ borderColor: 'rgba(31,41,55,0.5)' }}
    >
      {WINDOW_LABELS.map((w) => {
        const active = w === selected;
        return (
          <button
            key={w}
            role="tab"
            aria-selected={active}
            onClick={() => onSelect(w)}
            className={`px-3 py-1 text-xs uppercase tracking-widest transition-colors ${
              active
                ? 'bg-bg-tertiary text-text-primary'
                : 'text-text-secondary hover:bg-bg-tertiary/60 hover:text-text-primary'
            }`}
          >
            {w}
          </button>
        );
      })}
    </div>
  );
}

function EmptyApplications() {
  const curlSnippet = `curl -X POST $WEAVE_HOST/api/v2/developer/applications \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer $WEAVE_TOKEN" \\
  -d '{
    "name": "My App",
    "description": "What this app does",
    "redirectUris": ["https://my-app.example.com/oauth/callback"],
    "scopes": ["ontology.read"]
  }'`;
  return (
    <div
      data-testid="metrics-empty-applications"
      className="rounded border px-6 py-10 max-w-3xl mx-auto"
      style={{ borderColor: 'rgba(31,41,55,0.5)', background: 'rgba(13,17,23,0.4)' }}
    >
      <p className="text-sm text-text-primary font-semibold text-center">
        No applications yet
      </p>
      <p className="text-xs text-text-secondary mt-2 text-center">
        Register an application to start collecting API usage metrics.
        Once registered the client_id will appear in the dropdown above.
      </p>
      <pre
        className="mt-5 px-4 py-3 rounded text-[11px] leading-relaxed font-mono overflow-x-auto"
        style={{
          background: 'rgba(0,0,0,0.45)',
          border: '1px solid rgba(31,41,55,0.5)',
          color: '#9CA3AF',
        }}
      >
        <code>{curlSnippet}</code>
      </pre>
      <div className="flex items-center justify-center gap-4 mt-5 text-xs">
        <Link
          to="/developer/playground"
          data-testid="metrics-empty-playground-link"
          className="px-3 py-1.5 rounded transition-colors"
          style={{
            background: 'rgba(20,184,166,0.12)',
            color: '#14B8A6',
            border: '1px solid rgba(20,184,166,0.3)',
          }}
        >
          Open API Playground
        </Link>
        <a
          href="/swagger"
          target="_blank"
          rel="noopener noreferrer"
          className="text-text-secondary hover:text-text-primary underline-offset-4 hover:underline"
        >
          View API docs
        </a>
      </div>
    </div>
  );
}

function MetricsBody({ window }: { window: UsageSummary }) {
  const errorRate = window.total > 0 ? (window.errors / window.total) * 100 : 0;

  const methodData = Object.entries(window.byMethod)
    .filter(([, v]) => v > 0)
    .map(([label, value]) => ({ label, value, color: METHOD_COLORS[label] }))
    .sort((a, b) => b.value - a.value);

  const statusData = Object.entries(window.byStatus)
    .filter(([, v]) => v > 0)
    .map(([label, value]) => ({ label, value, color: STATUS_COLORS[label] }))
    .sort((a, b) => a.label.localeCompare(b.label));

  const errorRoutes = window.topRoutes
    .filter((r) => r.errors > 0)
    .map((r) => ({
      label: `${r.method} ${r.endpoint}`,
      value: r.errors,
      color: '#EF4444',
    }))
    .sort((a, b) => b.value - a.value)
    .slice(0, 10);

  return (
    <div className="flex flex-col gap-6">
      <section className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-3">
        <KpiCard label="Total requests" value={formatInt(window.total)} />
        <KpiCard
          label="Errors"
          value={formatInt(window.errors)}
          tone={window.errors > 0 ? 'warn' : undefined}
        />
        <KpiCard
          label="Error rate"
          value={`${errorRate.toFixed(2)}%`}
          tone={errorRate > 5 ? 'warn' : undefined}
        />
        <KpiCard label="P50 latency" value={`${formatMs(window.p50Ms)}`} />
        <KpiCard label="P95 latency" value={`${formatMs(window.p95Ms)}`} />
        <KpiCard label="P99 latency" value={`${formatMs(window.p99Ms)}`} />
      </section>

      <section
        className="rounded border p-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)', background: 'rgba(13,17,23,0.4)' }}
      >
        <SectionTitle>Request volume by method</SectionTitle>
        <BarChart data={methodData} emptyMessage="No requests in this window." />
      </section>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <section
          className="rounded border p-4"
          style={{
            borderColor: 'rgba(31,41,55,0.5)',
            background: 'rgba(13,17,23,0.4)',
          }}
        >
          <SectionTitle>Response status distribution</SectionTitle>
          <BarChart data={statusData} emptyMessage="No responses yet." />
        </section>

        <section
          className="rounded border p-4"
          style={{
            borderColor: 'rgba(31,41,55,0.5)',
            background: 'rgba(13,17,23,0.4)',
          }}
        >
          <SectionTitle>Top endpoints by errors</SectionTitle>
          <BarChart
            data={errorRoutes}
            emptyMessage="No errors recorded in this window."
            truncateLabel
          />
        </section>
      </div>

      <section
        className="rounded border p-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)', background: 'rgba(13,17,23,0.4)' }}
      >
        <SectionTitle>Endpoint latency</SectionTitle>
        <EndpointTable routes={window.topRoutes} />
      </section>
    </div>
  );
}

function SectionTitle({ children }: { children: React.ReactNode }) {
  return (
    <h2 className="text-[10px] uppercase tracking-widest text-text-secondary mb-3">
      {children}
    </h2>
  );
}

function KpiCard({
  label,
  value,
  tone,
}: {
  label: string;
  value: string;
  tone?: 'warn';
}) {
  const accent = tone === 'warn' ? '#F59E0B' : '#14B8A6';
  return (
    <div
      className="rounded border px-4 py-3"
      style={{
        borderColor: 'rgba(31,41,55,0.5)',
        background: 'rgba(13,17,23,0.6)',
      }}
    >
      <p className="text-[10px] uppercase tracking-widest text-text-secondary">
        {label}
      </p>
      <p
        className="mt-1 text-xl font-semibold font-mono"
        style={{ color: accent }}
      >
        {value}
      </p>
    </div>
  );
}

interface BarDatum {
  label: string;
  value: number;
  color?: string;
}

function BarChart({
  data,
  emptyMessage,
  truncateLabel = false,
}: {
  data: BarDatum[];
  emptyMessage: string;
  truncateLabel?: boolean;
}) {
  if (data.length === 0) {
    return (
      <p className="text-xs text-text-secondary py-4">{emptyMessage}</p>
    );
  }

  const maxValue = Math.max(...data.map((d) => d.value), 1);

  return (
    <div className="flex flex-col gap-2" data-testid="bar-chart">
      {data.map((d) => {
        const pct = (d.value / maxValue) * 100;
        const color = d.color ?? '#14B8A6';
        const displayLabel =
          truncateLabel && d.label.length > 48
            ? `${d.label.slice(0, 45)}…`
            : d.label;
        return (
          <div key={d.label} className="flex items-center gap-3 text-xs">
            <span
              className="w-40 flex-shrink-0 truncate text-text-secondary font-mono"
              title={d.label}
            >
              {displayLabel}
            </span>
            <div
              className="flex-1 h-4 rounded overflow-hidden"
              style={{ background: 'rgba(31,41,55,0.4)' }}
            >
              <div
                data-bar
                className="h-full rounded"
                style={{
                  width: `${Math.max(pct, 1)}%`,
                  background: color,
                  transition: 'width 200ms ease',
                }}
              />
            </div>
            <span className="w-20 flex-shrink-0 text-right font-mono text-text-primary">
              {formatInt(d.value)}
            </span>
          </div>
        );
      })}
    </div>
  );
}

function EndpointTable({ routes }: { routes: RouteStat[] }) {
  if (routes.length === 0) {
    return (
      <p className="text-xs text-text-secondary py-4">
        No endpoint traffic in this window.
      </p>
    );
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-xs">
        <thead>
          <tr
            className="text-left text-text-secondary uppercase tracking-widest"
            style={{ fontSize: '10px' }}
          >
            <th className="py-2 pr-4 font-medium">Method</th>
            <th className="py-2 pr-4 font-medium">Endpoint</th>
            <th className="py-2 pr-4 font-medium text-right">Count</th>
            <th className="py-2 pr-4 font-medium text-right">Errors</th>
            <th className="py-2 pr-4 font-medium text-right">Error %</th>
            <th className="py-2 pr-4 font-medium text-right">P95 (ms)</th>
          </tr>
        </thead>
        <tbody>
          {routes.map((r) => {
            const pct = r.count > 0 ? (r.errors / r.count) * 100 : 0;
            const color = METHOD_COLORS[r.method] ?? '#6B7280';
            return (
              <tr
                key={`${r.method} ${r.endpoint}`}
                className="border-t"
                style={{ borderColor: 'rgba(31,41,55,0.3)' }}
              >
                <td className="py-1.5 pr-4">
                  <span
                    className="inline-block px-1.5 py-0.5 rounded text-[9px] font-semibold uppercase tracking-wider"
                    style={{
                      background: `${color}22`,
                      color,
                      border: `1px solid ${color}44`,
                      minWidth: '3rem',
                      textAlign: 'center',
                    }}
                  >
                    {r.method}
                  </span>
                </td>
                <td
                  className="py-1.5 pr-4 font-mono text-text-primary"
                  style={{ fontFamily: 'var(--font-mono)' }}
                >
                  {r.endpoint}
                </td>
                <td className="py-1.5 pr-4 text-right font-mono text-text-primary">
                  {formatInt(r.count)}
                </td>
                <td
                  className="py-1.5 pr-4 text-right font-mono"
                  style={{ color: r.errors > 0 ? '#F59E0B' : 'inherit' }}
                >
                  {formatInt(r.errors)}
                </td>
                <td
                  className="py-1.5 pr-4 text-right font-mono"
                  style={{ color: pct > 5 ? '#F59E0B' : 'inherit' }}
                >
                  {pct.toFixed(2)}%
                </td>
                <td className="py-1.5 pr-4 text-right font-mono text-text-primary">
                  {formatMs(r.p95Ms)}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function formatInt(n: number): string {
  if (!Number.isFinite(n)) return '0';
  return Math.round(n).toLocaleString('en-US');
}

function formatMs(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return '0 ms';
  if (ms < 1) return `${ms.toFixed(2)} ms`;
  if (ms < 10) return `${ms.toFixed(1)} ms`;
  return `${Math.round(ms)} ms`;
}
