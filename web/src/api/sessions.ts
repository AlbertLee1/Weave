import { request } from './client';

// Session mirrors pkg/auth/session_handler.go SessionView — one active
// login belonging to the caller. `user_id` is deliberately never on the
// wire (the list endpoint already scopes to the authenticated caller), so
// it has no place in this type.
export interface Session {
  id: string;
  ip?: string;
  user_agent?: string;
  created_at: string;
  last_seen: string;
  // true for the device the caller is currently authenticated on. Absent
  // (omitempty on the backend) for every other row.
  current?: boolean;
}

// SessionListResponse mirrors pkg/auth SessionListResponse.
interface SessionListResponse {
  sessions: Session[] | null;
}

// RevokeOthersResponse mirrors pkg/auth RevokeOthersResponse — how many
// other sessions were destroyed and which session the caller stayed on.
export interface RevokeOthersResponse {
  revoked: number;
  currentSessionId: string;
}

// listSessions returns the caller's active sessions (newest activity first,
// as ordered by the backend). The backend may serialise an empty list as
// `null`; normalise to [] so callers can always `.map`.
export async function listSessions(): Promise<Session[]> {
  const res = await request<SessionListResponse>('GET', '/api/auth/sessions');
  return res.sessions ?? [];
}

// revokeSession destroys a single session the caller owns. Resolves on the
// backend's 204; rejects (ApiRequestError) on 403/404/5xx.
export function revokeSession(sessionID: string): Promise<void> {
  return request<void>(
    'DELETE',
    `/api/auth/sessions/${encodeURIComponent(sessionID)}`,
  );
}

// revokeOtherSessions logs the caller out of every device EXCEPT the one
// they're currently on ("log out other devices").
export function revokeOtherSessions(): Promise<RevokeOthersResponse> {
  return request<RevokeOthersResponse>(
    'POST',
    '/api/auth/sessions/revoke-others',
  );
}
