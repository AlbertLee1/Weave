import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import App from '../../App';

const EMPTY_METRICS = [
  '# HELP weave_http_requests_total HTTP requests',
  '# TYPE weave_http_requests_total counter',
  'weave_http_requests_total{method="GET",status="200"} 0',
].join('\n');

const server = setupServer(
  http.get('/api/v2/me', () =>
    HttpResponse.json({
      id: 'admin',
      email: 'admin@example.com',
      name: 'Admin',
      roles: ['admin'],
      ontologyRoles: {},
      permissions: ['user.manage', 'ontology.write'],
    }),
  ),
  http.get('/metrics', () =>
    new HttpResponse(EMPTY_METRICS, {
      status: 200,
      headers: { 'Content-Type': 'text/plain; version=0.0.4' },
    }),
  ),
);

beforeAll(() => server.listen({ onUnhandledRequest: 'bypass' }));
afterEach(() => {
  cleanup();
  server.resetHandlers();
  window.history.pushState({}, '', '/');
});
afterAll(() => server.close());

describe('BDD: admin performance route alias', () => {
  it('Given an admin opens /admin/performance, Then the SPA resolves the Performance Dashboard', async () => {
    window.history.pushState({}, '', '/admin/performance');

    render(<App />);

    await waitFor(() => {
      expect(
        screen.getByRole('heading', { name: /performance dashboard/i }),
      ).toBeInTheDocument();
    });
    expect(window.location.pathname).toBe('/admin/perf');
    expect(screen.queryByTestId('not-found-page')).not.toBeInTheDocument();
  });
});
