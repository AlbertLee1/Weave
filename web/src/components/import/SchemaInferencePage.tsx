import { useCallback, useMemo, useRef, useState } from 'react';
import {
  inferSchema,
  SUPPORTED_BASE_TYPES,
  type SchemaInferenceResult,
  type SchemaField,
  type SupportedBaseType,
} from '../../api/schemaInference';
import { ApiRequestError } from '../../api/client';
import { LoadingSpinner } from '../common/LoadingSpinner';

type Format = 'csv' | 'json' | 'ndjson';

interface FieldOverride {
  baseType: SupportedBaseType;
  nullable: boolean;
  rename: string;
  exclude: boolean;
}

interface AdjustedField {
  name: string;
  baseType: SupportedBaseType;
  nullable: boolean;
  excluded: boolean;
  inferred: SchemaField;
}

const SAMPLE_ROWS_DEFAULT = 1000;

function isSupportedBaseType(t: string): t is SupportedBaseType {
  return (SUPPORTED_BASE_TYPES as readonly string[]).includes(t);
}

function normaliseInferredType(t: string): SupportedBaseType {
  return isSupportedBaseType(t) ? t : 'string';
}

export function SchemaInferencePage() {
  const [format, setFormat] = useState<Format>('csv');
  const [hasHeader, setHasHeader] = useState(true);
  const [delimiter, setDelimiter] = useState(',');
  const [sampleRows, setSampleRows] = useState(SAMPLE_ROWS_DEFAULT);
  const [sample, setSample] = useState('');
  const [fileName, setFileName] = useState('');
  const [running, setRunning] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<SchemaInferenceResult | null>(null);
  const [overrides, setOverrides] = useState<Record<string, FieldOverride>>({});
  const [dragOver, setDragOver] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const adjusted: AdjustedField[] = useMemo(() => {
    if (!result) return [];
    return result.fields.map((f) => {
      const o = overrides[f.name];
      const inferredBT = normaliseInferredType(f.baseType);
      return {
        name: o?.rename?.trim() || f.name,
        baseType: o?.baseType ?? inferredBT,
        nullable: o?.nullable ?? f.nullable,
        excluded: o?.exclude ?? false,
        inferred: f,
      };
    });
  }, [result, overrides]);

  const handleRun = useCallback(async () => {
    setError(null);
    setResult(null);
    if (!sample.trim()) {
      setError('Please paste a sample or upload a file before running inference.');
      return;
    }
    setRunning(true);
    try {
      const res = await inferSchema({
        format,
        sample,
        options: {
          sampleRows,
          hasHeader,
          delimiter: format === 'csv' ? delimiter : undefined,
        },
      });
      setResult(res);
      setOverrides({});
    } catch (e) {
      if (e instanceof ApiRequestError) {
        const reason = e.parameters?.reason ?? e.errorName;
        setError(`${e.errorName}: ${reason}`);
      } else if (e instanceof Error) {
        setError(e.message);
      } else {
        setError('Inference failed');
      }
    } finally {
      setRunning(false);
    }
  }, [format, sample, sampleRows, hasHeader, delimiter]);

  const handleFile = useCallback(async (file: File) => {
    setFileName(file.name);
    const text = await file.text();
    setSample(text);
    // Naive format auto-detection: filename suffix takes precedence,
    // body shape (`[` / `{`) is the fallback.
    const lower = file.name.toLowerCase();
    if (lower.endsWith('.json')) {
      setFormat('json');
    } else if (lower.endsWith('.ndjson') || lower.endsWith('.jsonl')) {
      setFormat('ndjson');
    } else if (lower.endsWith('.tsv')) {
      setFormat('csv');
      setDelimiter('\t');
    } else if (lower.endsWith('.csv')) {
      setFormat('csv');
      setDelimiter(',');
    } else {
      const trimmed = text.trimStart();
      if (trimmed.startsWith('[') || trimmed.startsWith('{')) {
        setFormat('json');
      } else {
        setFormat('csv');
      }
    }
  }, []);

  const onDrop = useCallback(
    (e: React.DragEvent<HTMLDivElement>) => {
      e.preventDefault();
      setDragOver(false);
      const f = e.dataTransfer.files?.[0];
      if (f) {
        void handleFile(f);
      }
    },
    [handleFile],
  );

  const updateOverride = useCallback(
    (fieldName: string, patch: Partial<FieldOverride>) => {
      setOverrides((prev) => {
        const cur = prev[fieldName] ?? {
          baseType: 'string' as SupportedBaseType,
          nullable: false,
          rename: '',
          exclude: false,
        };
        return { ...prev, [fieldName]: { ...cur, ...patch } };
      });
    },
    [],
  );

  const downloadSchemaJSON = useCallback(() => {
    if (!result) return;
    const payload = {
      format: result.format,
      rowsScanned: result.rowsScanned,
      fields: adjusted
        .filter((a) => !a.excluded)
        .map((a) => ({
          name: a.name,
          baseType: a.baseType,
          nullable: a.nullable,
        })),
    };
    const blob = new Blob([JSON.stringify(payload, null, 2)], {
      type: 'application/json',
    });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'schema.json';
    a.click();
    URL.revokeObjectURL(url);
  }, [result, adjusted]);

  return (
    <div className="space-y-6 p-6" data-testid="schema-inference-page">
      <div>
        <h1 className="text-xl font-semibold">Schema Inference</h1>
        <p className="text-sm text-gray-500">
          Paste or upload a CSV / JSON sample (up to {SAMPLE_ROWS_DEFAULT} rows
          scanned by default). Inferred per-column types appear below — review,
          rename, retype, or exclude before exporting the schema.
        </p>
      </div>

      <div className="flex flex-wrap gap-4 rounded border border-gray-200 bg-white p-4">
        <label className="flex flex-col text-sm">
          <span className="font-medium">Format</span>
          <select
            value={format}
            onChange={(e) => setFormat(e.target.value as Format)}
            className="mt-1 rounded border border-gray-300 px-2 py-1"
            data-testid="format-select"
          >
            <option value="csv">CSV / TSV</option>
            <option value="json">JSON (array of objects)</option>
            <option value="ndjson">NDJSON (one object per line)</option>
          </select>
        </label>

        {format === 'csv' && (
          <>
            <label className="flex items-center gap-2 self-end text-sm">
              <input
                type="checkbox"
                checked={hasHeader}
                onChange={(e) => setHasHeader(e.target.checked)}
                data-testid="has-header"
              />
              <span>First row is header</span>
            </label>
            <label className="flex flex-col text-sm">
              <span className="font-medium">Delimiter</span>
              <select
                value={delimiter}
                onChange={(e) => setDelimiter(e.target.value)}
                className="mt-1 rounded border border-gray-300 px-2 py-1"
                data-testid="delimiter-select"
              >
                <option value=",">Comma (,)</option>
                <option value="\t">Tab</option>
                <option value=";">Semicolon (;)</option>
                <option value="|">Pipe (|)</option>
              </select>
            </label>
          </>
        )}

        <label className="flex flex-col text-sm">
          <span className="font-medium">Sample rows</span>
          <input
            type="number"
            min={1}
            max={100000}
            value={sampleRows}
            onChange={(e) =>
              setSampleRows(Math.max(1, Number(e.target.value) || 1))
            }
            className="mt-1 w-28 rounded border border-gray-300 px-2 py-1"
            data-testid="sample-rows"
          />
        </label>
      </div>

      <div
        onDragOver={(e) => {
          e.preventDefault();
          setDragOver(true);
        }}
        onDragLeave={() => setDragOver(false)}
        onDrop={onDrop}
        onClick={() => fileInputRef.current?.click()}
        className={`flex cursor-pointer flex-col items-center justify-center rounded border-2 border-dashed p-6 text-center text-sm transition-colors ${
          dragOver
            ? 'border-amber-400 bg-amber-50'
            : 'border-gray-300 bg-gray-50 hover:bg-gray-100'
        }`}
        data-testid="dropzone"
      >
        <input
          ref={fileInputRef}
          type="file"
          accept=".csv,.tsv,.json,.ndjson,.jsonl,text/*"
          className="hidden"
          onChange={(e) => {
            const f = e.target.files?.[0];
            if (f) void handleFile(f);
          }}
        />
        {fileName ? (
          <span>
            Loaded <span className="font-medium">{fileName}</span> ({sample.length}{' '}
            chars). Click or drop a new file to replace.
          </span>
        ) : (
          <span>Drop a CSV / JSON file here, or click to choose one.</span>
        )}
      </div>

      <details className="rounded border border-gray-200 bg-white p-3 text-sm">
        <summary className="cursor-pointer select-none font-medium">
          Or paste sample directly
        </summary>
        <textarea
          value={sample}
          onChange={(e) => {
            setSample(e.target.value);
            setFileName('');
          }}
          rows={8}
          spellCheck={false}
          className="mt-2 w-full rounded border border-gray-300 p-2 font-mono text-xs"
          placeholder="Paste raw CSV / JSON here…"
          data-testid="sample-textarea"
        />
      </details>

      <div className="flex items-center gap-3">
        <button
          type="button"
          onClick={handleRun}
          disabled={running}
          className="rounded bg-amber-500 px-4 py-2 text-sm font-semibold text-white hover:bg-amber-600 disabled:opacity-50"
          data-testid="run-inference"
        >
          {running ? 'Inferring…' : 'Infer schema'}
        </button>
        {result && (
          <button
            type="button"
            onClick={downloadSchemaJSON}
            className="rounded border border-gray-300 px-4 py-2 text-sm hover:bg-gray-100"
            data-testid="download-schema"
          >
            Download schema.json
          </button>
        )}
        {running && <LoadingSpinner size="sm" />}
      </div>

      {error && (
        <div
          className="rounded border border-red-200 bg-red-50 p-3 text-sm text-red-700"
          data-testid="error-banner"
        >
          {error}
        </div>
      )}

      {result && (
        <section className="space-y-3" data-testid="result-section">
          <header className="flex items-center justify-between">
            <h2 className="text-lg font-semibold">Inferred fields</h2>
            <span className="text-xs text-gray-500" data-testid="rows-scanned">
              {result.rowsScanned} rows scanned
              {result.truncated ? ' (sample truncated)' : ''}
              {result.warningCount ? ` · ${result.warningCount} warning(s)` : ''}
            </span>
          </header>

          <div className="overflow-x-auto rounded border border-gray-200 bg-white">
            <table className="min-w-full text-sm">
              <thead className="bg-gray-50">
                <tr>
                  <th className="px-3 py-2 text-left">Field</th>
                  <th className="px-3 py-2 text-left">Inferred</th>
                  <th className="px-3 py-2 text-left">Type</th>
                  <th className="px-3 py-2 text-left">Nullable</th>
                  <th className="px-3 py-2 text-left">Samples</th>
                  <th className="px-3 py-2 text-left">Counts</th>
                  <th className="px-3 py-2 text-left">Include</th>
                </tr>
              </thead>
              <tbody>
                {adjusted.map((a) => {
                  const row = a.inferred;
                  const ov = overrides[row.name];
                  const renamedTo = ov?.rename?.trim() || row.name;
                  const renameDirty = renamedTo !== row.name;
                  const typeChanged = a.baseType !== normaliseInferredType(row.baseType);
                  return (
                    <tr
                      key={row.name}
                      className={`border-t border-gray-100 ${a.excluded ? 'opacity-50' : ''}`}
                      data-testid={`field-row-${row.name}`}
                    >
                      <td className="px-3 py-2">
                        <input
                          type="text"
                          value={renamedTo}
                          onChange={(e) =>
                            updateOverride(row.name, { rename: e.target.value })
                          }
                          className={`w-40 rounded border px-2 py-1 text-sm ${
                            renameDirty ? 'border-amber-400' : 'border-gray-300'
                          }`}
                          data-testid={`rename-${row.name}`}
                        />
                      </td>
                      <td className="px-3 py-2 font-mono text-xs text-gray-600">
                        {row.baseType}
                      </td>
                      <td className="px-3 py-2">
                        <select
                          value={a.baseType}
                          onChange={(e) =>
                            updateOverride(row.name, {
                              baseType: e.target.value as SupportedBaseType,
                            })
                          }
                          className={`rounded border px-2 py-1 text-sm ${
                            typeChanged ? 'border-amber-400' : 'border-gray-300'
                          }`}
                          data-testid={`type-${row.name}`}
                        >
                          {SUPPORTED_BASE_TYPES.map((t) => (
                            <option key={t} value={t}>
                              {t}
                            </option>
                          ))}
                        </select>
                      </td>
                      <td className="px-3 py-2">
                        <input
                          type="checkbox"
                          checked={a.nullable}
                          onChange={(e) =>
                            updateOverride(row.name, { nullable: e.target.checked })
                          }
                          data-testid={`nullable-${row.name}`}
                        />
                      </td>
                      <td className="px-3 py-2 font-mono text-xs text-gray-700">
                        {(row.samples ?? []).slice(0, 5).join(', ') || '—'}
                      </td>
                      <td className="px-3 py-2 text-xs text-gray-500">
                        {row.nonNullCount} non-null · {row.nullCount} null
                      </td>
                      <td className="px-3 py-2">
                        <label className="flex items-center gap-1 text-xs">
                          <input
                            type="checkbox"
                            checked={!a.excluded}
                            onChange={(e) =>
                              updateOverride(row.name, { exclude: !e.target.checked })
                            }
                            data-testid={`include-${row.name}`}
                          />
                          <span>{a.excluded ? 'Skip' : 'Use'}</span>
                        </label>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </section>
      )}
    </div>
  );
}
