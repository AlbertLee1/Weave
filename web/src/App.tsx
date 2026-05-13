import { BrowserRouter, Routes, Route } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { GlobalErrorBoundary } from './components/common/ErrorBoundary';
import { AuthProvider } from './auth/AuthContext';
import { LoginPage } from './auth/LoginPage';
import { ProtectedRoute } from './auth/ProtectedRoute';
import { PermissionRoute } from './auth/PermissionRoute';
import { Shell } from './components/layout/Shell';
import { DashboardPage } from './components/dashboard/DashboardPage';
import { ExplorerPage } from './components/explorer/ExplorerPage';
import { BrowserPage } from './components/browser/BrowserPage';
import { InterfaceMethodsConsolePage } from './components/browser/InterfaceMethodsConsolePage';
import { ActionConsolePage } from './components/actions/ActionConsolePage';
import { ActionHistoryPage } from './components/actions/ActionHistoryPage';
import { AggregationPage } from './components/aggregation/AggregationPage';
import { ObjectSetPage } from './components/objectsets/ObjectSetPage';
import { ObjectSetDiffPage } from './components/objectsets/ObjectSetDiffPage';
import { SavedObjectSetsPage } from './components/objectsets/SavedObjectSetsPage';
import { BranchDiffPage } from './components/explorer/BranchDiffPage';
import { BranchReconcilePage } from './components/explorer/BranchReconcilePage';
import { PlaygroundPage } from './components/developer/PlaygroundPage';
import { MetricsPage } from './components/developer/MetricsPage';
import { ObjectTypeAdminPage } from './components/admin/ObjectTypeAdminPage';
import { LinkTypeAdminPage } from './components/admin/LinkTypeAdminPage';
import { ActionTypeAdminPage } from './components/admin/ActionTypeAdminPage';
import { InterfaceAdminPage } from './components/admin/InterfaceAdminPage';
import { ValueTypeAdminPage } from './components/admin/ValueTypeAdminPage';
import { SchemaGraphPage } from './components/admin/SchemaGraphPage';
import { AuditHistoryPage } from './components/admin/AuditHistoryPage';
import { AuditReportPage } from './components/audit/AuditReportPage';
import { MarkingAdminPage } from './components/admin/MarkingAdminPage';
import { ComplianceReportsPage } from './components/admin/ComplianceReportsPage';
import { PerformanceDashboardPage } from './components/admin/PerformanceDashboardPage';
import { ImportWizardPage } from './components/import/ImportWizardPage';
import { SchemaInferencePage } from './components/import/SchemaInferencePage';
import { ApprovalsPage } from './components/approvals/ApprovalsPage';
import { SagaDLQPage } from './components/sagaDLQ/SagaDLQPage';
import { SagaJobsPage } from './components/sagaJobs/SagaJobsPage';
import { ThreadsPage } from './components/threads/ThreadsPage';
import { LogicFlowsPage } from './components/aiplogic/LogicFlowsPage';
import { PipelinesPage } from './components/pipelines/PipelinesPage';
import { LineagePage } from './components/lineage/LineagePage';
import { DashboardEditorPage } from './components/dashboards/DashboardEditorPage';
import { AppEditorPage } from './components/apps/AppEditorPage';
import { QuiverPage } from './components/quiver/QuiverPage';
import { QuiverViewPage } from './components/quiver/QuiverViewPage';
import { PermissionRequestsPage } from './components/permissionrequests/PermissionRequestsPage';
import { MentionsPage } from './components/notifications/MentionsPage';
import { NotificationsPage } from './components/notifications/NotificationsPage';
import { MarketplacePage } from './components/marketplace/MarketplacePage';
import { FunctionDiffPage } from './components/functions/FunctionDiffPage';
import { FunctionCodePage } from './components/functions/FunctionCodePage';
import { FunctionRepoPage } from './components/functions/FunctionRepoPage';
import { SettingsPage } from './components/settings/SettingsPage';
import { AutomationRulesPage } from './components/automation/AutomationRulesPage';
import { ProposalsPage } from './components/proposals/ProposalsPage';
import { SecurityPoliciesPage } from './components/securityPolicies/SecurityPoliciesPage';
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

