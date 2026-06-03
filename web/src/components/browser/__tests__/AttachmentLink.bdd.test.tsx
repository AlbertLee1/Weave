import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AttachmentLink } from '../AttachmentLink';

const META_PATH =
  '/api/v2/ontologies/northwind/objects/order/10248/attachments/invoice';

const server = setupServer(
  http.get(META_PATH, () =>
    HttpResponse.json({
      rid: 'ri.attachment.main.attachment.abc',
      filename: 'invoice.pdf',
      mediaType: 'application/pdf',
      sizeBytes: 2048,
    }),
  ),
);

beforeAll(() => server.listen({ onUnhandledRequest: 'bypass' }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function renderLink() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <AttachmentLink
        ontologyApiName="northwind"
        objectType="order"
        primaryKey="10248"
        property="invoice"
      />
    </QueryClientProvider>,
  );
}

describe('BDD: AttachmentLink (object attachment download)', () => {
  it('Given attachment metadata, Then it renders a download link to the content endpoint with the filename', async () => {
    renderLink();

    // The link renders immediately; its label resolves to the filename once
    // the metadata query settles.
    await screen.findByText('invoice.pdf');
    const link = screen.getByTestId('attachment-download-invoice');
    expect(link).toHaveAttribute(
      'href',
      '/api/v2/ontologies/northwind/objects/order/10248/attachments/invoice/content',
    );
    expect(link).toHaveAttribute('download', 'invoice.pdf');
  });

  it('Given the metadata fetch fails, Then the link still works with a generic label', async () => {
    server.use(
      http.get(META_PATH, () =>
        HttpResponse.json({ errorName: 'NotFound' }, { status: 404 }),
      ),
    );
    renderLink();

    const link = await screen.findByTestId('attachment-download-invoice');
    expect(link).toHaveTextContent('Download');
    expect(link).toHaveAttribute(
      'href',
      '/api/v2/ontologies/northwind/objects/order/10248/attachments/invoice/content',
    );
  });
});
