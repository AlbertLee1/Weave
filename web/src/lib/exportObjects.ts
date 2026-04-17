import { listObjects, searchObjects } from '../api/objects';
import type { ObjectPage, ObjectType, WireObject, WhereClause } from '../api/types';

export type ExportFormat = 'csv' | 'json';

export interface ExportQuery {
  ontologyApiName: string;
  objectType: string;
  where?: WhereClause;
  orderBy?: { field: string; direction?: 'asc' | 'desc' };
  select: string[];
  hasActiveSearch: boolean;
}

const EXPORT_PAGE_SIZE = 200;
const EXPORT_HARD_CAP = 100_000;

export async function fetchAllForExport(
  query: ExportQuery,
  onProgress?: (count: number) => void,
): Promise<WireObject[]> {
  const rows: WireObject[] = [];
  let pageToken: string | undefined = undefined;

  while (rows.length < EXPORT_HARD_CAP) {
    const page: ObjectPage = query.hasActiveSearch
      ? await searchObjects({
          ontologyApiName: query.ontologyApiName,
          objectType: query.objectType,
          where: query.where,
          orderBy: query.orderBy,
          select: query.select,
          pageSize: EXPORT_PAGE_SIZE,
          pageToken,
        })
      : await listObjects({
          ontologyApiName: query.ontologyApiName,
          objectType: query.objectType,
          pageSize: EXPORT_PAGE_SIZE,
          pageToken,
          orderBy: query.orderBy
            ? `${query.orderBy.field}:${query.orderBy.direction ?? 'asc'}`
            : undefined,
        });

    rows.push(...page.data);
    onProgress?.(rows.length);

    if (!page.nextPageToken) break;
    pageToken = page.nextPageToken;
  }

  return rows;
}

export function escapeCsvField(value: unknown): string {
  if (value === null || value === undefined) return '';
  let text: string;
  if (typeof value === 'object') {
    text = JSON.stringify(value);
  } else {
    text = String(value);
  }
  if (/[",\r\n]/.test(text)) {
    return `"${text.replace(/"/g, '""')}"`;
  }
  return text;
}

export function toCsv(
  rows: WireObject[],
  columns: string[],
): string {
  const header = columns.map(escapeCsvField).join(',');
  const lines = rows.map((row) =>
    columns.map((col) => escapeCsvField(row[col])).join(','),
  );
  return [header, ...lines].join('\r\n');
}

export interface JsonExportEnvelope {
  data: WireObject[];
  metadata: {
    objectType: string;
    exportedAt: string;
    count: number;
  };
}

export function toJsonEnvelope(
  rows: WireObject[],
  objectType: string,
  exportedAt: Date = new Date(),
): JsonExportEnvelope {
  return {
    data: rows,
    metadata: {
      objectType,
      exportedAt: exportedAt.toISOString(),
      count: rows.length,
    },
  };
}

export function resolveExportColumns(
  objectType: ObjectType | undefined,
): string[] {
  if (!objectType?.properties) return [];
  return Object.keys(objectType.properties);
}

export function triggerDownload(
  content: string,
  filename: string,
  mimeType: string,
): void {
  const blob = new Blob([content], { type: mimeType });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  URL.revokeObjectURL(url);
}

export async function exportObjects(
  format: ExportFormat,
  query: ExportQuery,
  objectType: ObjectType,
  onProgress?: (count: number) => void,
): Promise<{ filename: string; count: number }> {
  const rows = await fetchAllForExport(query, onProgress);
  const columns = resolveExportColumns(objectType);
  const apiName = objectType.apiName;

  if (format === 'csv') {
    const csv = toCsv(rows, columns);
    const filename = `${apiName}-export.csv`;
    triggerDownload(csv, filename, 'text/csv;charset=utf-8;');
    return { filename, count: rows.length };
  }

  const envelope = toJsonEnvelope(rows, apiName);
  const filename = `${apiName}-export.json`;
  triggerDownload(
    JSON.stringify(envelope, null, 2),
    filename,
    'application/json;charset=utf-8;',
  );
  return { filename, count: rows.length };
}
