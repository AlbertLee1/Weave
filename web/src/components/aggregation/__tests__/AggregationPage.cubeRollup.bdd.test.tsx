import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AggregationPage } from '../AggregationPage';

let capturedAggregateBody:
  | { aggregation?: unknown; groupBy?: unknown; cube?: boolean; rollup?: boolean }
  | null = null;

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
        shipCity: { dataType: { type: 'string' }, rid: 'ri.p.order.city' },
      },
    }),
  ),
  http.post('/api/v2/ontologies/:ontology/objects/:objectType/aggregate', async ({ request }) => {
    capturedAggregateBody = (await request.json()) as typeof capturedAggregateBody;
    // Emulate a cube/rollup response: the grand-total row has NO group keys,
    // and a partial-subset row omits the shipCity dimension entirely.
    return HttpResponse.json({
      data: [
        {
          group: { shipCountry: 'USA', shipCity: 'Seattle' },
          metrics: [{ name: 'count', value: 4 }],
        },
        {
          // (N-1)-subset / rolled-up row: shipCity dimension is absent.
          group: { shipCountry: 'USA' },
          metrics: [{ name: 'count', value: 9 }],
        },
        {
          // grand-total row: every groupBy dimension is absent.
          group: {},
          metrics: [{ name: 'count', value: 12 }],
        },
      ],
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

async function addCountByCountryAndCity() {
  // metric 0 stays as count (the page default)
  await screen.findByTestId('aggregation-execute');
  // groupBy 0: shipCountry
  fireEvent.click(screen.getByTestId('groupby-add'));
  fireEvent.change(screen.getByTestId('groupby-0-field'), {
    target: { value: 'shipCountry' },
  });
  // groupBy 1: shipCity
  fireEvent.click(screen.getByTestId('groupby-add'));
  fireEvent.change(screen.getByTestId('groupby-1-field'), {
    target: { value: 'shipCity' },
  });
}

describe('BDD: AggregationPage cube / rollup subtotal modes', () => {
  it('Given the subtotal-mode control, When there is no groupBy clause, Then it is disabled', async () => {
    renderPage();
    const control = await screen.findByTestId('aggregation-subtotal-mode');
    expect(control).toBeDisabled();
  });

  it('Given groupBys and cube mode selected, When Execute is clicked, Then the request body carries cube:true and no rollup', async () => {
    renderPage();
    await addCountByCountryAndCity();

    const control = screen.getByTestId('aggregation-subtotal-mode');
    expect(control).not.toBeDisabled();
    fireEvent.change(control, { target: { value: 'cube' } });
    fireEvent.click(screen.getByTestId('aggregation-execute'));

    await waitFor(() => {
      expect(capturedAggregateBody).not.toBeNull();
    });

    expect(capturedAggregateBody?.cube).toBe(true);
    expect(
      Object.prototype.hasOwnProperty.call(capturedAggregateBody ?? {}, 'rollup'),
    ).toBe(false);
  });

  it('Given groupBys and rollup mode selected, When Execute is clicked, Then the request body carries rollup:true and no cube', async () => {
    renderPage();
    await addCountByCountryAndCity();

    fireEvent.change(screen.getByTestId('aggregation-subtotal-mode'), {
      target: { value: 'rollup' },
    });
    fireEvent.click(screen.getByTestId('aggregation-execute'));

    await waitFor(() => {
      expect(capturedAggregateBody).not.toBeNull();
    });

    expect(capturedAggregateBody?.rollup).toBe(true);
    expect(
      Object.prototype.hasOwnProperty.call(capturedAggregateBody ?? {}, 'cube'),
    ).toBe(false);
  });

  it('Given the default (none) mode, When Execute is clicked, Then the request omits both cube and rollup', async () => {
    renderPage();
    await addCountByCountryAndCity();

    fireEvent.click(screen.getByTestId('aggregation-execute'));

    await waitFor(() => {
      expect(capturedAggregateBody).not.toBeNull();
    });

    expect(
      Object.prototype.hasOwnProperty.call(capturedAggregateBody ?? {}, 'cube'),
    ).toBe(false);
    expect(
      Object.prototype.hasOwnProperty.call(capturedAggregateBody ?? {}, 'rollup'),
    ).toBe(false);
  });

  it('Given a cube response with subtotal rows, When rendered, Then rows missing groupBy keys show an "(all)" subtotal marker instead of blanks', async () => {
    renderPage();
    await addCountByCountryAndCity();

    fireEvent.change(screen.getByTestId('aggregation-subtotal-mode'), {
      target: { value: 'cube' },
    });
    fireEvent.click(screen.getByTestId('aggregation-execute'));

    const table = await screen.findByTestId('aggregation-bucket-tree');

    // The grand-total row (no group keys) must render without crashing and show
    // "(all)" for BOTH dimensions. The (N-1)-subset row shows "(all)" only for
    // the absent shipCity dimension.
    await waitFor(() => {
      expect(within(table).getAllByText('(all)').length).toBeGreaterThanOrEqual(3);
    });

    // The concrete leaf row still shows its real key value. "USA" appears in
    // both the leaf row and the (N-1)-subset row, so assert it renders at all.
    expect(within(table).getByText('Seattle')).toBeInTheDocument();
    expect(within(table).getAllByText('USA').length).toBeGreaterThanOrEqual(2);
  });
});
