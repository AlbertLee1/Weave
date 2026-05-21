import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';

// Mock the quiver API module so the tests don't issue real network
// requests when the page mounts and lists saved dashboards. Default
// impls keep the panel quiet — tests that exercise the save flow
// override the mock per-case.
const quiverMocks = vi.hoisted(() => ({
  listQuiverDashboards: vi
    .fn()
    .mockResolvedValue({ dashboards: [] }),
  getQuiverDashboard: vi.fn(),
  saveQuiverDashboard: vi.fn(),
  deleteQuiverDashboard: vi.fn(),
  viewQuiverDashboard: vi.fn(),
}));
vi.mock('../../../api/quiver', () => quiverMocks);

import { QuiverPage } from '../QuiverPage';
import * as timeseriesApi from '../../../api/timeseries';
import { useBranchStore } from '../../../stores/branchStore';

function renderPage(initialPath = '/quiver/test') {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialPath]}>
        <Routes>
          <Route path="/quiver/:ontology" element={<QuiverPage />} />
          <Route path="/quiver/:ontology/:rid" element={<QuiverPage />} />
          <Route path="*" element={<QuiverPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('QuiverPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    // US-404: branchStore persists per-ontology selection in localStorage
    // (Zustand persist middleware). Clear it between tests so a stale
    // selection from a sibling test cannot drift the form's default
    // branch and corrupt assertions on the saved config payload.
    localStorage.removeItem('weave-active-branch');
    useBranchStore.setState({ selections: {} });
    quiverMocks.saveQuiverDashboard.mockClear();
    quiverMocks.listQuiverDashboards.mockClear();
    quiverMocks.getQuiverDashboard.mockClear();
    quiverMocks.deleteQuiverDashboard.mockClear();
    quiverMocks.viewQuiverDashboard.mockClear();
  });

  it('renders the empty state until a series is added', () => {
    renderPage();
    expect(screen.getByTestId('quiver-page')).toBeInTheDocument();
    expect(screen.getByText(/no series yet/i)).toBeInTheDocument();
    expect(screen.getByTestId('quiver-add-button')).toBeDisabled();
  });

  it('disables JSON export until the workbench has at least one series', () => {
    renderPage();
    expect(screen.getByTestId('quiver-export-json-button')).toBeDisabled();
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

  it('exports the current workbench as JSON with ontology, name, rid, and series config', async () => {
    vi.spyOn(timeseriesApi, 'streamTimeSeriesPoints').mockResolvedValue([]);
    const createObjectURL = vi
      .spyOn(URL, 'createObjectURL')
      .mockReturnValue('blob:quiver-export');
    const revokeObjectURL = vi
      .spyOn(URL, 'revokeObjectURL')
      .mockImplementation(() => {});
    const originalCreateElement = document.createElement.bind(document);
    let clickedDownload = '';
    let clickedHref = '';
    vi.spyOn(document, 'createElement').mockImplementation((tagName: string) => {
      const el = originalCreateElement(tagName);
      if (tagName.toLowerCase() === 'a') {
        const anchor = el as HTMLAnchorElement;
        anchor.click = () => {
          clickedDownload = anchor.download;
          clickedHref = anchor.href;
        };
      }
      return el;
    });

    quiverMocks.getQuiverDashboard.mockResolvedValue({
      rid: 'ri.quiver.main.dashboard.loaded',
      name: 'Loaded Dashboard',
      owner: 'user:test',
      config: { ontologyApiName: 'test', series: [] },
      createdAt: '2026-04-18T10:00:00Z',
      updatedAt: '2026-04-18T10:00:00Z',
    });
    renderPage('/quiver/test/ri.quiver.main.dashboard.loaded');

    fireEvent.change(screen.getByTestId('quiver-input-objectType'), {
      target: { value: 'Server' },
    });
    fireEvent.change(screen.getByTestId('quiver-input-primaryKey'), {
      target: { value: 's1' },
    });
    fireEvent.change(screen.getByTestId('quiver-input-property'), {
      target: { value: 'cpu' },
    });
    fireEvent.change(screen.getByTestId('quiver-input-label'), {
      target: { value: 'CPU Load' },
    });
    fireEvent.change(screen.getByTestId('quiver-input-branch'), {
      target: { value: 'feature-x' },
    });
    fireEvent.change(screen.getByTestId('quiver-dashboard-name'), {
      target: { value: 'Capacity Review' },
    });
    fireEvent.click(screen.getByTestId('quiver-add-button'));

    fireEvent.click(screen.getByTestId('quiver-export-json-button'));

    await waitFor(() => {
      expect(createObjectURL).toHaveBeenCalledTimes(1);
    });
    expect(clickedHref).toBe('blob:quiver-export');
    expect(clickedDownload).toBe('capacity-review.quiver.json');
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:quiver-export');

    const blob = createObjectURL.mock.calls[0][0] as Blob;
    expect(blob.type).toBe('application/json');
    const payload = JSON.parse(await blob.text());
    expect(payload).toMatchObject({
      ontologyApiName: 'test',
      rid: 'ri.quiver.main.dashboard.loaded',
      name: 'Capacity Review',
      config: {
        ontologyApiName: 'test',
        series: [
          {
            objectType: 'Server',
            primaryKey: 's1',
            property: 'cpu',
            label: 'CPU Load',
            branch: 'feature-x',
          },
        ],
      },
    });
    expect(payload.config.series[0].id).toMatch(/^Server\|s1\|cpu\|feature-x\|/);
    expect(payload.config.series[0].color).toMatch(/^#/);
    expect(payload.exportedAt).toMatch(/^\d{4}-\d{2}-\d{2}T/);
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

  // US-404: per-series branch override field on the picker form. The
  // submitted SeriesSpec persists `branch`, the timeseries fetch passes
  // it through, and the row badge reveals which branch the spec resolves
  // on. Two overlays of the same (objectType, primaryKey, property)
  // share a colour but render with different dash patterns in the chart
  // (asserted in the QuiverWorkbenchView/MultiSeriesChart unit tests).
  it('persists a per-series branch override and forwards it to the timeseries fetch', async () => {
    const spy = vi
      .spyOn(timeseriesApi, 'streamTimeSeriesPoints')
      .mockResolvedValue([]);
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
    fireEvent.change(screen.getByTestId('quiver-input-branch'), {
      target: { value: 'feature-x' },
    });
    fireEvent.click(screen.getByTestId('quiver-add-button'));

    await waitFor(() => expect(spy).toHaveBeenCalled());
    const call = spy.mock.calls.find(
      (c) => (c[0] as { branch?: string }).branch === 'feature-x',
    );
    expect(call).toBeDefined();
    const row = screen.getByTestId(/^quiver-row-Server\|s1\|cpu\|feature-x/);
    const rid = row.getAttribute('data-testid')!.replace('quiver-row-', '');
    expect(screen.getByTestId(`quiver-branch-${rid}`)).toHaveTextContent(
      'feature-x',
    );
  });

  it('reuses the same colour for two branches of the same property and tags both rows (US-404)', async () => {
    vi.spyOn(timeseriesApi, 'streamTimeSeriesPoints').mockResolvedValue([]);
    renderPage();

    function addBranchSeries(branch: string) {
      fireEvent.change(screen.getByTestId('quiver-input-objectType'), {
        target: { value: 'Server' },
      });
      fireEvent.change(screen.getByTestId('quiver-input-primaryKey'), {
        target: { value: 's1' },
      });
      fireEvent.change(screen.getByTestId('quiver-input-property'), {
        target: { value: 'cpu' },
      });
      fireEvent.change(screen.getByTestId('quiver-input-branch'), {
        target: { value: branch },
      });
      fireEvent.click(screen.getByTestId('quiver-add-button'));
    }

    addBranchSeries('main');
    addBranchSeries('feature-x');

    const swatches = screen.getAllByTestId(/^quiver-color-/);
    expect(swatches).toHaveLength(2);
    // Two branch overlays of the same (ot, pk, property) reuse the slot
    // colour. The non-default branch swatch carries a dashed gradient
    // so the rendered backgrounds are NOT identical even though both
    // strokes use the same RGB value — assert the underlying chart
    // series colour by inspecting the raw style for the rgb token.
    const mainBg = (swatches[0] as HTMLElement).style.background;
    const branchBg = (swatches[1] as HTMLElement).style.background;
    expect(mainBg).not.toBe('');
    expect(branchBg).not.toBe('');
    expect(branchBg).toContain(mainBg);

    const branchTags = screen.getAllByTestId(/^quiver-branch-/);
    expect(branchTags.map((el) => el.textContent)).toEqual(
      expect.arrayContaining(['main', 'feature-x']),
    );
  });

  it('saves the workbench config to the backend (US-403)', async () => {
    vi.spyOn(timeseriesApi, 'streamTimeSeriesPoints').mockResolvedValue([]);
    quiverMocks.saveQuiverDashboard.mockResolvedValue({
      rid: 'ri.quiver.main.dashboard.new',
      name: 'Demo',
      owner: 'user:test',
      config: {
        ontologyApiName: 'test',
        series: [
          {
            id: 'a',
            objectType: 'Server',
            primaryKey: 's1',
            property: 'cpu',
            label: 'CPU',
            color: '#22d3ee',
          },
        ],
      },
      createdAt: '2026-04-18T10:00:00Z',
      updatedAt: '2026-04-18T10:00:00Z',
    });

    renderPage();

    // Save button is hidden when no series + no name → start by adding a series.
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

    fireEvent.change(screen.getByTestId('quiver-dashboard-name'), {
      target: { value: 'Demo' },
    });
    expect(screen.getByTestId('quiver-save-button')).not.toBeDisabled();
    fireEvent.click(screen.getByTestId('quiver-save-button'));

    await waitFor(() => {
      expect(quiverMocks.saveQuiverDashboard).toHaveBeenCalledTimes(1);
    });
    const arg = quiverMocks.saveQuiverDashboard.mock.calls[0][0];
    expect(arg.name).toBe('Demo');
    expect(arg.config.ontologyApiName).toBe('test');
    expect(arg.config.series).toHaveLength(1);
    expect(arg.config.series[0].objectType).toBe('Server');
    expect(arg.config.series[0].primaryKey).toBe('s1');
    expect(arg.config.series[0].property).toBe('cpu');
    // First save (no existing rid) should not include rid in payload.
    expect(arg.rid).toBeUndefined();
  });

  it('includes the per-series branch in the saved config payload (US-404)', async () => {
    vi.spyOn(timeseriesApi, 'streamTimeSeriesPoints').mockResolvedValue([]);
    quiverMocks.saveQuiverDashboard.mockResolvedValue({
      rid: 'ri.quiver.main.dashboard.new',
      name: 'BranchedDemo',
      owner: 'user:test',
      config: { ontologyApiName: 'test', series: [] },
      createdAt: '2026-04-18T10:00:00Z',
      updatedAt: '2026-04-18T10:00:00Z',
    });

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
    fireEvent.change(screen.getByTestId('quiver-input-branch'), {
      target: { value: 'feature-x' },
    });
    fireEvent.click(screen.getByTestId('quiver-add-button'));

    fireEvent.change(screen.getByTestId('quiver-dashboard-name'), {
      target: { value: 'BranchedDemo' },
    });
    fireEvent.click(screen.getByTestId('quiver-save-button'));

    await waitFor(() => {
      expect(quiverMocks.saveQuiverDashboard).toHaveBeenCalledTimes(1);
    });
    const arg = quiverMocks.saveQuiverDashboard.mock.calls[0][0];
    expect(arg.config.series).toHaveLength(1);
    expect(arg.config.series[0].branch).toBe('feature-x');
  });
});