// US-392: route wrapper for the App Editor. Mirror DashboardEditorRoute
// so the rid in the URL becomes a shareable link after a fresh save.
function AppEditorRoute() {
  const params = useParams<{ rid?: string }>();
  const navigate = useNavigate();
  return (
    <AppEditorPage
      key={params.rid ?? '__new__'}
      rid={params.rid}
      onSaved={(rid) => navigate(`/apps/${rid}`)}
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
    <GlobalErrorBoundary>
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
              <Route path="explorer/:ontology/branches/:branch/reconcile" element={<BranchReconcilePage />} />
              <Route path="explorer/:ontology/:objectType" element={<ExplorerPage />} />
              <Route path="browser/:ontology/:objectType" element={<BrowserPage />} />
              <Route
                path="methods/:ontology/:objectType/:primaryKey"
                element={<InterfaceMethodsConsolePage />}
              />
              <Route path="actions/:ontology" element={<ActionConsolePage />} />
              <Route path="actions/:ontology/history" element={<ActionHistoryPage />} />
              <Route path="actions/history" element={<ActionHistoryPage />} />
              <Route
                path="actions/:ontology/jobs"
                element={
                  <AdminGuard>
                    <SagaJobsPage />
                  </AdminGuard>
                }
              />
              <Route path="threads" element={<ThreadsPage />} />
              <Route path="logic-flows" element={<LogicFlowsPage />} />
              <Route path="pipelines" element={<PipelinesPage />} />
              <Route path="lineage/:rid" element={<LineagePage />} />
              <Route path="dashboards" element={<DashboardEditorRoute />} />
              <Route path="dashboards/:id" element={<DashboardEditorRoute />} />
              <Route path="apps" element={<AppEditorRoute />} />
              <Route path="apps/:rid" element={<AppEditorRoute />} />
              <Route path="approvals" element={<ApprovalsPage />} />
              <Route path="approvals/:ontology" element={<ApprovalsPage />} />
              <Route
                path="admin/saga-dlq"
                element={
                  <PermissionRoute permission="ontology.write">
                    <SagaDLQPage />
                  </PermissionRoute>
                }
              />
              <Route
                path="admin/:ontology/saga-dlq"
                element={
                  <AdminGuard>
                    <SagaDLQPage />
                  </AdminGuard>
                }
              />
              <Route path="permission-requests" element={<PermissionRequestsPage />} />
              <Route path="mentions" element={<MentionsPage />} />
              <Route path="notifications" element={<NotificationsPage />} />
              <Route path="marketplace" element={<MarketplacePage />} />
              <Route
                path="functions/:ontology/:functionRid/diff"
                element={<FunctionDiffPage />}
              />
              <Route
                path="functions/:ontology/:functionRid/code"
                element={<FunctionCodePage />}
              />
              <Route
                path="functions/:ontology/:functionRid/repo"
                element={<FunctionRepoPage />}
              />
              <Route path="settings" element={<SettingsPage />} />
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
              <Route path="aggregation/:ontology/:objectType" element={<AggregationPage />} />
              <Route path="quiver/:ontology" element={<QuiverPage />} />
              <Route path="quiver/:ontology/:rid" element={<QuiverPage />} />
              <Route path="quiver/:ontology/:rid/view" element={<QuiverViewPage />} />
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
                path="admin/:ontology/valueTypes"
                element={
                  <AdminGuard>
                    <ValueTypeAdminPage />
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
              <Route
                path="admin/compliance"
                element={
                  <PermissionRoute permission="user.manage">
                    <ComplianceReportsPage />
                  </PermissionRoute>
                }
              />
              <Route
                path="admin/perf"
                element={
                  <PermissionRoute permission="user.manage">
                    <PerformanceDashboardPage />
                  </PermissionRoute>
                }
              />
              <Route
                path="audit"
                element={
                  <PermissionRoute permission="user.manage">
                    <AuditReportPage />
                  </PermissionRoute>
                }
              />
            </Route>
          </Routes>
        </AuthProvider>
      </BrowserRouter>
      </QueryClientProvider>
    </GlobalErrorBoundary>
  );
}
