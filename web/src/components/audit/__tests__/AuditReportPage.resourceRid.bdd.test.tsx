import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { createElement } from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AuditReportPage } from '../AuditReportPage';

let capturedUrls: string[] = [];

const ALL_ROWS = [
  {
    id: 'a1',
    actor_id: 'user-1',
    action: 'create',
    resource_type: 'ObjectType',
    resource_rid: 'ri.ontology.main.object-type.order',
    diff_json: { before: null, after: { name: 'Order' } },
    ip: '10.0.0.1',
    user_agent: 'curl/8',
    ts: '2026-06-01T10:00:00Z',
  },
  {
    id: 'a2',
    actor_id: 'user-2',
    action: 'modify',
    resource_type: 'ObjectType',
    resource_rid: 'ri.ontology.main.object-type.customer',
    diff_json: { before: {}, after: {} },
    ip: '10.0.0.2',
    user_agent: 'curl/8',
    ts: '2026-06-01T11:00:00Z',
  },
];

const TARGET_RID = 'ri.ontology.main.object-type.order';

const server = setupServer(
  http.get('/api/v2/admin/auditEvents', ({ request }) => {
    const url = new URL(request.url);
    capturedUrls.push(request.url);
    const rid = url.searchParams.get('resourceRid');
    const data = rid
      ? ALL_ROWS.filter((r) => r.resource_rid === rid)
      : ALL_ROWS;
    return HttpResponse.json({ data, nextPageToken: '' });
  }),
);

beforeAll(() => server.listen({ onUnhandledRequest: 'bypass' }));
afterEach(() => {
  capturedUrls = [];
  server.resetHandlers();
});
afterAll(() => server.close());

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(
    createElement(
      QueryClientProvider,
      { client: qc },
      createElement(
        MemoryRouter,
        { initialEntries: ['/admin/audit'] },
        createElement(AuditReportPage, null),
      ),
    ),
  );
}

describe('BDD: AuditReportPage resourceRid filter (US-493)', () => {
  it('Given a Resource RID is entered, When Apply is clicked, Then the request carries resourceRid and only the matching row renders', async () => {
    renderPage();

    // Initial unfiltered load shows both rows.
    await waitFor(() =>
      expect(screen.getAllByTestId('audit-report-row')).toHaveLength(2),
    );

    const input = screen.getByTestId('audit-report-filter-resource-rid');
    fireEvent.change(input, { target: { value: TARGET_RID } });
    fireEvent.click(screen.getByTestId('audit-report-filter-apply-btn'));

    // The filtered request carries the resourceRid query param.
    await waitFor(() => {
      const hit = capturedUrls.find((u) =>
        new URL(u).searchParams.get('resourceRid') === TARGET_RID,
      );
      expect(hit).toBeTruthy();
    });

    // Only the matching row remains.
    await waitFor(() => {
      const rows = screen.getAllByTestId('audit-report-row');
      expect(rows).toHaveLength(1);
      expect(rows[0]).toHaveAttribute('data-audit-id', 'a1');
    });
  });

  it('Given results are showing, When a Resource RID cell is clicked, Then the filter is pre-filled and applied', async () => {
    renderPage();

    await waitFor(() =>
      expect(screen.getAllByTestId('audit-report-row')).toHaveLength(2),
    );

    // Click the resource_rid cell of the second (customer) row.
    const cells = screen.getAllByTestId('audit-report-row-resource-rid');
    const customerCell = cells.find(
      (c) => c.getAttribute('data-rid') === 'ri.ontology.main.object-type.customer',
    );
    expect(customerCell).toBeTruthy();
    const filterBtn = customerCell!.querySelector(
      '[data-testid="audit-report-row-resource-rid-filter-btn"]',
    ) as HTMLElement;
    expect(filterBtn).toBeTruthy();
    fireEvent.click(filterBtn);

    // The filter input is pre-filled with the clicked RID.
    await waitFor(() =>
      expect(
        (screen.getByTestId('audit-report-filter-resource-rid') as HTMLInputElement)
          .value,
      ).toBe('ri.ontology.main.object-type.customer'),
    );

    // And the backend received the resourceRid param for that RID.
    await waitFor(() => {
      const hit = capturedUrls.find(
        (u) =>
          new URL(u).searchParams.get('resourceRid') ===
          'ri.ontology.main.object-type.customer',
      );
      expect(hit).toBeTruthy();
    });

    // Only the customer row remains.
    await waitFor(() => {
      const rows = screen.getAllByTestId('audit-report-row');
      expect(rows).toHaveLength(1);
      expect(rows[0]).toHaveAttribute('data-audit-id', 'a2');
    });
  });
});
