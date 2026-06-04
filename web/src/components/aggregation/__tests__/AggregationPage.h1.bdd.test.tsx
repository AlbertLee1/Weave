import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AggregationPage } from '../AggregationPage';

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
  http.post('/api/v2/ontologies/:ontology/objects/:objectType/aggregate', () =>
    HttpResponse.json({ data: [], accuracy: 'APPROXIMATE' }),
  ),
);

beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
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

describe('BDD: AggregationPage heading accessibility (h1)', () => {
  it('Given the aggregation page is rendered, Then the main title "Aggregation" is a level-1 heading', async () => {
    renderPage();

    const heading = await screen.findByRole('heading', { level: 1, name: /Aggregation/i });
    expect(heading).toBeInTheDocument();
  });

  it('Given the aggregation page is rendered, Then there is exactly one level-1 heading', async () => {
    renderPage();

    // Ensure the page has settled (config panel rendered).
    await screen.findByRole('heading', { level: 1, name: /Aggregation/i });

    const h1s = screen.getAllByRole('heading', { level: 1 });
    expect(h1s).toHaveLength(1);
  });
});
