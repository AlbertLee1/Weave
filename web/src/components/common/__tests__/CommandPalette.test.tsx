import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { CommandPalette } from '../CommandPalette';
import type { Ontology, ObjectType } from '../../../api/types';

const ontologies: Ontology[] = [
  {
    rid: 'ri.ontology.main.ontology.northwind',
    apiName: 'northwind',
    displayName: 'Northwind',
  },
  {
    rid: 'ri.ontology.main.ontology.chinook',
    apiName: 'chinook',
    displayName: 'Chinook',
  },
];

const northwindObjectTypes: ObjectType[] = [
  {
    rid: 'ri.phonograph2-objects.main.object-type.product',
    apiName: 'Product',
    displayName: 'Product',
    primaryKey: 'productId',
    status: 'ACTIVE',
    visibility: 'PROMINENT',
  },
  {
    rid: 'ri.phonograph2-objects.main.object-type.customer',
    apiName: 'Customer',
    displayName: 'Customer',
    primaryKey: 'customerId',
    status: 'ACTIVE',
    visibility: 'PROMINENT',
  },
  {
    rid: 'ri.phonograph2-objects.main.object-type.order',
    apiName: 'Order',
    displayName: 'Sales Order',
    primaryKey: 'orderId',
    status: 'ACTIVE',
    visibility: 'PROMINENT',
  },
];

function setupFetchStub() {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.endsWith('/api/v2/ontologies')) {
        return new Response(JSON.stringify({ data: ontologies }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }
      if (url.endsWith('/api/v2/ontologies/northwind/objectTypes')) {
        return new Response(JSON.stringify({ data: northwindObjectTypes }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }
      if (url.includes('/objectTypes')) {
        return new Response(JSON.stringify({ data: [] }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }
      return new Response('{}', { status: 200 });
    }),
  );
}

function renderPalette(props: { open: boolean; onClose?: () => void; activeOntology?: string | null } = { open: true }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/']}>
        <CommandPalette
          open={props.open}
          onClose={props.onClose ?? (() => {})}
          activeOntology={props.activeOntology ?? null}
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('CommandPalette', () => {
  it('does not render when closed', () => {
    setupFetchStub();
    renderPalette({ open: false });
    expect(screen.queryByTestId('command-palette')).not.toBeInTheDocument();
  });

  it('renders modal with search input when open', () => {
    setupFetchStub();
    renderPalette({ open: true });
    expect(screen.getByTestId('command-palette')).toBeInTheDocument();
    expect(screen.getByPlaceholderText(/search/i)).toBeInTheDocument();
  });

  it('lists static pages (Dashboard, AIP Threads)', () => {
    setupFetchStub();
    renderPalette({ open: true });
    expect(screen.getByRole('option', { name: /dashboard/i })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: /aip threads/i })).toBeInTheDocument();
  });

  it('lists object types from the active ontology', async () => {
    setupFetchStub();
    renderPalette({ open: true, activeOntology: 'northwind' });
    expect(
      await screen.findByRole('option', { name: /product/i }),
    ).toBeInTheDocument();
    expect(screen.getByRole('option', { name: /customer/i })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: /sales order/i })).toBeInTheDocument();
  });

  it('filters across object types as the user types (cross-type search)', async () => {
    setupFetchStub();
    const user = userEvent.setup();
    renderPalette({ open: true, activeOntology: 'northwind' });
    await screen.findByRole('option', { name: /product/i });

    const input = screen.getByPlaceholderText(/search/i);
    await user.type(input, 'cust');

    // Filtered down to Customer only — Product should be gone.
    expect(screen.getByRole('option', { name: /customer/i })).toBeInTheDocument();
    expect(screen.queryByRole('option', { name: /^product$/i })).not.toBeInTheDocument();
  });

  it('shows empty state when nothing matches', async () => {
    setupFetchStub();
    const user = userEvent.setup();
    renderPalette({ open: true, activeOntology: 'northwind' });
    await screen.findByRole('option', { name: /product/i });
    const input = screen.getByPlaceholderText(/search/i);
    await user.type(input, 'zzznomatchxyz');
    expect(screen.getByTestId('command-palette-empty')).toBeInTheDocument();
  });

  it('closes on Escape key', () => {
    setupFetchStub();
    const onClose = vi.fn();
    renderPalette({ open: true, onClose });
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalled();
  });

  it('closes when clicking the overlay', () => {
    setupFetchStub();
    const onClose = vi.fn();
    renderPalette({ open: true, onClose });
    fireEvent.click(screen.getByTestId('command-palette-overlay'));
    expect(onClose).toHaveBeenCalled();
  });

  it('navigates and closes when an option is selected with Enter', async () => {
    setupFetchStub();
    const user = userEvent.setup();
    const onClose = vi.fn();
    renderPalette({ open: true, onClose, activeOntology: 'northwind' });
    await screen.findByRole('option', { name: /product/i });

    const input = screen.getByPlaceholderText(/search/i);
    await user.type(input, 'product');
    await user.keyboard('{Enter}');

    expect(onClose).toHaveBeenCalled();
  });
});
