interface BadgeProps {
  children: React.ReactNode;
  variant?: 'default' | 'success' | 'warning' | 'error' | 'info';
  className?: string;
}

const variantClasses: Record<string, string> = {
  default: 'bg-bg-tertiary text-text-secondary',
  success: 'bg-accent-success/15 text-accent-success',
  warning: 'bg-accent-warning/15 text-accent-warning',
  error: 'bg-accent-error/15 text-accent-error',
  info: 'bg-accent-cyan/15 text-accent-cyan',
};

export function Badge({ children, variant = 'default', className = '' }: BadgeProps) {
  return (
    <span
      className={`inline-flex items-center px-2 py-0.5 text-xs font-mono rounded ${variantClasses[variant]} ${className}`}
    >
      {children}
    </span>
  );
}
