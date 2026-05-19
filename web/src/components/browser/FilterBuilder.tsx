import { useState } from 'react';
import type { FilterCondition } from '../../lib/whereBuilder';
import { FilterChip } from '../common/FilterChip';

const OPERATORS = [
  { value: 'eq', label: '=' },
  { value: 'gt', label: '>' },
  { value: 'lt', label: '<' },
  { value: 'contains', label: 'contains' },
  { value: 'containsAnyTerm', label: 'contains any term' },
] as const;

interface FilterBuilderProps {
  properties: Record<string, { dataType: { type: string; itemType?: unknown }; rid: string }>;
  filters: FilterCondition[];
  onFiltersChange: (filters: FilterCondition[]) => void;
}

export function FilterBuilder({
  properties,
  filters,
  onFiltersChange,
}: FilterBuilderProps) {
  const propertyNames = Object.keys(properties);
  const [selectedField, setSelectedField] = useState(propertyNames[0] ?? '');
  const [selectedOp, setSelectedOp] = useState<string>(OPERATORS[0].value);
  const [value, setValue] = useState('');

  function handleAdd() {
    if (!selectedField || !value.trim()) return;

    let parsedValue: unknown = value.trim();
    if (selectedOp === 'containsAnyTerm') {
      parsedValue = (parsedValue as string)
        .split(/\s+/)
        .filter((t: string) => t.length > 0)
        .join(' ');
    } else if (selectedOp === 'gt' || selectedOp === 'lt') {
      const num = Number(parsedValue);
      if (!isNaN(num)) parsedValue = num;
    }

    const newFilter: FilterCondition = {
      field: selectedField,
      op: selectedOp,
      value: parsedValue,
    };

    onFiltersChange([...filters, newFilter]);
    setValue('');
  }

  function handleRemove(index: number) {
    onFiltersChange(filters.filter((_, i) => i !== index));
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Enter') handleAdd();
  }

  function formatFilterValue(filter: FilterCondition): string {
    if (Array.isArray(filter.value)) return filter.value.join(', ');
    return String(filter.value);
  }

  function formatFilterLabel(filter: FilterCondition): string {
    const opLabel = OPERATORS.find((o) => o.value === filter.op)?.label ?? filter.op;
    return `${filter.field} ${opLabel}`;
  }

  return (
    <div className="space-y-3">
      {/* Active filter chips */}
      {filters.length > 0 && (
        <div className="flex flex-wrap gap-2" data-testid="active-filters">
          {filters.map((filter, i) => (
            <FilterChip
              key={i}
              label={formatFilterLabel(filter)}
              value={formatFilterValue(filter)}
              onRemove={() => handleRemove(i)}
            />
          ))}
        </div>
      )}

      {/* Add new filter row */}
      <div className="flex items-center gap-2">
        <select
          value={selectedField}
          onChange={(e) => setSelectedField(e.target.value)}
          aria-label="Filter field"
          className="px-2 py-1.5 bg-bg-primary border border-border rounded text-xs font-mono text-text-primary focus:outline-none focus:border-accent-cyan focus-visible:ring-2 focus-visible:ring-accent-cyan/50"
          data-testid="filter-field-select"
        >
          {propertyNames.map((name) => (
            <option key={name} value={name}>
              {name}
            </option>
          ))}
        </select>

        <select
          value={selectedOp}
          onChange={(e) => setSelectedOp(e.target.value)}
          aria-label="Filter operator"
          className="px-2 py-1.5 bg-bg-primary border border-border rounded text-xs font-mono text-text-primary focus:outline-none focus:border-accent-cyan focus-visible:ring-2 focus-visible:ring-accent-cyan/50"
          data-testid="filter-op-select"
        >
          {OPERATORS.map((op) => (
            <option key={op.value} value={op.value}>
              {op.label}
            </option>
          ))}
        </select>

        <input
          type="text"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Value"
          aria-label="Filter value"
          className="flex-1 px-2 py-1.5 bg-bg-primary border border-border rounded text-xs font-mono text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent-cyan focus-visible:ring-2 focus-visible:ring-accent-cyan/50"
          data-testid="filter-value-input"
        />

        <button
          onClick={handleAdd}
          disabled={!selectedField || !value.trim()}
          className="px-3 py-1.5 bg-accent-cyan/10 border border-accent-cyan/30 rounded text-xs font-sans text-accent-cyan hover:bg-accent-cyan/20 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
          data-testid="filter-add-btn"
        >
          Add
        </button>
      </div>
    </div>
  );
}
