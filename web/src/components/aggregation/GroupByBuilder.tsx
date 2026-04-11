import type { GroupByClause } from '../../api/types';

interface GroupByBuilderProps {
  groupBy: GroupByClause[];
  onChange: (groupBy: GroupByClause[]) => void;
  availableFields: string[];
}

const groupByTypes: GroupByClause['type'][] = ['exact', 'ranges', 'fixedWidth'];

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

  const inputClass =
    'bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none';
  const labelClass = 'text-xs text-text-secondary font-sans mb-1';

  return (
    <div className="flex flex-col gap-3">
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

      {groupBy.map((clause, index) => (
        <div
          key={index}
          data-testid={`groupby-row-${index}`}
          className="flex items-center gap-2"
        >
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
            onChange={(e) => updateGroupBy(index, { type: e.target.value as GroupByClause['type'] })}
            data-testid={`groupby-${index}-type`}
            className={`${inputClass} flex-shrink-0`}
          >
            {groupByTypes.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>

          <button
            type="button"
            onClick={() => removeGroupBy(index)}
            data-testid={`groupby-${index}-remove`}
            className="bg-accent-error/15 text-accent-error border border-accent-error/30 px-4 py-2 rounded text-sm hover:bg-accent-error/25"
          >
            Remove
          </button>
        </div>
      ))}
    </div>
  );
}
