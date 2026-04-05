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
  search: 'M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z',
  compass: 'M12 2a10 10 0 100 20 10 10 0 000-20zm0 0l4.5 4.5L12 12l-4.5-4.5L12 2z',
  zap: 'M13 2L3 14h9l-1 10 10-12h-9l1-10z',
  'bar-chart': 'M12 20V10M18 20V4M6 20v-4',
};

function SvgIcon({ name }: { name: string }) {
  const d = iconPaths[name];
  if (!d) return null;
  return (
    <svg
      className="w-5 h-5 flex-shrink-0"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.75"
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
      className={`relative flex flex-col border-r transition-all duration-300 ${collapsed ? 'w-14' : 'w-52'}`}
      style={{
        background: 'rgba(13, 17, 23, 0.80)',
        backdropFilter: 'blur(20px)',
        WebkitBackdropFilter: 'blur(20px)',
        borderColor: 'rgba(31, 41, 55, 0.5)',
      }}
    >
      {/* Logo row */}
      <div
        className="flex items-center h-12 px-3 border-b"
        style={{ borderColor: 'rgba(31, 41, 55, 0.5)' }}
      >
        <button
          onClick={toggle}
          className="font-sans font-semibold text-sm tracking-widest transition-colors hover:opacity-80"
          style={{
            background: 'linear-gradient(90deg, #F59E0B, #14B8A6)',
            WebkitBackgroundClip: 'text',
            WebkitTextFillColor: 'transparent',
            backgroundClip: 'text',
          }}
          aria-label="Toggle sidebar"
        >
          {collapsed ? 'W' : 'WEAVE'}
        </button>
      </div>

      {/* Nav */}
      <nav className="flex-1 py-2">
        {navItems.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.to === '/'}
            className={({ isActive }) =>
              `relative flex items-center gap-3 px-3 py-2.5 text-sm transition-all duration-200 ${
                isActive
                  ? 'text-text-primary bg-bg-tertiary'
                  : 'text-text-secondary hover:text-text-primary hover:bg-bg-tertiary'
              }`
            }
          >
            {({ isActive }) => (
              <>
                {/* Active left-border indicator */}
                <span
                  className="absolute left-0 top-0 bottom-0 w-[3px] rounded-r transition-all duration-200"
                  style={{
                    background: isActive ? '#F59E0B' : 'transparent',
                    boxShadow: isActive ? '0 0 8px rgba(245,158,11,0.6)' : 'none',
                  }}
                />
                <span
                  className="transition-transform duration-200"
                  style={{
                    transform: isActive ? 'scale(1.1)' : 'scale(1)',
                    filter: isActive
                      ? 'drop-shadow(0 0 4px rgba(245,158,11,0.5))'
                      : 'none',
                  }}
                >
                  <SvgIcon name={item.icon} />
                </span>
                {!collapsed && <span>{item.label}</span>}
              </>
            )}
          </NavLink>
        ))}
      </nav>

      {/* Bottom fade */}
      <div
        className="pointer-events-none absolute bottom-0 left-0 right-0 h-16"
        style={{
          background:
            'linear-gradient(to top, rgba(8,11,22,0.9) 0%, transparent 100%)',
        }}
      />
    </aside>
  );
}
