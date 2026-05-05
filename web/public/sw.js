/* Weave Service Worker (US-354 + US-452).
 *
 * Strategy:
 *   - Precache the SPA shell (`/`, `/index.html`) on install so cold loads
 *     work offline. The hashed Vite asset bundles handle themselves via
 *     the runtime cache below.
 *   - Network-first for navigation requests, falling back to the cached
 *     shell when offline.
 *   - Stale-while-revalidate for same-origin GETs to `/assets/`, the
 *     favicon, and other static resources.
 *   - For mutations against `/api/` (POST/PUT/PATCH/DELETE), forward to
 *     the network. On a network failure the SW posts a `replay-queue`
 *     message to every controlled client so the SPA can persist the
 *     mutation through `offlineRequestQueue` (US-452) and retry on the
 *     next `online` event. We do NOT mirror Workbox's in-SW Background
 *     Sync queue here because the request body has already been consumed
 *     by the caller and the client-side queue owns auth + branch
 *     scoping that the SW cannot reproduce safely.
 *
 * Versioned cache name: bumping `CACHE_VERSION` invalidates all entries
 * on the next activation and `clients.claim()` ensures the new SW is in
 * charge of pages on first activation.
 */
const CACHE_VERSION = 'weave-v2';
const SHELL_CACHE = `${CACHE_VERSION}-shell`;
const RUNTIME_CACHE = `${CACHE_VERSION}-runtime`;
const SHELL_ASSETS = ['/', '/index.html', '/favicon.svg'];

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches
      .open(SHELL_CACHE)
      .then((cache) => cache.addAll(SHELL_ASSETS))
      .then(() => self.skipWaiting()),
  );
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((names) =>
        Promise.all(
          names
            .filter((n) => !n.startsWith(CACHE_VERSION))
            .map((n) => caches.delete(n)),
        ),
      )
      .then(() => self.clients.claim()),
  );
});

async function broadcastReplayHint(payload) {
  const clients = await self.clients.matchAll({ includeUncontrolled: true });
  for (const client of clients) {
    try {
      client.postMessage({ type: 'weave/offline-replay-hint', ...payload });
    } catch {
      // postMessage can throw if the client is gone; ignore.
    }
  }
}

self.addEventListener('fetch', (event) => {
  const req = event.request;
  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return;

  // Mutations: pass through but signal the SPA on network failure so the
  // client-side replay queue (US-452) can take over.
  if (req.method !== 'GET') {
    if (url.pathname.startsWith('/api/')) {
      event.respondWith(
        fetch(req.clone()).catch((err) => {
          broadcastReplayHint({ method: req.method, path: url.pathname });
          throw err;
        }),
      );
    }
    return;
  }

  // SPA navigations: try network, fall back to cached shell.
  if (req.mode === 'navigate') {
    event.respondWith(
      fetch(req)
        .then((resp) => {
          const clone = resp.clone();
          caches.open(SHELL_CACHE).then((c) => c.put('/', clone));
          return resp;
        })
        .catch(() =>
          caches.match('/').then((cached) => cached || caches.match('/index.html')),
        ),
    );
    return;
  }

  // Static assets: stale-while-revalidate.
  if (
    url.pathname.startsWith('/assets/') ||
    url.pathname.endsWith('.svg') ||
    url.pathname.endsWith('.css') ||
    url.pathname.endsWith('.js') ||
    url.pathname.endsWith('.woff2')
  ) {
    event.respondWith(
      caches.open(RUNTIME_CACHE).then((cache) =>
        cache.match(req).then((cached) => {
          const network = fetch(req)
            .then((resp) => {
              if (resp.ok) cache.put(req, resp.clone());
              return resp;
            })
            .catch(() => cached);
          return cached || network;
        }),
      ),
    );
    return;
  }

  // Everything else (API GET): pass through. The SPA's TanStack Query
  // layer + offlineCache handle data caching at the application level.
});

// Background Sync (Workbox-style hook). Browsers that implement the
// SyncManager API will fire this when connectivity returns; we use it as
// an additional nudge to the SPA to drain the queue. The actual replay
// still runs in the page context because the SW does not have access to
// the auth token used by `request()`.
self.addEventListener('sync', (event) => {
  if (event.tag === 'weave-offline-replay') {
    event.waitUntil(broadcastReplayHint({ via: 'sync' }));
  }
});
