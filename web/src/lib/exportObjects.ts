import * as XLSX from 'xlsx';
import { listObjects, searchObjects } from '../api/objects';
import type { ObjectPage, ObjectType, WireObject, WhereClause } from '../api/types';

export type ExportFormat = 'csv' | 'json' | 'xlsx';

const NUMERIC_BASE_TYPES = new Set([
  'integer',
  'long',
  'short',
  'byte',
  'double',
  'float',
  'decimal',
]);

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

class ExportLimitExceededError extends Error {
  constructor(totalCount?: number) {
    const countText =
      typeof totalCount === 'number'
        ? `would include ${totalCount} rows, which`
        : 'has more rows remaining and';
    super(
      `Export ${countText} exceeds the ${EXPORT_HARD_CAP} row export limit. ` +
        'Refine the Browser search or filters before exporting.',
    );
    this.name = 'ExportLimitExceededError';
  }
}

function parseTotalCount(value: string | undefined): number | undefined {
  if (!value) return undefined;
  const n = Number(value);
  if (!Number.isFinite(n) || n < 0) return undefined;
  return Math.floor(n);
}

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

    const totalCount = parseTotalCount(page.totalCount);
    if (totalCount !== undefined && totalCount > EXPORT_HARD_CAP) {
      throw new ExportLimitExceededError(totalCount);
    }

    rows.push(...page.data);

    if (
      rows.length > EXPORT_HARD_CAP ||
      (rows.length >= EXPORT_HARD_CAP && page.nextPageToken)
    ) {
      throw new ExportLimitExceededError(totalCount);
    }

    if (!page.nextPageToken) break;
    onProgress?.(rows.length);
    pageToken = page.nextPageToken;
  }

  onProgress?.(rows.length);
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

function isNumericColumn(objectType: ObjectType, column: string): boolean {
  const prop = objectType.properties?.[column];
  return !!prop && NUMERIC_BASE_TYPES.has(prop.dataType.type);
}

export type AggregationRow = [
  string,
  number,
  number,
  number,
  number | '',
  number | '',
  number | '',
  number | '',
];

export function computeAggregationSheet(
  rows: WireObject[],
  columns: string[],
  objectType: ObjectType,
): (string | number)[][] {
  const header: (string | number)[] = [
    'column',
    'count',
    'nonNull',
    'distinct',
    'min',
    'max',
    'sum',
    'avg',
  ];
  const data: (string | number)[][] = columns.map((col) => {
    let nonNull = 0;
    const distinct = new Set<unknown>();
    const numericValues: number[] = [];
    const numeric = isNumericColumn(objectType, col);
    for (const row of rows) {
      const v = row[col];
      if (v === null || v === undefined) continue;
      nonNull += 1;
      distinct.add(typeof v === 'object' ? JSON.stringify(v) : v);
      if (numeric && typeof v === 'number' && Number.isFinite(v)) {
        numericValues.push(v);
      }
    }
    let min: number | '' = '';
    let max: number | '' = '';
    let sum: number | '' = '';
    let avg: number | '' = '';
    if (numericValues.length > 0) {
      min = numericValues[0];
      max = numericValues[0];
      let runningSum = 0;
      for (const n of numericValues) {
        if (n < (min as number)) min = n;
        if (n > (max as number)) max = n;
        runningSum += n;
      }
      sum = runningSum;
      avg = runningSum / numericValues.length;
    }
    return [col, rows.length, nonNull, distinct.size, min, max, sum, avg];
  });
  return [header, ...data];
}

function dataSheetMatrix(
  rows: WireObject[],
  columns: string[],
): (string | number | boolean | null)[][] {
  const header: (string | number | boolean | null)[] = [...columns];
  const matrix: (string | number | boolean | null)[][] = [header];
  for (const row of rows) {
    matrix.push(
      columns.map((col) => {
        const v = row[col];
        if (v === null || v === undefined) return '';
        if (typeof v === 'object') return JSON.stringify(v);
        if (typeof v === 'string' || typeof v === 'number' || typeof v === 'boolean') {
          return v;
        }
        return String(v);
      }),
    );
  }
  return matrix;
}

export function buildXlsxWorkbook(
  rows: WireObject[],
  columns: string[],
  objectType: ObjectType,
): XLSX.WorkBook {
  const wb = XLSX.utils.book_new();
  const dataMatrix = dataSheetMatrix(rows, columns);
  const dataSheet = XLSX.utils.aoa_to_sheet(dataMatrix);
  XLSX.utils.book_append_sheet(wb, dataSheet, 'Data');
  const summaryMatrix = computeAggregationSheet(rows, columns, objectType);
  const summarySheet = XLSX.utils.aoa_to_sheet(summaryMatrix);
  XLSX.utils.book_append_sheet(wb, summarySheet, 'Summary');
  return wb;
}

function workbookToBlob(wb: XLSX.WorkBook): Blob {
  const buf = XLSX.write(wb, { type: 'array', bookType: 'xlsx' });
  return new Blob([buf], {
    type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
  });
}

function triggerBlobDownload(blob: Blob, filename: string): void {
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

  if (format === 'xlsx') {
    const wb = buildXlsxWorkbook(rows, columns, objectType);
    const filename = `${apiName}-export.xlsx`;
    triggerBlobDownload(workbookToBlob(wb), filename);
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
