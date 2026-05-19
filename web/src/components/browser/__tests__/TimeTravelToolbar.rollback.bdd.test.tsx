import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { TimeTravelToolbar } from '../TimeTravelToolbar';
import * as datasetsApi from '../../../api/datasets';
import { useTimeTravelStore } from '../../../stores/timeTravelStore';

function renderToolbar() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <TimeTravelToolbar ontologyApiName="iotDemo" />
    </QueryClientProvider>,
  );
}

describe('TimeTravelToolbar rollback status (SELF-447)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useTimeTravelStore.setState({ selections: {} });
    window.localStorage.clear();
  });

  it('labels rolled-back checkpoints and keeps them selectable', async () => {
    vi.spyOn(datasetsApi, 'listDatasetHistory').mockResolvedValue({
      transactions: [
        {
          txId: 'tx-rolled-back',
          ontologyApiName: 'iotDemo',
          committedAt: '2026-05-19T08:00:00Z',
          editsCount: 3,
          rolledBackAt: '2026-05-19T08:15:00Z',
          rolledBackToTxId: 'tx-target',
        },
      ],
      truncated: false,
    });

    renderToolbar();

    const option = await screen.findByTestId('time-travel-option');
    expect(option).toHaveValue('tx-rolled-back');
    expect(option.textContent ?? '').toMatch(/rolled back/i);
    expect(option.textContent ?? '').toMatch(/tx-target/i);
  });

  it('warns when the active historical checkpoint was rolled back', async () => {
    useTimeTravelStore.getState().setAsOf('iotDemo', 'tx-rolled-back');
    vi.spyOn(datasetsApi, 'listDatasetHistory').mockResolvedValue({
      transactions: [
        {
          txId: 'tx-rolled-back',
          ontologyApiName: 'iotDemo',
          committedAt: '2026-05-19T08:00:00Z',
          editsCount: 3,
          rolledBackAt: '2026-05-19T08:15:00Z',
          rolledBackToTxId: 'tx-target',
        },
      ],
      truncated: false,
    });

    renderToolbar();

    await waitFor(() => {
      expect(
        screen.getByTestId('time-travel-active-badge').textContent ?? '',
      ).toMatch(/rolled back/i);
    });
    expect(
      screen.getByTestId('time-travel-active-badge').textContent ?? '',
    ).toMatch(/tx-target/i);
  });
});
