interface StatsBarProps {
  ontologyCount: number;
  objectTypeCount: number;
}

export function StatsBar({ ontologyCount, objectTypeCount }: StatsBarProps) {
  return (
    <div className="flex items-center gap-6 px-4 py-3 bg-bg-secondary border border-border rounded-lg">
      <div className="flex items-center gap-2">
        <span className="text-xs text-text-muted font-sans">Ontologies</span>
        <span className="text-sm font-mono text-accent-cyan">{ontologyCount}</span>
      </div>
      <div className="w-px h-4 bg-border" />
      <div className="flex items-center gap-2">
        <span className="text-xs text-text-muted font-sans">Object Types</span>
        <span className="text-sm font-mono text-accent-cyan">{objectTypeCount}</span>
      </div>
    </div>
  );
}
