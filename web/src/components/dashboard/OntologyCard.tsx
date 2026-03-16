import type { Ontology } from '../../api/types';

interface OntologyCardProps {
  ontology: Ontology;
  objectTypeCount: number;
  onClick: () => void;
}

export function OntologyCard({ ontology, objectTypeCount, onClick }: OntologyCardProps) {
  return (
    <button
      onClick={onClick}
      className="w-full text-left bg-bg-secondary border border-border rounded-lg p-5 hover:border-accent-cyan/40 hover:bg-bg-tertiary transition-all duration-150 group focus:outline-none focus:ring-1 focus:ring-accent-cyan/50"
    >
      <div className="flex items-start justify-between mb-3">
        <h3 className="text-sm font-medium text-text-primary group-hover:text-accent-cyan transition-colors truncate">
          {ontology.displayName}
        </h3>
        <span className="ml-2 shrink-0 inline-flex items-center px-2 py-0.5 rounded text-xs font-mono text-accent-cyan bg-accent-cyan/10 border border-accent-cyan/20">
          {objectTypeCount}
        </span>
      </div>

      <p className="text-xs text-text-secondary line-clamp-2 mb-4 min-h-[2rem]">
        {ontology.description || 'No description'}
      </p>

      <div className="flex items-center justify-between text-xs text-text-muted">
        <span className="font-mono">{ontology.apiName}</span>
        <span className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity text-accent-cyan">
          Explore
          <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
            <path d="M5 12h14M12 5l7 7-7 7" />
          </svg>
        </span>
      </div>
    </button>
  );
}
