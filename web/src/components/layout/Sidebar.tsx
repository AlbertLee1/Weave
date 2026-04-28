import { NavLink, useParams } from 'react-router';
import { useOntologyStore } from '../../stores/ontologyStore';
import { useAuth } from '../../auth/useAuth';

const iconPaths: Record<string, string> = {
  grid: 'M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z',
  settings:
    'M12 15a3 3 0 100-6 3 3 0 000 6zM19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 01-2.83 2.83l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06A1.65 1.65 0 004.68 15a1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06A1.65 1.65 0 009 4.68a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06A1.65 1.65 0 0019.4 9a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z',
  search: 'M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z',
  compass: 'M12 2a10 10 0 100 20 10 10 0 000-20zm0 0l4.5 4.5L12 12l-4.5-4.5L12 2z',
  zap: 'M13 2L3 14h9l-1 10 10-12h-9l1-10z',
  'bar-chart': 'M12 20V10M18 20V4M6 20v-4',
  layers:
    'M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5',
  code: 'M8 6l-6 6 6 6M16 6l6 6-6 6M14 4l-4 16',
  link: 'M10 13a5 5 0 007.54.54l3-3a5 5 0 00-7.07-7.07l-1.72 1.71M14 11a5 5 0 00-7.54-.54l-3 3a5 5 0 007.07 7.07l1.71-1.71',
  bolt: 'M13 2L3 14h9l-1 10 10-12h-9l1-10z',
  share: 'M4 12v8a2 2 0 002 2h12a2 2 0 002-2v-8M16 6l-4-4-4 4M12 2v14',
  graph: 'M18 5a2 2 0 11-4 0 2 2 0 014 0zM10 19a2 2 0 11-4 0 2 2 0 014 0zM22 19a2 2 0 11-4 0 2 2 0 014 0zM8.5 17.5L14 7M15.5 7.5L20 18',
  clock: 'M12 8v5l3 2M21 12a9 9 0 11-18 0 9 9 0 0118 0z',
  upload: 'M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4M17 8l-5-5-5 5M12 3v12',
  check:
    'M22 11.08V12a10 10 0 11-5.93-9.14M22 4L12 14.01l-3-3',
  shield: 'M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z',
  chat: 'M21 15a2 2 0 01-2 2H7l-4 4V5a2 2 0 012-2h14a2 2 0 012 2z',
  workflow:
    'M5 5h4v4H5zM15 5h4v4h-4zM5 15h4v4H5zM15 15h4v4h-4zM9 7h6M7 9v6M17 9v6M9 17h6',
  pipeline:
    'M3 7h6M15 7h6M3 17h6M15 17h6M9 7a3 3 0 003 3 3 3 0 003-3M9 17a3 3 0 013-3 3 3 0 013 3M12 10v4',
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

interface NavItem {
  to: string;
  label: string;
  icon: string;
}

function SidebarLink({ item, collapsed }: { item: NavItem; collapsed: boolean }) {
  return (
    <NavLink
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
  );
}

export function Sidebar() {
  const collapsed = useOntologyStore((s) => s.sidebarCollapsed);
  const toggle = useOntologyStore((s) => s.toggleSidebar);
  const selectedOntology = useOntologyStore((s) => s.selectedOntology);
  const params = useParams();
  const { user, can } = useAuth();
  const activeOntology =
    (params.ontology as string | undefined) ?? selectedOntology ?? null;

  const showAdminSection =
    user !== null &&
    (user.roles.some((r) => r === 'admin' || r === 'ontology-owner') ||
      Object.values(user.ontologyRoles ?? {}).some(
        (r) => r === 'admin' || r === 'ontology-owner',
      ) ||
      can('ontology.write'));

  const navItems: NavItem[] = [
    { to: '/', label: 'Dashboard', icon: 'grid' },
    { to: '/dashboards', label: 'Dashboards', icon: 'bar-chart' },
    {
      to: activeOntology ? `/objectsets/${activeOntology}` : '/',
      label: 'Query Builder',
      icon: 'layers',
    },
    { to: '/threads', label: 'AIP Threads', icon: 'chat' },
    { to: '/logic-flows', label: 'AIP Logic', icon: 'workflow' },
    { to: '/pipelines', label: 'Pipelines', icon: 'pipeline' },
    { to: '/developer/playground', label: 'API Playground', icon: 'code' },
    { to: '/developer/metrics', label: 'API Metrics', icon: 'bar-chart' },
  ];

  if (activeOntology) {
    navItems.push({
      to: `/import/${activeOntology}`,
      label: 'Import Data',
      icon: 'upload',
    });
    navItems.push({
      to: `/approvals/${activeOntology}`,
      label: 'Approvals',
      icon: 'check',
    });
    navItems.push({
      to: `/actions/${activeOntology}/history`,
      label: 'Action History',
      icon: 'clock',
    });
  }
  navItems.push({
    to: '/schema/infer',
    label: 'Schema Inference',
    icon: 'compass',
  });

  const adminItems: NavItem[] = activeOntology
    ? [
        {
          to: `/admin/${activeOntology}/objectTypes`,
          label: 'Object Types',
          icon: 'settings',
        },
        {
          to: `/admin/${activeOntology}/linkTypes`,
          label: 'Link Types',
          icon: 'link',
        },
        {
          to: `/admin/${activeOntology}/actionTypes`,
          label: 'Action Types',
          icon: 'bolt',
        },
        {
          to: `/admin/${activeOntology}/interfaces`,
          label: 'Interfaces',
          icon: 'share',
        },
        {
          to: `/admin/${activeOntology}/graph`,
          label: 'Schema Graph',
          icon: 'graph',
        },
        {
          to: `/admin/${activeOntology}/history`,
          label: 'History',
          icon: 'clock',
        },
        {
          to: '/admin/markings',
          label: 'Markings',
          icon: 'shield',
        },
      ]
    : [
        {
          to: '/admin/markings',
          label: 'Markings',
          icon: 'shield',
        },
      ];

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
      <nav className="flex-1 py-2 overflow-y-auto">
        {navItems.map((item) => (
          <SidebarLink key={item.to} item={item} collapsed={collapsed} />
        ))}

        {showAdminSection && adminItems.length > 0 && (
          <div
            data-testid="sidebar-admin-section"
            aria-label="Admin"
            className="mt-4 pt-3 border-t"
            style={{ borderColor: 'rgba(31,41,55,0.5)' }}
          >
            {!collapsed && (
              <div className="px-3 pb-1 text-[10px] font-semibold uppercase tracking-widest text-text-secondary">
                Admin
              </div>
            )}
            {adminItems.map((item) => (
              <SidebarLink key={item.to} item={item} collapsed={collapsed} />
            ))}
          </div>
        )}
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
