import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { AuthProvider } from '../../auth/AuthContext';
import { ProtectedRoute } from '../../auth/ProtectedRoute';
import { PermissionRoute } from '../../auth/PermissionRoute';
import { AutomationRulesPage } from '../automation/AutomationRulesPage';
import { ProposalsPage } from '../proposals/ProposalsPage';
import { SecurityPoliciesPage } from '../securityPolicies/SecurityPoliciesPage';
import { ExplorerPage } from '../explorer/ExplorerPage';

// Dogfood report #4: Automation Rules / Security Policies / Proposals were
// rendering Schema Graph instead of their own content. Root cause was that
// the dogfood build pre-dated these routes; React Router fell through to a
// catch-all that landed on Explorer. This regression test pins down that
// each ontology sub-route mounts its own page component — never Explorer's
// Schema Graph view — for an authenticated ontology-owner.

const server = setupServer(
  http.get('/api/v2/me', () =>
    HttpResponse.json({
      id: 'admin',
      email: 'admin@example.com',
      name: 'Admin',
      roles: ['admin'],
      ontologyRoles: { 'ri.ontology.main.ontology.northwind': 'admin' },
      permissions: ['ontology.write', 'ontology.read'],
    }),
  ),
  http.get('/api/v2/ontologies/:ontology/automationRules', () =>
    HttpResponse.json({ rules: [] }),
  ),
  http.get('/api/v2/ontologies/:ontology/proposals', () =>
    HttpResponse.json({ proposals: [] }),
  ),
  http.get('/api/v2/ontologies/:ontology/securityPolicies/rowPolicies', () =>
    HttpResponse.json({ policies: [] }),
  ),
  http.get('/api/v2/ontologies/:ontology/securityPolicies/columnMasks', () =>
    HttpResponse.json({ masks: [] }),
  ),
  http.get('/api/v2/ontologies/:ontology/securityPolicies/cellMasks', () =>
    HttpResponse.json({ masks: [] }),
  ),
  http.get('/api/v2/ontologies/:ontology/objectTypes', () =>
    HttpResponse.json({ objectTypes: [] }),
  ),
);

beforeAll(() => server.listen({ onUnhandledRequest: 'bypass' }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function AdminGuard({ children }: { children: React.ReactNode }) {
  return (
    <PermissionRoute permission="ontology.write" scopeToOntologyParam>
      {children}
    </PermissionRoute>
  );
}

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
                      <Route
                        path="explorer/:ontology"
                        element={<ExplorerPage />}
                      />
                      <Route
                        path="automation/:ontology"
                        element={
                          <AdminGuard>
                            <AutomationRulesPage />
                          </AdminGuard>
                        }
                      />
                      <Route
                        path="proposals/:ontology"
                        element={
                          <AdminGuard>
                            <ProposalsPage />
                          </AdminGuard>
                        }
                      />
                      <Route
                        path="admin/:ontology/security"
                        element={
                          <AdminGuard>
                            <SecurityPoliciesPage />
                          </AdminGuard>
                        }
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

describe('Dogfood report #4: ontology sub-routes mount their own pages', () => {
  it('/automation/:ontology renders AutomationRulesPage, not Schema Graph', async () => {
    renderAt('/automation/northwind');
    await waitFor(() => {
      expect(screen.getByTestId('automation-rules-page')).toBeInTheDocument();
    });
    expect(screen.queryByTestId('explorer-page')).not.toBeInTheDocument();
  });

  it('/proposals/:ontology renders ProposalsPage, not Schema Graph', async () => {
    renderAt('/proposals/northwind');
    await waitFor(() => {
      expect(screen.getByTestId('proposals-page')).toBeInTheDocument();
    });
    expect(screen.queryByTestId('explorer-page')).not.toBeInTheDocument();
  });

  it('/admin/:ontology/security renders SecurityPoliciesPage, not Schema Graph', async () => {
    renderAt('/admin/northwind/security');
    await waitFor(() => {
      expect(screen.getByTestId('security-policies-page')).toBeInTheDocument();
    });
    expect(screen.queryByTestId('explorer-page')).not.toBeInTheDocument();
  });
});
