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
      className="flex items-center h-12 px-6 bg-bg-secondary border-b border-border"
    >
      <div className="flex items-center gap-2 text-sm font-mono">
        {breadcrumbs.map((crumb, i) => (
          <span key={i} className="flex items-center gap-2">
            {i > 0 && <span className="text-text-muted">/</span>}
            <span
              className={
                i === breadcrumbs.length - 1
                  ? 'text-text-primary'
                  : 'text-text-secondary'
              }
            >
              {crumb}
            </span>
          </span>
        ))}
      </div>
    </header>
  );
}
