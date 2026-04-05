import { create } from 'zustand';
import { persist } from 'zustand/middleware';

const RECENTLY_VIEWED_KEY = 'weave-recently-viewed';
const MAX_RECENTLY_VIEWED = 10;

interface OntologyStoreState {
  selectedOntology: string | null;
  selectedObjectType: string | null;
  sidebarCollapsed: boolean;
  recentlyViewed: string[];
  setSelectedOntology: (apiName: string | null) => void;
  setSelectedObjectType: (apiName: string | null) => void;
  toggleSidebar: () => void;
  addRecentlyViewed: (rid: string) => void;
}

export const useOntologyStore = create<OntologyStoreState>()(
  persist(
    (set) => ({
      selectedOntology: null,
      selectedObjectType: null,
      sidebarCollapsed: false,
      recentlyViewed: [],
      setSelectedOntology: (apiName) =>
        set({ selectedOntology: apiName, selectedObjectType: null }),
      setSelectedObjectType: (apiName) => set({ selectedObjectType: apiName }),
      toggleSidebar: () =>
        set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),
      addRecentlyViewed: (rid) =>
        set((state) => {
          const filtered = state.recentlyViewed.filter((r) => r !== rid);
          return { recentlyViewed: [rid, ...filtered].slice(0, MAX_RECENTLY_VIEWED) };
        }),
    }),
    {
      name: RECENTLY_VIEWED_KEY,
      partialize: (state) => ({ recentlyViewed: state.recentlyViewed }),
    },
  ),
);
