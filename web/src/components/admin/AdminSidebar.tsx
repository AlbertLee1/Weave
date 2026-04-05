interface AdminSidebarProps {
  activeSection: string;
  onSectionChange: (section: string) => void;
  counts?: { objectTypes?: number; linkTypes?: number; actionTypes?: number };
}

const sections = [
  {
    key: 'objectTypes',
    label: 'Object Types',
    icon: (
      <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
        <rect x="3" y="3" width="7" height="7" rx="1" />
        <rect x="14" y="3" width="7" height="7" rx="1" />
        <rect x="3" y="14" width="7" height="7" rx="1" />
        <rect x="14" y="14" width="7" height="7" rx="1" />
      </svg>
    ),
  },
  {
    key: 'linkTypes',
    label: 'Link Types',
    icon: (
      <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
        <path d="M10 13a5 5 0 007.54.54l3-3a5 5 0 00-7.07-7.07l-1.72 1.71" />
        <path d="M14 11a5 5 0 00-7.54-.54l-3 3a5 5 0 007.07 7.07l1.71-1.71" />
      </svg>
    ),
  },
  {
    key: 'actionTypes',
    label: 'Action Types',
    icon: (
      <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
        <path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z" />
      </svg>
    ),
  },
] as const;

export function AdminSidebar({ activeSection, onSectionChange, counts }: AdminSidebarProps) {
  return (
    <div className="w-48 bg-bg-secondary border-r border-border flex flex-col py-2">
      {sections.map((section) => {
        const isActive = activeSection === section.key;
        const count = counts?.[section.key as keyof NonNullable<AdminSidebarProps['counts']>];
        return (
          <button
            key={section.key}
            onClick={() => onSectionChange(section.key)}
            className={`flex items-center gap-2 px-3 py-2 text-sm transition-colors ${
              isActive
                ? 'text-accent-cyan bg-bg-tertiary'
                : 'text-text-secondary hover:text-text-primary hover:bg-bg-tertiary/50'
            }`}
          >
            {section.icon}
            <span>{section.label}</span>
            {count !== undefined && (
              <span className="ml-auto text-xs text-text-muted">{count}</span>
            )}
          </button>
        );
      })}
    </div>
  );
}
