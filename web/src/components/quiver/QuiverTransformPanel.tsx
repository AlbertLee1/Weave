import { useMemo, useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import {
  transformTimeSeries,
  type TransformSpec,
  type TransformOp,
  type TimeSeriesPoint,
} from '../../api/timeseries';
import { ApiRequestError } from '../../api/client';
import type { SeriesSpec } from './QuiverWorkbenchView';

// US-402: per-series transform-chain workbench. The user picks one of the
// workbench series as the `source`, stacks transform steps (at minimum a
// resample with interval + agg), then runs the chain against the backend
// /timeseries/transform endpoint. The transformed series is shown inline
// so a reader can compare the reduction without leaving the page.

const TRANSFORM_OPS: readonly TransformOp[] = [
  'resample',
  'diff',
  'sma',
  'ema',
  'scale',
];

const RESAMPLE_AGGS = ['avg', 'sum', 'min', 'max', 'count', 'first', 'last'] as const;

interface DraftStep {
  op: TransformOp;
  // op-specific fields; only the ones relevant to `op` are read when the
  // step is committed to the chain.
  interval: string;
  agg: string;
  window: string;
  alpha: string;
  factor: string;
  offset: string;
}

const EMPTY_DRAFT: DraftStep = {
  op: 'resample',
  interval: '1h',
  agg: 'avg',
  window: '5',
  alpha: '0.3',
  factor: '1',
  offset: '0',
};

// draftToSpec translates a draft form row into the wire TransformSpec,
// keeping only the params that op cares about so the POST body stays
// minimal and matches the backend's per-op validation.
function draftToSpec(d: DraftStep): TransformSpec {
  switch (d.op) {
    case 'resample':
      return { op: 'resample', params: { interval: d.interval, agg: d.agg } };
    case 'sma':
      return { op: 'sma', params: { window: Number(d.window) } };
    case 'ema':
      return { op: 'ema', params: { alpha: Number(d.alpha) } };
    case 'scale':
      return {
        op: 'scale',
        params: { factor: Number(d.factor), offset: Number(d.offset) },
      };
    case 'diff':
    default:
      return { op: 'diff' };
  }
}

function describeStep(spec: TransformSpec): string {
  const p = spec.params ?? {};
  switch (spec.op) {
    case 'resample':
      return `resample ${String(p.interval ?? '')} ${String(p.agg ?? '')}`;
    case 'sma':
      return `sma window=${String(p.window ?? '')}`;
    case 'ema':
      return `ema alpha=${String(p.alpha ?? '')}`;
    case 'scale':
      return `scale ×${String(p.factor ?? '')}+${String(p.offset ?? '')}`;
    case 'diff':
    default:
      return 'diff';
  }
}

interface QuiverTransformPanelProps {
  ontologyApiName: string;
  seriesList: SeriesSpec[];
}

export function QuiverTransformPanel({
  ontologyApiName,
  seriesList,
}: QuiverTransformPanelProps) {
  const [selectedId, setSelectedId] = useState<string>('');
  const [draft, setDraft] = useState<DraftStep>(EMPTY_DRAFT);
  const [chain, setChain] = useState<TransformSpec[]>([]);
  const [result, setResult] = useState<TimeSeriesPoint[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  // The selected series resolves the source descriptor. Default to the
  // first series so the panel is usable the instant a series exists.
  const effectiveId =
    selectedId && seriesList.some((s) => s.id === selectedId)
      ? selectedId
      : (seriesList[0]?.id ?? '');
  const selected = seriesList.find((s) => s.id === effectiveId);

  const mutation = useMutation({
    mutationFn: () => {
      if (!selected) {
        return Promise.reject(new Error('no series selected'));
      }
      return transformTimeSeries(ontologyApiName, {
        source: {
          objectType: selected.objectType,
          primaryKey: selected.primaryKey,
          property: selected.property,
        },
        transforms: chain,
      });
    },
    onSuccess: (resp) => {
      setResult(resp.points ?? []);
      setError(null);
    },
    onError: (err: unknown) => {
      setResult(null);
      if (err instanceof ApiRequestError) {
        setError(`${err.errorName}: ${JSON.stringify(err.parameters ?? {})}`);
      } else {
        setError(String(err));
      }
    },
  });

  function handleAddStep() {
    setChain((prev) => [...prev, draftToSpec(draft)]);
  }

  function handleRemoveStep(idx: number) {
    setChain((prev) => prev.filter((_, i) => i !== idx));
    setResult(null);
  }

  function handleClearChain() {
    setChain([]);
    setResult(null);
    setError(null);
  }

  const canRun = chain.length > 0 && !!selected && !mutation.isPending;

  const resultPreview = useMemo(() => {
    if (!result) return [];
    return result.slice(0, 8);
  }, [result]);

  if (seriesList.length === 0) return null;

  return (
    <div
      className="border border-border rounded bg-bg-tertiary"
      data-testid="quiver-transform-panel"
    >
      <div className="flex items-center justify-between px-4 py-2 border-b border-border">
        <h3 className="text-xs font-medium text-text-primary">
          Transform chain
        </h3>
        <span className="text-[10px] font-mono text-text-muted">
          POST /timeseries/transform
        </span>
      </div>

      <div className="p-4 space-y-3">
        {/* Source series + step builder */}
        <div className="flex flex-wrap items-end gap-2">
          <label className="flex flex-col gap-1 text-[10px] text-text-secondary">
            Series
            <select
              value={effectiveId}
              onChange={(e) => setSelectedId(e.target.value)}
              data-testid="transform-series-select"
              className="px-2 py-1 text-xs bg-bg-secondary border border-border rounded text-text-primary font-mono"
            >
              {seriesList.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.label} ({s.objectType}/{s.primaryKey}.{s.property})
                </option>
              ))}
            </select>
          </label>

          <label className="flex flex-col gap-1 text-[10px] text-text-secondary">
            Op
            <select
              value={draft.op}
              onChange={(e) =>
                setDraft((d) => ({ ...d, op: e.target.value as TransformOp }))
              }
              data-testid="transform-op-select"
              className="px-2 py-1 text-xs bg-bg-secondary border border-border rounded text-text-primary font-mono"
            >
              {TRANSFORM_OPS.map((op) => (
                <option key={op} value={op}>
                  {op}
                </option>
              ))}
            </select>
          </label>

          {draft.op === 'resample' && (
            <>
              <label className="flex flex-col gap-1 text-[10px] text-text-secondary">
                Interval
                <input
                  type="text"
                  value={draft.interval}
                  onChange={(e) =>
                    setDraft((d) => ({ ...d, interval: e.target.value }))
                  }
                  data-testid="transform-interval-input"
                  className="px-2 py-1 text-xs bg-bg-secondary border border-border rounded text-text-primary font-mono w-20"
                />
              </label>
              <label className="flex flex-col gap-1 text-[10px] text-text-secondary">
                Agg
                <select
                  value={draft.agg}
                  onChange={(e) =>
                    setDraft((d) => ({ ...d, agg: e.target.value }))
                  }
                  data-testid="transform-agg-select"
                  className="px-2 py-1 text-xs bg-bg-secondary border border-border rounded text-text-primary font-mono"
                >
                  {RESAMPLE_AGGS.map((a) => (
                    <option key={a} value={a}>
                      {a}
                    </option>
                  ))}
                </select>
              </label>
            </>
          )}

          {draft.op === 'sma' && (
            <label className="flex flex-col gap-1 text-[10px] text-text-secondary">
              Window
              <input
                type="number"
                min={1}
                value={draft.window}
                onChange={(e) =>
                  setDraft((d) => ({ ...d, window: e.target.value }))
                }
                data-testid="transform-window-input"
                className="px-2 py-1 text-xs bg-bg-secondary border border-border rounded text-text-primary font-mono w-20"
              />
            </label>
          )}

          {draft.op === 'ema' && (
            <label className="flex flex-col gap-1 text-[10px] text-text-secondary">
              Alpha
              <input
                type="number"
                min={0}
                max={1}
                step={0.05}
                value={draft.alpha}
                onChange={(e) =>
                  setDraft((d) => ({ ...d, alpha: e.target.value }))
                }
                data-testid="transform-alpha-input"
                className="px-2 py-1 text-xs bg-bg-secondary border border-border rounded text-text-primary font-mono w-20"
              />
            </label>
          )}

          {draft.op === 'scale' && (
            <>
              <label className="flex flex-col gap-1 text-[10px] text-text-secondary">
                Factor
                <input
                  type="number"
                  value={draft.factor}
                  onChange={(e) =>
                    setDraft((d) => ({ ...d, factor: e.target.value }))
                  }
                  data-testid="transform-factor-input"
                  className="px-2 py-1 text-xs bg-bg-secondary border border-border rounded text-text-primary font-mono w-20"
                />
              </label>
              <label className="flex flex-col gap-1 text-[10px] text-text-secondary">
                Offset
                <input
                  type="number"
                  value={draft.offset}
                  onChange={(e) =>
                    setDraft((d) => ({ ...d, offset: e.target.value }))
                  }
                  data-testid="transform-offset-input"
                  className="px-2 py-1 text-xs bg-bg-secondary border border-border rounded text-text-primary font-mono w-20"
                />
              </label>
            </>
          )}

          <button
            type="button"
            onClick={handleAddStep}
            data-testid="transform-add-step"
            className="px-3 py-1 text-xs border border-border rounded text-text-secondary hover:text-text-primary"
          >
            + step
          </button>
        </div>

        {/* Committed chain */}
        {chain.length > 0 && (
          <ol
            className="flex flex-wrap items-center gap-2 text-[11px]"
            data-testid="transform-chain"
          >
            {chain.map((spec, idx) => (
              <li
                key={idx}
                className="flex items-center gap-1 border border-border rounded px-2 py-0.5 font-mono text-text-secondary"
                data-testid={`transform-chain-step-${idx}`}
              >
                <span className="text-text-muted">{idx + 1}.</span>
                {describeStep(spec)}
                <button
                  type="button"
                  onClick={() => handleRemoveStep(idx)}
                  className="text-text-muted hover:text-accent-error"
                  aria-label={`Remove step ${idx + 1}`}
                  data-testid={`transform-remove-step-${idx}`}
                >
                  ×
                </button>
              </li>
            ))}
          </ol>
        )}

        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => mutation.mutate()}
            disabled={!canRun}
            data-testid="transform-run"
            className="bg-accent-cyan text-bg-primary px-3 py-1 rounded text-xs font-medium hover:bg-accent-cyan/80 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {mutation.isPending ? 'Running…' : 'Run transform'}
          </button>
          {chain.length > 0 && (
            <button
              type="button"
              onClick={handleClearChain}
              data-testid="transform-clear"
              className="px-3 py-1 text-xs border border-border rounded text-text-secondary hover:text-text-primary"
            >
              Clear
            </button>
          )}
        </div>

        {error && (
          <div
            className="text-[11px] text-accent-error"
            data-testid="transform-error"
          >
            {error}
          </div>
        )}

        {result && (
          <div data-testid="transform-result" className="text-[11px]">
            <div className="text-text-secondary mb-1">
              Result:{' '}
              <span
                className="font-mono text-text-primary"
                data-testid="transform-result-count"
              >
                {result.length}
              </span>{' '}
              points
            </div>
            <table className="font-mono text-text-muted">
              <tbody>
                {resultPreview.map((p, i) => (
                  <tr key={i} data-testid={`transform-result-row-${i}`}>
                    <td className="pr-3">{p.time}</td>
                    <td>{String(p.value)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}
