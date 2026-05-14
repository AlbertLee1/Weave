import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { TimeTravelToolbar } from '../TimeTravelToolbar';
import * as datasetsApi from '../../../api/datasets';

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

describe('TimeTravelToolbar empty transactions (dogfood #7)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('option text and hint guide the user to apply an action', async () => {
    vi.spyOn(datasetsApi, 'listDatasetHistory').mockResolvedValue({
      transactions: [],
    });

    renderToolbar();

    // Wait for the picker to render. The default option's label should
    // explain what to do when no transactions have been recorded.
    const picker = await screen.findByTestId('time-travel-picker');
    expect(picker.textContent ?? '').toMatch(/no transactions/i);
    expect(picker.textContent ?? '').toMatch(/apply an action/i);
  });
});
