import type { GroupByClause } from '../../api/types';

interface GroupByBuilderProps {
  groupBy: GroupByClause[];
  onChange: (groupBy: GroupByClause[]) => void;
  availableFields: string[];
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
// Limited to the periods the backend `parseDuration` accepts (P1D/P1W/P1M/P1Y);
// offering an unsupported period like P3M or PT1H would make the aggregate
// request fail with "unsupported duration".
const durationPeriods = ['P1D', 'P1W', 'P1M', 'P1Y'];

// Types that accept an optional maxGroupCount (wire key `maxGroupCount`).
const maxGroupCountTypes: GroupByClause['type'][] = ['exact', 'topValues'];

export function GroupByBuilder({ groupBy, onChange, availableFields }: GroupByBuilderProps) {
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
      <div className="flex items-center justify-between">
        <label className={labelClass}>Group By</label>
        <button
          type="button"
          onClick={addGroupBy}
          data-testid="groupby-add"
          className="bg-bg-tertiary border border-border text-text-primary px-4 py-2 rounded text-sm hover:bg-bg-elevated"
        >
          + Add Group By
        </button>
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
