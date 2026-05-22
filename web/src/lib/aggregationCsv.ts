import type { AggregationBucket } from '../api/aggregation';

export function buildAggregationCsv(data: AggregationBucket[]): string {
  if (data.length === 0) return '';

  const groupKeys = new Set<string>();
  const metricKeys = new Set<string>();
  for (const bucket of data) {
    if (bucket.group) {
      for (const key of Object.keys(bucket.group)) groupKeys.add(key);
    }
    for (const key of Object.keys(bucket.metrics)) metricKeys.add(key);
  }

  const groups = Array.from(groupKeys);
  const metrics = Array.from(metricKeys);
  const columns = [...groups, ...metrics];
  const lines = [columns.map(escapeCsvCell).join(',')];

  for (const bucket of data) {
    const cells = [
      ...groups.map((key) => bucket.group?.[key]),
      ...metrics.map((key) => bucket.metrics[key]),
    ];
    lines.push(cells.map(formatCsvValue).map(escapeCsvCell).join(','));
  }

  return `${lines.join('\n')}\n`;
}

export function downloadAggregationCsv(
  data: AggregationBucket[],
  filename: string,
): void {
  const csv = buildAggregationCsv(data);
  if (!csv) return;

  const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  anchor.style.display = 'none';
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}

export function aggregationCsvFilename(
  ontology: string,
  objectType: string,
): string {
  const prefix = [ontology, objectType]
    .map((part) => part.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-'))
    .map((part) => part.replace(/^-+|-+$/g, ''))
    .filter(Boolean)
    .join('-');
  return `${prefix || 'aggregation'}-aggregation.csv`;
}

function formatCsvValue(value: unknown): string {
  if (value === null || value === undefined) return '';
  if (typeof value === 'object') return JSON.stringify(value);
  return String(value);
}

function escapeCsvCell(value: unknown): string {
  const text = String(value ?? '');
  if (!/[",\r\n]/.test(text)) return text;
  return `"${text.replace(/"/g, '""')}"`;
}
