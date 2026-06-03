import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AggregationPage } from '../AggregationPage';

interface CapturedGroupBy {
  field?: string;
  type?: string;
  fixedWidth?: number;
}

let capturedAggregateBody: { groupBy?: CapturedGroupBy[] } | null = null;

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
        freight: { dataType: { type: 'double' }, rid: 'ri.p.order.freight' },
      },
    }),
  ),
  http.post('/api/v2/ontologies/:ontology/objects/:objectType/aggregate', async ({ request }) => {
    capturedAggregateBody = (await request.json()) as typeof capturedAggregateBody;
    return HttpResponse.json({
      data: [
        { group: { freight: '[0,10)' }, metrics: [{ name: 'count', value: 4 }] },
        { group: { freight: '[10,20)' }, metrics: [{ name: 'count', value: 2 }] },
      ],
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

async function addFixedWidthGroupBy() {
  fireEvent.click(await screen.findByTestId('groupby-add'));
  fireEvent.change(screen.getByTestId('groupby-0-field'), {
    target: { value: 'freight' },
  });
  fireEvent.change(screen.getByTestId('groupby-0-type'), {
    target: { value: 'fixedWidth' },
  });
}

describe('BDD: AggregationPage fixedWidth groupBy bucket width', () => {
  it('Given a fixedWidth groupBy with a width, When Execute is clicked, Then the request carries fixedWidth', async () => {
    renderPage();
    await addFixedWidthGroupBy();

    fireEvent.change(screen.getByTestId('groupby-0-fixedWidth'), {
      target: { value: '10' },
    });
    fireEvent.click(screen.getByTestId('aggregation-execute'));

    await waitFor(() => {
      expect(capturedAggregateBody).not.toBeNull();
    });

    expect(capturedAggregateBody?.groupBy).toEqual([
      { field: 'freight', type: 'fixedWidth', fixedWidth: 10 },
    ]);
  });

  it('Given a fixedWidth groupBy with no width, Then Execute is disabled (a width-less request always errors server-side)', async () => {
    renderPage();
    await addFixedWidthGroupBy();

    const execute = screen.getByTestId('aggregation-execute') as HTMLButtonElement;
    expect(execute).toBeDisabled();

    // Supplying a width re-enables it.
    fireEvent.change(screen.getByTestId('groupby-0-fixedWidth'), {
      target: { value: '5' },
    });
    expect(execute).not.toBeDisabled();
  });

  it('Given the groupBy type is not fixedWidth, Then no width input is shown', async () => {
    renderPage();
    fireEvent.click(await screen.findByTestId('groupby-add'));
    fireEvent.change(screen.getByTestId('groupby-0-field'), {
      target: { value: 'freight' },
    });
    // default type is 'exact'
    expect(screen.queryByTestId('groupby-0-fixedWidth')).toBeNull();
  });
});
