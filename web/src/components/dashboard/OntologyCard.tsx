import type { Ontology } from '../../api/types';

interface OntologyCardProps {
  ontology: Ontology;
  objectTypeCount: number;
  onClick: () => void;
}

export function OntologyCard({ ontology, objectTypeCount, onClick }: OntologyCardProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={`Open ontology ${ontology.displayName}`}
      className="relative w-full text-left rounded-xl p-5 group focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-cyan/50 transition-all duration-200"
      style={{
        background: 'rgba(13,17,23,0.8)',
        border: '1px solid rgba(31,41,55,0.8)',
        backdropFilter: 'blur(8px)',
        WebkitBackdropFilter: 'blur(8px)',
        boxShadow: '0 2px 12px rgba(0,0,0,0.3)',
        transition: 'transform 200ms ease, box-shadow 200ms ease, border-color 200ms ease',
      }}
      onMouseEnter={(e) => {
        const el = e.currentTarget;
        el.style.transform = 'scale(1.02)';
        el.style.boxShadow =
          '0 8px 32px rgba(0,0,0,0.5), 0 0 0 1px rgba(245,158,11,0.25), 0 0 20px rgba(245,158,11,0.08)';
        el.style.borderColor = 'rgba(245,158,11,0.35)';
      }}
      onMouseLeave={(e) => {
        const el = e.currentTarget;
        el.style.transform = 'scale(1)';
        el.style.boxShadow = '0 2px 12px rgba(0,0,0,0.3)';
        el.style.borderColor = 'rgba(31,41,55,0.8)';
      }}
    >
      {/* Card header */}
      <div className="flex items-start justify-between mb-3">
        <h3
          className="text-sm font-semibold text-text-primary group-hover:text-accent-cyan transition-colors truncate leading-snug tracking-tight"
          style={{ fontFamily: 'var(--font-sans)' }}
        >
          {ontology.displayName}
        </h3>
        {/* Object type count chip */}
        <span
          className="ml-2 shrink-0 inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium"
          style={{
            fontFamily: 'var(--font-sans)',
            background: 'rgba(245,158,11,0.1)',
            border: '1px solid rgba(245,158,11,0.25)',
            color: '#F59E0B',
            boxShadow: 'inset 0 0 8px rgba(245,158,11,0.08)',
          }}
        >
          {objectTypeCount} types
        </span>
      </div>

      {/* Description */}
      <p className="text-xs text-text-secondary line-clamp-2 mb-4 min-h-[2rem] leading-relaxed">
        {ontology.description || 'No description provided.'}
      </p>

      {/* Footer */}
      <div className="flex items-center justify-between">
        <span
          className="text-xs text-text-muted"
          style={{ fontFamily: 'var(--font-mono)' }}
        >
          {ontology.apiName}
        </span>
        <span
          className="flex items-center gap-1 text-xs font-medium opacity-0 group-hover:opacity-100 transition-all duration-150"
          style={{ color: '#14B8A6' }}
        >
          Explore
          <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true" focusable="false">
            <path d="M5 12h14M12 5l7 7-7 7" />
          </svg>
        </span>
      </div>

      {/* Bottom amber glow line on hover */}
      <div
        className="absolute bottom-0 left-4 right-4 h-px opacity-0 group-hover:opacity-100 transition-opacity duration-200 rounded-full"
        style={{ background: 'linear-gradient(90deg, transparent, rgba(245,158,11,0.4), transparent)' }}
      />
    </button>
  );
}
