import { useMemo } from 'react';
import type { ObjectType, WireObject } from '../../api/types';
import { baseTypeOf } from '../../lib/geoParser';

const WIDTH = 1000;
const ROW_HEIGHT = 28;
const HEADER_HEIGHT = 36;
const LABEL_WIDTH = 220;
const BAR_PAD_Y = 6;
const BAR_COLOR = '#22d3ee';
const TICK_COLOR = '#64748b';

const TEMPORAL_TYPES = new Set(['date', 'datetime', 'timestamp']);

// Candidate (start, end) field name pairs, in priority order. Falls back to
// the first two declaration-order temporal properties if no pair matches.
const PAIR_CANDIDATES: Array<[string, string]> = [
  ['startDate', 'endDate'],
  ['start_date', 'end_date'],
  ['startTime', 'endTime'],
  ['start_time', 'end_time'],
  ['start', 'end'],
  ['from', 'to'],
  ['fromDate', 'toDate'],
  ['from_date', 'to_date'],
  ['beginDate', 'endDate'],
  ['begin', 'end'],
];

interface GanttChartProps {
  objectType: ObjectType;
  data: WireObject[];
  onRowClick?: (row: WireObject) => void;
}

interface TemporalPair {
  startField: string;
  endField: string;
}

interface GanttRow {
  row: WireObject;
  label: string;
  startMs: number;
  endMs: number;
}

interface AxisTick {
  ms: number;
  label: string;
}

// Detect the first temporal-typed property pair that maps onto start/end.
// Returns null when the object type has no recognisable pair.
function detectTemporalPair(objectType: ObjectType): TemporalPair | null {
  const props = objectType.properties ?? {};
  const temporalNames: string[] = [];
  for (const [name, prop] of Object.entries(props)) {
    if (TEMPORAL_TYPES.has(baseTypeOf(prop.dataType))) temporalNames.push(name);
  }
  if (temporalNames.length === 0) return null;
  const set = new Set(temporalNames);
  for (const [s, e] of PAIR_CANDIDATES) {
    if (set.has(s) && set.has(e)) return { startField: s, endField: e };
  }
  if (temporalNames.length >= 2) {
    return { startField: temporalNames[0], endField: temporalNames[1] };
  }
  return null;
}

// Permissive temporal coercion: accepts ISO strings, RFC3339 timestamps,
// and YYYY-MM-DD shapes. Returns NaN on unparseable input so callers can
// filter rows uniformly.
function parseTemporal(value: unknown): number {
  if (value === null || value === undefined) return NaN;
  if (typeof value === 'number' && Number.isFinite(value)) {
    // Heuristic: treat sub-1e12 numbers as seconds, else milliseconds.
    return value < 1e12 ? value * 1000 : value;
  }
  if (typeof value !== 'string') return NaN;
  const trimmed = value.trim();
  if (!trimmed) return NaN;
  const ms = Date.parse(trimmed);
  return Number.isNaN(ms) ? NaN : ms;
}

function rowLabel(row: WireObject, objectType: ObjectType): string {
  const titleProp = objectType.titleProperty ?? objectType.primaryKey;
  const val = row[titleProp];
  if (val === null || val === undefined || val === '') {
    return String(row.__primaryKey ?? '');
  }
  return String(val);
}

const DAY_MS = 24 * 60 * 60 * 1000;
const MONTH_MS = 30 * DAY_MS;
const YEAR_MS = 365 * DAY_MS;

// Pick a tick spacing (in ms) that keeps the axis readable for the visible
// span. Returns ~5–8 ticks across the chart width regardless of duration.
function pickTickStep(spanMs: number): number {
  const targetTicks = 6;
  const raw = spanMs / targetTicks;
  const candidates = [
    60 * 1000,
    5 * 60 * 1000,
    15 * 60 * 1000,
    60 * 60 * 1000,
    6 * 60 * 60 * 1000,
    DAY_MS,
    7 * DAY_MS,
    MONTH_MS,
    3 * MONTH_MS,
    6 * MONTH_MS,
    YEAR_MS,
    5 * YEAR_MS,
  ];
  for (const c of candidates) {
    if (raw <= c) return c;
  }
  return YEAR_MS * 10;
}

