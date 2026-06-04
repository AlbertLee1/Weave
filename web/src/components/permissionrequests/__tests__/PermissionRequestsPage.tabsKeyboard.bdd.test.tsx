import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
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

// The status-filter tablist follows the WAI-ARIA tabs keyboard contract:
// ArrowRight/Down + ArrowLeft/Up move (and activate) with wrap-around, Home/End
// jump to the ends, and a roving tabindex keeps only the selected tab tabbable.
// Status tabs in render order: Pending, Approved, Rejected, Cancelled, All.
describe('BDD: PermissionRequestsPage status tablist keyboard navigation (WAI-ARIA tabs)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(api, 'listPermissionRequests').mockResolvedValue({
      requests: [makeRequest('p-1')],
      total: 1,
      limit: PAGE_SIZE,
      offset: 0,
    });
  });

  it('Given the Pending tab is focused, When ArrowRight is pressed, Then focus and selection move to the next tab and wrap back to the first', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() =>
      expect(screen.getByTestId('permission-request-card')).toBeInTheDocument(),
    );

    const pending = screen.getByRole('tab', { name: 'Pending' });
    const approved = screen.getByRole('tab', { name: 'Approved' });
    const rejected = screen.getByRole('tab', { name: 'Rejected' });
    const cancelled = screen.getByRole('tab', { name: 'Cancelled' });
    const all = screen.getByRole('tab', { name: 'All' });

    pending.focus();
    expect(pending).toHaveFocus();

    await user.keyboard('{ArrowRight}');
    expect(approved).toHaveFocus();
    expect(approved).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{ArrowRight}');
    expect(rejected).toHaveFocus();
    expect(rejected).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{ArrowRight}');
    expect(cancelled).toHaveFocus();

    await user.keyboard('{ArrowRight}');
    expect(all).toHaveFocus();
    expect(all).toHaveAttribute('aria-selected', 'true');

    // Wrap-around from the last tab back to the first.
    await user.keyboard('{ArrowRight}');
    expect(pending).toHaveFocus();
    expect(pending).toHaveAttribute('aria-selected', 'true');
  });

  it('Given the Pending tab is focused, When ArrowLeft is pressed, Then focus and selection wrap to the last tab and move backwards', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() =>
      expect(screen.getByTestId('permission-request-card')).toBeInTheDocument(),
    );

    const pending = screen.getByRole('tab', { name: 'Pending' });
    const cancelled = screen.getByRole('tab', { name: 'Cancelled' });
    const all = screen.getByRole('tab', { name: 'All' });

    pending.focus();

    // Wrap-around from the first tab back to the last.
    await user.keyboard('{ArrowLeft}');
    expect(all).toHaveFocus();
    expect(all).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{ArrowLeft}');
    expect(cancelled).toHaveFocus();
    expect(cancelled).toHaveAttribute('aria-selected', 'true');
  });

  it('Given any tab is focused, When Home/End are pressed, Then focus and selection jump to the first/last tab', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() =>
      expect(screen.getByTestId('permission-request-card')).toBeInTheDocument(),
    );

    const pending = screen.getByRole('tab', { name: 'Pending' });
    const all = screen.getByRole('tab', { name: 'All' });

    pending.focus();

    await user.keyboard('{End}');
    expect(all).toHaveFocus();
    expect(all).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{Home}');
    expect(pending).toHaveFocus();
    expect(pending).toHaveAttribute('aria-selected', 'true');
  });

  it('Given ArrowDown/ArrowUp mirror ArrowRight/ArrowLeft, When pressed, Then focus moves forward/backward', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() =>
      expect(screen.getByTestId('permission-request-card')).toBeInTheDocument(),
    );

    const pending = screen.getByRole('tab', { name: 'Pending' });
    const approved = screen.getByRole('tab', { name: 'Approved' });

    pending.focus();

    await user.keyboard('{ArrowDown}');
    expect(approved).toHaveFocus();
    expect(approved).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{ArrowUp}');
    expect(pending).toHaveFocus();
    expect(pending).toHaveAttribute('aria-selected', 'true');
  });

  it('Given the tablist follows the roving tabindex pattern, Then only the selected tab is in the tab order, and mouse click still updates it', async () => {
    renderPage();
    await waitFor(() =>
      expect(screen.getByTestId('permission-request-card')).toBeInTheDocument(),
    );

    const pending = screen.getByRole('tab', { name: 'Pending' });
    const approved = screen.getByRole('tab', { name: 'Approved' });
    const rejected = screen.getByRole('tab', { name: 'Rejected' });

    // Pending is the default selection.
    expect(pending).toHaveAttribute('tabindex', '0');
    expect(approved).toHaveAttribute('tabindex', '-1');
    expect(rejected).toHaveAttribute('tabindex', '-1');

    // Mouse click still works and updates the roving tabindex + selection.
    fireEvent.click(approved);
    expect(approved).toHaveAttribute('aria-selected', 'true');
    expect(approved).toHaveAttribute('tabindex', '0');
    expect(pending).toHaveAttribute('tabindex', '-1');
    expect(rejected).toHaveAttribute('tabindex', '-1');
  });

  it('Given the status filter, When a tab is activated via keyboard, Then the list request carries the new status (filter logic intact)', async () => {
    const user = userEvent.setup();
    const listSpy = vi
      .spyOn(api, 'listPermissionRequests')
      .mockResolvedValue({
        requests: [makeRequest('p-1')],
        total: 1,
        limit: PAGE_SIZE,
        offset: 0,
      });
    renderPage();
    await waitFor(() =>
      expect(screen.getByTestId('permission-request-card')).toBeInTheDocument(),
    );

    const pending = screen.getByRole('tab', { name: 'Pending' });
    pending.focus();

    await user.keyboard('{ArrowRight}');

    await waitFor(() => expect(lastQuery(listSpy).status).toBe('APPROVED'));

    // 'All' maps to an undefined status filter.
    await user.keyboard('{End}');
    await waitFor(() => expect(lastQuery(listSpy).status).toBeUndefined());
  });
});
