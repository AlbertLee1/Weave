import { useEffect } from 'react';

export function useCommandPaletteShortcut(toggle: () => void) {
  useEffect(() => {
    function handler(e: KeyboardEvent) {
      if (e.key !== 'k' && e.key !== 'K') return;
      if (!(e.metaKey || e.ctrlKey)) return;
      if (e.altKey || e.shiftKey) return;
      e.preventDefault();
      toggle();
    }
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [toggle]);
}
