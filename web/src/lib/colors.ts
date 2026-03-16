const STATUS_COLORS: Record<string, string> = {
  ACTIVE: 'text-accent-success',
  EXPERIMENTAL: 'text-accent-warning',
  DEPRECATED: 'text-accent-error',
};

export function statusColor(status: string): string {
  return STATUS_COLORS[status] ?? 'text-text-secondary';
}

const STATUS_BG: Record<string, string> = {
  ACTIVE: 'bg-accent-success/15 text-accent-success',
  EXPERIMENTAL: 'bg-accent-warning/15 text-accent-warning',
  DEPRECATED: 'bg-accent-error/15 text-accent-error',
};

export function statusBgColor(status: string): string {
  return STATUS_BG[status] ?? 'bg-bg-tertiary text-text-secondary';
}
