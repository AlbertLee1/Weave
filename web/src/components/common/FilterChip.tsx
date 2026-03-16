interface FilterChipProps {
  label: string;
  value: string;
  onRemove: () => void;
}

export function FilterChip({ label, value, onRemove }: FilterChipProps) {
  return (
    <span className="inline-flex items-center gap-1 px-2 py-1 bg-bg-tertiary border border-border rounded text-xs font-mono">
      <span className="text-text-secondary">{label}:</span>
      <span className="text-accent-cyan">{value}</span>
      <button
        onClick={onRemove}
        className="ml-1 text-text-muted hover:text-accent-error transition-colors"
        aria-label={`Remove filter ${label}`}
      >
        <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M18 6L6 18M6 6l12 12" />
        </svg>
      </button>
    </span>
  );
}
