import { useEffect } from 'react';
import { useLocation } from 'react-router';
import { resolveRouteTitle } from '../../hooks/useRouteTitle';

/**
 * RouteTitle — keeps `document.title` in sync with the active route so
 * browser tabs / history entries are distinguishable and screen readers
 * announce a meaningful page title on navigation (WCAG 2.4.2, Page
 * Titled).
 *
 * Renders nothing. Must be mounted inside a Router (it reads the current
 * pathname via `useLocation()`). The pathname → title mapping lives in
 * `resolveRouteTitle` so it stays a single, testable source of truth.
 */
export function RouteTitle(): null {
  const { pathname } = useLocation();

  useEffect(() => {
    document.title = resolveRouteTitle(pathname);
  }, [pathname]);

  return null;
}
