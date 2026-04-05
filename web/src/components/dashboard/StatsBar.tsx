interface StatsBarProps {
  ontologyCount: number;
  objectTypeCount: number;
}

interface StatItemProps {
  icon: React.ReactNode;
  label: string;
  value: number;
  color: string;
  delay: number;
}

function StatItem({ icon, label, value, color, delay }: StatItemProps) {
  return (
    <div
      className="flex items-center gap-3 px-5 py-3.5 rounded-xl flex-1"
      style={{
        background: 'rgba(13,17,23,0.6)',
        border: `1px solid ${color}22`,
        boxShadow: `0 0 16px ${color}08`,
        animation: `fadeInUp 400ms ${delay}ms ease-out both`,
      }}
    >
      <div
        className="flex items-center justify-center w-8 h-8 rounded-lg shrink-0"
        style={{
          background: `${color}15`,
          border: `1px solid ${color}30`,
        }}
      >
        {icon}
      </div>
      <div>
        <div
          className="text-xl font-semibold leading-none"
          style={{ color, fontFamily: 'var(--font-sans)' }}
        >
          {value}
        </div>
        <div className="text-xs text-text-secondary mt-0.5" style={{ fontFamily: 'var(--font-sans)' }}>
          {label}
        </div>
      </div>
    </div>
  );
}

export function StatsBar({ ontologyCount, objectTypeCount }: StatsBarProps) {
  return (
    <div className="flex items-stretch gap-3">
      <StatItem
        icon={
          <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="#F59E0B" strokeWidth="1.5">
            <path d="M4 7a1 1 0 011-1h14a1 1 0 011 1v2a1 1 0 01-1 1H5a1 1 0 01-1-1V7zM4 15a1 1 0 011-1h14a1 1 0 011 1v2a1 1 0 01-1 1H5a1 1 0 01-1-1v-2z" />
          </svg>
        }
        label="Ontologies"
        value={ontologyCount}
        color="#F59E0B"
        delay={0}
      />
      <StatItem
        icon={
          <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="#14B8A6" strokeWidth="1.5">
            <rect x="3" y="3" width="7" height="7" rx="1" />
            <rect x="14" y="3" width="7" height="7" rx="1" />
            <rect x="3" y="14" width="7" height="7" rx="1" />
            <rect x="14" y="14" width="7" height="7" rx="1" />
          </svg>
        }
        label="Object Types"
        value={objectTypeCount}
        color="#14B8A6"
        delay={80}
      />
    </div>
  );
}
