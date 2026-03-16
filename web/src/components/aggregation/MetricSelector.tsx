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

  const inputClass =
    'bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none';
  const labelClass = 'text-xs text-text-secondary font-sans mb-1';

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <label className={labelClass}>Metrics</label>
        <button
          type="button"
          onClick={addMetric}
          className="bg-bg-tertiary border border-border text-text-primary px-4 py-2 rounded text-sm hover:bg-bg-elevated"
        >
          + Add Metric
        </button>
      </div>

      {metrics.length === 0 && (
        <div className="text-xs text-text-secondary">No metrics configured. Add at least one metric.</div>
      )}

      {metrics.map((metric, index) => (
        <div key={index} className="flex items-center gap-2">
          <select
            value={metric.type}
            onChange={(e) => updateMetric(index, { type: e.target.value as AggregationMetric['type'] })}
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
            className={`${inputClass} w-28`}
          />

          <button
            type="button"
            onClick={() => removeMetric(index)}
            className="bg-accent-error/15 text-accent-error border border-accent-error/30 px-4 py-2 rounded text-sm hover:bg-accent-error/25"
          >
            Remove
          </button>
        </div>
      ))}
    </div>
  );
}
