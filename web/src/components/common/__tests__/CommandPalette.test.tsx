import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { CommandPalette } from '../CommandPalette';
import { useRecentCommandsStore } from '../../../stores/recentCommandsStore';
import type {
  ActionType,
  ObjectType,
  Ontology,
  OntologyBranch,
} from '../../../api/types';

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

const northwindActionTypes: ActionType[] = [
  {
    rid: 'ri.actions.main.action-type.createOrder',
    apiName: 'createOrder',
    displayName: 'Create Order',
    status: 'ACTIVE',
    parameters: {},
  },
  {
    rid: 'ri.actions.main.action-type.shipOrder',
    apiName: 'shipOrder',
    displayName: 'Ship Order',
    status: 'ACTIVE',
    parameters: {},
  },
];

const northwindBranches: OntologyBranch[] = [
  {
    id: 'br-feature-x',
    ontologyRid: 'ri.ontology.main.ontology.northwind',
    name: 'feature-x',
    baseVersion: 1,
    status: 'open',
    createdBy: 'alice',
    createdAt: '2026-04-01T00:00:00Z',
    updatedAt: '2026-04-01T00:00:00Z',
  },
  {
    id: 'br-bugfix',
    ontologyRid: 'ri.ontology.main.ontology.northwind',
    name: 'bugfix',
    baseVersion: 1,
    status: 'open',
    createdBy: 'bob',
    createdAt: '2026-04-02T00:00:00Z',
    updatedAt: '2026-04-02T00:00:00Z',
  },
];

interface MinimalApp {
  rid: string;
  name: string;
}

const apps: MinimalApp[] = [
  { rid: 'ri.apps.main.app.dash1', name: 'CRM Dashboard' },
  { rid: 'ri.apps.main.app.console2', name: 'Approval Console' },
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
      if (url.endsWith('/api/v2/ontologies/northwind/actionTypes')) {
        return new Response(JSON.stringify({ data: northwindActionTypes }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }
      if (url.endsWith('/api/v2/ontologies/northwind/branches')) {
        return new Response(JSON.stringify({ data: northwindBranches }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }
      if (url.endsWith('/api/v2/apps')) {
        return new Response(JSON.stringify({ apps }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }
      if (url.includes('/objectTypes') || url.includes('/actionTypes') || url.includes('/branches')) {
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
  beforeEach(() => {
    // Reset persisted recents between tests so ordering assertions are
    // deterministic regardless of test execution order.
    useRecentCommandsStore.getState().clear();
    window.localStorage.clear();
  });

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

  it('lists action types from the active ontology', async () => {
    setupFetchStub();
    renderPalette({ open: true, activeOntology: 'northwind' });
    expect(
      await screen.findByRole('option', { name: /create order/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole('option', { name: /ship order/i }),
    ).toBeInTheDocument();
  });

  it('lists branches from the active ontology', async () => {
    setupFetchStub();
    renderPalette({ open: true, activeOntology: 'northwind' });
    expect(
      await screen.findByRole('option', { name: /feature-x/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole('option', { name: /bugfix/i }),
    ).toBeInTheDocument();
  });

  it('lists apps', async () => {
    setupFetchStub();
    renderPalette({ open: true });
    expect(
      await screen.findByRole('option', { name: /crm dashboard/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole('option', { name: /approval console/i }),
    ).toBeInTheDocument();
  });

  it('renders a category heading for each of the 5 result groups', async () => {
    setupFetchStub();
    renderPalette({ open: true, activeOntology: 'northwind' });
    await screen.findByRole('option', { name: /product/i });
    // Headings come from cmdk's <Command.Group heading="...">; cmdk
    // renders them with role="presentation", but the visible text is
    // exposed in the DOM. We assert each heading is present once.
    expect(screen.getByText(/^pages$/i)).toBeInTheDocument();
    expect(screen.getByText(/^actions$/i)).toBeInTheDocument();
    expect(screen.getByText(/^objects$/i)).toBeInTheDocument();
    expect(screen.getByText(/^branches$/i)).toBeInTheDocument();
    expect(screen.getByText(/^apps$/i)).toBeInTheDocument();
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

  it('records selections under Recent and surfaces them on the next open', async () => {
    setupFetchStub();
    const user = userEvent.setup();
    const { unmount } = renderPalette({ open: true, activeOntology: 'northwind' });
    const product = await screen.findByRole('option', { name: /product/i });
    await user.click(product);

    unmount();

    setupFetchStub();
    renderPalette({ open: true, activeOntology: 'northwind' });
    const recentHeading = await screen.findByText(/^recent$/i);
    expect(recentHeading).toBeInTheDocument();
    // The Recent group should contain the just-selected Product entry.
    const recentGroup = recentHeading.closest('[cmdk-group]') as HTMLElement | null;
    expect(recentGroup).not.toBeNull();
    if (recentGroup) {
      expect(
        within(recentGroup).getByRole('option', { name: /product/i }),
      ).toBeInTheDocument();
    }
  });

  it('orders Recent entries most-recent-first and dedupes repeated picks', async () => {
    setupFetchStub();
    const user = userEvent.setup();
    const first = renderPalette({ open: true, activeOntology: 'northwind' });
    await user.click(await screen.findByRole('option', { name: /product/i }));
    first.unmount();

    setupFetchStub();
    const second = renderPalette({ open: true, activeOntology: 'northwind' });
    await user.click(await screen.findByRole('option', { name: /customer/i }));
    second.unmount();

    setupFetchStub();
    const third = renderPalette({ open: true, activeOntology: 'northwind' });
    // Re-pick Product so Customer falls behind.
    await user.click(await screen.findByRole('option', { name: /product/i }));
    third.unmount();

    setupFetchStub();
    renderPalette({ open: true, activeOntology: 'northwind' });
    const recentHeading = await screen.findByText(/^recent$/i);
    const recentGroup = recentHeading.closest('[cmdk-group]') as HTMLElement | null;
    expect(recentGroup).not.toBeNull();
    if (recentGroup) {
      const options = within(recentGroup).getAllByRole('option');
      // No dupes — at most one entry per kind+id.
      const labels = options.map((o) => o.textContent ?? '');
      expect(labels.filter((l) => /Product/.test(l)).length).toBe(1);
      // Most-recent first: Product (re-picked) is above Customer.
      const productIdx = labels.findIndex((l) => /Product/.test(l));
      const customerIdx = labels.findIndex((l) => /Customer/.test(l));
      expect(productIdx).toBeGreaterThanOrEqual(0);
      expect(customerIdx).toBeGreaterThan(productIdx);
    }
  });
});
