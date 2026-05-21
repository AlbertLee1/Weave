import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AggregationPage } from '../AggregationPage';

let capturedAggregateBody: unknown = null;

const server = setupServer(
  http.get('/api/v2/ontologies/:ontology/objectTypes/:objectType', ({ params }) =>
    HttpResponse.json({
      rid: 'ri.ontology.main.object-type.order',
      apiName: String(params.objectType ?? 'order'),
      displayName: 'Order',
      primaryKey: 'orderID',
      status: 'ACTIVE',
      visibility: 'PROMINENT',
      properties: {
        orderID: { dataType: { type: 'string' }, rid: 'ri.p.order.id' },
        shipCountry: { dataType: { type: 'string' }, rid: 'ri.p.order.country' },
        freight: { dataType: { type: 'double' }, rid: 'ri.p.order.freight' },
        active: { dataType: { type: 'boolean' }, rid: 'ri.p.order.active' },
      },
    }),
  ),
  http.post('/api/v2/ontologies/:ontology/objects/:objectType/aggregate', async ({ request }) => {
    capturedAggregateBody = await request.json();
    const where = (capturedAggregateBody as { where?: { field?: string; value?: unknown } })?.where;
    const count = where?.field === 'freight' && where.value === 10 ? 2 : where?.field === 'active' && where.value === true ? 3 : 1;
    return HttpResponse.json({
      data: [{ metrics: [{ name: 'count', value: count }] }],
      accuracy: 'ACCURATE',
    });
  }),
);

beforeAll(() => server.listen());
afterEach(() => {
  capturedAggregateBody = null;
  server.resetHandlers();
});
afterAll(() => server.close());

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/aggregation/northwind/order']}>
        <Routes>
          <Route path="/aggregation/:ontology/:objectType" element={<AggregationPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BDD: AggregationPage where filter contract (P2B-502)', () => {
  it('Given a where filter is configured, When Execute is clicked, Then the aggregate request includes where', async () => {
    renderPage();

    fireEvent.change(await screen.findByTestId('filter-field-select'), {
      target: { value: 'shipCountry' },
    });
    fireEvent.change(screen.getByTestId('filter-op-select'), {
      target: { value: 'eq' },
    });
    fireEvent.change(screen.getByTestId('filter-value-input'), {
      target: { value: 'USA' },
    });
    fireEvent.click(screen.getByTestId('filter-add-btn'));
    fireEvent.click(screen.getByTestId('aggregation-execute'));

    await waitFor(() => {
      expect(capturedAggregateBody).not.toBeNull();
    });

    const body = capturedAggregateBody as {
      aggregation?: unknown;
      groupBy?: unknown;
      where?: unknown;
    };
    expect(body.aggregation).toEqual([{ type: 'count' }]);
    expect(body.where).toEqual({ type: 'eq', field: 'shipCountry', value: 'USA' });
    expect(Object.prototype.hasOwnProperty.call(body, 'groupBy')).toBe(false);
  });

  it('Given a numeric equality filter, When Execute is clicked, Then where.value is a number and results reflect the typed filter', async () => {
    renderPage();

    fireEvent.change(await screen.findByTestId('filter-field-select'), {
      target: { value: 'freight' },
    });
    fireEvent.change(screen.getByTestId('filter-op-select'), {
      target: { value: 'eq' },
    });
    fireEvent.change(screen.getByTestId('filter-value-input'), {
      target: { value: '10' },
    });
    fireEvent.click(screen.getByTestId('filter-add-btn'));
    fireEvent.click(screen.getByTestId('aggregation-execute'));

    await waitFor(() => {
      expect(capturedAggregateBody).not.toBeNull();
    });

    const body = capturedAggregateBody as { where?: { field?: string; value?: unknown } };
    expect(body.where).toEqual({ type: 'eq', field: 'freight', value: 10 });
    await screen.findByText('2');
  });

  it('Given a boolean equality filter, When Execute is clicked, Then where.value is a boolean and results reflect the typed filter', async () => {
    renderPage();

    fireEvent.change(await screen.findByTestId('filter-field-select'), {
      target: { value: 'active' },
    });
    fireEvent.change(screen.getByTestId('filter-op-select'), {
      target: { value: 'eq' },
    });
    fireEvent.change(screen.getByTestId('filter-boolean-value-select'), {
      target: { value: 'true' },
    });
    fireEvent.click(screen.getByTestId('filter-add-btn'));
    fireEvent.click(screen.getByTestId('aggregation-execute'));

    await waitFor(() => {
      expect(capturedAggregateBody).not.toBeNull();
    });

    const body = capturedAggregateBody as { where?: { field?: string; value?: unknown } };
    expect(body.where).toEqual({ type: 'eq', field: 'active', value: true });
    await screen.findByText('3');
  });

  it('Given no where filter is configured, When Execute is clicked, Then the request omits where', async () => {
    renderPage();

    await screen.findByTestId('aggregation-execute');
    fireEvent.click(screen.getByTestId('aggregation-execute'));

    await waitFor(() => {
      expect(capturedAggregateBody).not.toBeNull();
    });

    const body = capturedAggregateBody as { where?: unknown };
    expect(body.where).toBeUndefined();
    expect(Object.prototype.hasOwnProperty.call(body, 'where')).toBe(false);
  });
});
