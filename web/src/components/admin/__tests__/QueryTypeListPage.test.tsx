import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { QueryTypeListPage } from '../QueryTypeListPage';
import * as adminApi from '../../../api/admin';

vi.mock('../../../api/admin', async () => {
  const actual = await vi.importActual<typeof import('../../../api/admin')>(
    '../../../api/admin',
  );
  return {
    ...actual,
    listQueryTypes: vi.fn(),
    createQueryType: vi.fn(),
    deleteQueryType: vi.fn(),
    executeQueryType: vi.fn(),
  };
});

function renderWithClient(ui: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

describe('QueryTypeListPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders empty state when no query types', async () => {
    vi.mocked(adminApi.listQueryTypes).mockResolvedValue({ data: [] });

    renderWithClient(<QueryTypeListPage ontologyApiName="test-ont" />);

    expect(await screen.findByText('No Query Types')).toBeInTheDocument();
  });

  it('renders query types with apiName and status badge', async () => {
    vi.mocked(adminApi.listQueryTypes).mockResolvedValue({
      data: [
        {
          rid: 'ri.q.1',
          apiName: 'topCustomers',
          displayName: 'Top Customers',
          parameters: {},
          output: {},
          query: {},
          status: 'ACTIVE',
        },
      ],
    });

    renderWithClient(<QueryTypeListPage ontologyApiName="test-ont" />);

    expect(await screen.findByText('topCustomers')).toBeInTheDocument();
    expect(screen.getByText('ACTIVE')).toBeInTheDocument();
  });

  it('validates JSON fields in create form', async () => {
    vi.mocked(adminApi.listQueryTypes).mockResolvedValue({ data: [] });

    renderWithClient(<QueryTypeListPage ontologyApiName="test-ont" />);

    await screen.findByText('No Query Types');
    const createButtons = screen.getAllByText(/Create Query Type/);
    fireEvent.click(createButtons[0]);

    fireEvent.change(screen.getByPlaceholderText('e.g. topCustomers'), {
      target: { value: 'foo' },
    });
    fireEvent.change(screen.getByPlaceholderText('e.g. Top Customers'), {
      target: { value: 'Foo' },
    });
    fireEvent.change(screen.getByPlaceholderText(/"limit"/), {
      target: { value: '{not json' },
    });
    fireEvent.change(screen.getByPlaceholderText(/"type": "array"/), {
      target: { value: '{}' },
    });
    fireEvent.change(screen.getByPlaceholderText(/"type": "aggregation"/), {
      target: { value: '{}' },
    });

    fireEvent.click(screen.getByRole('button', { name: /^Create$/ }));

    expect(await screen.findByText('Invalid JSON')).toBeInTheDocument();
    expect(adminApi.createQueryType).not.toHaveBeenCalled();
  });

  it('calls createQueryType with parsed JSON fields', async () => {
    vi.mocked(adminApi.listQueryTypes).mockResolvedValue({ data: [] });
    vi.mocked(adminApi.createQueryType).mockResolvedValue({
      rid: 'ri.new',
      apiName: 'foo',
      displayName: 'Foo',
      parameters: {},
      output: {},
      query: {},
      status: 'ACTIVE',
    });

    renderWithClient(<QueryTypeListPage ontologyApiName="test-ont" />);

    await screen.findByText('No Query Types');
    const createButtons = screen.getAllByText(/Create Query Type/);
    fireEvent.click(createButtons[0]);

    fireEvent.change(screen.getByPlaceholderText('e.g. topCustomers'), {
      target: { value: 'foo' },
    });
    fireEvent.change(screen.getByPlaceholderText('e.g. Top Customers'), {
      target: { value: 'Foo' },
    });
    fireEvent.change(screen.getByPlaceholderText(/"limit"/), {
      target: { value: '{"limit": 10}' },
    });
    fireEvent.change(screen.getByPlaceholderText(/"type": "array"/), {
      target: { value: '{"type": "array"}' },
    });
    fireEvent.change(screen.getByPlaceholderText(/"type": "aggregation"/), {
      target: { value: '{"type": "aggregation"}' },
    });

    fireEvent.click(screen.getByRole('button', { name: /^Create$/ }));

    await waitFor(() => {
      expect(adminApi.createQueryType).toHaveBeenCalledWith(
        'test-ont',
        expect.objectContaining({
          apiName: 'foo',
          displayName: 'Foo',
          parameters: { limit: 10 },
          output: { type: 'array' },
          query: { type: 'aggregation' },
          status: 'ACTIVE',
        }),
      );
    });
  });

  it('deletes a query type after confirmation', async () => {
    vi.mocked(adminApi.listQueryTypes).mockResolvedValue({
      data: [
        {
          rid: 'ri.q.del',
          apiName: 'toDel',
          displayName: 'ToDel',
          parameters: {},
          output: {},
          query: {},
          status: 'ACTIVE',
        },
      ],
    });
    vi.mocked(adminApi.deleteQueryType).mockResolvedValue(undefined);

    renderWithClient(<QueryTypeListPage ontologyApiName="test-ont" />);

    await screen.findByText('toDel');
    fireEvent.click(screen.getByTitle('Delete'));

    const deleteBtns = await screen.findAllByRole('button', { name: /^Delete$/ });
    fireEvent.click(deleteBtns[deleteBtns.length - 1]);

    await waitFor(() => {
      expect(adminApi.deleteQueryType).toHaveBeenCalledWith('ri.q.del');
    });
  });

  it('opens slide panel and executes query', async () => {
    vi.mocked(adminApi.listQueryTypes).mockResolvedValue({
      data: [
        {
          rid: 'ri.q.1',
          apiName: 'topCustomers',
          displayName: 'Top Customers',
          description: 'Return top customers',
          parameters: { limit: { type: 'integer' } },
          output: {},
          query: {},
          status: 'ACTIVE',
        },
      ],
    });
    vi.mocked(adminApi.executeQueryType).mockResolvedValue({ rows: [{ id: 1 }] });

    renderWithClient(<QueryTypeListPage ontologyApiName="test-ont" />);

    await screen.findByText('topCustomers');
    fireEvent.click(screen.getByText('topCustomers'));

    // Slide panel opens
    expect(await screen.findByText(/Execute: topCustomers/)).toBeInTheDocument();
    expect(screen.getByText('Return top customers')).toBeInTheDocument();

    // Fill parameters and execute
    const paramsInput = screen.getByPlaceholderText(/^\{\}$/);
    fireEvent.change(paramsInput, { target: { value: '{"limit": 5}' } });

    fireEvent.click(screen.getByRole('button', { name: /^Execute$/ }));

    await waitFor(() => {
      expect(adminApi.executeQueryType).toHaveBeenCalledWith(
        'test-ont',
        'topCustomers',
        { limit: 5 },
      );
    });

    // Result is displayed
    expect(await screen.findByTestId('execute-result')).toHaveTextContent('rows');
  });

  it('shows error when execute fails', async () => {
    vi.mocked(adminApi.listQueryTypes).mockResolvedValue({
      data: [
        {
          rid: 'ri.q.1',
          apiName: 'topCustomers',
          displayName: 'Top Customers',
          parameters: {},
          output: {},
          query: {},
          status: 'ACTIVE',
        },
      ],
    });
    vi.mocked(adminApi.executeQueryType).mockRejectedValue(new Error('BadRequest: failed'));

    renderWithClient(<QueryTypeListPage ontologyApiName="test-ont" />);

    await screen.findByText('topCustomers');
    fireEvent.click(screen.getByText('topCustomers'));

    await screen.findByText(/Execute: topCustomers/);
    fireEvent.click(screen.getByRole('button', { name: /^Execute$/ }));

    expect(await screen.findByText(/BadRequest: failed/)).toBeInTheDocument();
  });
});
