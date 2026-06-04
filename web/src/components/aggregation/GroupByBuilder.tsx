import type { GroupByClause, HavingClause } from '../../api/types';

// SubtotalMode selects the Palantir cube/rollup subtotal expansion the backend
// applies to the declared groupBys:
//   - 'none'   — one row per concrete groupBy combination (default).
//   - 'cube'   — every 2^N subset of the groupBys (+ grand total).
//   - 'rollup' — the hierarchical chain [gb0..N] … [gb0] [] (+ grand total).
// cube and rollup are mutually exclusive on the wire (AggregationRequest), so a
// single-select control is the right shape. Only meaningful with ≥1 groupBy.
export type SubtotalMode = 'none' | 'cube' | 'rollup';

const subtotalModes: { value: SubtotalMode; label: string }[] = [
  { value: 'none', label: 'No subtotals' },
  { value: 'cube', label: 'Cube (all subsets)' },
  { value: 'rollup', label: 'Rollup (hierarchy)' },
];

interface GroupByBuilderProps {
  groupBy: GroupByClause[];
  onChange: (groupBy: GroupByClause[]) => void;
  availableFields: string[];
  // Subtotal expansion mode. The control is disabled until at least one groupBy
  // clause exists, since cube/rollup are no-ops without grouping dimensions.
  // Optional so existing callers that don't surface cube/rollup (e.g. the
  // ObjectSet groupBy builder) keep working; when omitted the control hides.
  subtotalMode?: SubtotalMode;
  onSubtotalModeChange?: (mode: SubtotalMode) => void;
}

// HavingDraft is the editor-side row for a post-aggregation HavingClause.
// `value` is optional while the user is typing — a blank value marks an
// incomplete clause the page drops before it sends `having` to the backend.
// `op` mirrors the backend's closed operator set (pkg/oss/aggregation/having.go).
export interface HavingDraft {
  metric: string;
  op: HavingClause['op'];
  value?: number;
}

// havingOps is the closed comparison-operator set the backend accepts
// (pkg/oss/aggregation/having.go havingOps). Labels keep the dropdown readable
// while the wire value stays the bare op token.
const havingOps: { value: HavingClause['op']; label: string }[] = [
  { value: 'eq', label: '= (eq)' },
  { value: 'ne', label: '≠ (ne)' },
  { value: 'gt', label: '> (gt)' },
  { value: 'gte', label: '≥ (gte)' },
  { value: 'lt', label: '< (lt)' },
  { value: 'lte', label: '≤ (lte)' },
];

interface HavingBuilderProps {
  having: HavingDraft[];
  onChange: (having: HavingDraft[]) => void;
  // Metric names already aliased in the request, offered as datalist hints so
  // a having clause can name the exact metric output it filters on.
  metricNames: string[];
}

// toHavingClauses converts editor drafts into the wire `having` array, dropping
// rows that are still incomplete (no metric name or no numeric value). Returns
// undefined when nothing valid remains so the request omits `having` entirely.
export function toHavingClauses(drafts: HavingDraft[]): HavingClause[] | undefined {
  const clauses = drafts
    .filter((d) => d.metric.trim() !== '' && typeof d.value === 'number' && Number.isFinite(d.value))
    .map((d) => ({ metric: d.metric.trim(), op: d.op, value: d.value as number }));
  return clauses.length > 0 ? clauses : undefined;
}

type RangeRow = NonNullable<GroupByClause['ranges']>[number];

const groupByTypes: GroupByClause['type'][] = [
  'exact',
  'fixedWidth',
  'ranges',
  'duration',
  'topValues',
  'geohash',
];

// ISO 8601 period options for the duration groupBy (wire key `duration`).
// Full set the backend `parseDuration` accepts — P1D/P1W/P1M/P3M/P1Y/PT1H
// (engine.go:1202; P3M quarter + PT1H hour added in commit 2baa122).
const durationPeriods = ['P1D', 'P1W', 'P1M', 'P3M', 'P1Y', 'PT1H'];

// Types that accept an optional maxGroupCount (wire key `maxGroupCount`).
const maxGroupCountTypes: GroupByClause['type'][] = ['exact', 'topValues'];

