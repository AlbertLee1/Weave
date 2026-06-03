import type { AggregationMetric } from '../../api/types';

interface MetricSelectorProps {
  metrics: AggregationMetric[];
  onChange: (metrics: AggregationMetric[]) => void;
  availableFields: string[];
}

const metricTypes: AggregationMetric['type'][] = ['count', 'sum', 'avg', 'min', 'max'];

export function MetricSelector({ metrics, onChange, availableFields }: MetricSelectorProps) {
  function addMetric() {
    onChange([...metrics, { type: 'count' }]);
  }

  function removeMetric(index: number) {
    onChange(metrics.filter((_, i) => i !== index));
  }

  function updateMetric(index: number, updates: Partial<AggregationMetric>) {
    onChange(
      metrics.map((m, i) => (i === index ? { ...m, ...updates } : m)),
    );
  }

  // The backend orders groupBy rows by the FIRST direction-bearing metric, so
  // the UI enforces a single sort key: setting a direction on one metric clears
  // it on the others; clearing simply drops it from this metric.
  function setDirection(index: number, direction: AggregationMetric['direction']) {
    onChange(
      metrics.map((m, i) => {
        if (i === index) {
          const next = { ...m };
          if (direction) next.direction = direction;
          else delete next.direction;
          return next;
        }
        if (direction && m.direction) {
          const cleared = { ...m };
          delete cleared.direction;
          return cleared;
        }
        return m;
      }),
    );
  }

  const inputClass =
    'bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none';
  const labelClass = 'text-xs text-text-secondary font-sans mb-1';

  return (
    <div data-testid="metrics-section" className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <label className={labelClass}>Metrics</label>
        <button
          type="button"
          onClick={addMetric}
          data-testid="metric-add"
          className="bg-bg-tertiary border border-border text-text-primary px-4 py-2 rounded text-sm hover:bg-bg-elevated"
        >
          + Add Metric
        </button>
      </div>

      {metrics.length === 0 && (
        <div data-testid="metrics-empty" className="text-xs text-text-secondary">
          No metrics configured. Add at least one metric.
        </div>
      )}

      {metrics.map((metric, index) => (
        <div
          key={index}
          data-testid={`metric-row-${index}`}
          data-metric-type={metric.type}
          className="flex items-center gap-2"
        >
          <select
            value={metric.type}
            onChange={(e) => updateMetric(index, { type: e.target.value as AggregationMetric['type'] })}
            data-testid={`metric-${index}-type`}
            className={`${inputClass} flex-shrink-0`}
          >
            {metricTypes.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>

          {metric.type !== 'count' && (
            <select
              value={metric.field ?? ''}
              onChange={(e) => updateMetric(index, { field: e.target.value || undefined })}
              data-testid={`metric-${index}-field`}
              className={`${inputClass} flex-1`}
            >
              <option value="">Select field...</option>
              {availableFields.map((f) => (
                <option key={f} value={f}>
                  {f}
                </option>
              ))}
            </select>
          )}

          <input
            type="text"
            value={metric.name ?? ''}
            onChange={(e) => updateMetric(index, { name: e.target.value || undefined })}
            placeholder="alias"
            data-testid={`metric-${index}-name`}
            className={`${inputClass} w-28`}
          />

          <select
            value={metric.direction ?? ''}
            onChange={(e) =>
              setDirection(index, (e.target.value || undefined) as AggregationMetric['direction'])
            }
            data-testid={`metric-${index}-direction`}
            aria-label="Sort groupBy by this metric"
            title="Order groupBy result rows by this metric's value"
            className={`${inputClass} flex-shrink-0 w-28`}
          >
            <option value="">No sort</option>
            <option value="DESC">Sort ↓ DESC</option>
            <option value="ASC">Sort ↑ ASC</option>
          </select>

          <button
            type="button"
            onClick={() => removeMetric(index)}
            data-testid={`metric-${index}-remove`}
            className="bg-accent-error/15 text-accent-error border border-accent-error/30 px-4 py-2 rounded text-sm hover:bg-accent-error/25"
          >
            Remove
          </button>
        </div>
      ))}
    </div>
  );
}
