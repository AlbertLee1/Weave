// US-352: Global / route-scoped error boundary.
//
// We layer the boundary twice:
//   - GlobalErrorBoundary wraps the entire app shell. If something inside the
//     React provider tree itself throws (auth, query client, router init), we
//     fall back to a stand-alone full-screen error page that does NOT depend
//     on i18n / Shell so it never fails to render.
//   - RouteErrorBoundary wraps the routed page content INSIDE the Shell. A
//     page-level crash leaves the sidebar / topbar usable so the user can
//     navigate away.
//
// Both boundaries delegate to react-error-boundary's <ErrorBoundary> and
// invoke `reportError` on every catch. Resetting the boundary state is
// exposed via the fallback's "Retry" button (resetErrorBoundary) and via a
// `resetKeys` prop so route changes auto-recover.
import { ErrorBoundary as ReactErrorBoundary } from 'react-error-boundary';
import type { FallbackProps } from 'react-error-boundary';
import { useTranslation } from 'react-i18next';
import { useLocation } from 'react-router';
import { reportError } from '../../lib/errorReporter';

interface FallbackUIProps extends FallbackProps {
  variant: 'global' | 'route';
}

function FallbackUI({ error, resetErrorBoundary, variant }: FallbackUIProps) {
  // Avoid useTranslation in the GLOBAL variant — if i18n itself crashed, the
  // hook would re-throw and the outermost boundary would have nothing to
  // render. The global variant uses hardcoded bilingual fallback strings so
  // it is provably crash-resistant.
  const isGlobal = variant === 'global';
  return (
    <div
      role="alert"
      aria-live="assertive"
      className={
        isGlobal
          ? 'fixed inset-0 z-[100] flex items-center justify-center bg-bg-primary text-text-primary p-6'
          : 'flex flex-col items-center justify-center py-20 text-center'
      }
      data-testid={isGlobal ? 'global-error-fallback' : 'route-error-fallback'}
    >
      <div className="max-w-lg w-full rounded-2xl border border-border-primary bg-bg-secondary p-8 shadow-lg">
        <div className="mb-4 inline-flex h-12 w-12 items-center justify-center rounded-full bg-red-500/10 text-red-500">
          <svg
            aria-hidden="true"
            xmlns="http://www.w3.org/2000/svg"
            width="24"
            height="24"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
            <line x1="12" y1="9" x2="12" y2="13" />
            <line x1="12" y1="17" x2="12.01" y2="17" />
          </svg>
        </div>
        {isGlobal ? (
          <GlobalFallbackText error={error} />
        ) : (
          <RouteFallbackText error={error} />
        )}
        <div className="mt-6 flex flex-wrap gap-3">
          <button
            type="button"
            onClick={resetErrorBoundary}
            className="px-4 py-2 rounded-md bg-amber-500 text-white text-sm font-medium hover:bg-amber-600 focus:outline-none focus-visible:ring-2 focus-visible:ring-amber-400"
          >
            {isGlobal ? '重试 / Retry' : <RetryLabel />}
          </button>
          {isGlobal ? (
            <button
              type="button"
              onClick={() => {
                if (typeof window !== 'undefined') window.location.reload();
              }}
              className="px-4 py-2 rounded-md border border-border-primary bg-bg-tertiary text-text-primary text-sm font-medium hover:bg-bg-secondary focus:outline-none focus-visible:ring-2 focus-visible:ring-amber-400"
            >
              刷新页面 / Reload page
            </button>
          ) : null}
        </div>
      </div>
    </div>
  );
}

function GlobalFallbackText({ error }: { error: unknown }) {
  const message = error instanceof Error ? error.message : String(error);
  return (
    <>
      <h1 className="text-xl font-semibold text-text-primary">应用发生错误 / Something went wrong</h1>
      <p className="mt-2 text-sm text-text-secondary">
        我们已记录此错误。请重试或刷新页面。 / We have logged the error. Please retry or reload the page.
      </p>
      {message ? (
        <pre
          data-testid="error-message"
          className="mt-4 max-h-40 overflow-auto rounded-md bg-bg-tertiary p-3 text-xs text-text-secondary whitespace-pre-wrap break-all"
        >
          {message}
        </pre>
      ) : null}
    </>
  );
}

function RouteFallbackText({ error }: { error: unknown }) {
  const { t } = useTranslation();
  const message = error instanceof Error ? error.message : String(error);
  return (
    <>
      <h2 className="text-lg font-semibold text-text-primary">{t('errorBoundary.title')}</h2>
      <p className="mt-2 text-sm text-text-secondary">{t('errorBoundary.description')}</p>
      {message ? (
        <pre
          data-testid="error-message"
          className="mt-4 max-h-40 overflow-auto rounded-md bg-bg-tertiary p-3 text-xs text-text-secondary whitespace-pre-wrap break-all"
        >
          {message}
        </pre>
      ) : null}
    </>
  );
}

function RetryLabel() {
  const { t } = useTranslation();
  return <>{t('common.retry')}</>;
}

function handleBoundaryError(error: unknown, info: { componentStack?: string | null }) {
  reportError(error, info.componentStack ?? undefined);
}

interface GlobalErrorBoundaryProps {
  children: React.ReactNode;
}

/**
 * Outermost boundary — sits OUTSIDE the React Query / Router / Auth providers
 * so a crash inside any provider still renders a fallback. The fallback UI
 * intentionally avoids hooks that depend on those providers.
 */
export function GlobalErrorBoundary({ children }: GlobalErrorBoundaryProps) {
  return (
    <ReactErrorBoundary
      onError={handleBoundaryError}
      fallbackRender={(props) => <FallbackUI {...props} variant="global" />}
    >
      {children}
    </ReactErrorBoundary>
  );
}

interface RouteErrorBoundaryProps {
  children: React.ReactNode;
}

/**
 * Per-route boundary mounted INSIDE the Shell. A page crash leaves the
 * sidebar / topbar usable. The boundary auto-resets on pathname changes via
 * `resetKeys` so the user navigating away clears the error state.
 */
export function RouteErrorBoundary({ children }: RouteErrorBoundaryProps) {
  const location = useLocation();
  return (
    <ReactErrorBoundary
      onError={handleBoundaryError}
      resetKeys={[location.pathname]}
      fallbackRender={(props) => <FallbackUI {...props} variant="route" />}
    >
      {children}
    </ReactErrorBoundary>
  );
}