export function GroupByBuilder({
  groupBy,
  onChange,
  availableFields,
  subtotalMode = 'none',
  onSubtotalModeChange,
}: GroupByBuilderProps) {
  const subtotalDisabled = groupBy.length === 0;
  function addGroupBy() {
    onChange([...groupBy, { field: availableFields[0] ?? '', type: 'exact' }]);
  }

  function removeGroupBy(index: number) {
    onChange(groupBy.filter((_, i) => i !== index));
  }

  function updateGroupBy(index: number, updates: Partial<GroupByClause>) {
    onChange(
      groupBy.map((g, i) => (i === index ? { ...g, ...updates } : g)),
    );
  }

  // Switching type strips fields that don't belong to the new type so the
  // request body never carries stale keys (e.g. a leftover `ranges` array
  // after switching to `exact`).
  function changeType(index: number, type: GroupByClause['type']) {
    const next: GroupByClause = { field: groupBy[index].field, type };
    if (type === 'duration') next.duration = durationPeriods[0];
    if (type === 'geohash') next.precision = groupBy[index].precision;
    if (maxGroupCountTypes.includes(type)) next.maxGroupCount = groupBy[index].maxGroupCount;
    if (type === 'ranges') next.ranges = groupBy[index].ranges ?? [];
    onChange(groupBy.map((g, i) => (i === index ? next : g)));
  }

  function updateRange(index: number, rangeIndex: number, updates: Partial<RangeRow>) {
    const rows = groupBy[index].ranges ?? [];
    const nextRows = rows.map((r, i) => (i === rangeIndex ? { ...r, ...updates } : r));
    updateGroupBy(index, { ranges: nextRows });
  }

  function addRange(index: number) {
    const rows = groupBy[index].ranges ?? [];
    updateGroupBy(index, { ranges: [...rows, {}] });
  }

  function removeRange(index: number, rangeIndex: number) {
    const rows = groupBy[index].ranges ?? [];
    updateGroupBy(index, { ranges: rows.filter((_, i) => i !== rangeIndex) });
  }

  const inputClass =
    'bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none';
  const labelClass = 'text-xs text-text-secondary font-sans mb-1';

  return (
    <div data-testid="groupby-section" className="flex flex-col gap-3">
      <div className="flex items-center justify-between gap-2">
        <label className={labelClass}>Group By</label>
        <div className="flex items-center gap-2">
          {onSubtotalModeChange && (
            <select
              value={subtotalMode}
              onChange={(e) => onSubtotalModeChange(e.target.value as SubtotalMode)}
              disabled={subtotalDisabled}
              data-testid="aggregation-subtotal-mode"
              aria-label="Subtotal mode"
              title={
                subtotalDisabled
                  ? 'Add at least one group by to enable cube/rollup subtotals'
                  : 'Cube computes every subset of the group by dimensions; rollup computes the hierarchical chain'
              }
              className={`${inputClass} flex-shrink-0 disabled:opacity-50`}
            >
              {subtotalModes.map((m) => (
                <option key={m.value} value={m.value}>
                  {m.label}
                </option>
              ))}
            </select>
          )}
          <button
            type="button"
            onClick={addGroupBy}
            data-testid="groupby-add"
            className="bg-bg-tertiary border border-border text-text-primary px-4 py-2 rounded text-sm hover:bg-bg-elevated"
          >
            + Add Group By
          </button>
        </div>
      </div>

      {groupBy.length === 0 && (
        <div className="text-xs text-text-secondary">No group by clauses. Results will be a single aggregate.</div>
      )}

      {groupBy.map((clause, index) => {
        const ranges = clause.ranges ?? [];
        return (
          <div
            key={index}
            data-testid={`groupby-row-${index}`}
            data-groupby-type={clause.type}
            className="flex flex-col gap-2"
          >
            <div className="flex items-center gap-2">
              <select
                value={clause.field}
                onChange={(e) => updateGroupBy(index, { field: e.target.value })}
                data-testid={`groupby-${index}-field`}
                className={`${inputClass} flex-1`}
              >
                <option value="">Select field...</option>
                {availableFields.map((f) => (
                  <option key={f} value={f}>
                    {f}
                  </option>
                ))}
              </select>

              <select
                value={clause.type}
                onChange={(e) => changeType(index, e.target.value as GroupByClause['type'])}
                data-testid={`groupby-${index}-type`}
                className={`${inputClass} flex-shrink-0`}
              >
                {groupByTypes.map((t) => (
                  <option key={t} value={t}>
                    {t}
                  </option>
                ))}
              </select>

              {clause.type === 'duration' && (
                <select
                  value={clause.duration ?? durationPeriods[0]}
                  onChange={(e) => updateGroupBy(index, { duration: e.target.value })}
                  data-testid={`groupby-${index}-duration`}
                  aria-label="duration period"
                  className={`${inputClass} flex-shrink-0`}
                >
                  {durationPeriods.map((p) => (
                    <option key={p} value={p}>
                      {p}
                    </option>
                  ))}
                </select>
              )}

              {maxGroupCountTypes.includes(clause.type) && (
                <input
                  type="number"
                  min={1}
                  value={clause.maxGroupCount ?? ''}
                  onChange={(e) =>
                    updateGroupBy(index, {
                      maxGroupCount: e.target.value === '' ? undefined : Number(e.target.value),
                    })
                  }
                  placeholder="maxGroups"
                  aria-label="maxGroupCount"
                  title="max group count"
                  data-testid={`groupby-${index}-maxGroupCount`}
                  className={`${inputClass} w-24`}
                />
              )}

              {clause.type === 'geohash' && (
                <input
                  type="number"
                  min={1}
                  max={12}
                  value={clause.precision ?? ''}
                  onChange={(e) =>
                    updateGroupBy(index, {
                      precision: e.target.value === '' ? undefined : Number(e.target.value),
                    })
                  }
                  placeholder="precision"
                  aria-label="geohash precision"
                  title="geohash precision (1-12)"
                  data-testid={`groupby-${index}-geohashPrecision`}
                  className={`${inputClass} w-24`}
                />
              )}

              {clause.type === 'fixedWidth' && (
                <input
                  type="number"
                  min="0"
                  step="any"
                  value={clause.fixedWidth ?? ''}
                  onChange={(e) => {
                    const raw = e.target.value;
                    const n = Number(raw);
                    updateGroupBy(index, {
                      fixedWidth:
                        raw.trim() !== '' && Number.isFinite(n) ? n : undefined,
                    });
                  }}
                  placeholder="width"
                  aria-label="Bucket width"
                  title="Numeric bucket width (required for fixedWidth grouping)"
                  data-testid={`groupby-${index}-fixedWidth`}
                  className={`${inputClass} w-24`}
                />
              )}

              <button
                type="button"
                onClick={() => removeGroupBy(index)}
                data-testid={`groupby-${index}-remove`}
                className="bg-accent-error/15 text-accent-error border border-accent-error/30 px-4 py-2 rounded text-sm hover:bg-accent-error/25"
              >
                Remove
              </button>
            </div>

            {clause.type === 'ranges' && (
              <div
                data-testid={`groupby-${index}-ranges-editor`}
                className="flex flex-col gap-2 border border-border rounded p-2 bg-bg-tertiary/40"
              >
                <div className="flex items-center justify-between">
                  <span className="text-xs text-text-secondary font-sans">Ranges</span>
                  <button
                    type="button"
                    onClick={() => addRange(index)}
                    data-testid={`groupby-${index}-range-add`}
                    className="bg-bg-tertiary border border-border text-text-primary px-3 py-1 rounded text-xs hover:bg-bg-elevated"
                  >
                    + Add Range
                  </button>
                </div>

                {ranges.length === 0 && (
                  <div
                    data-testid={`groupby-${index}-ranges-empty`}
                    className="text-xs text-accent-error"
                  >
                    Add at least one range, or this group by produces no buckets.
                  </div>
                )}

                {ranges.map((row, rangeIndex) => (
                  <div
                    key={rangeIndex}
                    data-testid={`groupby-${index}-range-${rangeIndex}`}
                    className="flex items-center gap-2"
                  >
                    <input
                      type="text"
                      value={row.name ?? ''}
                      onChange={(e) =>
                        updateRange(index, rangeIndex, { name: e.target.value || undefined })
                      }
                      placeholder="name"
                      aria-label="range name"
                      data-testid={`groupby-${index}-range-${rangeIndex}-name`}
                      className={`${inputClass} flex-1`}
                    />
                    <input
                      type="number"
                      value={row.startValue ?? ''}
                      onChange={(e) =>
                        updateRange(index, rangeIndex, {
                          startValue: e.target.value === '' ? undefined : Number(e.target.value),
                        })
                      }
                      placeholder="start"
                      aria-label="range start"
                      data-testid={`groupby-${index}-range-${rangeIndex}-start`}
                      className={`${inputClass} w-24`}
                    />
                    <input
                      type="number"
                      value={row.endValue ?? ''}
                      onChange={(e) =>
                        updateRange(index, rangeIndex, {
                          endValue: e.target.value === '' ? undefined : Number(e.target.value),
                        })
                      }
                      placeholder="end"
                      aria-label="range end"
                      data-testid={`groupby-${index}-range-${rangeIndex}-end`}
                      className={`${inputClass} w-24`}
                    />
                    <button
                      type="button"
                      onClick={() => removeRange(index, rangeIndex)}
                      data-testid={`groupby-${index}-range-${rangeIndex}-remove`}
                      className="bg-accent-error/15 text-accent-error border border-accent-error/30 px-3 py-1 rounded text-xs hover:bg-accent-error/25"
                    >
                      ✕
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

// HavingBuilder edits post-aggregation row filters (SQL HAVING). Each row names
// a metric (the metric's output/alias name), a comparison op, and a numeric
// threshold; the backend drops result rows where any clause fails. Incomplete
// rows are tolerated in the UI and dropped by `toHavingClauses` at send time.
export function HavingBuilder({ having, onChange, metricNames }: HavingBuilderProps) {
  function addClause() {
    onChange([...having, { metric: metricNames[0] ?? '', op: 'gt' }]);
  }

  function removeClause(index: number) {
    onChange(having.filter((_, i) => i !== index));
  }

  function updateClause(index: number, updates: Partial<HavingDraft>) {
    onChange(having.map((c, i) => (i === index ? { ...c, ...updates } : c)));
  }

  const inputClass =
    'bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none';
  const labelClass = 'text-xs text-text-secondary font-sans mb-1';
  const metricListId = 'having-metric-names';

  return (
    <div data-testid="having-section" className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <label className={labelClass} title="Filter aggregation result rows by a metric value (SQL HAVING)">
          Having
        </label>
        <button
          type="button"
          onClick={addClause}
          data-testid="having-add"
          className="bg-bg-tertiary border border-border text-text-primary px-4 py-2 rounded text-sm hover:bg-bg-elevated"
        >
          + Add Having
        </button>
      </div>

      {having.length === 0 && (
        <div className="text-xs text-text-secondary">
          No having clauses. All aggregated rows are returned.
        </div>
      )}

      {metricNames.length > 0 && (
        <datalist id={metricListId}>
          {metricNames.map((name) => (
            <option key={name} value={name} />
          ))}
        </datalist>
      )}

      {having.map((clause, index) => (
        <div
          key={index}
          data-testid={`having-row-${index}`}
          className="flex items-center gap-2"
        >
          <input
            type="text"
            list={metricListId}
            value={clause.metric}
            onChange={(e) => updateClause(index, { metric: e.target.value })}
            placeholder="metric name"
            aria-label="having metric name"
            title="Output/alias name of the metric to filter on"
            data-testid={`having-${index}-metric`}
            className={`${inputClass} flex-1`}
          />

          <select
            value={clause.op}
            onChange={(e) =>
              updateClause(index, { op: e.target.value as HavingClause['op'] })
            }
            data-testid={`having-${index}-op`}
            aria-label="having comparison operator"
            className={`${inputClass} flex-shrink-0`}
          >
            {havingOps.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>

          <input
            type="number"
            step="any"
            value={clause.value ?? ''}
            onChange={(e) => {
              const raw = e.target.value;
              const n = Number(raw);
              updateClause(index, {
                value: raw.trim() !== '' && Number.isFinite(n) ? n : undefined,
              });
            }}
            placeholder="value"
            aria-label="having threshold value"
            data-testid={`having-${index}-value`}
            className={`${inputClass} w-28`}
          />

          <button
            type="button"
            onClick={() => removeClause(index)}
            data-testid={`having-${index}-remove`}
            className="bg-accent-error/15 text-accent-error border border-accent-error/30 px-4 py-2 rounded text-sm hover:bg-accent-error/25"
          >
            Remove
          </button>
        </div>
      ))}
    </div>
  );
}
