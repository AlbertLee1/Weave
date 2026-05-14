import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { createElement } from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AuditReportPage } from '../AuditReportPage';

const server = setupServer();
beforeAll(() => server.listen({ onUnhandledRequest: 'bypass' }));
afterEach(() => server.resetHandlers());
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

describe('AuditReportPage export buttons (disabled tooltip)', () => {
  it('shows an explanatory title attribute on disabled CSV/JSON export buttons when no rows are loaded', async () => {
    server.use(
      http.get('/api/v2/admin/auditEvents', () =>
        HttpResponse.json({ data: [], nextPageToken: '' }),
      ),
    );
    renderPage();
    const csvBtn = await waitFor(() =>
      screen.getByTestId('audit-report-export-csv-btn'),
    );
    const jsonBtn = screen.getByTestId('audit-report-export-json-btn');
    expect(csvBtn).toBeDisabled();
    expect(jsonBtn).toBeDisabled();
    expect(csvBtn.getAttribute('title') ?? '').toMatch(/no rows to export/i);
    expect(jsonBtn.getAttribute('title') ?? '').toMatch(/no rows to export/i);
    expect(csvBtn).toHaveAttribute('aria-disabled', 'true');
  });
});
