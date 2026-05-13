import { create } from 'zustand';
import { persist } from 'zustand/middleware';

const TIME_TRAVEL_STORAGE_KEY = 'weave-active-time-travel';

// US-048: per-ontology time-travel selection. Holds either a `tx-...`
// dataset transaction id or an RFC3339 timestamp string — the same wire
// format the OSS `?asOf=` query parameter accepts (handler.go:258-266).
// Empty / blank values are normalised to "off" so the store mirrors the
// backend's no-asOf default.
interface TimeTravelStoreState {
  selections: Record<string, string>;
  setAsOf: (ontologyApiName: string, asOf: string) => void;
  clearAsOf: (ontologyApiName: string) => void;
}

export const useTimeTravelStore = create<TimeTravelStoreState>()(
  persist(
    (set) => ({
      selections: {},
      setAsOf: (ontologyApiName, asOf) =>
        set((state) => {
          const next = { ...state.selections };
          const normalised = (asOf ?? '').trim();
          if (normalised.length === 0) {
            delete next[ontologyApiName];
          } else {
            next[ontologyApiName] = normalised;
          }
          return { selections: next };
        }),
      clearAsOf: (ontologyApiName) =>
        set((state) => {
          if (!(ontologyApiName in state.selections)) return state;
          const next = { ...state.selections };
          delete next[ontologyApiName];
          return { selections: next };
        }),
    }),
    {
      name: TIME_TRAVEL_STORAGE_KEY,
      partialize: (state) => ({ selections: state.selections }),
    },
  ),
);

// activeAsOfFor is the synchronous read used by the API client so it
// does not have to depend on the React render cycle. Mirrors what a
// `getAsOf` selector would return, but is safe to call from any module.
// Returns an empty string when no time-travel is active for the
// ontology — the API client treats that as "do not inject ?asOf=".
export function activeAsOfFor(
  ontologyApiName: string | null | undefined,
): string {
  if (!ontologyApiName) return '';
  return (useTimeTravelStore.getState().selections[ontologyApiName] ?? '').trim();
}

// isTimeTravelActive is the convenience reader UI components use to
// disable mutation affordances (edit / delete / live toggle) when the
// page is rendering a historical snapshot.
export function isTimeTravelActive(
  ontologyApiName: string | null | undefined,
): boolean {
  return activeAsOfFor(ontologyApiName).length > 0;
}
