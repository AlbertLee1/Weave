export function formatBaseType(baseType: string): string {
  const map: Record<string, string> = {
    string: 'String',
    integer: 'Integer',
    long: 'Long',
    double: 'Double',
    boolean: 'Boolean',
    timestamp: 'Timestamp',
    date: 'Date',
    byte: 'Byte',
    short: 'Short',
    float: 'Float',
    decimal: 'Decimal',
  };
  return map[baseType] ?? baseType;
}

export function formatStatus(status: string): string {
  return status.charAt(0) + status.slice(1).toLowerCase();
}

export function truncate(text: string, maxLength: number): string {
  if (text.length <= maxLength) return text;
  return text.slice(0, maxLength - 1) + '\u2026';
}

export function formatCount(count: string | number | undefined): string {
  if (count === undefined) return '-';
  const n = typeof count === 'string' ? parseInt(count, 10) : count;
  if (isNaN(n)) return '-';
  return n.toLocaleString();
}
