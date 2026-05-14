import { Link, useLocation } from 'react-router';
import { EmptyState } from './EmptyState';

// Dogfood round 2: catch-all 404 so unknown URLs (e.g. stale bookmarks
// or label-slug guesses like /aip-threads before redirect aliases were
// added) render an informative page instead of a blank white screen.
export function NotFoundPage() {
  const { pathname } = useLocation();
  return (
    <div className="p-8" data-testid="not-found-page">
      <EmptyState
        title="Page not found"
        description={`No page is registered at ${pathname}. Use the sidebar to navigate, or jump back to the Dashboard.`}
        action={
          <Link
            to="/"
            data-testid="not-found-dashboard-link"
            className="inline-flex items-center px-4 py-2 text-sm font-medium rounded-md text-text-primary bg-bg-tertiary hover:bg-bg-elevated transition-colors"
          >
            Back to Dashboard
          </Link>
        }
      />
    </div>
  );
}
