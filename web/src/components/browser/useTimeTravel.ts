import { useTimeTravelStore } from '../../stores/timeTravelStore';

// US-048: subscribes to the live store so any component reading this
// hook re-renders when the user toggles Time Travel on/off for the
// active ontology. Lives in its own module (not inside
// TimeTravelToolbar.tsx) so `react-refresh/only-export-components`
// stays happy — toolbar.tsx then exports only the component.
export function useTimeTravelActive(
  ontologyApiName: string | null | undefined,
): boolean {
  return useTimeTravelStore((s) => {
    if (!ontologyApiName) return false;
    return (s.selections[ontologyApiName] ?? '').trim().length > 0;
  });
}

// useTimeTravelAsOf returns the active asOf string (tx-... or RFC3339)
// for the given ontology, or '' when no time-travel is active. Used by
// UI surfaces that want to render the active tx id in a banner / hint.
export function useTimeTravelAsOf(
  ontologyApiName: string | null | undefined,
): string {
  return useTimeTravelStore((s) => {
    if (!ontologyApiName) return '';
    return (s.selections[ontologyApiName] ?? '').trim();
  });
}
