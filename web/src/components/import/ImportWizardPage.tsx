import { useMemo, useRef, useState } from 'react';
import { useParams } from 'react-router';
import type { ActionType, ObjectType } from '../../api/types';
import { applyAction } from '../../api/actions';
import { useObjectTypes } from '../../hooks/useObjectTypes';
import { useActionTypes } from '../../hooks/useActions';
import {
  parseCsv,
  autoMapColumns,
  convertCellValue,
  validateCell,
  type ParsedCsv,
} from '../../lib/csvParser';
import { LoadingSpinner } from '../common/LoadingSpinner';

type WizardStep = 1 | 2 | 3 | 4;

interface RowFailure {
  rowIndex: number;
  error: string;
}

function baseTypeOf(dt: { type: string; itemType?: { type: string } }): string {
  if (dt.type === 'array' && dt.itemType) return dt.itemType.type;
  return dt.type;
}

function findCreateAction(
  actions: ActionType[],
  objectTypeApiName: string,
): ActionType | null {
  for (const action of actions) {
    if (rulesCreatingObjectType(action.rules, objectTypeApiName)) return action;
  }
  return null;
}

function rulesCreatingObjectType(v: unknown, objectTypeApiName: string): boolean {
  if (v == null) return false;
  if (Array.isArray(v)) {
    return v.some((item) => rulesCreatingObjectType(item, objectTypeApiName));
  }
  if (typeof v === 'object') {
    const obj = v as Record<string, unknown>;
    if (
      (obj.type === 'createObject' || obj.type === 'createOrModifyObject') &&
      typeof obj.objectType === 'string' &&
      obj.objectType === objectTypeApiName
    ) {
      return true;
    }
    for (const key of Object.keys(obj)) {
      if (rulesCreatingObjectType(obj[key], objectTypeApiName)) return true;
    }
  }
  return false;
}

