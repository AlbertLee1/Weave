import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export const RECENT_COMMANDS_LIMIT = 6;

const RECENT_COMMANDS_STORAGE_KEY = 'weave-recent-commands';

// RecentCommandKind is the discriminator that drives icon + heading
// labelling in the palette. Keeping it on the entry rather than reading
// the URL means we don't have to reverse-engineer the resource type from
// `to` when re-rendering the Recent group.
export type RecentCommandKind =
  | 'page'
  | 'action'
  | 'object'
  | 'branch'
  | 'app'
  | 'ontology';

export interface RecentCommand {
  id: string;
  kind: RecentCommandKind;
  label: string;
  to: string;
  hint?: string;
  pickedAt: number;
}

interface RecentCommandsState {
  entries: RecentCommand[];
  record: (entry: Omit<RecentCommand, 'pickedAt'>) => void;
  clear: () => void;
}

export const useRecentCommandsStore = create<RecentCommandsState>()(
  persist(
    (set) => ({
      entries: [],
      record: (entry) =>
        set((state) => {
          const next: RecentCommand = { ...entry, pickedAt: Date.now() };
          // Dedup by (kind, id) — re-picking an existing entry pushes
          // it back to the top and bumps `pickedAt` rather than
          // creating a duplicate row in the Recent group.
          const filtered = state.entries.filter(
            (e) => !(e.kind === entry.kind && e.id === entry.id),
          );
          return {
            entries: [next, ...filtered].slice(0, RECENT_COMMANDS_LIMIT),
          };
        }),
      clear: () => set({ entries: [] }),
    }),
    {
      name: RECENT_COMMANDS_STORAGE_KEY,
      partialize: (state) => ({ entries: state.entries }),
    },
  ),
);
