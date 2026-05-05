import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './styles/index.css'
import './i18n'
import App from './App.tsx'
import { applyThemeToDocument, readPersistedTheme } from './hooks/useTheme'
import { registerServiceWorker } from './lib/serviceWorker'
import { startAutoReplay } from './lib/offlineRequestQueue'
import { executeQueuedEntry } from './lib/queuedRequest'

// Apply the persisted theme synchronously before React mounts so the first
// paint already carries the right palette — avoids a dark→light flash for
// users who chose `light`.
applyThemeToDocument(readPersistedTheme())

// Register the offline-shell service worker (US-354). No-op in dev / test;
// production-only registration so HMR + jsdom stay unaffected.
registerServiceWorker()

// Drain any mutations enqueued while offline (US-452) as soon as the
// browser fires `online`. The disposer is intentionally discarded — the
// listener lives for the lifetime of the SPA.
if (typeof window !== 'undefined') {
  startAutoReplay(executeQueuedEntry)
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
