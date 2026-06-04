import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AggregationPage } from '../AggregationPage';

interface CapturedHaving {
  metric?: string;
  op?: string;
  value?: number;
}

let capturedAggregateBody:
  | { aggregation?: unknown; groupBy?: unknown; having?: CapturedHaving[] }
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
        freight: { dataType: { type: 'double' }, rid: 'ri.p.order.freight' },
      },
    }),
  ),
  http.post('/api/v2/ontologies/:ontology/objects/:objectType/aggregate', async ({ request }) => {
    capturedAggregateBody = (await request.json()) as typeof capturedAggregateBody;
    // The backend applies the having clause post-aggregation; here we mirror a
    // server that has already dropped rows failing the clause.
    return HttpResponse.json({
      data: [
        { group: { shipCountry: 'USA' }, metrics: [{ name: 'total', value: 300 }] },
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

async function configureSumByCountry() {
  // metric: sum(freight) aliased "total" so a having clause can name it.
  fireEvent.change(await screen.findByTestId('metric-0-type'), {
    target: { value: 'sum' },
  });
  fireEvent.change(screen.getByTestId('metric-0-field'), {
    target: { value: 'freight' },
  });
  fireEvent.change(screen.getByTestId('metric-0-name'), {
    target: { value: 'total' },
  });
  // groupBy: shipCountry
  fireEvent.click(screen.getByTestId('groupby-add'));
  fireEvent.change(screen.getByTestId('groupby-0-field'), {
    target: { value: 'shipCountry' },
  });
}

describe('BDD: AggregationPage having-clause post-aggregation filters', () => {
  it('Given a metric "total", When a having clause (total gt 100) is added and Execute clicked, Then the request carries having:[{metric,op,value}]', async () => {
    renderPage();
    await configureSumByCountry();

    fireEvent.click(screen.getByTestId('having-add'));
    fireEvent.change(screen.getByTestId('having-0-metric'), {
      target: { value: 'total' },
    });
    fireEvent.change(screen.getByTestId('having-0-op'), {
      target: { value: 'gt' },
    });
    fireEvent.change(screen.getByTestId('having-0-value'), {
      target: { value: '100' },
    });

    fireEvent.click(screen.getByTestId('aggregation-execute'));

    await waitFor(() => {
      expect(capturedAggregateBody).not.toBeNull();
    });

    expect(capturedAggregateBody?.having).toEqual([
      { metric: 'total', op: 'gt', value: 100 },
    ]);
  });

  it('Given no having clause is configured, When Execute is clicked, Then the request omits having', async () => {
    renderPage();
    await configureSumByCountry();

    fireEvent.click(screen.getByTestId('aggregation-execute'));

    await waitFor(() => {
      expect(capturedAggregateBody).not.toBeNull();
    });

    expect(
      Object.prototype.hasOwnProperty.call(capturedAggregateBody ?? {}, 'having'),
    ).toBe(false);
  });

  it('Given two having clauses, When one is removed, Then only the remaining clause is sent', async () => {
    renderPage();
    await configureSumByCountry();

    fireEvent.click(screen.getByTestId('having-add'));
    fireEvent.click(screen.getByTestId('having-add'));

    fireEvent.change(screen.getByTestId('having-0-metric'), {
      target: { value: 'total' },
    });
    fireEvent.change(screen.getByTestId('having-0-op'), {
      target: { value: 'gte' },
    });
    fireEvent.change(screen.getByTestId('having-0-value'), {
      target: { value: '50' },
    });

    fireEvent.change(screen.getByTestId('having-1-metric'), {
      target: { value: 'total' },
    });
    fireEvent.change(screen.getByTestId('having-1-op'), {
      target: { value: 'lt' },
    });
    fireEvent.change(screen.getByTestId('having-1-value'), {
      target: { value: '999' },
    });

    // Remove the first clause; the second becomes index 0.
    fireEvent.click(screen.getByTestId('having-0-remove'));

    fireEvent.click(screen.getByTestId('aggregation-execute'));

    await waitFor(() => {
      expect(capturedAggregateBody).not.toBeNull();
    });

    expect(capturedAggregateBody?.having).toEqual([
      { metric: 'total', op: 'lt', value: 999 },
    ]);
  });

  it('Given a having clause whose value is blank, When Execute is clicked, Then that incomplete clause is dropped from the request', async () => {
    renderPage();
    await configureSumByCountry();

    fireEvent.click(screen.getByTestId('having-add'));
    fireEvent.change(screen.getByTestId('having-0-metric'), {
      target: { value: 'total' },
    });
    fireEvent.change(screen.getByTestId('having-0-op'), {
      target: { value: 'gt' },
    });
    // leave value blank

    fireEvent.click(screen.getByTestId('aggregation-execute'));

    await waitFor(() => {
      expect(capturedAggregateBody).not.toBeNull();
    });

    expect(
      Object.prototype.hasOwnProperty.call(capturedAggregateBody ?? {}, 'having'),
    ).toBe(false);
  });
});
