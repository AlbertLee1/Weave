import { NavLink } from 'react-router';
import { useOntologyStore } from '../../stores/ontologyStore';

const navItems = [
  { to: '/', label: 'Dashboard', icon: 'grid' },
  { to: '/admin', label: 'Admin', icon: 'settings' },
];

const iconPaths: Record<string, string> = {
  grid: 'M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z',
  settings:
    'M12 15a3 3 0 100-6 3 3 0 000 6zM19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 01-2.83 2.83l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06A1.65 1.65 0 004.68 15a1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06A1.65 1.65 0 009 4.68a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06A1.65 1.65 0 0019.4 9a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z',
  search:
    'M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z',
  compass:
    'M12 2a10 10 0 100 20 10 10 0 000-20zm0 0l4.5 4.5L12 12l-4.5-4.5L12 2z',
  zap: 'M13 2L3 14h9l-1 10 10-12h-9l1-10z',
  'bar-chart':
    'M12 20V10M18 20V4M6 20v-4',
};

function SvgIcon({ name }: { name: string }) {
  const d = iconPaths[name];
  if (!d) return null;
  return (
    <svg
      className="w-4 h-4"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d={d} />
    </svg>
  );
}

export function Sidebar() {
  const collapsed = useOntologyStore((s) => s.sidebarCollapsed);
  const toggle = useOntologyStore((s) => s.toggleSidebar);

  return (
    <aside
      data-testid="sidebar"
      className={`flex flex-col bg-bg-secondary border-r border-border transition-all ${collapsed ? 'w-14' : 'w-52'}`}
    >
      <div className="flex items-center h-12 px-3 border-b border-border">
        <button
          onClick={toggle}
          className="text-accent-cyan font-mono font-bold text-sm hover:text-text-primary transition-colors"
          aria-label="Toggle sidebar"
        >
          {collapsed ? 'W' : 'WEAVE'}
        </button>
      </div>

      <nav className="flex-1 py-2">
        {navItems.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.to === '/'}
            className={({ isActive }) =>
              `flex items-center gap-3 px-3 py-2 text-sm transition-colors ${
                isActive
                  ? 'text-accent-cyan bg-bg-tertiary'
                  : 'text-text-secondary hover:text-text-primary hover:bg-bg-tertiary'
              }`
            }
          >
            <SvgIcon name={item.icon} />
            {!collapsed && <span>{item.label}</span>}
          </NavLink>
        ))}
      </nav>
    </aside>
  );
}
