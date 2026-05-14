import { create } from 'zustand';

// Dogfood Round 3 #1 / #3: the notification drawer was previously held
// as a local `useState` inside `<Topbar>`. That worked for the open/close
// click, but it leaked the drawer's DOM (translate-x-full) across routes
// and made it impossible for the `/notifications` full page to ask
// "is the drawer logically open?" so it could suppress its duplicate.
//
// Hoisting the open flag onto a small shared Zustand store solves both:
//
//  * the Topbar bell still toggles via `openDrawer()` / `closeDrawer()`;
//  * the `/notifications` route can refuse to mount the slide panel at
//    all when it owns the surface, regardless of the previous flag;
//  * tests can read `useUIStore.getState().notificationDrawerOpen`
//    without rendering the Topbar tree.
//
// The store is intentionally NOT persisted — drawer state is per-session
// and should reset on a fresh tab to match the closed-on-load contract
// surveyors expect.
interface UIState {
  notificationDrawerOpen: boolean;
  openDrawer: () => void;
  closeDrawer: () => void;
  toggleDrawer: () => void;
}

export const useUIStore = create<UIState>((set) => ({
  notificationDrawerOpen: false,
  openDrawer: () => set({ notificationDrawerOpen: true }),
  closeDrawer: () => set({ notificationDrawerOpen: false }),
  toggleDrawer: () =>
    set((s) => ({ notificationDrawerOpen: !s.notificationDrawerOpen })),
}));
