import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { DatasourceBindingListPage } from '../DatasourceBindingListPage';
import * as adminApi from '../../../api/admin';
import * as ontologiesApi from '../../../api/ontologies';

vi.mock('../../../api/admin', async () => {
  const actual = await vi.importActual<typeof import('../../../api/admin')>(
    '../../../api/admin',
  );
  return {
    ...actual,
    listDatasourceBindings: vi.fn(),
    createDatasourceBinding: vi.fn(),
    deleteDatasourceBinding: vi.fn(),
  };
});

vi.mock('../../../api/ontologies', async () => {
  const actual = await vi.importActual<typeof import('../../../api/ontologies')>(
    '../../../api/ontologies',
  );
  return {
    ...actual,
    listObjectTypes: vi.fn(),
  };
});

function renderWithClient(ui: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

const mockObjectTypes = [
  {
    rid: 'ri.ot.1',
    apiName: 'Customer',
    displayName: 'Customer',
    primaryKey: 'id',
    status: 'ACTIVE' as const,
    visibility: 'NORMAL' as const,
  },
];

describe('DatasourceBindingListPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(ontologiesApi.listObjectTypes).mockResolvedValue(mockObjectTypes);
  });

  it('shows empty state when no object type is selected', async () => {
    renderWithClient(<DatasourceBindingListPage ontologyApiName="test-ont" />);

    expect(await screen.findByText('Select an Object Type')).toBeInTheDocument();
  });

  it('loads bindings after selecting an object type', async () => {
    vi.mocked(adminApi.listDatasourceBindings).mockResolvedValue({
      data: [
        {
          rid: 'ri.b.1',
          datasetRid: 'ri.foundry.main.dataset.abc',
          branch: 'master',
          isPrimary: true,
        },
      ],
    });

    renderWithClient(<DatasourceBindingListPage ontologyApiName="test-ont" />);

    // Wait for the object types selector to populate
    await waitFor(() => {
      expect(screen.getByRole('combobox')).toBeInTheDocument();
    });

    fireEvent.change(screen.getByRole('combobox'), {
      target: { value: 'ri.ot.1' },
    });

    expect(await screen.findByText('ri.foundry.main.dataset.abc')).toBeInTheDocument();
    expect(screen.getByText('primary')).toBeInTheDocument();
  });

  it('rejects invalid columnMapping JSON in create form', async () => {
    vi.mocked(adminApi.listDatasourceBindings).mockResolvedValue({ data: [] });

    renderWithClient(<DatasourceBindingListPage ontologyApiName="test-ont" />);

    await waitFor(() => {
      expect(screen.getByRole('combobox')).toBeInTheDocument();
    });
    fireEvent.change(screen.getByRole('combobox'), { target: { value: 'ri.ot.1' } });

    await screen.findByText('No Datasource Bindings');
    const openButtons = screen.getAllByText(/Create Binding/);
    fireEvent.click(openButtons[0]);

    fireEvent.change(screen.getByPlaceholderText('ri.foundry.main.dataset.uuid'), {
      target: { value: 'ri.ds.1' },
    });
    fireEvent.change(screen.getByPlaceholderText(/"id": "user_id"/), {
      target: { value: '{not json' },
    });

    const submit = screen.getByRole('button', { name: /^Create$/ });
    fireEvent.click(submit);

    expect(await screen.findByText('Invalid JSON')).toBeInTheDocument();
    expect(adminApi.createDatasourceBinding).not.toHaveBeenCalled();
  });

  it('calls createDatasourceBinding with valid input', async () => {
    vi.mocked(adminApi.listDatasourceBindings).mockResolvedValue({ data: [] });
    vi.mocked(adminApi.createDatasourceBinding).mockResolvedValue({
      rid: 'ri.new',
      datasetRid: 'ri.ds.1',
      branch: 'master',
      isPrimary: false,
    });

    renderWithClient(<DatasourceBindingListPage ontologyApiName="test-ont" />);

    await waitFor(() => {
      expect(screen.getByRole('combobox')).toBeInTheDocument();
    });
    fireEvent.change(screen.getByRole('combobox'), { target: { value: 'ri.ot.1' } });

    await screen.findByText('No Datasource Bindings');
    const openButtons = screen.getAllByText(/Create Binding/);
    fireEvent.click(openButtons[0]);

    fireEvent.change(screen.getByPlaceholderText('ri.foundry.main.dataset.uuid'), {
      target: { value: 'ri.ds.1' },
    });
    const submit = screen.getByRole('button', { name: /^Create$/ });
    fireEvent.click(submit);

    await waitFor(() => {
      expect(adminApi.createDatasourceBinding).toHaveBeenCalledWith(
        'ri.ot.1',
        expect.objectContaining({ datasetRid: 'ri.ds.1', branch: 'master' }),
      );
    });
  });
});
