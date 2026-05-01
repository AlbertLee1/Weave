/* Weave Service Worker (US-354).
 *
 * Strategy:
 *   - Precache the SPA shell (`/`, `/index.html`) on install so cold loads
 *     work offline. The hashed Vite asset bundles handle themselves via
 *     the runtime cache below.
 *   - Network-first for navigation requests, falling back to the cached
 *     shell when offline.
 *   - Stale-while-revalidate for same-origin GETs to `/assets/`, the
 *     favicon, and other static resources.
 *   - Bypass non-GET requests entirely (mutations must hit the network).
 *
 * Versioned cache name: bumping `CACHE_VERSION` invalidates all entries
 * on the next activation and `clients.claim()` ensures the new SW is in
 * charge of pages on first activation.
 */
const CACHE_VERSION = 'weave-v1';
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

self.addEventListener('fetch', (event) => {
  const req = event.request;
  if (req.method !== 'GET') return;
  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return;

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

  // Everything else (API calls): pass through. The SPA's TanStack Query
  // layer + offlineCache handle data caching at the application level.
});
