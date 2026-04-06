import { createContext, useEffect, useMemo, useState, type ReactNode } from 'react';

/**
 * MeResponse mirrors pkg/auth/handlers.go:MeResponse on the backend.
 */
export interface MeResponse {
  id: string;
  email: string;
  name: string;
  roles: string[];
  ontologyRoles: Record<string, string>;
  permissions: string[];
}

export interface AuthContextValue {
  user: MeResponse | null;
  loading: boolean;
  error: Error | null;
  /** True if the global permission set granted by current roles includes `perm`. */
  can: (perm: string) => boolean;
  /** True if the user holds either a global grant for `perm` or a scoped grant on the given ontology. */
  canOnOntology: (ontologyRid: string, perm: string) => boolean;
  /** Re-fetch /api/v2/me, e.g. after a token change. */
  refresh: () => Promise<void>;
}

// Static role -> permission map mirroring pkg/auth/permissions.go. We keep
// this in sync with the backend so that scoped role grants (which the /me
// endpoint reports as ontologyRoles) can be evaluated client-side without a
// second round-trip per scoped check.
const ROLE_PERMISSIONS: Record<string, string[]> = {
  viewer: [
    'ontology.read',
    'objectType.read',
    'linkType.read',
    'actionType.read',
    'interface.read',
    'sharedProperty.read',
    'typeGroup.read',
    'valueType.read',
    'queryType.read',
    'object.read',
    'actionLog.read',
  ],
  editor: [
    'ontology.read',
    'objectType.read',
    'linkType.read',
    'actionType.read',
    'interface.read',
    'sharedProperty.read',
    'typeGroup.read',
    'valueType.read',
    'queryType.read',
    'object.read',
    'object.write',
    'action.execute',
    'actionLog.read',
  ],
  'ontology-owner': [
    'ontology.read',
    'ontology.write',
    'objectType.read',
    'objectType.write',
    'linkType.read',
    'linkType.write',
    'actionType.read',
    'actionType.write',
    'interface.read',
    'interface.write',
    'sharedProperty.read',
    'sharedProperty.write',
    'typeGroup.read',
    'typeGroup.write',
    'valueType.read',
    'valueType.write',
    'queryType.read',
    'queryType.write',
    'object.read',
    'object.write',
    'action.execute',
    'datasourceBinding.manage',
    'snapshot.manage',
    'actionLog.read',
  ],
  admin: [
    'ontology.read',
    'ontology.write',
    'objectType.read',
    'objectType.write',
    'linkType.read',
    'linkType.write',
    'actionType.read',
    'actionType.write',
    'interface.read',
    'interface.write',
    'sharedProperty.read',
    'sharedProperty.write',
    'typeGroup.read',
    'typeGroup.write',
    'valueType.read',
    'valueType.write',
    'queryType.read',
    'queryType.write',
    'object.read',
    'object.write',
    'action.execute',
    'datasourceBinding.manage',
    'securityPolicy.manage',
    'snapshot.manage',
    'actionLog.read',
    'user.manage',
  ],
};

function permsForRoles(roles: string[]): Set<string> {
  const out = new Set<string>();
  for (const r of roles) {
    for (const p of ROLE_PERMISSIONS[r] ?? []) {
      out.add(p);
    }
  }
  return out;
}

export const AuthContext = createContext<AuthContextValue | null>(null);

interface AuthProviderProps {
  children: ReactNode;
}

export function AuthProvider({ children }: AuthProviderProps) {
  const [user, setUser] = useState<MeResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);

  const fetchMe = async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetch('/api/v2/me', {
        headers: { 'Content-Type': 'application/json' },
      });
      if (!res.ok) {
        setUser(null);
        return;
      }
      const data: MeResponse = await res.json();
      setUser(data);
    } catch (e) {
      setError(e instanceof Error ? e : new Error(String(e)));
      setUser(null);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchMe();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const value = useMemo<AuthContextValue>(() => {
    // Permissions reported by /me are the union of global + scoped role
    // grants. For per-ontology checks we need the global-only set so we can
    // refuse a write that the user only owns on a different ontology.
    const permSet = new Set(user?.permissions ?? []);
    const globalPermSet = permsForRoles(user?.roles ?? []);
    return {
      user,
      loading,
      error,
      can: (perm: string) => permSet.has(perm),
      canOnOntology: (ontologyRid: string, perm: string) => {
        if (globalPermSet.has(perm)) return true;
        const scopedRole = user?.ontologyRoles?.[ontologyRid];
        if (!scopedRole) return false;
        const scopedPerms = ROLE_PERMISSIONS[scopedRole] ?? [];
        return scopedPerms.includes(perm);
      },
      refresh: fetchMe,
    };
  }, [user, loading, error]);

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
