import { useContext } from 'react';
import { AuthContext, type AuthContextValue } from './AuthContext';

/**
 * useAuth returns the current authenticated user, loading state, and
 * permission helpers. Must be called from a component nested under
 * <AuthProvider>.
 */
export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return ctx;
}
