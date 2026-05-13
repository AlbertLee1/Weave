import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route, Navigate } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { AuthProvider } from '../../auth/AuthContext';
import { ProtectedRoute } from '../../auth/ProtectedRoute';
import { PermissionRoute } from '../../auth/PermissionRoute';
import { LogicFlowsPage } from '../aiplogic/LogicFlowsPage';
import { AuditReportPage } from '../audit/AuditReportPage';

// Dogfood report #1 + #2: ensure legacy URLs /admin/audit and /aip-logic
// continue to resolve to the canonical Audit Report / AIP Logic pages.

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
