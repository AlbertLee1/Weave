import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QuiverPage } from '../QuiverPage';
import * as timeseriesApi from '../../../api/timeseries';

function renderPage(initialPath = '/quiver/test') {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialPath]}>
        <Routes>
          <Route path="/quiver/:ontology" element={<QuiverPage />} />
          <Route path="*" element={<QuiverPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('QuiverPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders the empty state until a series is added', () => {
    renderPage();
    expect(screen.getByTestId('quiver-page')).toBeInTheDocument();
    expect(screen.getByText(/no series yet/i)).toBeInTheDocument();
    expect(screen.getByTestId('quiver-add-button')).toBeDisabled();
  });

  it('disables the Add button until all required fields are filled', () => {
    renderPage();
    fireEvent.change(screen.getByTestId('quiver-input-objectType'), {
      target: { value: 'Server' },
    });
    expect(screen.getByTestId('quiver-add-button')).toBeDisabled();
    fireEvent.change(screen.getByTestId('quiver-input-primaryKey'), {
      target: { value: 's1' },
    });
    expect(screen.getByTestId('quiver-add-button')).toBeDisabled();
    fireEvent.change(screen.getByTestId('quiver-input-property'), {
      target: { value: 'cpu' },
    });
    expect(screen.getByTestId('quiver-add-button')).not.toBeDisabled();
  });

  it('adds a series and renders aggregate stats over its points', async () => {
    const points = [
      { time: '2026-04-18T10:00:00Z', value: 10 },
      { time: '2026-04-18T11:00:00Z', value: 30 },
    ];
    vi.spyOn(timeseriesApi, 'streamTimeSeriesPoints').mockResolvedValue(points);

    renderPage();
    fireEvent.change(screen.getByTestId('quiver-input-objectType'), {
      target: { value: 'Server' },
    });
    fireEvent.change(screen.getByTestId('quiver-input-primaryKey'), {
      target: { value: 's1' },
    });
    fireEvent.change(screen.getByTestId('quiver-input-property'), {
      target: { value: 'cpu' },
    });
    fireEvent.click(screen.getByTestId('quiver-add-button'));

    expect(screen.getAllByText('Server/s1.cpu').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByTestId('quiver-aggregate-panel')).toBeInTheDocument();

    await waitFor(() => {
      const row = screen.getByTestId(/^quiver-row-Server\|s1\|cpu/);
      expect(within(row).getByText('2')).toBeInTheDocument();
    });

    const row = screen.getByTestId(/^quiver-row-Server\|s1\|cpu/);
    const rid = row.getAttribute('data-testid')!.replace('quiver-row-', '');
    expect(screen.getByTestId(`quiver-sum-${rid}`)).toHaveTextContent('40.00');
    expect(screen.getByTestId(`quiver-avg-${rid}`)).toHaveTextContent('20.00');
    expect(screen.getByTestId(`quiver-max-${rid}`)).toHaveTextContent('30.00');
  });

  it('uses distinct colors for additional series', async () => {
    vi.spyOn(timeseriesApi, 'streamTimeSeriesPoints').mockResolvedValue([]);
    renderPage();

    function addSeries(ot: string, pk: string, prop: string) {
      fireEvent.change(screen.getByTestId('quiver-input-objectType'), {
        target: { value: ot },
      });
      fireEvent.change(screen.getByTestId('quiver-input-primaryKey'), {
        target: { value: pk },
      });
      fireEvent.change(screen.getByTestId('quiver-input-property'), {
        target: { value: prop },
      });
      fireEvent.click(screen.getByTestId('quiver-add-button'));
    }

    addSeries('Server', 's1', 'cpu');
    addSeries('Server', 's1', 'mem');

    const swatches = screen.getAllByTestId(/^quiver-color-/);
    expect(swatches).toHaveLength(2);
    const c1 = (swatches[0] as HTMLElement).style.background;
    const c2 = (swatches[1] as HTMLElement).style.background;
    expect(c1).not.toBe('');
    expect(c2).not.toBe('');
    expect(c1).not.toBe(c2);
  });

  it('removes a series via the row × button', async () => {
    vi.spyOn(timeseriesApi, 'streamTimeSeriesPoints').mockResolvedValue([]);
    renderPage();

    fireEvent.change(screen.getByTestId('quiver-input-objectType'), {
      target: { value: 'Server' },
    });
    fireEvent.change(screen.getByTestId('quiver-input-primaryKey'), {
      target: { value: 's1' },
    });
    fireEvent.change(screen.getByTestId('quiver-input-property'), {
      target: { value: 'cpu' },
    });
    fireEvent.click(screen.getByTestId('quiver-add-button'));

    const row = screen.getByTestId(/^quiver-row-Server\|s1\|cpu/);
    const rid = row.getAttribute('data-testid')!.replace('quiver-row-', '');
    fireEvent.click(screen.getByTestId(`quiver-remove-${rid}`));
    expect(screen.getByText(/no series yet/i)).toBeInTheDocument();
  });

  it('shows the missing-ontology empty state when the URL has no ontology', () => {
    renderPage('/');
    expect(screen.getByText(/missing ontology/i)).toBeInTheDocument();
  });
});
