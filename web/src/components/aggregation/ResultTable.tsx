import type { AggregationBucket } from '../../api/aggregation';

interface ResultTableProps {
  data: AggregationBucket[];
}

export function ResultTable({ data }: ResultTableProps) {
  if (data.length === 0) {
    return (
      <div className="text-xs text-text-secondary py-4">No aggregation results.</div>
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
          {data.map((bucket, i) => (
            <tr
              key={i}
              className="border-b border-border last:border-b-0"
            >
              {groupCols.map((col) => (
                <td
                  key={`g-${col}`}
                  className="px-3 py-2 text-xs font-mono text-text-primary"
                >
                  {String(bucket.group?.[col] ?? '')}
                </td>
              ))}
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
          ))}
        </tbody>
      </table>
    </div>
  );
}
