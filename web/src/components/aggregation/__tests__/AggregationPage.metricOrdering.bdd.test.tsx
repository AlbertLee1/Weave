import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AggregationPage } from '../AggregationPage';

interface CapturedMetric {
  type?: string;
  field?: string;
  name?: string;
  direction?: string;
}

let capturedAggregateBody: { aggregation?: CapturedMetric[]; groupBy?: unknown } | null = null;

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
      },
    }),
  ),
  http.post('/api/v2/ontologies/:ontology/objects/:objectType/aggregate', async ({ request }) => {
    capturedAggregateBody = (await request.json()) as typeof capturedAggregateBody;
    const metric = capturedAggregateBody?.aggregation?.[0];
    // Mirror the backend "按聚合值排序" contract: when a metric declares a
    // direction, the server returns the groupBy rows already ordered by that
    // metric's value. The frontend renders rows in server order.
    const rows = [
      { group: { shipCountry: 'USA' }, metrics: [{ name: 'freight.sum', value: 300 }] },
      { group: { shipCountry: 'France' }, metrics: [{ name: 'freight.sum', value: 200 }] },
      { group: { shipCountry: 'Brazil' }, metrics: [{ name: 'freight.sum', value: 100 }] },
    ];
    if (metric?.direction === 'ASC') rows.reverse();
    return HttpResponse.json({ data: rows, accuracy: 'ACCURATE' });
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

async function configureSumByCountry() {
  // metric: sum(freight)
  fireEvent.change(await screen.findByTestId('metric-0-type'), {
    target: { value: 'sum' },
  });
  fireEvent.change(screen.getByTestId('metric-0-field'), {
    target: { value: 'freight' },
  });
  // groupBy: shipCountry
  fireEvent.click(screen.getByTestId('groupby-add'));
  fireEvent.change(screen.getByTestId('groupby-0-field'), {
    target: { value: 'shipCountry' },
  });
}

describe('BDD: AggregationPage 按聚合值排序 (metric ordering direction)', () => {
  it('Given a metric sort direction DESC, When Execute is clicked, Then the request metric carries direction "DESC"', async () => {
    renderPage();
    await configureSumByCountry();

    // The sort-direction control for the metric.
    fireEvent.change(screen.getByTestId('metric-0-direction'), {
      target: { value: 'DESC' },
    });
    fireEvent.click(screen.getByTestId('aggregation-execute'));

    await waitFor(() => {
      expect(capturedAggregateBody).not.toBeNull();
    });

    const metric = capturedAggregateBody?.aggregation?.[0];
    expect(metric).toMatchObject({ type: 'sum', field: 'freight', direction: 'DESC' });
  });

  it('Given DESC ordering, When results render, Then rows appear in server-returned descending order', async () => {
    renderPage();
    await configureSumByCountry();

    fireEvent.change(screen.getByTestId('metric-0-direction'), {
      target: { value: 'DESC' },
    });
    fireEvent.click(screen.getByTestId('aggregation-execute'));

    const table = await screen.findByTestId('aggregation-bucket-tree');
    await within(table).findByText('USA');
    const bodyRows = within(table).getAllByRole('row').slice(1); // drop header row
    const firstCol = bodyRows.map(
      (r) => within(r).getAllByRole('cell')[0]?.textContent ?? '',
    );
    expect(firstCol).toEqual(['USA', 'France', 'Brazil']);
  });

  it('Given no direction is chosen, When Execute is clicked, Then the request metric omits direction', async () => {
    renderPage();
    await configureSumByCountry();

    fireEvent.click(screen.getByTestId('aggregation-execute'));

    await waitFor(() => {
      expect(capturedAggregateBody).not.toBeNull();
    });

    const metric = capturedAggregateBody?.aggregation?.[0] ?? {};
    expect(Object.prototype.hasOwnProperty.call(metric, 'direction')).toBe(false);
  });
});
