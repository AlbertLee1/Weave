import type { AggregationBucket } from '../../api/aggregation';

interface ResultTableProps {
  data: AggregationBucket[];
}

export function ResultTable({ data }: ResultTableProps) {
  if (data.length === 0) {
    return (
      <div
        data-testid="aggregation-empty-results"
        className="text-xs text-text-secondary py-4"
      >
        No aggregation results.
      </div>
    );
  }

  // Derive column keys from data
  const groupKeys = new Set<string>();
  const metricKeys = new Set<string>();

  for (const bucket of data) {
    if (bucket.group) {
      for (const key of Object.keys(bucket.group)) {
        groupKeys.add(key);
      }
    }
    for (const key of Object.keys(bucket.metrics)) {
      metricKeys.add(key);
    }
  }

  const groupCols = Array.from(groupKeys);
  const metricCols = Array.from(metricKeys);

  // SUBTOTAL_LABEL marks a cube/rollup subtotal cell — a row produced by the
  // backend's cube/rollup expansion aggregates away one or more dimensions, so
  // those keys are ABSENT from the row's `group` object (not null-valued). We
  // surface that as "(all)" so the column lines up and the row never renders as
  // a confusing blank. A row is a subtotal when at least one displayed groupBy
  // column is missing from its group map.
  const SUBTOTAL_LABEL = '(all)';
  const hasGroupKey = (group: Record<string, unknown> | undefined, key: string) =>
    group != null && Object.prototype.hasOwnProperty.call(group, key);
  const isSubtotalRow = (group: Record<string, unknown> | undefined) =>
    groupCols.some((col) => !hasGroupKey(group, col));

  return (
    <div
      className="overflow-x-auto border border-border rounded"
      data-testid="aggregation-bucket-tree"
      data-groupby-depth={groupCols.length}
    >
      <table className="w-full text-sm">
        <thead>
          <tr className="bg-bg-tertiary border-b border-border">
            {groupCols.map((col) => (
              <th
                key={`g-${col}`}
                className="px-3 py-2 text-left text-xs font-mono text-text-secondary font-medium"
              >
                {col}
              </th>
            ))}
            {metricCols.map((col) => (
              <th
                key={`m-${col}`}
                className="px-3 py-2 text-right text-xs font-mono text-accent-cyan font-medium"
              >
                {col}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {data.map((bucket, i) => {
            const subtotal = isSubtotalRow(bucket.group);
            return (
            <tr
              key={i}
              data-testid={`aggregation-bucket-row-${i}`}
              data-subtotal={subtotal ? 'true' : undefined}
              className={
                subtotal
                  ? 'border-b border-border last:border-b-0 bg-bg-tertiary/40 italic'
                  : 'border-b border-border last:border-b-0'
              }
            >
              {groupCols.map((col) => {
                const present = hasGroupKey(bucket.group, col);
                return (
                <td
                  key={`g-${col}`}
                  className={
                    present
                      ? 'px-3 py-2 text-xs font-mono text-text-primary'
                      : 'px-3 py-2 text-xs font-mono text-text-secondary'
                  }
                >
                  {present ? String(bucket.group?.[col] ?? '') : SUBTOTAL_LABEL}
                </td>
                );
              })}
              {metricCols.map((col) => (
                <td
                  key={`m-${col}`}
                  className="px-3 py-2 text-xs font-mono text-text-primary text-right"
                >
                  {bucket.metrics[col] != null
                    ? Number(bucket.metrics[col]).toLocaleString()
                    : '-'}
                </td>
              ))}
            </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
