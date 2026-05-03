import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export const DEFAULT_BRANCH = 'main';

const BRANCH_STORAGE_KEY = 'weave-active-branch';

interface BranchStoreState {
  // Per-ontology branch selection. Key is the ontology apiName; value is
  // the branch id (default DEFAULT_BRANCH). A blank string is normalised to
  // DEFAULT_BRANCH on read so the store mirrors the backend's
  // BranchScopeFromContext default.
  selections: Record<string, string>;
  setBranch: (ontologyApiName: string, branchId: string) => void;
  clearBranch: (ontologyApiName: string) => void;
  getBranch: (ontologyApiName: string | null | undefined) => string;
}

export const useBranchStore = create<BranchStoreState>()(
  persist(
    (set, get) => ({
      selections: {},
      setBranch: (ontologyApiName, branchId) =>
        set((state) => {
          const next = { ...state.selections };
          const normalised = normaliseBranch(branchId);
          if (normalised === DEFAULT_BRANCH) {
            delete next[ontologyApiName];
          } else {
            next[ontologyApiName] = normalised;
          }
          return { selections: next };
        }),
      clearBranch: (ontologyApiName) =>
        set((state) => {
          if (!(ontologyApiName in state.selections)) return state;
          const next = { ...state.selections };
          delete next[ontologyApiName];
          return { selections: next };
        }),
      getBranch: (ontologyApiName) => {
        if (!ontologyApiName) return DEFAULT_BRANCH;
        return normaliseBranch(get().selections[ontologyApiName]);
      },
    }),
    {
      name: BRANCH_STORAGE_KEY,
      partialize: (state) => ({ selections: state.selections }),
    },
  ),
);

function normaliseBranch(value: string | undefined | null): string {
  const trimmed = (value ?? '').trim();
  return trimmed.length === 0 ? DEFAULT_BRANCH : trimmed;
}

// activeBranchFor is the synchronous read used by the API client so it does
// not have to depend on the React render cycle. Mirrors getBranch() but is
// safe to call from any module.
export function activeBranchFor(ontologyApiName: string | null | undefined): string {
  if (!ontologyApiName) return DEFAULT_BRANCH;
  return normaliseBranch(useBranchStore.getState().selections[ontologyApiName]);
}
