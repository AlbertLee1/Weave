import { create } from 'zustand';

/**
 * AuthState holds only the in-memory access token. The refresh token lives in
 * an httpOnly cookie set by the backend; this store deliberately does not
 * persist the access token to localStorage so an XSS attacker cannot survive
 * a tab reload.
 */
export interface AuthState {
  accessToken: string | null;
  setAccessToken: (token: string | null) => void;
  clear: () => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  accessToken: null,
  setAccessToken: (token) => set({ accessToken: token }),
  clear: () => set({ accessToken: null }),
}));
