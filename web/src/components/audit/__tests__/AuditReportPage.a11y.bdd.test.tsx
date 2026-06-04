import {
  describe,
  it,
  expect,
  beforeAll,
  afterAll,
  afterEach,
  vi,
} from 'vitest';
import { createElement } from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AuditReportPage } from '../AuditReportPage';

// US-045 (PC-A11) accessibility hardening:
//   1. The export-complete confirmation is a screen-reader live region —
//      `role="status"` MUST be paired with `aria-live="polite"` so assistive
//      tech announces "Exported N rows…" (mirrors Skeleton.tsx:67).
//   2. Each row's expand toggle is a disclosure control — it MUST point at
//      the payload it reveals via `aria-controls`, and the revealed payload
//      container MUST carry the matching, event-id-stable `id`.

const ROWS = [
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
];

const server = setupServer(
  http.get('/api/v2/admin/auditEvents', () =>
    HttpResponse.json({ data: ROWS, nextPageToken: '' }),
  ),
);

beforeAll(() => {
  server.listen({ onUnhandledRequest: 'bypass' });
  // jsdom lacks URL.createObjectURL / revokeObjectURL used by the CSV/JSON
  // download path; stub them so the export flow can run headless.
  if (!('createObjectURL' in URL)) {
    // @ts-expect-error – test shim
    URL.createObjectURL = vi.fn(() => 'blob:mock');
  } else {
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:mock');
  }
  if (!('revokeObjectURL' in URL)) {
    // @ts-expect-error – test shim
    URL.revokeObjectURL = vi.fn();
  } else {
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {});
  }
});
afterEach(() => server.resetHandlers());
afterAll(() => {
  server.close();
  vi.restoreAllMocks();
});

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

describe('BDD: AuditReportPage a11y — live region + disclosure association', () => {
  it('Given rows are loaded, When an export completes, Then the status confirmation is a polite live region', async () => {
    const user = userEvent.setup();
    renderPage();

    await waitFor(() =>
      expect(screen.getByTestId('audit-report-row')).toBeInTheDocument(),
    );

    await user.click(screen.getByTestId('audit-report-export-csv-btn'));

    const status = await screen.findByTestId('audit-report-export-status');
    // It already advertises role="status"; the fix adds aria-live="polite"
    // so screen-reader users actually hear the export result.
    expect(status).toHaveAttribute('role', 'status');
    expect(status).toHaveAttribute('aria-live', 'polite');
    expect(status).toHaveTextContent(/Exported 1 row as/);
  });

  it("Given a row is collapsed, When its disclosure toggle is activated, Then aria-controls points at the revealed payload's matching id", async () => {
    const user = userEvent.setup();
    renderPage();

    const toggle = await waitFor(() =>
      screen.getByTestId('audit-report-row-expand-btn'),
    );

    // Collapsed: the button advertises what it controls even before expansion.
    const controls = toggle.getAttribute('aria-controls');
    expect(controls).toBe('audit-payload-a1');
    expect(toggle).toHaveAttribute('aria-expanded', 'false');

    await user.click(toggle);

    await waitFor(() =>
      expect(toggle).toHaveAttribute('aria-expanded', 'true'),
    );

    // The revealed payload container carries the id the button referenced.
    const payload = screen.getByTestId('audit-report-row-payload');
    expect(payload).toHaveAttribute('id', controls!);
    expect(payload).toHaveAttribute('id', 'audit-payload-a1');
  });
});