function formatTick(ms: number, step: number): string {
  const d = new Date(ms);
  if (step >= YEAR_MS) return `${d.getUTCFullYear()}`;
  if (step >= MONTH_MS) {
    return `${d.getUTCFullYear()}-${String(d.getUTCMonth() + 1).padStart(2, '0')}`;
  }
  if (step >= DAY_MS) {
    return `${d.getUTCFullYear()}-${String(d.getUTCMonth() + 1).padStart(2, '0')}-${String(d.getUTCDate()).padStart(2, '0')}`;
  }
  const h = String(d.getUTCHours()).padStart(2, '0');
  const m = String(d.getUTCMinutes()).padStart(2, '0');
  return `${h}:${m}`;
}

function buildAxisTicks(minMs: number, maxMs: number): AxisTick[] {
  if (!(maxMs > minMs)) {
    return [{ ms: minMs, label: formatTick(minMs, DAY_MS) }];
  }
  const step = pickTickStep(maxMs - minMs);
  const start = Math.ceil(minMs / step) * step;
  const ticks: AxisTick[] = [];
  for (let t = start; t <= maxMs; t += step) {
    ticks.push({ ms: t, label: formatTick(t, step) });
    if (ticks.length > 24) break;
  }
  if (ticks.length === 0) {
    ticks.push({ ms: minMs, label: formatTick(minMs, step) });
  }
  return ticks;
}

function truncate(label: string, max: number): string {
  if (label.length <= max) return label;
  return label.slice(0, max - 1) + '…';
}

