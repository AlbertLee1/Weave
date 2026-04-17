import { BrowserRouter, Routes, Route } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { AuthProvider } from './auth/AuthContext';
import { LoginPage } from './auth/LoginPage';
import { ProtectedRoute } from './auth/ProtectedRoute';
import { PermissionRoute } from './auth/PermissionRoute';
import { Shell } from './components/layout/Shell';
import { DashboardPage } from './components/dashboard/DashboardPage';
import { ExplorerPage } from './components/explorer/ExplorerPage';
import { BrowserPage } from './components/browser/BrowserPage';
import { ActionConsolePage } from './components/actions/ActionConsolePage';
import { AggregationPage } from './components/aggregation/AggregationPage';
import { ObjectSetPage } from './components/objectsets/ObjectSetPage';
import { BranchDiffPage } from './components/explorer/BranchDiffPage';
import { PlaygroundPage } from './components/developer/PlaygroundPage';
import { MetricsPage } from './components/developer/MetricsPage';
import { ObjectTypeAdminPage } from './components/admin/ObjectTypeAdminPage';
import { LinkTypeAdminPage } from './components/admin/LinkTypeAdminPage';
import { ActionTypeAdminPage } from './components/admin/ActionTypeAdminPage';
import { InterfaceAdminPage } from './components/admin/InterfaceAdminPage';
import {
  SchemaGraphPage,
  AuditHistoryPage,
} from './components/admin/AdminPlaceholderPage';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: 1,
    },
  },
});

function AdminGuard({ children }: { children: React.ReactNode }) {
  return (
    <PermissionRoute permission="ontology.write" scopeToOntologyParam>
      {children}
    </PermissionRoute>
  );
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <AuthProvider>
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route
              element={
                <ProtectedRoute>
                  <Shell />
                </ProtectedRoute>
              }
            >
              <Route index element={<DashboardPage />} />
              <Route path="explorer/:ontology" element={<ExplorerPage />} />
              <Route path="explorer/:ontology/branches/:branch/diff" element={<BranchDiffPage />} />
              <Route path="explorer/:ontology/:objectType" element={<ExplorerPage />} />
              <Route path="browser/:ontology/:objectType" element={<BrowserPage />} />
              <Route path="actions/:ontology" element={<ActionConsolePage />} />
              <Route path="aggregation/:ontology/:objectType" element={<AggregationPage />} />
              <Route path="objectsets/:ontology" element={<ObjectSetPage />} />
              <Route path="developer/playground" element={<PlaygroundPage />} />
              <Route path="developer/metrics" element={<MetricsPage />} />
              <Route
                path="admin/:ontology/objectTypes"
                element={
                  <AdminGuard>
                    <ObjectTypeAdminPage />
                  </AdminGuard>
                }
              />
              <Route
                path="admin/:ontology/linkTypes"
                element={
                  <AdminGuard>
                    <LinkTypeAdminPage />
                  </AdminGuard>
                }
              />
              <Route
                path="admin/:ontology/actionTypes"
                element={
                  <AdminGuard>
                    <ActionTypeAdminPage />
                  </AdminGuard>
                }
              />
              <Route
                path="admin/:ontology/interfaces"
                element={
                  <AdminGuard>
                    <InterfaceAdminPage />
                  </AdminGuard>
                }
              />
              <Route
                path="admin/:ontology/graph"
                element={
                  <AdminGuard>
                    <SchemaGraphPage />
                  </AdminGuard>
                }
              />
              <Route
                path="admin/:ontology/history"
                element={
                  <AdminGuard>
                    <AuditHistoryPage />
                  </AdminGuard>
                }
              />
            </Route>
          </Routes>
        </AuthProvider>
      </BrowserRouter>
    </QueryClientProvider>
  );
}
