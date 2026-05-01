import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './styles/index.css'
import App from './App.tsx'
import { applyThemeToDocument, readPersistedTheme } from './hooks/useTheme'

// Apply the persisted theme synchronously before React mounts so the first
// paint already carries the right palette — avoids a dark→light flash for
// users who chose `light`.
applyThemeToDocument(readPersistedTheme())

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
