// US-453: Compliance / GDPR report console. Operators with PermUserManage
// drive the existing `POST /api/admin/compliance/report` (US-270 / US-442) +
// `POST /api/admin/gdpr/export` (US-268 / US-443) endpoints from a single
// /admin/compliance route, choose a format + time window (or userId for the
// GDPR variant) and download the result.
import { useState } from 'react';
import { ApiRequestError } from '../../api/client';
import {
  generateComplianceReport,
  generateGDPRExport,
  triggerBlobDownload,
  type ComplianceReportFormat,
} from '../../api/compliance';

type ReportType = 'soc2' | 'gdpr';

const REPORT_TYPE_OPTIONS: { value: ReportType; label: string; hint: string }[] = [
  {
    value: 'soc2',
    label: 'SOC2 / ISO27001 evidence',
    hint:
      'Aggregates audit events into control-evidence groups. JSON for SDK consumers, HTML for browser preview, PDF for auditor delivery.',
  },
  {
    value: 'gdpr',
    label: 'GDPR data export',
    hint:
      'Streams the right-to-portability ZIP for a single user — profile, roles, audit trail, and any media blobs.',
  },
];

const FORMAT_OPTIONS: { value: ComplianceReportFormat; label: string }[] = [
  { value: 'pdf', label: 'PDF' },
  { value: 'html', label: 'HTML' },
  { value: 'json', label: 'JSON' },
];

function describeError(err: unknown): string {
  if (err instanceof ApiRequestError) {
    const reason = err.parameters?.reason;
    return reason ? `${err.errorName}: ${reason}` : err.errorName;
  }
  if (err instanceof Error) return err.message;
  return 'Report generation failed.';
}

// Convert a `<input type=date>` string (YYYY-MM-DD) to an RFC3339 instant.
// `from` widens to start-of-day, `to` widens to end-of-day so the inclusive
// UI semantics match the backend's half-open interval handling. Empty
// string passes through.
function widenToInstant(date: string, edge: 'from' | 'to'): string {
  if (!date) return '';
  const t = edge === 'from' ? '00:00:00.000Z' : '23:59:59.999Z';
  return `${date}T${t}`;
}

