import { BrowserRouter, Routes, Route } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Shell } from './components/layout/Shell';
import { DashboardPage } from './components/dashboard/DashboardPage';
import { ExplorerPage } from './components/explorer/ExplorerPage';
import { BrowserPage } from './components/browser/BrowserPage';
import { AdminPage } from './components/admin/AdminPage';
import { ObjectTypeDetailPage } from './components/admin/ObjectTypeDetailPage';
import { ActionTypeDetailPage } from './components/admin/ActionTypeDetailPage';
import { ActionConsolePage } from './components/actions/ActionConsolePage';
import { AggregationPage } from './components/aggregation/AggregationPage';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: 1,
    },
  },
});

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route element={<Shell />}>
            <Route index element={<DashboardPage />} />
            <Route path="explorer/:ontology" element={<ExplorerPage />} />
            <Route path="explorer/:ontology/:objectType" element={<ExplorerPage />} />
            <Route path="browser/:ontology/:objectType" element={<BrowserPage />} />
            <Route path="admin" element={<AdminPage />} />
            <Route path="admin/:ontology" element={<AdminPage />} />
            <Route path="admin/:ontology/object-types/:objectType" element={<ObjectTypeDetailPage />} />
            <Route path="admin/:ontology/action-types/:actionType" element={<ActionTypeDetailPage />} />
            <Route path="actions/:ontology" element={<ActionConsolePage />} />
            <Route path="aggregation/:ontology/:objectType" element={<AggregationPage />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  );
}
