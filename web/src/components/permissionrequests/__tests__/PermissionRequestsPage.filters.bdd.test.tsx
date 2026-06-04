import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router';
import { PermissionRequestsPage } from '../PermissionRequestsPage';
import * as api from '../../../api/permissionRequests';
import type {
  ListPermissionRequestsQuery,
  PermissionRequest,
} from '../../../api/permissionRequests';

const PAGE_SIZE = 25;

function makeRequest(id: string): PermissionRequest {
  return {
    id,
    targetRid: 'ri.ontology.main.object.alpha',
    requestedBy: 'user:bob',
    reason: 'I need access',
    status: 'PENDING',
    createdAt: '2026-04-28T12:00:00Z',
    updatedAt: '2026-04-28T12:00:00Z',
  };
}

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <PermissionRequestsPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

// Returns the query argument of the most recent listPermissionRequests call.
function lastQuery(
  spy: ReturnType<typeof vi.spyOn>,
): ListPermissionRequestsQuery {
  const calls = spy.mock.calls;
  return (calls[calls.length - 1]?.[0] ?? {}) as ListPermissionRequestsQuery;
}

describe('BDD: PermissionRequestsPage targetRid filter & pagination (backend targetRid/limit/offset)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('Given the list, When a targetRid is entered, Then the list request carries that targetRid', async () => {
    const listSpy = vi.spyOn(api, 'listPermissionRequests').mockResolvedValue({
      requests: [makeRequest('p-1')],
      total: 1,
      limit: PAGE_SIZE,
      offset: 0,
    });

    renderPage();

    await waitFor(() =>
      expect(screen.getByTestId('permission-request-card')).toBeInTheDocument(),
    );

    fireEvent.change(
      screen.getByTestId('permission-requests-targetrid-input'),
      { target: { value: 'ri.ontology.main.object.alpha' } },
    );

    await waitFor(() =>
      expect(lastQuery(listSpy).targetRid).toBe('ri.ontology.main.object.alpha'),
    );
  });

  it('Given a full page of results, When Next is clicked, Then the request advances the offset by the page size', async () => {
    const fullPage = Array.from({ length: PAGE_SIZE }, (_, i) =>
      makeRequest(`p-${i}`),
    );
    const listSpy = vi.spyOn(api, 'listPermissionRequests').mockResolvedValue({
      requests: fullPage,
      total: PAGE_SIZE * 3,
      limit: PAGE_SIZE,
      offset: 0,
    });

    renderPage();

    await waitFor(() =>
      expect(screen.getAllByTestId('permission-request-card').length).toBe(
        PAGE_SIZE,
      ),
    );

    // First load uses offset 0.
    expect(lastQuery(listSpy).offset ?? 0).toBe(0);

    fireEvent.click(screen.getByTestId('permission-requests-next-page'));

    await waitFor(() => expect(lastQuery(listSpy).offset).toBe(PAGE_SIZE));

    // And Previous walks the offset back to 0.
    fireEvent.click(screen.getByTestId('permission-requests-prev-page'));
    await waitFor(() => expect(lastQuery(listSpy).offset).toBe(0));
  });

  it('Given the first page, Then Previous is disabled', async () => {
    vi.spyOn(api, 'listPermissionRequests').mockResolvedValue({
      requests: [makeRequest('p-1')],
      total: 100,
      limit: PAGE_SIZE,
      offset: 0,
    });

    renderPage();

    await waitFor(() =>
      expect(screen.getByTestId('permission-request-card')).toBeInTheDocument(),
    );

    expect(screen.getByTestId('permission-requests-prev-page')).toBeDisabled();
  });
});