export function GanttChart({ objectType, data, onRowClick }: GanttChartProps) {
  const pair = useMemo(() => detectTemporalPair(objectType), [objectType]);

  const rows = useMemo<GanttRow[]>(() => {
    if (!pair) return [];
    const acc: GanttRow[] = [];
    for (const row of data) {
      const startMs = parseTemporal(row[pair.startField]);
      const endMs = parseTemporal(row[pair.endField]);
      if (Number.isNaN(startMs) || Number.isNaN(endMs)) continue;
      // Accept zero-duration (point-in-time) and reverse pairs (treat the
      // smaller value as the start so a typo in the wire data still renders).
      const lo = Math.min(startMs, endMs);
      const hi = Math.max(startMs, endMs);
      acc.push({
        row,
        label: rowLabel(row, objectType),
        startMs: lo,
        endMs: hi,
      });
    }
    return acc;
  }, [pair, data, objectType]);

  if (!pair) {
    return (
      <div
        data-testid="gantt-empty-no-pair"
        className="flex flex-col items-center justify-center py-12 text-center border border-border rounded"
      >
        <p className="text-sm font-sans text-text-primary">
          No temporal property pair
        </p>
        <p className="text-xs font-mono text-text-secondary mt-1">
          Gantt view needs two date / datetime / timestamp properties (e.g.
          startDate + endDate).
        </p>
      </div>
    );
  }

  if (rows.length === 0) {
    return (
      <div
        data-testid="gantt-empty-no-data"
        className="flex flex-col items-center justify-center py-12 text-center border border-border rounded"
      >
        <p className="text-sm font-sans text-text-primary">
          No temporal data in current results
        </p>
        <p className="text-xs font-mono text-text-secondary mt-1">
          Reading {pair.startField} / {pair.endField}; rows with missing or
          unparseable values are skipped.
        </p>
      </div>
    );
  }

  const minMs = rows.reduce((acc, r) => Math.min(acc, r.startMs), rows[0].startMs);
  const maxMsRaw = rows.reduce((acc, r) => Math.max(acc, r.endMs), rows[0].endMs);
  // Guard against a degenerate single-instant range: pad by one day so bars
  // remain visible instead of collapsing to zero width.
  const maxMs = maxMsRaw === minMs ? minMs + DAY_MS : maxMsRaw;
  const span = maxMs - minMs;
  const innerWidth = WIDTH - LABEL_WIDTH;
  const height = HEADER_HEIGHT + rows.length * ROW_HEIGHT + 8;
  const barHeight = ROW_HEIGHT - BAR_PAD_Y * 2;

  const ticks = buildAxisTicks(minMs, maxMs);

  const xFor = (ms: number): number => {
    const ratio = (ms - minMs) / span;
    return LABEL_WIDTH + Math.max(0, Math.min(1, ratio)) * innerWidth;
  };

  return (
    <div
      data-testid="gantt-chart"
      data-start-field={pair.startField}
      data-end-field={pair.endField}
      className="border border-border rounded overflow-hidden bg-bg-secondary"
    >
      <div className="flex items-center justify-between px-3 py-2 border-b border-border">
        <span className="text-xs font-mono text-text-secondary">
          Gantt · {pair.startField} → {pair.endField} · {rows.length}{' '}
          {rows.length === 1 ? 'row' : 'rows'}
        </span>
      </div>
      <div className="overflow-x-auto">
        <svg
          width={WIDTH}
          height={height}
          viewBox={`0 0 ${WIDTH} ${height}`}
          role="img"
          aria-label="Gantt chart"
        >
          {/* Tick grid */}
          {ticks.map((t, i) => {
            const x = xFor(t.ms);
            return (
              <g key={`tick-${i}`} data-testid="gantt-tick">
                <line
                  x1={x}
                  y1={HEADER_HEIGHT}
                  x2={x}
                  y2={height}
                  stroke={TICK_COLOR}
                  strokeOpacity={0.2}
                  strokeDasharray="2 4"
                />
                <text
                  x={x}
                  y={HEADER_HEIGHT - 8}
                  fill={TICK_COLOR}
                  fontSize={10}
                  fontFamily="monospace"
                  textAnchor="middle"
                >
                  {t.label}
                </text>
              </g>
            );
          })}

          {/* Header separator */}
          <line
            x1={0}
            y1={HEADER_HEIGHT}
            x2={WIDTH}
            y2={HEADER_HEIGHT}
            stroke={TICK_COLOR}
            strokeOpacity={0.4}
          />
          <line
            x1={LABEL_WIDTH}
            y1={0}
            x2={LABEL_WIDTH}
            y2={height}
            stroke={TICK_COLOR}
            strokeOpacity={0.4}
          />

          {/* Bars + labels */}
          {rows.map((r, i) => {
            const y = HEADER_HEIGHT + i * ROW_HEIGHT + BAR_PAD_Y;
            const x = xFor(r.startMs);
            const x2 = xFor(r.endMs);
            // Minimum visible bar width so zero-duration rows are still
            // clickable / discoverable on the chart surface.
            const w = Math.max(2, x2 - x);
            const pk = String(r.row.__primaryKey ?? '');
            const labelY = y + barHeight / 2 + 3;
            const onClick = onRowClick
              ? () => onRowClick(r.row)
              : undefined;
            const interactive = !!onRowClick;
            return (
              <g
                key={`row-${pk}-${i}`}
                data-testid="gantt-row"
                data-pk={pk}
                role={interactive ? 'button' : undefined}
                tabIndex={interactive ? 0 : undefined}
                onClick={onClick}
                onKeyDown={
                  interactive
                    ? (e) => {
                        if (e.key === 'Enter' || e.key === ' ') {
                          e.preventDefault();
                          onClick?.();
                        }
                      }
                    : undefined
                }
                style={{ cursor: interactive ? 'pointer' : 'default' }}
              >
                <text
                  x={LABEL_WIDTH - 8}
                  y={labelY}
                  fill="#cbd5e1"
                  fontSize={11}
                  fontFamily="sans-serif"
                  textAnchor="end"
                >
                  {truncate(r.label, 32)}
                </text>
                <rect
                  data-testid="gantt-bar"
                  x={x}
                  y={y}
                  width={w}
                  height={barHeight}
                  rx={3}
                  ry={3}
                  fill={BAR_COLOR}
                  fillOpacity={0.7}
                  stroke={BAR_COLOR}
                  strokeWidth={1}
                >
                  <title>{`${r.label} · ${new Date(r.startMs)
                    .toISOString()
                    .slice(0, 10)} → ${new Date(r.endMs)
                    .toISOString()
                    .slice(0, 10)}`}</title>
                </rect>
              </g>
            );
          })}
        </svg>
      </div>
    </div>
  );
}
