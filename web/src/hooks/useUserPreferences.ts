import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  getUserPreferences,
  updateUserPreferences,
  type UpdateUserPreferencesInput,
  type UserPreferences,
} from '../api/userPreferences';
import { ApiRequestError } from '../api/client';

const QUERY_KEY = ['user-preferences'] as const;

// Default returned when the deployment has no PG (degraded mode 404)
// or the route is unmounted. Mirrors `virtualDefault` on the backend
// so first-load callers see a stable shape.
const DEFAULT_PREFS: UserPreferences = {
  userId: '',
  theme: '',
  language: '',
  notifications: {},
  hotkeys: {},
};

export interface UseUserPreferencesResult {
  data: UserPreferences;
  isLoading: boolean;
  unavailable: boolean;
  error: unknown;
}

// Detects the degraded-mode "route unmounted" condition: a clean 404 or
// the typed UserPreferencesUnavailable error from the no-store branch.
// In both cases the Settings page should render with localStorage / OS
// defaults plus a banner explaining persistence isn't enabled.
function isDegradedMode(err: unknown): boolean {
  return (
    err instanceof ApiRequestError &&
    (err.statusCode === 404 || err.errorName === 'UserPreferencesUnavailable')
  );
}

// useUserPreferences returns the persisted prefs row. Degraded-mode
// deployments (no PG) surface as `unavailable: true` with `data` set
// to DEFAULT_PREFS so the page can still render and apply local-only
// changes. Genuine errors propagate via the `error` field.
export function useUserPreferences(): UseUserPreferencesResult {
  const q = useQuery<UserPreferences>({
    queryKey: QUERY_KEY,
    queryFn: getUserPreferences,
    staleTime: 60_000,
    retry: false,
  });
  const unavailable = isDegradedMode(q.error);
  return {
    data: q.data ?? DEFAULT_PREFS,
    isLoading: q.isLoading,
    unavailable,
    error: unavailable ? null : q.error,
  };
}

export function useUpdateUserPreferences() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: UpdateUserPreferencesInput) =>
      updateUserPreferences(input),
    onSuccess: (next) => {
      qc.setQueryData(QUERY_KEY, next);
    },
  });
}
