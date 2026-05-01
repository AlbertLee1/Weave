// US-352: Client-side error reporting hook used by the route-level
// ErrorBoundary. The reporter has three layers:
//
//   1. console.error always fires (dev visibility, browser-devtools capture).
//   2. A custom reporter registered via setErrorReporter() — production builds
//      can wire Sentry / Datadog / a backend POST without touching the
//      ErrorBoundary call sites.
//   3. A best-effort POST to /api/client-errors when window.__WEAVE_ERROR_REPORT_URL__
//      is set. The endpoint is optional; failures are swallowed because we are
//      already in an error path and must not throw cascading errors.
//
// Keep this file framework-agnostic so it can be invoked from anywhere
// (mutation onError handlers, top-level fetch utilities, etc.) without
// pulling in React.

export interface ClientErrorPayload {
  message: string;
  stack?: string;
  componentStack?: string;
  userAgent: string;
  url: string;
  timestamp: string;
}

export type ClientErrorReporter = (payload: ClientErrorPayload) => void;

let customReporter: ClientErrorReporter | null = null;

/**
 * Register a custom reporter. Production builds can wire Sentry / Datadog /
 * a backend POST without modifying ErrorBoundary. Pass `null` to clear.
 */
export function setErrorReporter(reporter: ClientErrorReporter | null): void {
  customReporter = reporter;
}

function defaultReportUrl(): string | null {
  if (typeof window === 'undefined') return null;
  const w = window as unknown as { __WEAVE_ERROR_REPORT_URL__?: unknown };
  return typeof w.__WEAVE_ERROR_REPORT_URL__ === 'string' ? w.__WEAVE_ERROR_REPORT_URL__ : null;
}

/**
 * Report a client-side error via console.error, the registered custom
 * reporter (if any), and an optional best-effort POST to a configured
 * endpoint. Never throws.
 */
export function reportError(error: unknown, componentStack?: string): void {
  const err = error instanceof Error ? error : new Error(String(error));
  const payload: ClientErrorPayload = {
    message: err.message,
    stack: err.stack,
    componentStack,
    userAgent: typeof navigator !== 'undefined' ? navigator.userAgent : '',
    url: typeof window !== 'undefined' ? window.location.href : '',
    timestamp: new Date().toISOString(),
  };

  console.error('[ErrorBoundary] caught error:', err, componentStack ?? '');

  if (customReporter) {
    try {
      customReporter(payload);
    } catch {
      // Reporter must never escalate.
    }
  }

  const url = defaultReportUrl();
  if (url) {
    try {
      // navigator.sendBeacon is fire-and-forget and survives page unload.
      // Fall back to fetch when sendBeacon is unavailable (jsdom / older browsers).
      const body = JSON.stringify(payload);
      if (typeof navigator !== 'undefined' && typeof navigator.sendBeacon === 'function') {
        navigator.sendBeacon(url, new Blob([body], { type: 'application/json' }));
      } else if (typeof fetch === 'function') {
        // We intentionally do not await — error reporting must not block render.
        void fetch(url, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body,
          keepalive: true,
        }).catch(() => {
          /* swallowed: error path must not throw */
        });
      }
    } catch {
      // POST best-effort; never throw from the reporter.
    }
  }
}
