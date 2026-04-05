export type BadgeVariant = 'default' | 'success' | 'warning' | 'error' | 'info' | 'example';

interface BadgeProps {
  children: React.ReactNode;
  variant?: BadgeVariant;
  className?: string;
}

const variantClasses: Record<string, string> = {
  default: 'bg-bg-tertiary text-text-secondary',
  success: 'bg-accent-success/15 text-accent-success',
  warning: 'bg-accent-warning/15 text-accent-warning',
  error: 'bg-accent-error/15 text-accent-error',
  info: 'bg-accent-cyan/15 text-accent-cyan',
  example: 'bg-purple-500/20 text-purple-400',
};

export function statusVariant(status: string): BadgeVariant {
  if (status === 'ACTIVE') return 'success';
  if (status === 'PROMOTED') return 'info';
  if (status === 'EXPERIMENTAL') return 'warning';
  if (status === 'DEPRECATED') return 'error';
  if (status === 'EXAMPLE') return 'example';
  return 'default';
}

export function Badge({ children, variant = 'default', className = '' }: BadgeProps) {
  return (
    <span
      className={`inline-flex items-center px-2 py-0.5 text-xs font-mono rounded ${variantClasses[variant]} ${className}`}
    >
      {children}
    </span>
  );
}
