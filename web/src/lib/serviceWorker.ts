// Service worker registration helper (US-354). Registers `/sw.js` once on
// boot when the browser supports SW. Stays a no-op in dev (Vite serves the
// app via HMR and a SW would interfere with module reloads) and in test
// (jsdom has no `navigator.serviceWorker`) — production-only by design.
//
// The actual SW lives at `web/public/sw.js` and ships verbatim through
// Vite's static copy step. Keep this file framework-agnostic so it can be
// imported from `main.tsx` without pulling in React.
export function registerServiceWorker(): void {
  if (typeof navigator === 'undefined') return;
  if (!('serviceWorker' in navigator)) return;
  if (typeof window === 'undefined') return;
  // Only register when the app is served from production-style assets;
  // Vite's dev server marks the page with `import.meta.env.DEV` truthy.
  // Falling through that guard during tests/dev keeps the SW from
  // intercepting fetches for HMR'd modules.
  try {
    if (import.meta.env?.DEV) return;
  } catch {
    // import.meta.env may be undefined in non-Vite test runners; treat
    // as production (the typeof navigator gate already covers tests).
  }
  window.addEventListener('load', () => {
    navigator.serviceWorker
      .register('/sw.js', { scope: '/' })
      .catch(() => {
        // Registration failures are non-fatal — the app continues without
        // offline asset caching. Don't bubble to the error reporter; SW
        // failures are usually deployment / CSP misconfiguration the
        // user can't act on.
      });
  });
}
