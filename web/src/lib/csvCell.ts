export function serializeCsvCell(value: unknown): string {
  const text = formatCsvValue(value);
  const protectedText =
    typeof value === 'string' && isFormulaLikeCsvString(value)
      ? `'${text}`
      : text;
  return escapeCsvCell(protectedText);
}

function formatCsvValue(value: unknown): string {
  if (value === null || value === undefined) return '';
  if (typeof value === 'object') return JSON.stringify(value);
  return String(value);
}

function isFormulaLikeCsvString(text: string): boolean {
  const candidate = text.replace(/^[ \n\f\v]+/, '');
  return /^[=+\-@\t\r]/.test(candidate);
}

function escapeCsvCell(text: string): string {
  if (!/[",\r\n]/.test(text)) return text;
  return `"${text.replace(/"/g, '""')}"`;
}
