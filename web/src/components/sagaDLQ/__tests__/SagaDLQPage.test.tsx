import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { SagaDLQPage } from '../SagaDLQPage';
import * as sagaDLQApi from '../../../api/sagaDLQ';
import type { SagaDLQEntry } from '../../../api/sagaDLQ';

const pendingEntry: SagaDLQEntry = {
  dlqId: 'dlq-001',
  sagaId: 'saga-abc',
  stepId: 'step-002',
  ontology: 'default',
  editsJson: [{ kind: 'MODIFY', objectType: 'Order', primaryKey: 'o-1' }],
  failureMessage: 'broken compensator',
  status: 'PENDING',
  attempts: 1,
  createdAt: '2026-04-30T12:00:00Z',
  updatedAt: '2026-04-30T12:00:00Z',
};

const resolvedEntry: SagaDLQEntry = {
  ...pendingEntry,
  dlqId: 'dlq-002',
  status: 'RESOLVED',
};

function renderPage(initial = '/admin/default/saga-dlq') {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initial]}>
        <Routes>
          <Route path="/admin/:ontology/saga-dlq" element={<SagaDLQPage />} />
          <Route path="/admin/saga-dlq" element={<SagaDLQPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('SagaDLQPage (US-440)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders pending DLQ entries with retry and drop buttons', async () => {
    vi.spyOn(sagaDLQApi, 'listSagaDLQ').mockResolvedValue({
      entries: [pendingEntry],
    });

    renderPage();

    await waitFor(() =>
      expect(screen.getByTestId('saga-dlq-card')).toBeInTheDocument(),
    );
    expect(screen.getByText('saga-abc')).toBeInTheDocument();
    expect(screen.getByText('broken compensator')).toBeInTheDocument();
    expect(screen.getByTestId('saga-dlq-retry-btn')).toBeInTheDocument();
    expect(screen.getByTestId('saga-dlq-drop-btn')).toBeInTheDocument();
  });

  it('retry button calls retry API and refetches the list', async () => {
    const listSpy = vi
      .spyOn(sagaDLQApi, 'listSagaDLQ')
      .mockResolvedValueOnce({ entries: [pendingEntry] })
      .mockResolvedValueOnce({ entries: [] });
    const retrySpy = vi
      .spyOn(sagaDLQApi, 'retrySagaDLQ')
      .mockResolvedValue({ dlqId: pendingEntry.dlqId, status: 'RESOLVED' });

    renderPage();

    fireEvent.click(await screen.findByTestId('saga-dlq-retry-btn'));

    await waitFor(() =>
      expect(retrySpy).toHaveBeenCalledWith('default', pendingEntry.dlqId),
    );
    await waitFor(() => expect(listSpy).toHaveBeenCalledTimes(2));
  });

  it('drop button calls drop API and refetches the list', async () => {
    const listSpy = vi
      .spyOn(sagaDLQApi, 'listSagaDLQ')
      .mockResolvedValueOnce({ entries: [pendingEntry] })
      .mockResolvedValueOnce({ entries: [] });
    const dropSpy = vi
      .spyOn(sagaDLQApi, 'dropSagaDLQ')
      .mockResolvedValue({ dlqId: pendingEntry.dlqId, status: 'DROPPED' });

    renderPage();

    fireEvent.click(await screen.findByTestId('saga-dlq-drop-btn'));

    await waitFor(() =>
      expect(dropSpy).toHaveBeenCalledWith('default', pendingEntry.dlqId),
    );
    await waitFor(() => expect(listSpy).toHaveBeenCalledTimes(2));
  });

  it('shows empty state when no rows are returned', async () => {
    vi.spyOn(sagaDLQApi, 'listSagaDLQ').mockResolvedValue({ entries: [] });

    renderPage();

    expect(await screen.findByText(/no dlq entries/i)).toBeInTheDocument();
  });

  it('switching status filter re-queries with the new status', async () => {
    const listSpy = vi.spyOn(sagaDLQApi, 'listSagaDLQ').mockImplementation(
      async (_o, params) => ({
        entries: params?.status === 'RESOLVED' ? [resolvedEntry] : [pendingEntry],
      }),
    );

    renderPage();

    await waitFor(() =>
      expect(listSpy).toHaveBeenCalledWith(
        'default',
        expect.objectContaining({ status: 'PENDING' }),
      ),
    );

    fireEvent.click(screen.getByRole('tab', { name: /resolved/i }));

    await waitFor(() =>
      expect(listSpy).toHaveBeenCalledWith(
        'default',
        expect.objectContaining({ status: 'RESOLVED' }),
      ),
    );
  });

  it('non-pending entries hide the action buttons', async () => {
    vi.spyOn(sagaDLQApi, 'listSagaDLQ').mockResolvedValue({
      entries: [resolvedEntry],
    });

    renderPage();

    await waitFor(() =>
      expect(screen.getByTestId('saga-dlq-card')).toBeInTheDocument(),
    );
    expect(screen.queryByTestId('saga-dlq-retry-btn')).not.toBeInTheDocument();
    expect(screen.queryByTestId('saga-dlq-drop-btn')).not.toBeInTheDocument();
  });

  it('surfaces a retry error to the operator', async () => {
    vi.spyOn(sagaDLQApi, 'listSagaDLQ').mockResolvedValue({
      entries: [pendingEntry],
    });
    vi.spyOn(sagaDLQApi, 'retrySagaDLQ').mockRejectedValue(
      new Error('replay failed'),
    );

    renderPage();

    fireEvent.click(await screen.findByTestId('saga-dlq-retry-btn'));

    await waitFor(() =>
      expect(screen.getByTestId('saga-dlq-error').textContent).toContain(
        'replay failed',
      ),
    );
  });
});
