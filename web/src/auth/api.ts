import { useAuthStore } from './authStore';

/**
 * LoginUser mirrors pkg/auth/login_handler.go LoginUser.
 */
export interface LoginUser {
  id: string;
  email: string;
  name: string;
  roles: string[];
  ontologyRoles: Record<string, string>;
}

/**
 * LoginResponse mirrors pkg/auth/login_handler.go LoginResponse.
 */
export interface LoginResponse {
  access_token: string;
  refresh_token: string;
  token_type: string;
  expires_in: number;
  user: LoginUser;
}

class AuthApiError extends Error {
  status: number;
  body: unknown;
  constructor(status: number, body: unknown, message: string) {
    super(message);
    this.name = 'AuthApiError';
    this.status = status;
    this.body = body;
  }
}

async function postJSON<T>(url: string, body: unknown): Promise<T> {
  const res = await fetch(url, {
    method: 'POST',
    credentials: 'include', // important: refresh cookie must round-trip
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    let err: unknown = null;
    try {
      err = await res.json();
    } catch {
      // body may be empty
    }
    throw new AuthApiError(res.status, err, `request failed: ${res.status}`);
  }
  if (res.status === 204) {
    return undefined as T;
  }
  return (await res.json()) as T;
}

/**
 * login posts email + password to /api/auth/login. On success the access
 * token is stored in the in-memory authStore. The refresh token is set as
 * an httpOnly cookie by the server and is not visible to JS.
 */
export async function login(email: string, password: string): Promise<LoginResponse> {
  const resp = await postJSON<LoginResponse>('/api/auth/login', { email, password });
  useAuthStore.getState().setAccessToken(resp.access_token);
  return resp;
}

/**
 * refresh calls /api/auth/refresh to mint a new access token from the
 * refresh cookie (and/or the refresh token in the request body). Updates
 * the in-memory store on success.
 */
export async function refresh(refreshToken?: string): Promise<LoginResponse> {
  const body: Record<string, string> = {};
  if (refreshToken) body.refresh_token = refreshToken;
  const resp = await postJSON<LoginResponse>('/api/auth/refresh', body);
  useAuthStore.getState().setAccessToken(resp.access_token);
  return resp;
}

/**
 * logout calls /api/auth/logout (best-effort) and always clears local state.
 */
export async function logout(): Promise<void> {
  try {
    await postJSON<void>('/api/auth/logout', {});
  } catch {
    // best-effort: ignore errors so the SPA can always redirect to /login.
  }
  useAuthStore.getState().clear();
}
