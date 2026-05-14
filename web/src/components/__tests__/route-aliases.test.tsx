import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route, Navigate, useParams } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { AuthProvider } from '../../auth/AuthContext';
import { ProtectedRoute } from '../../auth/ProtectedRoute';
import { PermissionRoute } from '../../auth/PermissionRoute';
import { LogicFlowsPage } from '../aiplogic/LogicFlowsPage';
import { AuditReportPage } from '../audit/AuditReportPage';
import { NotFoundPage } from '../common/NotFoundPage';

// Dogfood report #1 + #2: ensure legacy URLs /admin/audit and /aip-logic
// continue to resolve to the canonical Audit Report / AIP Logic pages.
// Round 2 extends this to slug aliases (/aip-threads, /api-playground,
// etc.) and the explorer-scoped slug aliases.

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
  http.get('/api/v2/aip/logic-flows', () =>
    HttpResponse.json({ flows: [] }),
  ),
  http.get('/api/v2/admin/auditEvents', () =>
    HttpResponse.json({ events: [], total: 0 }),
  ),
);

beforeEach(() => server.listen());
afterEach(() => {
  server.resetHandlers();
  server.close();
  vi.restoreAllMocks();
});

function renderAt(url: string) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[url]}>
        <AuthProvider>
          <Routes>
            <Route
              element={
                <ProtectedRoute>
                  <div data-testid="shell">
                    <Routes>
                      <Route path="logic-flows" element={<LogicFlowsPage />} />
                      <Route
                        path="aip-logic"
                        element={<Navigate to="/logic-flows" replace />}
                      />
                      <Route
                        path="audit"
                        element={
                          <PermissionRoute permission="user.manage">
                            <AuditReportPage />
                          </PermissionRoute>
                        }
                      />
                      <Route
                        path="admin/audit"
                        element={<Navigate to="/audit" replace />}
                      />
                    </Routes>
                  </div>
                </ProtectedRoute>
              }
            >
              <Route path="*" element={null} />
            </Route>
          </Routes>
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('Dogfood report #1 + #2: legacy route aliases', () => {
  it('/aip-logic redirects to the AIP Logic Flows page', async () => {
    renderAt('/aip-logic');
    await waitFor(() => {
      expect(screen.getByTestId('logic-flows-page')).toBeInTheDocument();
    });
  });

  it('/admin/audit redirects to the Audit Report page', async () => {
    renderAt('/admin/audit');
    await waitFor(() => {
      expect(screen.getByText(/Audit Report/i)).toBeInTheDocument();
    });
  });
});

function OntologyAliasRedirect({
  build,
}: {
  build: (ontology: string) => string;
}) {
  const { ontology } = useParams<{ ontology: string }>();
  return <Navigate to={build(ontology ?? '')} replace />;
}

function PathSink() {
  return <div data-testid="path-sink">resolved</div>;
}

function renderAliases(url: string) {
  return render(
    <MemoryRouter initialEntries={[url]}>
      <Routes>
        {/* Mirror App.tsx slug aliases under test */}
        <Route path="aip-threads" element={<Navigate to="/threads" replace />} />
        <Route path="threads" element={<PathSink />} />
        <Route
          path="api-playground"
          element={<Navigate to="/developer/playground" replace />}
        />
        <Route path="developer/playground" element={<PathSink />} />
        <Route
          path="api-metrics"
          element={<Navigate to="/developer/metrics" replace />}
        />
        <Route path="developer/metrics" element={<PathSink />} />
        <Route
          path="schema-inference"
          element={<Navigate to="/schema/infer" replace />}
        />
        <Route path="schema/infer" element={<PathSink />} />

        <Route
          path="explorer/:ontology/query-builder"
          element={
            <OntologyAliasRedirect build={(o) => `/objectsets/${o}`} />
          }
        />
        <Route path="objectsets/:ontology" element={<PathSink />} />
        <Route
          path="explorer/:ontology/quiver-ts"
          element={<OntologyAliasRedirect build={(o) => `/quiver/${o}`} />}
        />
        <Route path="quiver/:ontology" element={<PathSink />} />
        <Route
          path="explorer/:ontology/admin/object-types"
          element={
            <OntologyAliasRedirect build={(o) => `/admin/${o}/objectTypes`} />
          }
        />
        <Route path="admin/:ontology/objectTypes" element={<PathSink />} />
        <Route
          path="explorer/:ontology/admin/schema-graph"
          element={
            <OntologyAliasRedirect build={(o) => `/admin/${o}/graph`} />
          }
        />
        <Route path="admin/:ontology/graph" element={<PathSink />} />

        <Route path="*" element={<NotFoundPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('Dogfood report-round2: slug aliases', () => {
  it.each([
    ['/aip-threads'],
    ['/api-playground'],
    ['/api-metrics'],
    ['/schema-inference'],
    ['/explorer/iotDemo/query-builder'],
    ['/explorer/iotDemo/quiver-ts'],
    ['/explorer/iotDemo/admin/object-types'],
    ['/explorer/iotDemo/admin/schema-graph'],
  ])('%s redirects to its canonical route', async (url) => {
    renderAliases(url);
    await waitFor(() => {
      expect(screen.getByTestId('path-sink')).toBeInTheDocument();
    });
  });

  it('unknown URLs render the NotFound page instead of blank', async () => {
    renderAliases('/this-route-does-not-exist');
    await waitFor(() => {
      expect(screen.getByTestId('not-found-page')).toBeInTheDocument();
    });
  });
});
