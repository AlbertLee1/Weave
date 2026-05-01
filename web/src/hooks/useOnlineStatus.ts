import { useEffect, useState } from 'react';

// useOnlineStatus (US-354). Tracks `navigator.onLine` and updates on the
// browser's `online` / `offline` events. SSR-safe: the initial value is
// `true` when `navigator` is undefined so the offline banner does not
// flash during pre-hydration on the (currently hypothetical) SSR path.
//
// `navigator.onLine === false` is a strong signal of "no network" but
// `navigator.onLine === true` does NOT guarantee a working backend (the
// browser only knows about the OS-level link). Treat this hook as the
// gate for the offline indicator; rely on actual fetch failures to flip
// any optimistic UI back to "stale".
export function useOnlineStatus(): boolean {
  const [online, setOnline] = useState<boolean>(() => {
    if (typeof navigator === 'undefined') return true;
    return navigator.onLine;
  });

  useEffect(() => {
    if (typeof window === 'undefined') return;
    const handleOnline = () => setOnline(true);
    const handleOffline = () => setOnline(false);
    window.addEventListener('online', handleOnline);
    window.addEventListener('offline', handleOffline);
    return () => {
      window.removeEventListener('online', handleOnline);
      window.removeEventListener('offline', handleOffline);
    };
  }, []);

  return online;
}
