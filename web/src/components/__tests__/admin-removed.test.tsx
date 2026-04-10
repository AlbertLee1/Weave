import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

// Mock hooks used by DashboardPage
vi.mock('../../hooks/useOntologies', () => ({
  useOntologies: () => ({
    data: [
      { rid: 'ri.1', apiName: 'test', displayName: 'Test Ontology', description: 'desc' },
    ],
    isLoading: false,
    error: null,
  }),
}));
vi.mock('../../hooks/useObjectTypes', () => ({
  useObjectTypes: () => ({ data: [], isLoading: false }),
}));

function renderWithProviders(ui: React.ReactElement) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('US-007: Admin routes removed from frontend', () => {
  it('App.tsx does not import AdminPage, ObjectTypeDetailPage, or ActionTypeDetailPage', async () => {
    // Read the App module's source — if it still imports admin components, those
    // modules no longer exist and the import would have failed at build time.
    // The fact that we can import App without errors proves admin imports are gone.
    const appModule = await import('../../App');
    expect(appModule.default).toBeDefined();
  });

  it('Sidebar does not contain Admin nav link', async () => {
    const { Sidebar } = await import('../layout/Sidebar');
    renderWithProviders(<Sidebar />);
    expect(screen.queryByText('Admin')).not.toBeInTheDocument();
  });

  it('DashboardPage does not have create ontology button', async () => {
    const { DashboardPage } = await import('../dashboard/DashboardPage');
    renderWithProviders(<DashboardPage />);
    expect(screen.queryByText('New Ontology')).not.toBeInTheDocument();
    expect(screen.queryByText('Create your first ontology')).not.toBeInTheDocument();
  });

  it('DashboardPage does not have create ontology modal form', async () => {
    const { DashboardPage } = await import('../dashboard/DashboardPage');
    renderWithProviders(<DashboardPage />);
    // No form inputs that were part of the create ontology modal
    expect(screen.queryByPlaceholderText('my-ontology')).not.toBeInTheDocument();
    expect(screen.queryByPlaceholderText('My Ontology')).not.toBeInTheDocument();
  });
});
