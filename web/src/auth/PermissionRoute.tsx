import type { ReactNode } from 'react';
import { useParams } from 'react-router';
import { useAuth } from './useAuth';

interface PermissionRouteProps {
  /** Required permission (e.g. "ontology.write"). */
  permission: string;
  /**
   * When "scopeToOntologyParam" is true, the route's :ontology param is
   * resolved to an ontology RID via the authenticated user's ontologyRoles
   * keys; the check then applies scoped permissions so ontology-owners of
   * one ontology can't admin another.
   */
  scopeToOntologyParam?: boolean;
  children: ReactNode;
}

function findOntologyRidByApiName(
  ontologyRoles: Record<string, string> | undefined,
  apiName: string,
): string | null {
  if (!ontologyRoles) return null;
  for (const rid of Object.keys(ontologyRoles)) {
    const parts = rid.split('.');
    if (parts[parts.length - 1] === apiName) return rid;
  }
  return null;
}

/**
 * PermissionRoute guards a whole page. Unlike PermissionGate (which disables
 * a button in-place), this renders a full-page "Access Denied" panel when the
 * user lacks the permission — so the protected page never mounts.
 */
export function PermissionRoute({
  permission,
  scopeToOntologyParam,
  children,
}: PermissionRouteProps) {
  const { user, loading, can, canOnOntology } = useAuth();
  const params = useParams();

  if (loading) return null;

  let allowed = false;
  if (user) {
    if (scopeToOntologyParam && typeof params.ontology === 'string') {
      const rid =
        findOntologyRidByApiName(user.ontologyRoles, params.ontology) ??
        '__no_match__';
      allowed = canOnOntology(rid, permission);
    } else {
      allowed = can(permission);
    }
  }

  if (allowed) return <>{children}</>;

  return (
    <div
      data-testid="permission-route-denied"
      className="flex flex-col items-center justify-center h-[calc(100vh-3rem)] bg-bg-primary px-6 text-center"
    >
      <div className="text-xs uppercase tracking-widest text-accent-error mb-2">
        Access Denied
      </div>
      <h1 className="text-base font-semibold text-text-primary mb-2">
        You do not have permission to view this page
      </h1>
      <p className="text-sm text-text-secondary max-w-md">
        The <span className="font-mono">{permission}</span> permission is
        required. Ask an ontology owner or administrator for access.
      </p>
    </div>
  );
}
