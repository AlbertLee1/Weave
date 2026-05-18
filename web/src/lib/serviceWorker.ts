// Service worker registration helper (US-354 + US-452). Registers
// `/sw.js` once on boot when the browser supports SW. Stays a no-op in
// dev (Vite serves the app via HMR and a SW would interfere with module
// reloads) and in test (jsdom has no `navigator.serviceWorker`) —
// production-only by design.
//
// The actual SW lives at `web/public/sw.js` and ships verbatim through
// Vite's static copy step. Keep this file framework-agnostic so it can
// be imported from `main.tsx` without pulling in React.
import { replayQueue, startAutoReplay } from './offlineRequestQueue';
import { executeQueuedEntry } from './queuedRequest';

export interface ReplayHintMessage {
  type: 'weave/offline-replay-hint';
  method?: string;
  path?: string;
  via?: string;
}

function isReplayHint(data: unknown): data is ReplayHintMessage {
  return (
    !!data &&
    typeof data === 'object' &&
    (data as { type?: unknown }).type === 'weave/offline-replay-hint'
  );
}

/**
 * Wires the SW → page replay hint channel onto the offline-request
 * queue. The SW broadcasts `weave/offline-replay-hint` whenever a
 * mutation hits a network failure or the browser's SyncManager fires
 * the `weave-offline-replay` tag. Every hint triggers a queue drain in
 * the page context, where the auth token + branch scoping live. Called
 * once from `main.tsx`; safe to invoke in tests via `target` injection.
 */
export function attachServiceWorkerReplayBridge(
  target: EventTarget | null = typeof navigator !== 'undefined' &&
    'serviceWorker' in navigator
    ? (navigator.serviceWorker as unknown as EventTarget)
    : null,
): () => void {
  if (!target) return () => {};
  const handler = (event: Event) => {
    const data = (event as MessageEvent).data;
    if (!isReplayHint(data)) return;
    void replayOnce();
  };
  target.addEventListener('message', handler);
  return () => target.removeEventListener('message', handler);
}

let replayInFlight: Promise<void> | null = null;
async function replayOnce(): Promise<void> {
  if (replayInFlight) return replayInFlight;
  replayInFlight = (async () => {
    try {
      await replayQueue(executeQueuedEntry);
    } finally {
      replayInFlight = null;
    }
  })();
  return replayInFlight;
}

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
      .then(() => {
        attachServiceWorkerReplayBridge();
        // Wire the fallback online-event drain too — browsers without
        // SyncManager support never fire the `sync` hook in the SW, so
        // the page-level `online` listener is the only signal.
        startAutoReplay(executeQueuedEntry);
      })
      .catch(() => {
        // Registration failures are non-fatal — the app continues
        // without offline asset caching. Don't bubble to the error
        // reporter; SW failures are usually deployment / CSP
        // misconfiguration the user can't act on.
      });
  });
}
