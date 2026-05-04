import { test, type APIRequestContext } from '@playwright/test';

/**
 * Shared helpers for US-444 Playwright spec coverage of 20 core flows.
 *
 * Specs hit the real Weave backend at :9117 and the Vite dev server at
 * :5173 brought up via `make e2e-up` / `scripts/e2e-setup.sh`. The seed
 * baseline (`test/fixtures/e2e_seed.sh`) ships:
 *   - ontology=northwind with employee/customer/order/product types
 *   - users admin@test (admin) / manager@test (editor) / peer@test (viewer)
 *
 * Every spec is degraded-mode tolerant: if the backend is unreachable or
 * the optional feature surface returns 503/404 because its store is not
 * wired (PG-mode-only features in a partial deployment), the spec calls
 * `test.skip()` rather than failing. This keeps the suite useful both
 * for full local stacks AND for syntactic CI gates that only invoke
 * `playwright test --list`.
 */
export const API_BASE = 'http://localhost:9117';
export const ONTOLOGY = 'northwind';

const HEALTH_URL = `${API_BASE}/health`;
const HEALTH_TIMEOUT_MS = 1500;

/** Returns true when the backend health endpoint answers 200 within 1.5s. */
export async function backendReachable(
  request: APIRequestContext,
): Promise<boolean> {
  try {
    const res = await request.get(HEALTH_URL, { timeout: HEALTH_TIMEOUT_MS });
    return res.ok();
  } catch {
    return false;
  }
}

/**
 * Skip the current test (with a clear reason) when the backend is not up.
 * Use as the first call in every test body so the suite is portable
 * across CI lanes that only have the spec runner installed.
 */
export async function skipWhenBackendDown(
  request: APIRequestContext,
): Promise<void> {
  const ok = await backendReachable(request);
  test.skip(!ok, 'weave backend not reachable on :9117 — run `make e2e-up`');
}

/**
 * AUTH_MODE=dev (the default for `scripts/e2e-setup.sh`) leaves every
 * request authenticated as the synthetic dev user. AUTH_MODE=token paths
 * require a Bearer JWT — when present, callers should `await
 * loginAdmin(request)` and pass the returned header bag.
 */
export async function loginAdmin(
  request: APIRequestContext,
): Promise<Record<string, string>> {
  const res = await request.post(`${API_BASE}/api/auth/login`, {
    data: { email: 'admin@test', password: 'test1234' },
  });
  if (!res.ok()) return {};
  try {
    const body = (await res.json()) as { access_token?: string };
    if (!body.access_token) return {};
    return { Authorization: `Bearer ${body.access_token}` };
  } catch {
    return {};
  }
}

/**
 * Fetch JSON from a GET endpoint, returning the parsed body on 2xx,
 * `null` on 404 (so specs can choose between "feature unavailable" and
 * "test skip"), and throwing on other error statuses for visibility.
 */
export async function fetchJSON<T>(
  request: APIRequestContext,
  path: string,
  init?: { headers?: Record<string, string> },
): Promise<{ status: number; body: T | null }> {
  const res = await request.get(`${API_BASE}${path}`, init);
  const status = res.status();
  if (status === 404 || status === 503) {
    return { status, body: null };
  }
  if (!res.ok()) {
    throw new Error(`GET ${path} failed: ${status} ${await res.text()}`);
  }
  const body = (await res.json()) as T;
  return { status, body };
}

/** Generate a unique identifier for test isolation across reruns. */
export function uniqueName(prefix: string): string {
  return `${prefix}_${Date.now()}_${Math.random().toString(36).slice(2, 7)}`;
}
