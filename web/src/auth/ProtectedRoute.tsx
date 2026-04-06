import type { ReactNode } from 'react';
import { Navigate } from 'react-router';
import { useAuth } from './useAuth';

interface ProtectedRouteProps {
  children: ReactNode;
}

/**
 * ProtectedRoute redirects to /login when the AuthContext reports no user,
 * and renders its children otherwise. While the initial /api/v2/me request
 * is in flight it returns nothing (the surrounding Shell can show its own
 * spinner) so we never show protected content even briefly to an unauth'd
 * visitor.
 */
export function ProtectedRoute({ children }: ProtectedRouteProps) {
  const { user, loading } = useAuth();
  if (loading) {
    return null;
  }
  if (!user) {
    return <Navigate to="/login" replace />;
  }
  return <>{children}</>;
}
