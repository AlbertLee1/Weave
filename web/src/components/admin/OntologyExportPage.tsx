import { useState } from 'react';
import { useParams } from 'react-router';
import { useMutation } from '@tanstack/react-query';
import {
  exportOntology,
  generateSdk,
  type OntologyExport,
  type SdkLanguage,
} from '../../api/ontologyExport';
import { describeApiError } from '../../api/describeError';
import { useToastStore } from '../../stores/toastStore';

// SDK_LANGUAGES are the three client-SDK targets the sdkgen endpoint accepts.
// The wire value (`ts`/`python`/`go`) is sent as ?lang=; the label is what the
// operator picks in the <select>.
const SDK_LANGUAGES: { value: SdkLanguage; label: string }[] = [
  { value: 'ts', label: 'TypeScript' },
  { value: 'python', label: 'Python' },
  { value: 'go', label: 'Go' },
];

// The per-collection sections the export summary tallies. Each is optional on
// the wire, so the page treats a missing key as a zero-length list.
const EXPORT_SECTIONS: { key: keyof OntologyExport; label: string }[] = [
  { key: 'objectTypes', label: 'objectTypes' },
  { key: 'linkTypes', label: 'linkTypes' },
  { key: 'actionTypes', label: 'actionTypes' },
  { key: 'interfaces', label: 'interfaces' },
  { key: 'sharedProperties', label: 'sharedProperties' },
  { key: 'valueTypes', label: 'valueTypes' },
  { key: 'typeGroups', label: 'typeGroups' },
  { key: 'functions', label: 'functions' },
  { key: 'queryTypes', label: 'queryTypes' },
];

function countOf(data: OntologyExport, key: keyof OntologyExport): number {
  const v = data[key];
  return Array.isArray(v) ? v.length : 0;
}

// triggerBlobDownload mirrors the QuiverPage download idiom: wrap the payload
// in an object URL, click a transient anchor, then revoke the URL. Centralized
// here so both the JSON export and the zip SDK download share identical
// plumbing (and so the BDD test can assert createObjectURL / anchor.click).
function triggerBlobDownload(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  URL.revokeObjectURL(url);
}

export function OntologyExportPage() {
  const { ontology } = useParams<{ ontology: string }>();
  const ontologyApiName = ontology ?? '';
  const pushToast = useToastStore((s) => s.push);

  const [lang, setLang] = useState<SdkLanguage>('ts');
  const [summary, setSummary] = useState<OntologyExport | null>(null);

  const exportMutation = useMutation({
    mutationFn: () => exportOntology(ontologyApiName),
    onSuccess: (data) => {
      triggerBlobDownload(
        new Blob([JSON.stringify(data, null, 2)], {
          type: 'application/json',
        }),
        `${ontologyApiName}-ontology.json`,
      );
      setSummary(data);
    },
    onError: (err) => {
      pushToast({
        message: describeApiError(
          err,
          `Failed to export ontology "${ontologyApiName}".`,
        ),
        severity: 'error',
      });
    },
  });

  const sdkMutation = useMutation({
    mutationFn: () => generateSdk(ontologyApiName, lang),
    onSuccess: (blob) => {
      triggerBlobDownload(blob, `${ontologyApiName}-${lang}-sdk.zip`);
    },
    onError: (err) => {
      pushToast({
        message: describeApiError(
          err,
          `Failed to generate ${lang} SDK for "${ontologyApiName}".`,
        ),
        severity: 'error',
      });
    },
  });

  if (!ontologyApiName) {
    return (
      <div
        data-testid="ontology-export-no-ontology"
        className="flex items-center justify-center h-[calc(100vh-3rem)] text-text-secondary text-sm"
      >
        Select an ontology from the dashboard first.
      </div>
    );
  }

  return (
    <div
      data-testid="ontology-export-page"
      className="flex flex-col h-[calc(100vh-3rem)] bg-bg-primary overflow-y-auto"
    >
      <header
        className="px-6 py-4 border-b flex flex-wrap items-center gap-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <h1 className="text-base font-semibold tracking-wide text-text-primary">
          Export &amp; SDK
        </h1>
        <span className="text-xs text-text-secondary uppercase tracking-widest">
          {ontologyApiName}
        </span>
      </header>

      <div className="flex-1 px-6 py-6 flex flex-col gap-6 max-w-3xl">
        {/* ── Export Ontology Definition ───────────────────────────────── */}
        <section
          data-testid="ontology-export-section"
          className="rounded border p-5 flex flex-col gap-4"
          style={{
            borderColor: 'rgba(31,41,55,0.5)',
            background: 'rgba(13,17,23,0.4)',
          }}
        >
          <div>
            <h2 className="text-sm font-semibold text-text-primary">
              Export Ontology Definition
            </h2>
            <p className="text-xs text-text-secondary mt-1">
              Download the complete ontology definition — object types, link
              types, action types, interfaces, shared properties, value types,
              type groups, functions, and query types — as a single JSON
              document.
            </p>
          </div>

          <div className="flex items-center gap-3">
            <button
              type="button"
              data-testid="ontology-export-btn"
              onClick={() => exportMutation.mutate()}
              disabled={exportMutation.isPending}
              className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
            >
              {exportMutation.isPending ? 'Exporting…' : 'Export as JSON'}
            </button>
            <span className="text-[11px] text-text-muted font-mono">
              {ontologyApiName}-ontology.json
            </span>
          </div>

          {summary && (
            <dl
              data-testid="ontology-export-summary"
              className="grid grid-cols-3 gap-x-6 gap-y-1 text-xs"
            >
              {EXPORT_SECTIONS.map((s) => (
                <div
                  key={s.key as string}
                  data-testid={`ontology-export-count-${s.key as string}`}
                  className="flex items-center justify-between gap-2 border-b py-0.5"
                  style={{ borderColor: 'rgba(31,41,55,0.4)' }}
                >
                  <dt className="text-text-secondary font-mono">{s.label}</dt>
                  <dd className="text-text-primary font-semibold tabular-nums">
                    {countOf(summary, s.key)}
                  </dd>
                </div>
              ))}
            </dl>
          )}
        </section>

        {/* ── Generate Client SDK ──────────────────────────────────────── */}
        <section
          data-testid="ontology-sdk-section"
          className="rounded border p-5 flex flex-col gap-4"
          style={{
            borderColor: 'rgba(31,41,55,0.5)',
            background: 'rgba(13,17,23,0.4)',
          }}
        >
          <div>
            <h2 className="text-sm font-semibold text-text-primary">
              Generate Client SDK
            </h2>
            <p className="text-xs text-text-secondary mt-1">
              Generate a strongly-typed client SDK for this ontology and
              download it as a zip archive. Pick a target language, then
              generate.
            </p>
          </div>

          <div className="flex flex-wrap items-end gap-3">
            <label className="flex flex-col gap-1 text-xs text-text-secondary">
              <span className="uppercase tracking-widest">Language</span>
              <select
                data-testid="ontology-sdk-lang-select"
                aria-label="SDK language"
                value={lang}
                onChange={(e) => setLang(e.target.value as SdkLanguage)}
                className="px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40"
              >
                {SDK_LANGUAGES.map((l) => (
                  <option key={l.value} value={l.value}>
                    {l.label}
                  </option>
                ))}
              </select>
            </label>

            <button
              type="button"
              data-testid="ontology-sdk-generate-btn"
              onClick={() => sdkMutation.mutate()}
              disabled={sdkMutation.isPending}
              className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
            >
              {sdkMutation.isPending ? 'Generating…' : 'Generate & Download'}
            </button>
          </div>
        </section>
      </div>
    </div>
  );
}
