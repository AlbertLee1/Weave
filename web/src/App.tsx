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
import { ActionHistoryPage } from './components/actions/ActionHistoryPage';
import { AggregationPage } from './components/aggregation/AggregationPage';
import { ObjectSetPage } from './components/objectsets/ObjectSetPage';
import { ObjectSetDiffPage } from './components/objectsets/ObjectSetDiffPage';
import { SavedObjectSetsPage } from './components/objectsets/SavedObjectSetsPage';
import { BranchDiffPage } from './components/explorer/BranchDiffPage';
import { PlaygroundPage } from './components/developer/PlaygroundPage';
import { MetricsPage } from './components/developer/MetricsPage';
import { ObjectTypeAdminPage } from './components/admin/ObjectTypeAdminPage';
import { LinkTypeAdminPage } from './components/admin/LinkTypeAdminPage';
import { ActionTypeAdminPage } from './components/admin/ActionTypeAdminPage';
import { InterfaceAdminPage } from './components/admin/InterfaceAdminPage';
import { SchemaGraphPage } from './components/admin/SchemaGraphPage';
import { AuditHistoryPage } from './components/admin/AuditHistoryPage';
import { MarkingAdminPage } from './components/admin/MarkingAdminPage';
import { ImportWizardPage } from './components/import/ImportWizardPage';
import { SchemaInferencePage } from './components/import/SchemaInferencePage';
import { ApprovalsPage } from './components/approvals/ApprovalsPage';
import { ThreadsPage } from './components/threads/ThreadsPage';
import { LogicFlowsPage } from './components/aiplogic/LogicFlowsPage';
import { PipelinesPage } from './components/pipelines/PipelinesPage';
import { LineagePage } from './components/lineage/LineagePage';
import { DashboardEditorPage } from './components/dashboards/DashboardEditorPage';
import { PermissionRequestsPage } from './components/permissionrequests/PermissionRequestsPage';
import { MentionsPage } from './components/notifications/MentionsPage';
import { useNavigate, useParams } from 'react-router';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: 1,
    },
  },
});

// US-329: route wrapper that pulls the dashboard id from the URL and
// navigates to the new id after a fresh save so the URL becomes a
// shareable link. Keeping this here avoids react-router's hooks
// inside DashboardEditorPage so the component is unit-testable
// without a Router wrapper.
function DashboardEditorRoute() {
  const params = useParams<{ id?: string }>();
  const navigate = useNavigate();
  return (
    <DashboardEditorPage
      key={params.id ?? '__new__'}
      id={params.id}
      onSaved={(id) => navigate(`/dashboards/${id}`)}
    />
  );
}

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
              <Route path="actions/:ontology/history" element={<ActionHistoryPage />} />
              <Route path="actions/history" element={<ActionHistoryPage />} />
              <Route path="threads" element={<ThreadsPage />} />
              <Route path="logic-flows" element={<LogicFlowsPage />} />
              <Route path="pipelines" element={<PipelinesPage />} />
              <Route path="lineage/:rid" element={<LineagePage />} />
              <Route path="dashboards" element={<DashboardEditorRoute />} />
              <Route path="dashboards/:id" element={<DashboardEditorRoute />} />
              <Route path="approvals" element={<ApprovalsPage />} />
              <Route path="approvals/:ontology" element={<ApprovalsPage />} />
              <Route path="permission-requests" element={<PermissionRequestsPage />} />
              <Route path="mentions" element={<MentionsPage />} />
              <Route path="aggregation/:ontology/:objectType" element={<AggregationPage />} />
              <Route path="objectsets/:ontology" element={<ObjectSetPage />} />
              <Route path="objectsets/:ontology/saved" element={<SavedObjectSetsPage />} />
              <Route path="objectsets/:ontology/diff" element={<ObjectSetDiffPage />} />
              <Route path="import/:ontology" element={<ImportWizardPage />} />
              <Route path="schema/infer" element={<SchemaInferencePage />} />
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
              <Route
                path="admin/markings"
                element={
                  <PermissionRoute permission="user.manage">
                    <MarkingAdminPage />
                  </PermissionRoute>
                }
              />
            </Route>
          </Routes>
        </AuthProvider>
      </BrowserRouter>
    </QueryClientProvider>
  );
}
