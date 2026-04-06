import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { SharedPropertyListPage } from '../SharedPropertyListPage';
import * as adminApi from '../../../api/admin';

vi.mock('../../../api/admin', async () => {
  const actual = await vi.importActual<typeof import('../../../api/admin')>(
    '../../../api/admin',
  );
  return {
    ...actual,
    listSharedProperties: vi.fn(),
    createSharedProperty: vi.fn(),
    deleteSharedProperty: vi.fn(),
  };
});

function renderWithClient(ui: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

describe('SharedPropertyListPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders empty state when no shared properties', async () => {
    vi.mocked(adminApi.listSharedProperties).mockResolvedValue({ data: [] });

    renderWithClient(<SharedPropertyListPage ontologyApiName="test-ont" />);

    expect(await screen.findByText('No Shared Properties')).toBeInTheDocument();
  });

  it('renders shared properties from API', async () => {
    vi.mocked(adminApi.listSharedProperties).mockResolvedValue({
      data: [
        {
          rid: 'ri.1',
          apiName: 'latitude',
          displayName: 'Latitude',
          baseType: 'double',
          isArray: false,
        },
        {
          rid: 'ri.2',
          apiName: 'tags',
          displayName: 'Tags',
          baseType: 'string',
          isArray: true,
        },
      ],
    });

    renderWithClient(<SharedPropertyListPage ontologyApiName="test-ont" />);

    expect(await screen.findByText('latitude')).toBeInTheDocument();
    expect(screen.getByText('tags')).toBeInTheDocument();
    expect(screen.getByText('double')).toBeInTheDocument();
    expect(screen.getByText('scalar')).toBeInTheDocument();
    expect(screen.getByText('array')).toBeInTheDocument();
  });

  it('opens create modal and calls createSharedProperty', async () => {
    vi.mocked(adminApi.listSharedProperties).mockResolvedValue({ data: [] });
    vi.mocked(adminApi.createSharedProperty).mockResolvedValue({
      rid: 'ri.new',
      apiName: 'foo',
      baseType: 'string',
      isArray: false,
    });

    renderWithClient(<SharedPropertyListPage ontologyApiName="test-ont" />);

    await screen.findByText('No Shared Properties');
    // Click the CTA create button (in the EmptyState)
    const createButtons = screen.getAllByText(/Create Shared Property/);
    fireEvent.click(createButtons[0]);

    // Modal fields
    fireEvent.change(screen.getByPlaceholderText('e.g. latitude'), {
      target: { value: 'foo' },
    });

    const submit = screen.getByRole('button', { name: /^Create$/ });
    fireEvent.click(submit);

    await waitFor(() => {
      expect(adminApi.createSharedProperty).toHaveBeenCalledWith(
        'test-ont',
        expect.objectContaining({
          apiName: 'foo',
          baseType: 'string',
          isArray: false,
        }),
      );
    });
  });

  it('confirms and calls deleteSharedProperty', async () => {
    vi.mocked(adminApi.listSharedProperties).mockResolvedValue({
      data: [
        {
          rid: 'ri.del',
          apiName: 'toDelete',
          baseType: 'string',
          isArray: false,
        },
      ],
    });
    vi.mocked(adminApi.deleteSharedProperty).mockResolvedValue(undefined);

    renderWithClient(<SharedPropertyListPage ontologyApiName="test-ont" />);

    await screen.findByText('toDelete');
    fireEvent.click(screen.getByTitle('Delete'));

    // Confirm modal — heading "Delete Shared Property" also appears, so target red button
    const deleteBtns = await screen.findAllByRole('button', { name: /^Delete$/ });
    fireEvent.click(deleteBtns[deleteBtns.length - 1]);

    await waitFor(() => {
      expect(adminApi.deleteSharedProperty).toHaveBeenCalledWith('ri.del');
    });
  });
});