export function ComplianceReportsPage() {
  const [reportType, setReportType] = useState<ReportType>('soc2');
  const [format, setFormat] = useState<ComplianceReportFormat>('pdf');
  const [fromDate, setFromDate] = useState<string>('');
  const [toDate, setToDate] = useState<string>('');
  const [userId, setUserId] = useState<string>('');
  const [busy, setBusy] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const [lastDownload, setLastDownload] = useState<string | null>(null);

  const reportTypeMeta = REPORT_TYPE_OPTIONS.find((o) => o.value === reportType)!;

  const handleGenerate = async () => {
    setError(null);
    setLastDownload(null);
    if (reportType === 'gdpr') {
      const trimmed = userId.trim();
      if (!trimmed) {
        setError('User ID is required for GDPR exports.');
        return;
      }
      setBusy(true);
      try {
        const dl = await generateGDPRExport(trimmed);
        triggerBlobDownload(dl.blob, dl.filename);
        setLastDownload(dl.filename);
      } catch (err) {
        setError(describeError(err));
      } finally {
        setBusy(false);
      }
      return;
    }

    if (fromDate && toDate && toDate < fromDate) {
      setError('"To" must be on or after "From".');
      return;
    }
    setBusy(true);
    try {
      const dl = await generateComplianceReport({
        format,
        from: widenToInstant(fromDate, 'from'),
        to: widenToInstant(toDate, 'to'),
      });
      triggerBlobDownload(dl.blob, dl.filename);
      setLastDownload(dl.filename);
    } catch (err) {
      setError(describeError(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mx-auto max-w-4xl space-y-6">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight text-text-primary">
          Compliance Reports
        </h1>
        <p className="text-sm text-text-secondary">
          Generate auditor-ready evidence packages for SOC2 / ISO27001 control
          reviews, or fulfil GDPR data-portability requests.
        </p>
      </header>

      <section
        data-testid="compliance-form"
        className="space-y-5 rounded-lg border border-border/50 bg-bg-secondary/60 p-5"
      >
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
          <label className="block text-sm">
            <span className="mb-1 block text-text-secondary">Report type</span>
            <select
              aria-label="Report type"
              value={reportType}
              onChange={(e) => {
                setReportType(e.target.value as ReportType);
                setError(null);
                setLastDownload(null);
              }}
              className="w-full rounded-md border border-border/40 bg-bg-primary px-3 py-2 text-sm text-text-primary focus:border-accent-amber/60 focus:outline-none"
            >
              {REPORT_TYPE_OPTIONS.map((opt) => (
                <option key={opt.value} value={opt.value}>
                  {opt.label}
                </option>
              ))}
            </select>
          </label>

          {reportType === 'soc2' && (
            <label className="block text-sm">
              <span className="mb-1 block text-text-secondary">Format</span>
              <select
                aria-label="Format"
                value={format}
                onChange={(e) => setFormat(e.target.value as ComplianceReportFormat)}
                className="w-full rounded-md border border-border/40 bg-bg-primary px-3 py-2 text-sm text-text-primary focus:border-accent-amber/60 focus:outline-none"
              >
                {FORMAT_OPTIONS.map((opt) => (
                  <option key={opt.value} value={opt.value}>
                    {opt.label}
                  </option>
                ))}
              </select>
            </label>
          )}
        </div>

        <p className="text-xs text-text-secondary">{reportTypeMeta.hint}</p>

        {reportType === 'soc2' ? (
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            <label className="block text-sm">
              <span className="mb-1 block text-text-secondary">
                From (optional)
              </span>
              <input
                type="date"
                aria-label="From"
                value={fromDate}
                onChange={(e) => setFromDate(e.target.value)}
                className="w-full rounded-md border border-border/40 bg-bg-primary px-3 py-2 text-sm font-mono text-text-primary focus:border-accent-amber/60 focus:outline-none"
              />
            </label>
            <label className="block text-sm">
              <span className="mb-1 block text-text-secondary">To (optional)</span>
              <input
                type="date"
                aria-label="To"
                value={toDate}
                onChange={(e) => setToDate(e.target.value)}
                className="w-full rounded-md border border-border/40 bg-bg-primary px-3 py-2 text-sm font-mono text-text-primary focus:border-accent-amber/60 focus:outline-none"
              />
            </label>
          </div>
        ) : (
          <label className="block text-sm">
            <span className="mb-1 block text-text-secondary">User ID</span>
            <input
              type="text"
              aria-label="User ID"
              value={userId}
              onChange={(e) => setUserId(e.target.value)}
              placeholder="user:alice@example.com"
              className="w-full rounded-md border border-border/40 bg-bg-primary px-3 py-2 text-sm font-mono text-text-primary focus:border-accent-amber/60 focus:outline-none"
            />
          </label>
        )}

        {error && (
          <div
            role="alert"
            data-testid="compliance-error"
            className="rounded-md border border-rose-500/40 bg-rose-500/10 px-3 py-2 text-xs text-rose-300"
          >
            {error}
          </div>
        )}
        {lastDownload && !error && (
          <div
            data-testid="compliance-success"
            className="rounded-md border border-emerald-500/40 bg-emerald-500/10 px-3 py-2 text-xs text-emerald-300"
          >
            Saved <span className="font-mono">{lastDownload}</span>.
          </div>
        )}

        <div className="flex justify-end">
          <button
            type="button"
            disabled={busy}
            onClick={handleGenerate}
            data-testid="compliance-submit"
            className="rounded-md bg-amber-600 px-4 py-2 text-sm font-semibold text-white shadow-sm hover:bg-amber-500 disabled:opacity-60"
          >
            {busy ? 'Generating…' : 'Generate & Download'}
          </button>
        </div>
      </section>
    </div>
  );
}
