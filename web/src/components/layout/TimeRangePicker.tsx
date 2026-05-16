import { useCallback, useMemo } from 'react';
import { useLocation, useSearchParams } from 'react-router';

// US-482: TopBar-mounted time-range picker for Quiver pages. The picker
// is bidirectionally bound to the URL query params (?from=ISO&to=ISO&step=Xm)
// so:
//   - URL → picker: opening a deep link with the params highlights the
//     matching preset (or "custom" if no preset matches),
//   - picker → URL: clicking a preset writes the canonical params back,
// matching the contract the backend /api/v2/quiver/dashboards/{rid}/data
// endpoint expects on the wire.

export interface TimeRangePreset {
  // Symbolic label shown on the button and persisted in `?range=`.
  id: '1h' | '24h' | '7d' | '30d';
  // Window length used to compute `from` = now - duration.
  durationMs: number;
  // Bucket width handed to the backend as `step`.
  step: string;
}

export const TIME_RANGE_PRESETS: readonly TimeRangePreset[] = [
  { id: '1h', durationMs: 60 * 60 * 1000, step: '1m' },
  { id: '24h', durationMs: 24 * 60 * 60 * 1000, step: '5m' },
  { id: '7d', durationMs: 7 * 24 * 60 * 60 * 1000, step: '30m' },
  { id: '30d', durationMs: 30 * 24 * 60 * 60 * 1000, step: '2h' },
];

const DEFAULT_PRESET = TIME_RANGE_PRESETS[1]; // 24h matches the PRD acceptance.

// readActivePresetID returns the preset whose (from, to, step) the URL
// currently carries. Falls back to the default preset when nothing is
// set so a fresh load shows a sensible highlight without the URL
// needing pre-population.
function readActivePresetID(
  params: URLSearchParams,
  presets: readonly TimeRangePreset[],
): TimeRangePreset['id'] | null {
  const range = params.get('range');
  if (range) {
    const p = presets.find((p) => p.id === range);
    if (p) return p.id;
  }
  const step = params.get('step');
  const from = params.get('from');
  const to = params.get('to');
  if (!step || !from || !to) return null;
  const p = presets.find((p) => p.step === step);
  return p ? p.id : null;
}

export interface TimeRangePickerProps {
  // Test seam: in tests we inject a deterministic "now" so the URL
  // params don't drift with wall-clock time.
  now?: () => number;
  // Optional override of the route-prefix filter. Default: only render
  // on /quiver/* so the picker doesn't clutter unrelated pages.
  pathnamePrefix?: string;
}

export function TimeRangePicker({
  now = () => Date.now(),
  pathnamePrefix = '/quiver',
}: TimeRangePickerProps) {
  const location = useLocation();
  const [searchParams, setSearchParams] = useSearchParams();

  const onQuiver = location.pathname === pathnamePrefix ||
    location.pathname.startsWith(pathnamePrefix + '/');

  const activeID = useMemo<TimeRangePreset['id'] | null>(
    () => readActivePresetID(searchParams, TIME_RANGE_PRESETS),
    [searchParams],
  );
  const effectiveID: TimeRangePreset['id'] = activeID ?? DEFAULT_PRESET.id;

  const setRange = useCallback(
    (preset: TimeRangePreset) => {
      const nowMs = now();
      const to = new Date(nowMs).toISOString();
      const from = new Date(nowMs - preset.durationMs).toISOString();
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          next.set('range', preset.id);
          next.set('from', from);
          next.set('to', to);
          next.set('step', preset.step);
          return next;
        },
        { replace: false },
      );
    },
    [now, setSearchParams],
  );

  if (!onQuiver) return null;

  return (
    <div
      data-testid="time-range-picker"
      className="flex items-center gap-0.5 mr-2 rounded border border-border bg-bg-tertiary p-0.5"
    >
      {TIME_RANGE_PRESETS.map((preset) => {
        const active = preset.id === effectiveID;
        return (
          <button
            key={preset.id}
            type="button"
            onClick={() => setRange(preset)}
            data-testid={`time-range-${preset.id}`}
            aria-pressed={active}
            className={
              'px-2 py-1 text-xs font-mono rounded transition-colors ' +
              (active
                ? 'bg-accent-cyan text-bg-primary'
                : 'text-text-secondary hover:text-text-primary hover:bg-white/5')
            }
          >
            {preset.id}
          </button>
        );
      })}
    </div>
  );
}
