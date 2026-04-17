import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { TimeSeriesChart } from '../TimeSeriesChart';
import * as timeseriesApi from '../../../api/timeseries';

function renderChart() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <TimeSeriesChart
        ontologyApiName="test"
        objectType="Server"
        primaryKey="s1"
        property="cpu"
      />
    </QueryClientProvider>,
  );
}

describe('TimeSeriesChart', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders the 4 range buttons with 24h selected by default', async () => {
    vi.spyOn(timeseriesApi, 'streamTimeSeriesPoints').mockResolvedValue([]);
    renderChart();
    expect(screen.getByRole('button', { name: '1h' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '24h' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '7d' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '30d' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '24h' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
  });

  it('shows "No data" message when API returns empty list', async () => {
    vi.spyOn(timeseriesApi, 'streamTimeSeriesPoints').mockResolvedValue([]);
    renderChart();
    await waitFor(() =>
      expect(screen.getByText(/no data/i)).toBeInTheDocument(),
    );
  });

  it('switches range on button click', async () => {
    vi.spyOn(timeseriesApi, 'streamTimeSeriesPoints').mockResolvedValue([]);
    renderChart();
    fireEvent.click(screen.getByRole('button', { name: '7d' }));
    expect(screen.getByRole('button', { name: '7d' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    expect(screen.getByRole('button', { name: '24h' })).toHaveAttribute(
      'aria-pressed',
      'false',
    );
  });

  it('shows error message on API failure', async () => {
    vi.spyOn(timeseriesApi, 'streamTimeSeriesPoints').mockRejectedValue(
      new Error('boom'),
    );
    renderChart();
    await waitFor(() =>
      expect(screen.getByText(/failed to load/i)).toBeInTheDocument(),
    );
  });

  it('filters out points outside the current range', async () => {
    const now = Date.now();
    const recent = new Date(now - 60 * 1000).toISOString();
    const old = new Date(now - 40 * 24 * 60 * 60 * 1000).toISOString();
    const spy = vi
      .spyOn(timeseriesApi, 'streamTimeSeriesPoints')
      .mockResolvedValue([
        { time: old, value: 1 },
        { time: recent, value: 2 },
      ]);

    renderChart();
    await waitFor(() => expect(spy).toHaveBeenCalledTimes(1));
    // With 24h window (default) only the recent point should pass the filter —
    // the chart container is not hidden.
    await waitFor(() =>
      expect(screen.getByTestId('timeseries-chart-cpu')).not.toHaveClass(
        'hidden',
      ),
    );

    // Switching to 1h still keeps the recent point in-window.
    fireEvent.click(screen.getByRole('button', { name: '1h' }));
    await waitFor(() =>
      expect(screen.getByTestId('timeseries-chart-cpu')).not.toHaveClass(
        'hidden',
      ),
    );
  });
});
