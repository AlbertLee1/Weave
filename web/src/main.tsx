import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './styles/index.css'
import './i18n'
import App from './App.tsx'
import { applyThemeToDocument, readPersistedTheme } from './hooks/useTheme'
import { registerServiceWorker } from './lib/serviceWorker'

// Apply the persisted theme synchronously before React mounts so the first
// paint already carries the right palette — avoids a dark→light flash for
// users who chose `light`.
applyThemeToDocument(readPersistedTheme())

// Register the offline-shell service worker (US-354). No-op in dev / test;
// production-only registration so HMR + jsdom stay unaffected.
registerServiceWorker()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
