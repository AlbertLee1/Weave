export interface ParsedCsv {
  headers: string[];
  rows: Record<string, string>[];
}

export type CellConversion =
  | { value: string | number | boolean | null }
  | { error: string };

export function parseCsv(text: string): ParsedCsv {
  if (!text || !text.length) return { headers: [], rows: [] };

  const records = splitRecords(text);
  if (records.length === 0) return { headers: [], rows: [] };

  const headers = records[0].map((h) => h.trim());
  const rows: Record<string, string>[] = [];
  for (let i = 1; i < records.length; i++) {
    const fields = records[i];
    if (fields.length === 1 && fields[0] === '') continue;
    const obj: Record<string, string> = {};
    for (let j = 0; j < headers.length; j++) {
      obj[headers[j]] = j < fields.length ? fields[j] : '';
    }
    rows.push(obj);
  }
  return { headers, rows };
}

function splitRecords(text: string): string[][] {
  const records: string[][] = [];
  let field = '';
  let row: string[] = [];
  let inQuotes = false;
  let i = 0;
  while (i < text.length) {
    const c = text[i];
    if (inQuotes) {
      if (c === '"') {
        if (text[i + 1] === '"') {
          field += '"';
          i += 2;
          continue;
        }
        inQuotes = false;
        i++;
        continue;
      }
      field += c;
      i++;
      continue;
    }
    if (c === '"') {
      inQuotes = true;
      i++;
      continue;
    }
    if (c === ',') {
      row.push(field);
      field = '';
      i++;
      continue;
    }
    if (c === '\r') {
      if (text[i + 1] === '\n') i++;
      row.push(field);
      records.push(row);
      row = [];
      field = '';
      i++;
      continue;
    }
    if (c === '\n') {
      row.push(field);
      records.push(row);
      row = [];
      field = '';
      i++;
      continue;
    }
    field += c;
    i++;
  }
  if (field !== '' || row.length > 0) {
    row.push(field);
    records.push(row);
  }
  return records;
}

function normalizeHeader(s: string): string {
  return s.toLowerCase().replace(/[\s_-]+/g, '');
}

export function autoMapColumns(
  headers: string[],
  propertyNames: string[],
): Record<string, string> {
  const mapping: Record<string, string> = {};
  const normalizedProps = new Map<string, string>();
  for (const p of propertyNames) {
    normalizedProps.set(normalizeHeader(p), p);
  }
  for (const h of headers) {
    const match = normalizedProps.get(normalizeHeader(h));
    mapping[h] = match ?? '';
  }
  return mapping;
}

const ISO_DATE_RE = /^\d{4}-\d{2}-\d{2}$/;

export function convertCellValue(raw: string, baseType: string): CellConversion {
  const trimmed = raw.trim();
  if (trimmed === '') return { value: null };

  switch (baseType) {
    case 'string':
    case 'attachment':
    case 'mediaReference':
    case 'marking':
    case 'cipher':
      return { value: raw };
    case 'integer':
    case 'long':
    case 'short':
    case 'byte': {
      if (!/^-?\d+$/.test(trimmed)) {
        return { error: `"${trimmed}" is not a valid ${baseType}` };
      }
      const n = Number(trimmed);
      if (!Number.isFinite(n)) {
        return { error: `"${trimmed}" is not a valid ${baseType}` };
      }
      return { value: n };
    }
    case 'double':
    case 'float':
    case 'decimal': {
      const n = Number(trimmed);
      if (!Number.isFinite(n)) {
        return { error: `"${trimmed}" is not a valid ${baseType}` };
      }
      return { value: n };
    }
    case 'boolean': {
      const low = trimmed.toLowerCase();
      if (['true', '1', 'yes', 'y', 't'].includes(low)) return { value: true };
      if (['false', '0', 'no', 'n', 'f'].includes(low)) return { value: false };
      return { error: `"${trimmed}" is not a boolean` };
    }
    case 'date': {
      if (!ISO_DATE_RE.test(trimmed) || Number.isNaN(Date.parse(trimmed))) {
        return { error: `"${trimmed}" is not a valid ISO date (YYYY-MM-DD)` };
      }
      return { value: trimmed };
    }
    case 'timestamp': {
      if (Number.isNaN(Date.parse(trimmed))) {
        return { error: `"${trimmed}" is not a valid ISO timestamp` };
      }
      return { value: trimmed };
    }
    default:
      return { value: raw };
  }
}

export function validateCell(raw: string, baseType: string): string | null {
  const result = convertCellValue(raw, baseType);
  if ('error' in result) return result.error;
  return null;
}
