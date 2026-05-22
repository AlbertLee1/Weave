import type { WireObject } from '../api/types';
import { serializeCsvCell } from './csvCell';
import type { ObjectSetDiff } from './objectSetDiff';

const ENVELOPE_KEYS = new Set(['__rid', '__primaryKey', '__apiName']);
const CSV_COLUMNS = ['section', 'primaryKey', 'field', 'valueA', 'valueB'];

export function buildObjectSetDiffCsv(
  diff: ObjectSetDiff,
  propertyOrder: string[] = [],
): string {
  const lines = [CSV_COLUMNS.join(',')];

  appendOnlyRows(lines, 'Only in A', diff.onlyInA, propertyOrder, 'A');
  appendChangedRows(lines, diff.changed);
  appendOnlyRows(lines, 'Only in B', diff.onlyInB, propertyOrder, 'B');

  return `${lines.join('\n')}\n`;
}

export function downloadObjectSetDiffCsv(
  diff: ObjectSetDiff,
  propertyOrder: string[],
  filename: string,
): void {
  const csv = buildObjectSetDiffCsv(diff, propertyOrder);
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

export function objectSetDiffCsvFilename(
  ontology: string,
  savedAName?: string,
  savedBName?: string,
): string {
  const prefix = [ontology, savedAName || 'set-a', 'vs', savedBName || 'set-b']
    .map(sanitizeFilenamePart)
    .filter(Boolean)
    .join('-');
  return `${prefix || 'objectset'}-objectset-diff.csv`;
}

function appendOnlyRows(
  lines: string[],
  section: string,
  rows: WireObject[],
  propertyOrder: string[],
  side: 'A' | 'B',
): void {
  const fields = fieldOrderForRows(rows, propertyOrder);
  for (const row of rows) {
    const primaryKey = String(row.__primaryKey ?? '');
    for (const field of fields) {
      const valueA = side === 'A' ? row[field] : undefined;
      const valueB = side === 'B' ? row[field] : undefined;
      appendCsvLine(lines, section, primaryKey, field, valueA, valueB);
    }
  }
}

function appendChangedRows(
  lines: string[],
  rows: ObjectSetDiff['changed'],
): void {
  for (const row of rows) {
    for (const fieldChange of row.fieldChanges) {
      appendCsvLine(
        lines,
        'Changed',
        row.primaryKey,
        fieldChange.field,
        fieldChange.valueA,
        fieldChange.valueB,
      );
    }
  }
}

function appendCsvLine(
  lines: string[],
  section: string,
  primaryKey: string,
  field: string,
  valueA: unknown,
  valueB: unknown,
): void {
  lines.push(
    [section, primaryKey, field, valueA, valueB]
      .map(serializeCsvCell)
      .join(','),
  );
}

function fieldOrderForRows(rows: WireObject[], propertyOrder: string[]): string[] {
  const seen = new Set<string>();
  const fields: string[] = [];

  for (const field of propertyOrder) {
    if (rows.some((row) => Object.prototype.hasOwnProperty.call(row, field))) {
      seen.add(field);
      fields.push(field);
    }
  }

  for (const row of rows) {
    for (const field of Object.keys(row)) {
      if (ENVELOPE_KEYS.has(field) || seen.has(field)) continue;
      seen.add(field);
      fields.push(field);
    }
  }

  return fields;
}

function sanitizeFilenamePart(part: string): string {
  return part
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
}
