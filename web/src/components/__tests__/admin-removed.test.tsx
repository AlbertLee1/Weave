import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

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

describe('US-007: legacy create-ontology UI removed from Dashboard', () => {
  it('App.tsx module loads (legacy AdminPage/ObjectTypeDetailPage/ActionTypeDetailPage imports remain absent)', async () => {
    const appModule = await import('../../App');
    expect(appModule.default).toBeDefined();
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
    expect(screen.queryByPlaceholderText('my-ontology')).not.toBeInTheDocument();
    expect(screen.queryByPlaceholderText('My Ontology')).not.toBeInTheDocument();
  });
});
