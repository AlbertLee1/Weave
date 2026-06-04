import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  listSessions,
  revokeOtherSessions,
  revokeSession,
} from '../api/sessions';

const SESSIONS_KEY = ['auth', 'sessions'] as const;

// useSessions reads the caller's active login sessions. Diagnostic-style
// surface: a degraded deployment (auth in `dev` mode, store unwired) may
// 404/501, so callers gate rendering on `isSuccess` rather than crashing.
export function useSessions(options: { enabled?: boolean } = {}) {
  const { enabled = true } = options;
  return useQuery({
    queryKey: SESSIONS_KEY,
    queryFn: () => listSessions(),
    enabled,
    retry: false,
  });
}

// useRevokeSession destroys one session and refreshes the list on success.
export function useRevokeSession() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (sessionID: string) => revokeSession(sessionID),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: SESSIONS_KEY });
    },
  });
}

// useRevokeOtherSessions logs the caller out of every other device and
// refreshes the list so only the current session remains.
export function useRevokeOtherSessions() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => revokeOtherSessions(),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: SESSIONS_KEY });
    },
  });
}