export function ImportWizardPage() {
  const { ontology } = useParams<{ ontology: string }>();
  const ontologyApiName = ontology ?? '';

  const [step, setStep] = useState<WizardStep>(1);
  const [fileName, setFileName] = useState<string>('');
  const [parseError, setParseError] = useState<string | null>(null);
  const [parsed, setParsed] = useState<ParsedCsv | null>(null);
  const [selectedObjectTypeApi, setSelectedObjectTypeApi] = useState<string>('');
  const [columnMap, setColumnMap] = useState<Record<string, string>>({});
  const [dragOver, setDragOver] = useState(false);

  const [importing, setImporting] = useState(false);
  const [processedCount, setProcessedCount] = useState(0);
  const [successCount, setSuccessCount] = useState(0);
  const [failures, setFailures] = useState<RowFailure[]>([]);

  const { data: objectTypes } = useObjectTypes(ontologyApiName);
  const { data: actions } = useActionTypes(ontologyApiName);

  const fileInputRef = useRef<HTMLInputElement>(null);

  const selectedObjectType: ObjectType | null = useMemo(() => {
    if (!selectedObjectTypeApi || !objectTypes) return null;
    return objectTypes.find((t) => t.apiName === selectedObjectTypeApi) ?? null;
  }, [objectTypes, selectedObjectTypeApi]);

  const createAction: ActionType | null = useMemo(() => {
    if (!actions || !selectedObjectType) return null;
    return findCreateAction(actions, selectedObjectType.apiName);
  }, [actions, selectedObjectType]);

  async function handleFileChosen(file: File) {
    setParseError(null);
    setFileName(file.name);
    try {
      const text = await file.text();
      const result = parseCsv(text);
      if (result.headers.length === 0) {
        setParseError('CSV appears to be empty or malformed');
        setParsed(null);
        return;
      }
      setParsed(result);
    } catch (err) {
      setParseError(err instanceof Error ? err.message : 'Failed to parse CSV');
      setParsed(null);
    }
  }

  function onFileInputChange(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (file) void handleFileChosen(file);
  }

  function onDrop(e: React.DragEvent<HTMLDivElement>) {
    e.preventDefault();
    setDragOver(false);
    const file = e.dataTransfer.files?.[0];
    if (file) void handleFileChosen(file);
  }

  function onSelectObjectType(api: string) {
    setSelectedObjectTypeApi(api);
    if (!parsed) return;
    const target = objectTypes?.find((t) => t.apiName === api);
    if (!target) return;
    const propertyNames = Object.keys(target.properties ?? {});
    setColumnMap(autoMapColumns(parsed.headers, propertyNames));
  }

  function setMappingFor(header: string, propertyApi: string) {
    setColumnMap((m) => ({ ...m, [header]: propertyApi }));
  }

  const activeMappings = useMemo(
    () =>
      Object.entries(columnMap).filter(([, propApi]) => propApi.length > 0),
    [columnMap],
  );

  const previewRows = useMemo(() => parsed?.rows.slice(0, 10) ?? [], [parsed]);

  function getPropBaseType(propApiName: string): string {
    if (!selectedObjectType) return 'string';
    const prop = selectedObjectType.properties?.[propApiName];
    if (!prop) return 'string';
    return baseTypeOf(prop.dataType);
  }

  function findParamIdForProperty(propertyApi: string): string | null {
    if (!createAction) return null;
    const params = createAction.parameters ?? {};
    const ids = Object.keys(params);
    if (ids.includes(propertyApi)) return propertyApi;
    const norm = (s: string) => s.toLowerCase().replace(/[\s_-]+/g, '');
    const target = norm(propertyApi);
    return ids.find((id) => norm(id) === target) ?? null;
  }

  function buildParameters(
    row: Record<string, string>,
  ): { parameters: Record<string, unknown> } | { error: string } {
    if (!createAction || !selectedObjectType) {
      return { error: 'No create action resolved' };
    }
    const params: Record<string, unknown> = {};
    for (const [header, propApi] of activeMappings) {
      const paramId = findParamIdForProperty(propApi);
      if (!paramId) continue;
      const baseType = getPropBaseType(propApi);
      const raw = row[header] ?? '';
      const conv = convertCellValue(raw, baseType);
      if ('error' in conv) {
        return { error: `${header}: ${conv.error}` };
      }
      if (conv.value === null) continue;
      params[paramId] = conv.value;
    }
    return { parameters: params };
  }

  async function runImport() {
    if (!createAction || !parsed || !selectedObjectType) return;
    setImporting(true);
    setProcessedCount(0);
    setSuccessCount(0);
    setFailures([]);
    let ok = 0;
    const errs: RowFailure[] = [];
    for (let i = 0; i < parsed.rows.length; i++) {
      const row = parsed.rows[i];
      const built = buildParameters(row);
      if ('error' in built) {
        errs.push({ rowIndex: i + 1, error: built.error });
        setProcessedCount(i + 1);
        setFailures([...errs]);
        continue;
      }
      try {
        await applyAction(ontologyApiName, createAction.apiName, {
          parameters: built.parameters,
        });
        ok += 1;
        setSuccessCount(ok);
      } catch (err) {
        const msg = err instanceof Error ? err.message : 'Apply failed';
        errs.push({ rowIndex: i + 1, error: msg });
        setFailures([...errs]);
      }
      setProcessedCount(i + 1);
    }
    setImporting(false);
  }

  function resetWizard() {
    setStep(1);
    setFileName('');
    setParsed(null);
    setParseError(null);
    setSelectedObjectTypeApi('');
    setColumnMap({});
    setProcessedCount(0);
    setSuccessCount(0);
    setFailures([]);
    if (fileInputRef.current) fileInputRef.current.value = '';
  }

  const canGoStep2 = parsed !== null && parsed.rows.length > 0;
  const canGoStep3 =
    selectedObjectType !== null && activeMappings.length > 0;
  const canGoStep4 = createAction !== null;
  const progressPct =
    parsed && parsed.rows.length > 0
      ? Math.floor((processedCount / parsed.rows.length) * 100)
      : 0;

  return (
    <div className="p-6 max-w-5xl mx-auto">
      <h1
        className="text-2xl font-semibold tracking-tight mb-1"
        data-testid="import-wizard-heading"
      >
        Import CSV Data
      </h1>
      <p className="text-sm text-text-secondary mb-6">
        Upload a CSV file to bulk-create objects in{' '}
        <span className="font-mono">{ontologyApiName}</span>.
      </p>

      <StepIndicator step={step} />

      <div className="mt-6">
        {step === 1 && (
          <div>
            <h2 className="text-lg font-medium mb-3">Step 1 — Upload CSV</h2>
            <div
              data-testid="dropzone"
              onDragOver={(e) => {
                e.preventDefault();
                setDragOver(true);
              }}
              onDragLeave={() => setDragOver(false)}
              onDrop={onDrop}
              className={`rounded-xl border-2 border-dashed p-10 text-center transition-colors ${
                dragOver
                  ? 'border-accent-cyan/60 bg-accent-cyan/5'
                  : 'border-border bg-bg-secondary/40'
              }`}
            >
              <p className="text-sm text-text-secondary mb-4">
                Drag and drop a CSV file here, or click to browse.
              </p>
              <input
                ref={fileInputRef}
                type="file"
                accept=".csv,text/csv"
                onChange={onFileInputChange}
                className="hidden"
                data-testid="file-input"
                aria-label="CSV file"
              />
              <button
                type="button"
                onClick={() => fileInputRef.current?.click()}
                className="px-4 py-2 rounded border border-border hover:border-accent-cyan text-sm font-sans text-text-primary transition-colors"
                data-testid="browse-button"
              >
                Browse…
              </button>
              {fileName && (
                <p
                  className="mt-4 text-xs font-mono text-text-primary"
                  data-testid="file-name"
                >
                  {fileName}
                </p>
              )}
              {parseError && (
                <p
                  role="alert"
                  className="mt-2 text-xs font-mono text-accent-error"
                >
                  {parseError}
                </p>
              )}
              {parsed && (
                <p
                  className="mt-2 text-xs font-mono text-text-secondary"
                  data-testid="parse-summary"
                >
                  Parsed {parsed.rows.length} rows, {parsed.headers.length}{' '}
                  columns.
                </p>
              )}
            </div>
            <div className="flex justify-end mt-4">
              <button
                type="button"
                disabled={!canGoStep2}
                onClick={() => setStep(2)}
                data-testid="next-2"
                className="px-4 py-2 rounded text-sm bg-accent-cyan text-bg-primary disabled:opacity-40 disabled:cursor-not-allowed"
              >
                Next
              </button>
            </div>
          </div>
        )}

        {step === 2 && (
          <div>
            <h2 className="text-lg font-medium mb-3">
              Step 2 — Map CSV columns to properties
            </h2>
            <div className="flex items-center gap-3 mb-4">
              <label className="text-sm text-text-secondary" htmlFor="ot-select">
                Target object type
              </label>
              <select
                id="ot-select"
                data-testid="object-type-select"
                value={selectedObjectTypeApi}
                onChange={(e) => onSelectObjectType(e.target.value)}
                className="px-3 py-2 rounded border border-border bg-bg-secondary text-sm"
              >
                <option value="">Select…</option>
                {(objectTypes ?? []).map((ot) => (
                  <option key={ot.apiName} value={ot.apiName}>
                    {ot.displayName} ({ot.apiName})
                  </option>
                ))}
              </select>
            </div>
            {selectedObjectType && parsed && (
              <div className="rounded border border-border overflow-hidden">
                <table className="w-full text-sm">
                  <thead className="bg-bg-secondary text-text-secondary">
                    <tr>
                      <th className="text-left px-3 py-2">CSV column</th>
                      <th className="text-left px-3 py-2">Maps to property</th>
                    </tr>
                  </thead>
                  <tbody>
                    {parsed.headers.map((h) => (
                      <tr key={h} className="border-t border-border">
                        <td className="px-3 py-2 font-mono">{h}</td>
                        <td className="px-3 py-2">
                          <select
                            data-testid={`map-${h}`}
                            value={columnMap[h] ?? ''}
                            onChange={(e) => setMappingFor(h, e.target.value)}
                            className="px-2 py-1 rounded border border-border bg-bg-secondary text-sm"
                          >
                            <option value="">— skip —</option>
                            {Object.keys(selectedObjectType.properties ?? {}).map(
                              (p) => (
                                <option key={p} value={p}>
                                  {p}
                                </option>
                              ),
                            )}
                          </select>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            <div className="flex justify-between mt-4">
              <button
                type="button"
                onClick={() => setStep(1)}
                className="px-4 py-2 rounded text-sm border border-border"
              >
                Back
              </button>
              <button
                type="button"
                disabled={!canGoStep3}
                onClick={() => setStep(3)}
                data-testid="next-3"
                className="px-4 py-2 rounded text-sm bg-accent-cyan text-bg-primary disabled:opacity-40 disabled:cursor-not-allowed"
              >
                Next
              </button>
            </div>
          </div>
        )}

        {step === 3 && selectedObjectType && parsed && (
          <div>
            <h2 className="text-lg font-medium mb-3">
              Step 3 — Preview (first 10 rows)
            </h2>
            {!createAction && (
              <p
                role="alert"
                data-testid="no-create-action"
                className="mb-3 text-xs font-mono text-accent-error"
              >
                No ActionType with a createObject rule targeting "
                {selectedObjectType.apiName}" is defined. Configure one in the
                Ontology Manager before importing.
              </p>
            )}
            <div className="rounded border border-border overflow-x-auto">
              <table className="w-full text-xs">
                <thead className="bg-bg-secondary text-text-secondary">
                  <tr>
                    <th className="text-left px-2 py-1">#</th>
                    {activeMappings.map(([h, propApi]) => (
                      <th key={h} className="text-left px-2 py-1">
                        <span className="font-mono">{h}</span>
                        <span className="text-text-muted"> → </span>
                        <span className="font-mono text-accent-cyan">
                          {propApi}
                        </span>
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {previewRows.map((row, i) => (
                    <tr key={i} className="border-t border-border">
                      <td className="px-2 py-1 text-text-muted">{i + 1}</td>
                      {activeMappings.map(([h, propApi]) => {
                        const base = getPropBaseType(propApi);
                        const warning = validateCell(row[h] ?? '', base);
                        return (
                          <td key={h} className="px-2 py-1 align-top">
                            <div className="font-mono">{row[h] ?? ''}</div>
                            {warning && (
                              <span
                                data-testid={`warn-${i}-${h}`}
                                className="inline-block mt-1 px-1.5 py-0.5 rounded text-[10px] bg-accent-warning/20 text-accent-warning"
                              >
                                {warning}
                              </span>
                            )}
                          </td>
                        );
                      })}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="flex justify-between mt-4">
              <button
                type="button"
                onClick={() => setStep(2)}
                className="px-4 py-2 rounded text-sm border border-border"
              >
                Back
              </button>
              <button
                type="button"
                disabled={!canGoStep4}
                onClick={() => setStep(4)}
                data-testid="next-4"
                className="px-4 py-2 rounded text-sm bg-accent-cyan text-bg-primary disabled:opacity-40 disabled:cursor-not-allowed"
              >
                Next
              </button>
            </div>
          </div>
        )}

        {step === 4 && parsed && selectedObjectType && (
          <div>
            <h2 className="text-lg font-medium mb-3">Step 4 — Execute import</h2>
            <div className="rounded border border-border p-4 bg-bg-secondary/40">
              <p className="text-sm mb-3">
                About to import{' '}
                <strong data-testid="row-count">{parsed.rows.length}</strong>{' '}
                rows into{' '}
                <strong className="font-mono">
                  {selectedObjectType.apiName}
                </strong>{' '}
                via action{' '}
                <strong className="font-mono">
                  {createAction?.apiName ?? '—'}
                </strong>
                .
              </p>
              <div className="flex items-center gap-3 mb-2">
                <button
                  type="button"
                  disabled={importing || !createAction}
                  onClick={() => void runImport()}
                  data-testid="start-import"
                  className="px-4 py-2 rounded text-sm bg-accent-cyan text-bg-primary disabled:opacity-40 disabled:cursor-not-allowed"
                >
                  {importing ? 'Importing…' : 'Start import'}
                </button>
                {importing && <LoadingSpinner size="sm" />}
                {!importing && processedCount > 0 && (
                  <button
                    type="button"
                    onClick={resetWizard}
                    data-testid="reset"
                    className="px-4 py-2 rounded text-sm border border-border"
                  >
                    Start over
                  </button>
                )}
              </div>
              <div
                data-testid="progress-bar"
                className="h-2 rounded bg-bg-tertiary overflow-hidden"
              >
                <div
                  className="h-full bg-accent-cyan transition-all duration-200"
                  style={{ width: `${progressPct}%` }}
                />
              </div>
              <p className="mt-2 text-xs font-mono text-text-secondary">
                Processed <span data-testid="processed-count">{processedCount}</span>/
                {parsed.rows.length} · Success{' '}
                <span data-testid="success-count">{successCount}</span> · Failed{' '}
                <span data-testid="failure-count">{failures.length}</span>
              </p>
            </div>
            {failures.length > 0 && (
              <div
                className="mt-4 rounded border border-accent-error/40 bg-accent-error/5 p-4"
                data-testid="failure-summary"
              >
                <h3 className="text-sm font-semibold text-accent-error mb-2">
                  Failed rows ({failures.length})
                </h3>
                <ul className="text-xs font-mono space-y-1 max-h-64 overflow-y-auto">
                  {failures.map((f) => (
                    <li key={f.rowIndex}>
                      Row {f.rowIndex}: {f.error}
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function StepIndicator({ step }: { step: WizardStep }) {
  const steps = [
    { n: 1, label: 'Upload' },
    { n: 2, label: 'Map columns' },
    { n: 3, label: 'Preview' },
    { n: 4, label: 'Import' },
  ];
  return (
    <ol className="flex items-center gap-2 text-xs font-mono" data-testid="step-indicator">
      {steps.map((s, i) => {
        const active = step === s.n;
        const done = step > s.n;
        return (
          <li key={s.n} className="flex items-center gap-2">
            <span
              data-testid={`step-${s.n}`}
              data-state={active ? 'active' : done ? 'done' : 'pending'}
              className={`w-6 h-6 rounded-full flex items-center justify-center border ${
                active
                  ? 'bg-accent-cyan text-bg-primary border-accent-cyan'
                  : done
                    ? 'bg-accent-teal/20 text-accent-teal border-accent-teal/40'
                    : 'border-border text-text-secondary'
              }`}
            >
              {s.n}
            </span>
            <span
              className={
                active ? 'text-text-primary' : 'text-text-secondary'
              }
            >
              {s.label}
            </span>
            {i < steps.length - 1 && (
              <span className="text-text-muted">›</span>
            )}
          </li>
        );
      })}
    </ol>
  );
}
