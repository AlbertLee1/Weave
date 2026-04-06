import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { TypeGroupListPage } from '../TypeGroupListPage';
import * as adminApi from '../../../api/admin';
import * as ontologiesApi from '../../../api/ontologies';

vi.mock('../../../api/admin', async () => {
  const actual = await vi.importActual<typeof import('../../../api/admin')>(
    '../../../api/admin',
  );
  return {
    ...actual,
    listTypeGroups: vi.fn(),
    createTypeGroup: vi.fn(),
    deleteTypeGroup: vi.fn(),
    assignTypeGroup: vi.fn(),
    removeTypeGroup: vi.fn(),
    listTypeGroupsForObjectType: vi.fn(),
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

describe('TypeGroupListPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(ontologiesApi.listObjectTypes).mockResolvedValue(mockObjectTypes);
  });

  it('renders empty state when no groups exist', async () => {
    vi.mocked(adminApi.listTypeGroups).mockResolvedValue({ data: [] });

    renderWithClient(<TypeGroupListPage ontologyApiName="test-ont" />);

    expect(await screen.findByText('No Type Groups')).toBeInTheDocument();
  });

  it('renders groups with apiName and color', async () => {
    vi.mocked(adminApi.listTypeGroups).mockResolvedValue({
      data: [
        {
          rid: 'ri.g.1',
          apiName: 'people',
          displayName: 'People',
          color: '#3b82f6',
        },
      ],
    });

    renderWithClient(<TypeGroupListPage ontologyApiName="test-ont" />);

    expect(await screen.findByText('people')).toBeInTheDocument();
    expect(screen.getByText('People')).toBeInTheDocument();
    expect(screen.getByTestId('color-swatch-people')).toHaveStyle({
      backgroundColor: '#3b82f6',
    });
  });

  it('calls createTypeGroup on form submit', async () => {
    vi.mocked(adminApi.listTypeGroups).mockResolvedValue({ data: [] });
    vi.mocked(adminApi.createTypeGroup).mockResolvedValue({
      rid: 'ri.new',
      apiName: 'people',
      displayName: 'People',
    });

    renderWithClient(<TypeGroupListPage ontologyApiName="test-ont" />);

    await screen.findByText('No Type Groups');
    const createButtons = screen.getAllByText(/Create Type Group/);
    fireEvent.click(createButtons[0]);

    fireEvent.change(screen.getByPlaceholderText('e.g. people'), {
      target: { value: 'people' },
    });
    fireEvent.change(screen.getByPlaceholderText('e.g. People'), {
      target: { value: 'People' },
    });

    const submit = screen.getByRole('button', { name: /^Create$/ });
    fireEvent.click(submit);

    await waitFor(() => {
      expect(adminApi.createTypeGroup).toHaveBeenCalledWith(
        'test-ont',
        expect.objectContaining({ apiName: 'people', displayName: 'People' }),
      );
    });
  });

  it('calls deleteTypeGroup after confirmation', async () => {
    vi.mocked(adminApi.listTypeGroups).mockResolvedValue({
      data: [
        {
          rid: 'ri.g.1',
          apiName: 'people',
          displayName: 'People',
        },
      ],
    });
    vi.mocked(adminApi.deleteTypeGroup).mockResolvedValue(undefined);

    renderWithClient(<TypeGroupListPage ontologyApiName="test-ont" />);

    await screen.findByText('people');
    fireEvent.click(screen.getByTitle('Delete'));

    const deleteBtns = await screen.findAllByRole('button', { name: /^Delete$/ });
    fireEvent.click(deleteBtns[deleteBtns.length - 1]);

    await waitFor(() => {
      expect(adminApi.deleteTypeGroup).toHaveBeenCalledWith('ri.g.1');
    });
  });

  it('assigns and removes groups for an object type', async () => {
    vi.mocked(adminApi.listTypeGroups).mockResolvedValue({
      data: [
        { rid: 'ri.g.1', apiName: 'people', displayName: 'People' },
        { rid: 'ri.g.2', apiName: 'places', displayName: 'Places' },
      ],
    });
    vi.mocked(adminApi.listTypeGroupsForObjectType).mockResolvedValue({
      data: [{ rid: 'ri.g.1', apiName: 'people', displayName: 'People' }],
    });
    vi.mocked(adminApi.assignTypeGroup).mockResolvedValue(undefined);
    vi.mocked(adminApi.removeTypeGroup).mockResolvedValue(undefined);

    renderWithClient(<TypeGroupListPage ontologyApiName="test-ont" />);

    // Wait for groups to load
    await screen.findByText('people');

    // Select object type in assignment section
    const selects = screen.getAllByRole('combobox');
    // First combobox = object type selector
    fireEvent.change(selects[0], { target: { value: 'ri.ot.1' } });

    // Assigned groups should show "people"
    await waitFor(() => {
      expect(screen.getByLabelText('Remove people')).toBeInTheDocument();
    });

    // Remove assigned group
    fireEvent.click(screen.getByLabelText('Remove people'));
    await waitFor(() => {
      expect(adminApi.removeTypeGroup).toHaveBeenCalledWith('ri.ot.1', 'ri.g.1');
    });

    // The unassigned dropdown should include "places"
    const selectsAfter = screen.getAllByRole('combobox');
    // The assign dropdown is the last combobox
    const assignSelect = selectsAfter[selectsAfter.length - 1];
    fireEvent.change(assignSelect, { target: { value: 'ri.g.2' } });

    fireEvent.click(screen.getByRole('button', { name: /^Assign$/ }));
    await waitFor(() => {
      expect(adminApi.assignTypeGroup).toHaveBeenCalledWith('ri.ot.1', 'ri.g.2');
    });
  });
});
