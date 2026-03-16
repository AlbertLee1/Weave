import { create } from 'zustand';

interface OntologyStoreState {
  selectedOntology: string | null;
  selectedObjectType: string | null;
  sidebarCollapsed: boolean;
  setSelectedOntology: (apiName: string | null) => void;
  setSelectedObjectType: (apiName: string | null) => void;
  toggleSidebar: () => void;
}

export const useOntologyStore = create<OntologyStoreState>((set) => ({
  selectedOntology: null,
  selectedObjectType: null,
  sidebarCollapsed: false,
  setSelectedOntology: (apiName) =>
    set({ selectedOntology: apiName, selectedObjectType: null }),
  setSelectedObjectType: (apiName) => set({ selectedObjectType: apiName }),
  toggleSidebar: () =>
    set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),
}));
