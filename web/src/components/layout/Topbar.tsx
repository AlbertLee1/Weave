import { useLocation } from 'react-router';

function pathToBreadcrumbs(pathname: string): string[] {
  const segments = pathname.split('/').filter(Boolean);
  if (segments.length === 0) return ['Dashboard'];
  return segments.map((s) => s.charAt(0).toUpperCase() + s.slice(1));
}

export function Topbar() {
  const location = useLocation();
  const breadcrumbs = pathToBreadcrumbs(location.pathname);

  return (
    <header
      data-testid="topbar"
      className="relative flex items-center h-12 px-6 border-b"
      style={{
        background: 'rgba(13, 17, 23, 0.60)',
        backdropFilter: 'blur(12px)',
        WebkitBackdropFilter: 'blur(12px)',
        borderColor: 'rgba(31, 41, 55, 0.30)',
      }}
    >
      <div className="flex items-center gap-1.5 text-sm font-sans">
        {breadcrumbs.map((crumb, i) => (
          <span key={i} className="flex items-center gap-1.5">
            {i > 0 && (
              <span
                className="select-none"
                style={{ color: 'rgba(75, 85, 99, 0.6)', fontSize: '0.75rem' }}
              >
                /
              </span>
            )}
            <span
              className={
                i === breadcrumbs.length - 1
                  ? 'text-text-primary font-medium'
                  : 'text-text-secondary'
              }
            >
              {crumb}
            </span>
          </span>
        ))}
      </div>

      {/* Subtle gradient line below */}
      <div
        className="absolute bottom-0 left-0 right-0 h-px"
        style={{
          background:
            'linear-gradient(90deg, transparent 0%, rgba(245,158,11,0.15) 30%, rgba(20,184,166,0.15) 70%, transparent 100%)',
        }}
      />
    </header>
  );
}
