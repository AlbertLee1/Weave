export type BadgeVariant = 'default' | 'success' | 'warning' | 'error' | 'info' | 'example';

interface BadgeProps {
  children: React.ReactNode;
  variant?: BadgeVariant;
  className?: string;
}

const variantClasses: Record<string, string> = {
  default:
    'bg-bg-tertiary text-text-secondary border border-border ' +
    '[box-shadow:inset_0_0_8px_rgba(156,163,175,0.05)]',
  success:
    'bg-accent-success/10 text-accent-success border border-accent-success/30 ' +
    '[box-shadow:inset_0_0_8px_rgba(16,185,129,0.1)]',
  warning:
    'bg-accent-warning/10 text-accent-warning border border-accent-warning/30 ' +
    '[box-shadow:inset_0_0_8px_rgba(245,158,11,0.1)]',
  error:
    'bg-accent-error/10 text-accent-error border border-accent-error/30 ' +
    '[box-shadow:inset_0_0_8px_rgba(244,63,94,0.1)]',
  info:
    'bg-accent-cyan/10 text-accent-cyan border border-accent-cyan/30 ' +
    '[box-shadow:inset_0_0_8px_rgba(245,158,11,0.1)]',
  example:
    'bg-purple-500/10 text-purple-400 border border-purple-500/30 ' +
    '[box-shadow:inset_0_0_8px_rgba(168,85,247,0.1)]',
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
      className={`inline-flex items-center px-2.5 py-1 text-xs font-sans font-medium rounded-full tracking-wide ${variantClasses[variant]} ${className}`}
    >
      {children}
    </span>
  );
}
