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

export function formatRelativeTime(
  input: string | Date | undefined | null,
): string {
  if (!input) return '';
  const date = typeof input === 'string' ? new Date(input) : input;
  if (Number.isNaN(date.getTime())) return '';
  const seconds = Math.floor((Date.now() - date.getTime()) / 1000);
  if (seconds < 60) return 'just now';
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
  if (seconds < 7 * 86400) return `${Math.floor(seconds / 86400)}d ago`;
  return date.toLocaleDateString();
}
