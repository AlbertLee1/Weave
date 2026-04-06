import { describe, it, expect, beforeEach } from 'vitest';
import { useAuthStore } from '../authStore';

describe('authStore', () => {
  beforeEach(() => {
    useAuthStore.getState().clear();
  });

  it('starts with no access token', () => {
    expect(useAuthStore.getState().accessToken).toBeNull();
  });

  it('setToken stores the token in memory', () => {
    useAuthStore.getState().setAccessToken('jwt.token.here');
    expect(useAuthStore.getState().accessToken).toBe('jwt.token.here');
  });

  it('clear wipes the token', () => {
    useAuthStore.getState().setAccessToken('jwt');
    useAuthStore.getState().clear();
    expect(useAuthStore.getState().accessToken).toBeNull();
  });

  it('does not persist token to localStorage', () => {
    useAuthStore.getState().setAccessToken('secret');
    // Verify nothing weave-related made it into localStorage.
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i)!;
      const v = localStorage.getItem(k);
      expect(v).not.toContain('secret');
    }
  });
});
