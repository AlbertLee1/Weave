import { useCallback, useMemo, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useDatasetHistory } from '../../hooks/useDatasetHistory';
import { useTimeTravelStore } from '../../stores/timeTravelStore';
import type { DatasetTransaction } from '../../api/datasets';

interface TimeTravelToolbarProps {
  ontologyApiName: string;
}

// formatCommittedAt renders the committedAt instant as a short
// human-readable string for the picker dropdown. Keeps ISO precision in
// the value attribute (we round-trip the raw tx-... id) so the visual
// format can drift without affecting the wire shape.
function formatCommittedAt(iso: string): string {
  if (!iso) return '';
  const parsed = new Date(iso);
  if (Number.isNaN(parsed.getTime())) return iso;
  const yyyy = parsed.getUTCFullYear();
  const mm = String(parsed.getUTCMonth() + 1).padStart(2, '0');
  const dd = String(parsed.getUTCDate()).padStart(2, '0');
  const hh = String(parsed.getUTCHours()).padStart(2, '0');
  const mi = String(parsed.getUTCMinutes()).padStart(2, '0');
  return `${yyyy}-${mm}-${dd} ${hh}:${mi}Z`;
}

// shortTxId trims `tx-<uuid>` to `tx-<first8>` for the picker option label.
// The full id stays in the option's `value` so onChange round-trips losslessly.
function shortTxId(txId: string): string {
  if (txId.length <= 11) return txId;
  return `${txId.slice(0, 11)}…`;
}

// TimeTravelToolbar is the Browser-page topbar control for US-048. It
// holds two pieces of state: (a) an "enabled" toggle the user clicks to
// switch the page into historical mode, (b) the active asOf value the
// API client injects as ?asOf= on every ontology-scoped request (via
// withActiveAsOf in api/client.ts). When the dataset history endpoint
// is unavailable or the chain is empty, the picker shows an explanatory
// message and disables the toggle so the page never enters a "broken
// historical view".
export function TimeTravelToolbar({ ontologyApiName }: TimeTravelToolbarProps) {
  const setAsOf = useTimeTravelStore((s) => s.setAsOf);
  const clearAsOf = useTimeTravelStore((s) => s.clearAsOf);
  const persisted = useTimeTravelStore(
    (s) => s.selections[ontologyApiName] ?? '',
  );
  const queryClient = useQueryClient();

  const { data, isLoading, error } = useDatasetHistory(ontologyApiName);
  const transactions = useMemo<DatasetTransaction[]>(
    () => data?.transactions ?? [],
    [data],
  );

  // Draft state: user picks a tx from the dropdown before flipping the
  // toggle. Defaults to the most-recent tx so a "blank-then-toggle" UX
  // does not silently leave the request without an asOf.
  const [draftTxId, setDraftTxId] = useState<string>('');
  const effectiveDraft = draftTxId || transactions[0]?.txId || '';

  const enabled = persisted.length > 0;

  // applyAsOf wraps store mutations with a queryClient invalidate so
  // every React Query subscription re-runs through the API client and
  // picks up the new `?asOf=` (or its absence). Same pattern the branch
  // picker uses for `?branch=` — without it, the queryKey stays
  // identical and React Query never refetches.
  const applyAsOf = useCallback(
    (next: string) => {
      if (next.length === 0) {
        clearAsOf(ontologyApiName);
      } else {
        setAsOf(ontologyApiName, next);
      }
      queryClient.invalidateQueries();
    },
    [clearAsOf, setAsOf, ontologyApiName, queryClient],
  );

  const handleToggle = () => {
    if (enabled) {
      applyAsOf('');
      return;
    }
    const target = effectiveDraft;
    if (!target) return;
    applyAsOf(target);
  };

  const handleSelect = (value: string) => {
    setDraftTxId(value);
    // If the user is already in historical mode, switching the picker
    // immediately moves the view — saves an extra "toggle off then on"
    // click and matches the mental model of "this is the active tx".
    if (enabled) applyAsOf(value);
  };

  const hint = useMemo(() => {
    if (error) return 'Failed to load transactions';
    if (isLoading) return 'Loading transactions…';
    if (transactions.length === 0)
      return 'No dataset transactions recorded yet';
    return null;
  }, [error, isLoading, transactions]);

  const activeTx = useMemo(
    () => transactions.find((t) => t.txId === persisted),
    [transactions, persisted],
  );

  return (
    <div
      data-testid="time-travel-toolbar"
      data-time-travel-enabled={enabled ? 'true' : 'false'}
      data-time-travel-asof={persisted}
      className="flex items-center gap-3 px-3 py-2 rounded border border-border bg-bg-secondary"
    >
      <label
        className="flex items-center gap-2 cursor-pointer select-none"
        data-testid="time-travel-toggle-label"
      >
        <input
          type="checkbox"
          aria-label="Time Travel"
          data-testid="time-travel-toggle"
          checked={enabled}
          disabled={transactions.length === 0}
          onChange={handleToggle}
          className="sr-only peer"
        />
        <span
          aria-hidden="true"
          className={[
            'relative w-8 h-4 rounded-full transition-colors pointer-events-none',
            enabled
              ? 'bg-accent-amber'
              : 'bg-border-secondary peer-disabled:opacity-40',
            "after:content-[''] after:absolute after:top-0.5 after:left-0.5",
            'after:w-3 after:h-3 after:rounded-full after:bg-white',
            'after:transition-transform',
            enabled ? 'after:translate-x-4' : '',
          ].join(' ')}
        />
        <span className="text-xs font-mono text-text-secondary">
          Time Travel
        </span>
      </label>

      <select
        data-testid="time-travel-picker"
        aria-label="Dataset transaction"
        className="flex-1 max-w-md text-xs font-mono bg-bg-primary text-text-primary border border-border rounded px-2 py-1"
        value={enabled ? persisted : draftTxId}
        onChange={(e) => handleSelect(e.target.value)}
        disabled={transactions.length === 0}
      >
        <option value="">
          {transactions.length === 0
            ? '— no transactions —'
            : 'Latest (live)'}
        </option>
        {transactions.map((tx) => (
          <option key={tx.txId} value={tx.txId} data-testid="time-travel-option">
            {shortTxId(tx.txId)} · {formatCommittedAt(tx.committedAt)} ·{' '}
            {tx.editsCount} edit{tx.editsCount === 1 ? '' : 's'}
          </option>
        ))}
      </select>

      {enabled && (
        <span
          data-testid="time-travel-active-badge"
          data-time-travel-tx-id={persisted}
          className="text-[10px] font-mono uppercase tracking-wider text-accent-amber"
        >
          Historical
          {activeTx ? ` · ${formatCommittedAt(activeTx.committedAt)}` : ''}
        </span>
      )}

      {hint && (
        <span
          data-testid="time-travel-hint"
          className="text-[11px] font-mono text-text-secondary"
        >
          {hint}
        </span>
      )}
    </div>
  );
}

// Hook helpers (useTimeTravelActive / useTimeTravelAsOf) live in
// `./useTimeTravel.ts` so this module only exports the toolbar
// component — keeps `react-refresh/only-export-components` happy.
