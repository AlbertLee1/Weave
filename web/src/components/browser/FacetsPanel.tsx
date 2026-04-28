import { useState, useCallback } from 'react';
import type { FacetBucket } from '../../api/types';

export type FacetSelection = Record<string, string[]>;

interface FacetsPanelProps {
  fields: string[];
  facets?: Record<string, FacetBucket[]>;
  selected: FacetSelection;
  onToggle: (field: string, value: string) => void;
  onClear?: () => void;
}

export function FacetsPanel({
  fields,
  facets,
  selected,
  onToggle,
  onClear,
}: FacetsPanelProps) {
  if (fields.length === 0) return null;

  const hasAnySelection = Object.values(selected).some((v) => v.length > 0);

  return (
    <aside
      data-testid="facets-panel"
      className="w-56 shrink-0 border-r border-border pr-4 self-stretch"
      aria-label="Facets"
    >
      <div className="flex items-center justify-between mb-3">
        <h2 className="text-xs font-mono uppercase tracking-wider text-text-secondary">
          Facets
        </h2>
        {hasAnySelection && onClear && (
          <button
            type="button"
            onClick={onClear}
            data-testid="facets-clear"
            className="text-xs font-mono text-accent-cyan hover:underline"
          >
            Clear
          </button>
        )}
      </div>
      <div className="flex flex-col gap-4">
        {fields.map((field) => (
          <FacetGroup
            key={field}
            field={field}
            buckets={facets?.[field] ?? []}
            selected={selected[field] ?? []}
            onToggle={onToggle}
          />
        ))}
      </div>
    </aside>
  );
}

interface FacetGroupProps {
  field: string;
  buckets: FacetBucket[];
  selected: string[];
  onToggle: (field: string, value: string) => void;
}

const COLLAPSED_LIMIT = 8;

function FacetGroup({ field, buckets, selected, onToggle }: FacetGroupProps) {
  const [expanded, setExpanded] = useState(false);
  const hasMore = buckets.length > COLLAPSED_LIMIT;
  const visible = expanded ? buckets : buckets.slice(0, COLLAPSED_LIMIT);

  const handleChange = useCallback(
    (value: string) => onToggle(field, value),
    [field, onToggle],
  );

  return (
    <section data-testid={`facet-group-${field}`}>
      <h3 className="text-xs font-sans font-semibold text-text-primary mb-2">
        {field}
      </h3>
      {visible.length === 0 ? (
        <p className="text-xs font-mono text-text-muted">No values</p>
      ) : (
        <ul className="flex flex-col gap-1">
          {visible.map((bucket) => {
            const checked = selected.includes(bucket.value);
            return (
              <li key={bucket.value}>
                <label
                  className={[
                    'flex items-center gap-2 px-1 py-0.5 rounded cursor-pointer',
                    'hover:bg-bg-secondary',
                    checked ? 'text-accent-cyan' : 'text-text-primary',
                  ].join(' ')}
                  data-testid={`facet-option-${field}-${bucket.value}`}
                >
                  <input
                    type="checkbox"
                    checked={checked}
                    onChange={() => handleChange(bucket.value)}
                    className="accent-accent-cyan"
                    aria-label={`${field}: ${bucket.value}`}
                  />
                  <span
                    className="flex-1 truncate text-xs font-mono"
                    title={bucket.value}
                  >
                    {bucket.value}
                  </span>
                  <span className="text-xs font-mono text-text-muted">
                    {bucket.count}
                  </span>
                </label>
              </li>
            );
          })}
        </ul>
      )}
      {hasMore && (
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          data-testid={`facet-toggle-${field}`}
          className="mt-1 text-xs font-mono text-accent-cyan hover:underline"
        >
          {expanded ? 'Show less' : `Show ${buckets.length - COLLAPSED_LIMIT} more`}
        </button>
      )}
    </section>
  );
}
