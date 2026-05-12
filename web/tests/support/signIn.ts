import { type APIRequestContext } from '@playwright/test';

const API_BASE = 'http://localhost:9117';

export interface SignInOptions {
  email?: string;
  password?: string;
  /** Defaults to `${API_BASE}/api/auth/login`. Override for non-default deploys. */
  loginPath?: string;
}

/**
 * Acquire credentials for subsequent API calls.
 *
 * - `AUTH_MODE=dev` (the default for `scripts/e2e-setup.sh`): the backend
 *   short-circuits auth, so this helper returns an empty header bag.
 * - `AUTH_MODE=token`: POST to /api/auth/login with the supplied (or default)
 *   credentials and return `{ Authorization: 'Bearer …' }`.
 *
 * Returns `{}` on transport / 4xx / 5xx so smoke specs stay portable across
 * the two AUTH_MODEs without needing to branch on environment variables. If
 * a scenario *requires* token mode, assert on the response of a downstream
 * authenticated API call rather than on the shape of the header bag.
 */
export async function signIn(
  request: APIRequestContext,
  opts: SignInOptions = {},
): Promise<Record<string, string>> {
  const email = opts.email ?? 'admin@test';
  const password = opts.password ?? 'test1234';
  const loginPath = opts.loginPath ?? `${API_BASE}/api/auth/login`;

  try {
    const res = await request.post(loginPath, { data: { email, password } });
    if (!res.ok()) return {};
    const body = (await res.json()) as { access_token?: string };
    return body.access_token ? { Authorization: `Bearer ${body.access_token}` } : {};
  } catch {
    return {};
  }
}
