import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AggregationPage } from '../AggregationPage';

let capturedAggregateBody: { aggregation?: unknown; groupBy?: unknown; accuracy?: string } | null =
  null;

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
      },
    }),
  ),
  http.post('/api/v2/ontologies/:ontology/objects/:objectType/aggregate', async ({ request }) => {
    capturedAggregateBody = (await request.json()) as typeof capturedAggregateBody;
    return HttpResponse.json({
      data: [
        {
          group: { shipCountry: 'USA' },
          metrics: [{ name: 'shipCountry.exactDistinct', value: 3 }],
        },
      ],
      accuracy: capturedAggregateBody?.accuracy === 'REQUIRE_ACCURATE' ? 'ACCURATE' : 'APPROXIMATE',
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

async function configureDistinctByCountry() {
  // metric: approximateDistinct(shipCountry)
  fireEvent.change(await screen.findByTestId('metric-0-type'), {
    target: { value: 'approximateDistinct' },
  });
  fireEvent.change(screen.getByTestId('metric-0-field'), {
    target: { value: 'shipCountry' },
  });
  // groupBy: shipCountry
  fireEvent.click(screen.getByTestId('groupby-add'));
  fireEvent.change(screen.getByTestId('groupby-0-field'), {
    target: { value: 'shipCountry' },
  });
}

describe('BDD: AggregationPage accuracy-mode toggle (REQUIRE_ACCURATE)', () => {
  it('Given the user selects accuracy REQUIRE_ACCURATE, When Execute is clicked, Then the request body carries accuracy "REQUIRE_ACCURATE"', async () => {
    renderPage();
    await configureDistinctByCountry();

    fireEvent.change(screen.getByTestId('aggregation-accuracy-select'), {
      target: { value: 'REQUIRE_ACCURATE' },
    });
    fireEvent.click(screen.getByTestId('aggregation-execute'));

    await waitFor(() => {
      expect(capturedAggregateBody).not.toBeNull();
    });

    expect(capturedAggregateBody?.accuracy).toBe('REQUIRE_ACCURATE');
  });

  it('Given the default accuracy (ALLOW_APPROXIMATE), When Execute is clicked, Then the request body omits accuracy', async () => {
    renderPage();
    await configureDistinctByCountry();

    fireEvent.click(screen.getByTestId('aggregation-execute'));

    await waitFor(() => {
      expect(capturedAggregateBody).not.toBeNull();
    });

    expect(
      Object.prototype.hasOwnProperty.call(capturedAggregateBody ?? {}, 'accuracy'),
    ).toBe(false);
  });

  it('Given accuracy is explicitly set back to ALLOW_APPROXIMATE, When Execute is clicked, Then the request body still omits accuracy', async () => {
    renderPage();
    await configureDistinctByCountry();

    const select = screen.getByTestId('aggregation-accuracy-select');
    fireEvent.change(select, { target: { value: 'REQUIRE_ACCURATE' } });
    fireEvent.change(select, { target: { value: 'ALLOW_APPROXIMATE' } });
    fireEvent.click(screen.getByTestId('aggregation-execute'));

    await waitFor(() => {
      expect(capturedAggregateBody).not.toBeNull();
    });

    expect(
      Object.prototype.hasOwnProperty.call(capturedAggregateBody ?? {}, 'accuracy'),
    ).toBe(false);
  });
});
