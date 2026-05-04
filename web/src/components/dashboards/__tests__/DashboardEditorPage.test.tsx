import { describe, it, expect, vi, beforeEach } from 'vitest';
import {
  render,
  screen,
  fireEvent,
  within,
  createEvent,
  waitFor,
  act,
} from '@testing-library/react';

// Mock the dashboards API module so tests don't issue real network
// requests. Default impl resolves to empty/no-op so the existing US-327
// / US-328 tests that don't care about persistence still pass without
// modification — they were authored before US-329 added the API call.
const apiMocks = vi.hoisted(() => ({
  listDashboards: vi.fn(),
  getDashboard: vi.fn(),
  createDashboard: vi.fn(),
  updateDashboard: vi.fn(),
  deleteDashboard: vi.fn(),
}));

vi.mock('../../../api/dashboards', () => apiMocks);

// US-428: stub the aggregation API the new widget data-source binding
// dispatches against. Tests that don't bind a data source never reach
// the mock; the default rejection keeps misuse loud.
const aggMocks = vi.hoisted(() => ({
  aggregate: vi.fn(),
}));

vi.mock('../../../api/aggregation', () => aggMocks);

// react-leaflet reaches into leaflet's runtime at module-load. Stub it so
// the editor test suite doesn't require a real map surface.
vi.mock('../widgets/MapViewLeaflet', () => ({
  default: () => null,
}));

import { DashboardEditorPage } from '../DashboardEditorPage';

// jsdom doesn't fully model DataTransfer; supply a minimal stub so drag events
// round-trip a payload (same recipe as web/src/components/browser/__tests__/PivotTable.test.tsx).
function makeDataTransfer() {
  const map = new Map<string, string>();
  return {
    setData: (k: string, v: string) => {
      map.set(k, v);
    },
    getData: (k: string) => map.get(k) ?? '',
    types: [] as string[],
    effectAllowed: '',
    dropEffect: '',
  };
}

const DND_MIME = 'application/x-weave-dashboard';

function fireDragWithCoords(
  target: HTMLElement,
  type: 'dragStart' | 'dragOver' | 'drop',
  dataTransfer: ReturnType<typeof makeDataTransfer>,
  clientX = 0,
  clientY = 0,
) {
  // RTL's fireEvent.{dragStart,dragOver,drop} doesn't propagate clientX/Y into
  // the synthetic React event. createEvent.* + defineProperty is the supported
  // workaround — same recipe used in @testing-library docs for mouse events.
  const ev = createEvent[type](target, { dataTransfer });
  Object.defineProperty(ev, 'clientX', { value: clientX });
  Object.defineProperty(ev, 'clientY', { value: clientY });
  fireEvent(target, ev);
}

function dragWith(
  source: HTMLElement,
  target: HTMLElement,
  init: { clientX?: number; clientY?: number } = {},
) {
  const dt = makeDataTransfer();
  // Anchor the drag-start at the widget's top-left so move math snaps the
  // top-left under the drop point.
  fireDragWithCoords(source, 'dragStart', dt, 0, 0);
  dt.types = [DND_MIME];
  fireDragWithCoords(target, 'dragOver', dt, init.clientX, init.clientY);
  fireDragWithCoords(target, 'drop', dt, init.clientX, init.clientY);
}

// 12-column × 600px grid sized 1200px wide ⇒ each column is 100px and each
// row is 60px. Stub getBoundingClientRect on the grid surface so coordinate
// math is deterministic in jsdom.
function stubGridRect(grid: HTMLElement, width = 1200, height = 600) {
  grid.getBoundingClientRect = () =>
    ({
      x: 0,
      y: 0,
      left: 0,
      top: 0,
      right: width,
      bottom: height,
      width,
      height,
      toJSON: () => ({}),
    }) as DOMRect;
}

function getWidgets() {
  return screen.queryAllByTestId('dashboard-widget');
}

beforeEach(() => {
  apiMocks.listDashboards.mockReset();
  apiMocks.getDashboard.mockReset();
  apiMocks.createDashboard.mockReset();
  apiMocks.updateDashboard.mockReset();
  apiMocks.deleteDashboard.mockReset();
  apiMocks.listDashboards.mockResolvedValue({ dashboards: [] });
  apiMocks.getDashboard.mockRejectedValue(new Error('not configured'));
  aggMocks.aggregate.mockReset();
  aggMocks.aggregate.mockRejectedValue(new Error('not configured'));
});

describe('DashboardEditorPage', () => {
  it('renders an empty placeholder and an Add Widget affordance on first mount', () => {
    render(<DashboardEditorPage />);
    expect(screen.getByTestId('dashboard-empty')).toBeInTheDocument();
    expect(screen.getByTestId('dashboard-widget-add')).toBeInTheDocument();
    expect(getWidgets()).toHaveLength(0);
  });

  it('appends a Text widget when Add Widget is clicked and clears the empty state', () => {
    render(<DashboardEditorPage />);
    fireEvent.click(screen.getByTestId('dashboard-widget-add'));
    expect(screen.queryByTestId('dashboard-empty')).not.toBeInTheDocument();
    const widgets = getWidgets();
    expect(widgets).toHaveLength(1);
    expect(widgets[0].getAttribute('data-widget-type')).toBe('text');
  });

  it('lays each new widget on its own row and keeps unique ids', () => {
    render(<DashboardEditorPage />);
    fireEvent.click(screen.getByTestId('dashboard-widget-add'));
    fireEvent.click(screen.getByTestId('dashboard-widget-add'));
    const widgets = getWidgets();
    expect(widgets).toHaveLength(2);
    const ids = widgets.map((w) => w.getAttribute('data-widget-id'));
    expect(new Set(ids).size).toBe(2);
    // Default w=4 h=2 → second widget pushed to y=2.
    expect(widgets[0].getAttribute('data-widget-y')).toBe('0');
    expect(widgets[1].getAttribute('data-widget-y')).toBe('2');
  });

  it('opens the configure panel and persists title + content edits', () => {
    render(<DashboardEditorPage />);
    fireEvent.click(screen.getByTestId('dashboard-widget-add'));
    const widget = getWidgets()[0];
    fireEvent.click(within(widget).getByTestId('dashboard-widget-configure'));

    const titleInput = within(widget).getByTestId(
      'dashboard-widget-title-input',
    ) as HTMLInputElement;
    fireEvent.change(titleInput, { target: { value: 'Daily KPIs' } });
    const contentArea = within(widget).getByTestId(
      'dashboard-widget-content-input',
    ) as HTMLTextAreaElement;
    fireEvent.change(contentArea, { target: { value: 'Revenue: $42k' } });

    fireEvent.click(within(widget).getByTestId('dashboard-widget-configure'));
    expect(within(widget).getByTestId('dashboard-widget-title')).toHaveTextContent(
      'Daily KPIs',
    );
    expect(within(widget).getByTestId('dashboard-widget-content')).toHaveTextContent(
      'Revenue: $42k',
    );
  });

  it('removes a widget when its remove control is clicked', () => {
    render(<DashboardEditorPage />);
    fireEvent.click(screen.getByTestId('dashboard-widget-add'));
    fireEvent.click(screen.getByTestId('dashboard-widget-add'));
    expect(getWidgets()).toHaveLength(2);

    const first = getWidgets()[0];
    const firstId = first.getAttribute('data-widget-id');
    fireEvent.click(within(first).getByTestId('dashboard-widget-remove'));

    const remaining = getWidgets();
    expect(remaining).toHaveLength(1);
    expect(remaining[0].getAttribute('data-widget-id')).not.toBe(firstId);
  });

  it('moves a widget to a new column / row when its drag handle is dropped on the grid', () => {
    render(<DashboardEditorPage />);
    fireEvent.click(screen.getByTestId('dashboard-widget-add'));
    const widget = getWidgets()[0];
    expect(widget.getAttribute('data-widget-x')).toBe('0');
    expect(widget.getAttribute('data-widget-y')).toBe('0');

    const grid = screen.getByTestId('dashboard-grid');
    stubGridRect(grid);

    const handle = within(widget).getByTestId('dashboard-widget-drag-handle');
    // Drop near the centre of column 5, row 2 (clientX=550, clientY=120).
    dragWith(handle, grid, { clientX: 550, clientY: 120 });

    const moved = getWidgets()[0];
    expect(moved.getAttribute('data-widget-x')).toBe('5');
    expect(moved.getAttribute('data-widget-y')).toBe('2');
  });

  it('resizes a widget when its resize handle is dropped on the grid', () => {
    render(<DashboardEditorPage />);
    fireEvent.click(screen.getByTestId('dashboard-widget-add'));
    const widget = getWidgets()[0];
    expect(widget.getAttribute('data-widget-w')).toBe('4');
    expect(widget.getAttribute('data-widget-h')).toBe('2');

    const grid = screen.getByTestId('dashboard-grid');
    stubGridRect(grid);

    const resize = within(widget).getByTestId('dashboard-widget-resize-handle');
    // Widget anchored at (0,0); dropping at (700, 240) ⇒ new w=7 (col 7), h=4.
    dragWith(resize, grid, { clientX: 700, clientY: 240 });

    const resized = getWidgets()[0];
    expect(resized.getAttribute('data-widget-w')).toBe('7');
    expect(resized.getAttribute('data-widget-h')).toBe('4');
  });

  it('clamps drag drops to the 12-column band', () => {
    render(<DashboardEditorPage />);
    fireEvent.click(screen.getByTestId('dashboard-widget-add'));
    const grid = screen.getByTestId('dashboard-grid');
    stubGridRect(grid);

    const handle = within(getWidgets()[0]).getByTestId(
      'dashboard-widget-drag-handle',
    );
    // Default width is 4 ⇒ max x = 12 - 4 = 8. clientX way outside grid clamps
    // back into the band rather than walking off the right edge.
    dragWith(handle, grid, { clientX: 99999, clientY: 60 });

    const moved = getWidgets()[0];
    expect(Number(moved.getAttribute('data-widget-x'))).toBeLessThanOrEqual(8);
    expect(Number(moved.getAttribute('data-widget-x'))).toBeGreaterThanOrEqual(0);
  });
});

describe('DashboardEditorPage widget library (US-328)', () => {
  it('exposes type-specific add buttons for chart / table / stat / map alongside text', () => {
    render(<DashboardEditorPage />);
    expect(screen.getByTestId('dashboard-widget-add')).toBeInTheDocument();
    expect(screen.getByTestId('dashboard-widget-add-chart')).toBeInTheDocument();
    expect(screen.getByTestId('dashboard-widget-add-table')).toBeInTheDocument();
    expect(screen.getByTestId('dashboard-widget-add-stat')).toBeInTheDocument();
    expect(screen.getByTestId('dashboard-widget-add-map')).toBeInTheDocument();
  });

  it('appends a Chart widget with default chartType=bar', () => {
    render(<DashboardEditorPage />);
    fireEvent.click(screen.getByTestId('dashboard-widget-add-chart'));
    const widgets = getWidgets();
    expect(widgets).toHaveLength(1);
    expect(widgets[0].getAttribute('data-widget-type')).toBe('chart');
    const chart = within(widgets[0]).getByTestId('dashboard-widget-chart');
    expect(chart).toHaveAttribute('data-chart-type', 'bar');
  });

  it('appends a Table widget with a header / body grid', () => {
    render(<DashboardEditorPage />);
    fireEvent.click(screen.getByTestId('dashboard-widget-add-table'));
    const widget = getWidgets()[0];
    expect(widget.getAttribute('data-widget-type')).toBe('table');
    expect(within(widget).getByTestId('dashboard-widget-table')).toBeInTheDocument();
  });

  it('appends a Stat widget that renders the value and label', () => {
    render(<DashboardEditorPage />);
    fireEvent.click(screen.getByTestId('dashboard-widget-add-stat'));
    const widget = getWidgets()[0];
    expect(widget.getAttribute('data-widget-type')).toBe('stat');
    expect(within(widget).getByTestId('dashboard-widget-stat')).toBeInTheDocument();
  });

  it('appends a Map widget with default coordinates', () => {
    render(<DashboardEditorPage />);
    fireEvent.click(screen.getByTestId('dashboard-widget-add-map'));
    const widget = getWidgets()[0];
    expect(widget.getAttribute('data-widget-type')).toBe('map');
    expect(within(widget).getByTestId('dashboard-widget-map')).toBeInTheDocument();
  });

  it('chart config panel persists chartType and values', () => {
    render(<DashboardEditorPage />);
    fireEvent.click(screen.getByTestId('dashboard-widget-add-chart'));
    const widget = getWidgets()[0];
    fireEvent.click(within(widget).getByTestId('dashboard-widget-configure'));

    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-chart-type-select'),
      { target: { value: 'pie' } },
    );
    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-chart-values-input'),
      { target: { value: '10, 20, 30, 40' } },
    );

    fireEvent.click(within(widget).getByTestId('dashboard-widget-configure'));
    const chart = within(widget).getByTestId('dashboard-widget-chart');
    expect(chart).toHaveAttribute('data-chart-type', 'pie');
    expect(chart).toHaveAttribute('data-chart-values', '10,20,30,40');
  });

  it('table config panel persists columns and rows', () => {
    render(<DashboardEditorPage />);
    fireEvent.click(screen.getByTestId('dashboard-widget-add-table'));
    const widget = getWidgets()[0];
    fireEvent.click(within(widget).getByTestId('dashboard-widget-configure'));

    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-table-columns-input'),
      { target: { value: 'Name, Score' } },
    );
    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-table-rows-input'),
      { target: { value: 'Alice, 92\nBob, 85' } },
    );

    fireEvent.click(within(widget).getByTestId('dashboard-widget-configure'));
    const table = within(widget).getByTestId('dashboard-widget-table');
    expect(within(table).getByText('Name')).toBeInTheDocument();
    expect(within(table).getByText('Score')).toBeInTheDocument();
    expect(within(table).getByText('Alice')).toBeInTheDocument();
    expect(within(table).getByText('92')).toBeInTheDocument();
    expect(within(table).getByText('Bob')).toBeInTheDocument();
    expect(within(table).getByText('85')).toBeInTheDocument();
  });

  it('stat config panel persists value, label, and trend', () => {
    render(<DashboardEditorPage />);
    fireEvent.click(screen.getByTestId('dashboard-widget-add-stat'));
    const widget = getWidgets()[0];
    fireEvent.click(within(widget).getByTestId('dashboard-widget-configure'));

    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-stat-value-input'),
      { target: { value: '$42k' } },
    );
    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-stat-label-input'),
      { target: { value: 'Q4 Revenue' } },
    );
    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-stat-trend-select'),
      { target: { value: 'up' } },
    );

    fireEvent.click(within(widget).getByTestId('dashboard-widget-configure'));
    const stat = within(widget).getByTestId('dashboard-widget-stat');
    expect(stat).toHaveAttribute('data-stat-trend', 'up');
    expect(within(stat).getByText('$42k')).toBeInTheDocument();
    expect(within(stat).getByText('Q4 Revenue')).toBeInTheDocument();
  });

  it('map config panel persists latitude / longitude / zoom', () => {
    render(<DashboardEditorPage />);
    fireEvent.click(screen.getByTestId('dashboard-widget-add-map'));
    const widget = getWidgets()[0];
    fireEvent.click(within(widget).getByTestId('dashboard-widget-configure'));

    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-map-lat-input'),
      { target: { value: '37.7749' } },
    );
    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-map-lng-input'),
      { target: { value: '-122.4194' } },
    );
    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-map-zoom-input'),
      { target: { value: '12' } },
    );

    fireEvent.click(within(widget).getByTestId('dashboard-widget-configure'));
    const map = within(widget).getByTestId('dashboard-widget-map');
    expect(map).toHaveAttribute('data-map-lat', '37.7749');
    expect(map).toHaveAttribute('data-map-lng', '-122.4194');
    expect(map).toHaveAttribute('data-map-zoom', '12');
  });

  it('chart widget tolerates malformed values input by skipping non-numeric tokens', () => {
    render(<DashboardEditorPage />);
    fireEvent.click(screen.getByTestId('dashboard-widget-add-chart'));
    const widget = getWidgets()[0];
    fireEvent.click(within(widget).getByTestId('dashboard-widget-configure'));
    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-chart-values-input'),
      { target: { value: '5, abc, 12, , 7' } },
    );
    fireEvent.click(within(widget).getByTestId('dashboard-widget-configure'));
    const chart = within(widget).getByTestId('dashboard-widget-chart');
    expect(chart).toHaveAttribute('data-chart-values', '5,12,7');
  });

  it('mixes widget types on the same dashboard and stacks them on successive rows', () => {
    render(<DashboardEditorPage />);
    fireEvent.click(screen.getByTestId('dashboard-widget-add'));
    fireEvent.click(screen.getByTestId('dashboard-widget-add-chart'));
    fireEvent.click(screen.getByTestId('dashboard-widget-add-stat'));
    const widgets = getWidgets();
    expect(widgets).toHaveLength(3);
    expect(widgets[0].getAttribute('data-widget-type')).toBe('text');
    expect(widgets[1].getAttribute('data-widget-type')).toBe('chart');
    expect(widgets[2].getAttribute('data-widget-type')).toBe('stat');
    // Default h=2 ⇒ each new widget lands two rows below the previous.
    expect(widgets[0].getAttribute('data-widget-y')).toBe('0');
    expect(widgets[1].getAttribute('data-widget-y')).toBe('2');
    expect(widgets[2].getAttribute('data-widget-y')).toBe('4');
  });

  // ---------------------------------------------------------------
  // US-329 Save / Load / Share
  // ---------------------------------------------------------------

  it('Save creates a new dashboard, surfaces "Saved", and exposes a share link', async () => {
    apiMocks.createDashboard.mockResolvedValue({
      id: 'dash-1',
      name: 'My Dashboard',
      createdBy: 'user:alice',
      isPublic: false,
      definition: { widgets: [] },
      createdAt: '2026-04-28T00:00:00Z',
      updatedAt: '2026-04-28T00:00:00Z',
    });
    const onSaved = vi.fn();
    render(<DashboardEditorPage onSaved={onSaved} />);
    fireEvent.change(screen.getByTestId('dashboard-name-input'), {
      target: { value: 'My Dashboard' },
    });
    fireEvent.click(screen.getByTestId('dashboard-widget-add-stat'));
    await act(async () => {
      fireEvent.click(screen.getByTestId('dashboard-save'));
    });
    expect(apiMocks.createDashboard).toHaveBeenCalledTimes(1);
    expect(apiMocks.createDashboard).toHaveBeenCalledWith(
      expect.objectContaining({
        name: 'My Dashboard',
        definition: expect.objectContaining({
          widgets: expect.arrayContaining([
            expect.objectContaining({ type: 'stat' }),
          ]),
        }),
      }),
    );
    expect(onSaved).toHaveBeenCalledWith('dash-1');
    expect(screen.getByTestId('dashboard-save-status')).toHaveAttribute(
      'data-save-status',
      'saved',
    );
    expect(screen.getByTestId('dashboard-share')).toBeInTheDocument();
    expect(screen.getByTestId('dashboard-share-url').textContent).toContain(
      '/dashboards/dash-1',
    );
  });

  it('updates an existing saved dashboard via PUT instead of POST', async () => {
    apiMocks.getDashboard.mockResolvedValue({
      id: 'dash-2',
      name: 'Loaded',
      createdBy: 'user:alice',
      isPublic: true,
      definition: {
        widgets: [
          {
            id: 'w-1',
            type: 'text',
            title: 'Hello',
            content: 'Hi',
            x: 0,
            y: 0,
            w: 4,
            h: 2,
          },
        ],
      },
      createdAt: '2026-04-28T00:00:00Z',
      updatedAt: '2026-04-28T00:00:00Z',
    });
    apiMocks.updateDashboard.mockResolvedValue({
      id: 'dash-2',
      name: 'Loaded',
      createdBy: 'user:alice',
      isPublic: true,
      definition: { widgets: [] },
      createdAt: '2026-04-28T00:00:00Z',
      updatedAt: '2026-04-28T00:00:01Z',
    });
    render(<DashboardEditorPage id="dash-2" />);
    await waitFor(() => {
      expect(screen.getByTestId('dashboard-widget')).toBeInTheDocument();
    });
    expect(screen.getByTestId('dashboard-name-input')).toHaveValue('Loaded');
    await act(async () => {
      fireEvent.click(screen.getByTestId('dashboard-save'));
    });
    expect(apiMocks.createDashboard).not.toHaveBeenCalled();
    expect(apiMocks.updateDashboard).toHaveBeenCalledWith(
      expect.objectContaining({ id: 'dash-2', name: 'Loaded', isPublic: true }),
    );
  });

  it('renders the share link copy affordance only after a dashboard has an id', () => {
    render(<DashboardEditorPage />);
    expect(screen.queryByTestId('dashboard-share')).not.toBeInTheDocument();
    expect(screen.queryByTestId('dashboard-share-url')).not.toBeInTheDocument();
  });

  it('exposes the saved id on the page wrapper for the route to inspect', async () => {
    apiMocks.getDashboard.mockResolvedValue({
      id: 'dash-3',
      name: 'X',
      createdBy: 'user:alice',
      isPublic: false,
      definition: { widgets: [] },
      createdAt: '2026-04-28T00:00:00Z',
      updatedAt: '2026-04-28T00:00:00Z',
    });
    render(<DashboardEditorPage id="dash-3" />);
    await waitFor(() => {
      expect(screen.getByTestId('dashboard-editor-page')).toHaveAttribute(
        'data-dashboard-id',
        'dash-3',
      );
    });
  });

  it('populates a Load picker from listDashboards()', async () => {
    apiMocks.listDashboards.mockResolvedValue({
      dashboards: [
        {
          id: 'dash-a',
          name: 'Alpha',
          createdBy: 'user:alice',
          isPublic: false,
          definition: { widgets: [] },
          createdAt: '2026-04-28T00:00:00Z',
          updatedAt: '2026-04-28T00:00:00Z',
        },
      ],
    });
    const onSaved = vi.fn();
    render(<DashboardEditorPage onSaved={onSaved} />);
    await waitFor(() => {
      expect(screen.getByTestId('dashboard-load-select')).toBeInTheDocument();
    });
    fireEvent.change(screen.getByTestId('dashboard-load-select'), {
      target: { value: 'dash-a' },
    });
    expect(onSaved).toHaveBeenCalledWith('dash-a');
  });
});

// US-428: real chart / stat / map renderers + ObjectSet / Aggregation
// data binding.
describe('DashboardEditorPage US-428 widget renderers', () => {
  it('renders bar chart as SVG rects (one per value)', () => {
    render(<DashboardEditorPage />);
    fireEvent.click(screen.getByTestId('dashboard-widget-add-chart'));
    const chart = screen.getByTestId('dashboard-widget-chart');
    expect(within(chart).getByTestId('dashboard-widget-chart-svg')).toBeInTheDocument();
    const bars = within(chart).getAllByTestId('dashboard-widget-chart-bar');
    // Default chart has 5 values.
    expect(bars).toHaveLength(5);
  });

  it('renders line chart as an SVG path with one circle per value', () => {
    render(<DashboardEditorPage />);
    fireEvent.click(screen.getByTestId('dashboard-widget-add-chart'));
    const widget = screen.getByTestId('dashboard-widget');
    fireEvent.click(within(widget).getByTestId('dashboard-widget-configure'));
    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-chart-type-select'),
      { target: { value: 'line' } },
    );
    fireEvent.click(within(widget).getByTestId('dashboard-widget-configure'));

    const chart = within(widget).getByTestId('dashboard-widget-chart');
    expect(chart).toHaveAttribute('data-chart-type', 'line');
    expect(within(chart).getByTestId('dashboard-widget-chart-line')).toBeInTheDocument();
    expect(
      within(chart).getAllByTestId('dashboard-widget-chart-point'),
    ).toHaveLength(5);
  });

  it('renders pie chart with one slice per non-zero value', () => {
    render(<DashboardEditorPage />);
    fireEvent.click(screen.getByTestId('dashboard-widget-add-chart'));
    const widget = screen.getByTestId('dashboard-widget');
    fireEvent.click(within(widget).getByTestId('dashboard-widget-configure'));
    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-chart-type-select'),
      { target: { value: 'pie' } },
    );
    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-chart-values-input'),
      { target: { value: '10, 0, 30, 60' } },
    );
    fireEvent.click(within(widget).getByTestId('dashboard-widget-configure'));

    const slices = within(widget).getAllByTestId('dashboard-widget-chart-slice');
    expect(slices).toHaveLength(3);
  });

  it('renders stat sparkline when configured', () => {
    render(<DashboardEditorPage />);
    fireEvent.click(screen.getByTestId('dashboard-widget-add-stat'));
    const widget = screen.getByTestId('dashboard-widget');
    fireEvent.click(within(widget).getByTestId('dashboard-widget-configure'));
    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-stat-sparkline-input'),
      { target: { value: '1, 2, 3, 4, 5' } },
    );
    fireEvent.click(within(widget).getByTestId('dashboard-widget-configure'));

    const spark = within(widget).getByTestId('dashboard-widget-stat-sparkline');
    expect(spark).toHaveAttribute('data-spark-values', '1,2,3,4,5');
  });

  it('omits sparkline when fewer than 2 values are configured', () => {
    render(<DashboardEditorPage />);
    fireEvent.click(screen.getByTestId('dashboard-widget-add-stat'));
    const widget = screen.getByTestId('dashboard-widget');
    expect(
      within(widget).queryByTestId('dashboard-widget-stat-sparkline'),
    ).not.toBeInTheDocument();
  });

  it('persists GeoJSON on the map widget', () => {
    render(<DashboardEditorPage />);
    fireEvent.click(screen.getByTestId('dashboard-widget-add-map'));
    const widget = screen.getByTestId('dashboard-widget');
    fireEvent.click(within(widget).getByTestId('dashboard-widget-configure'));
    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-map-geojson-input'),
      {
        target: {
          value:
            '{"type":"Feature","geometry":{"type":"Point","coordinates":[0,0]}}',
        },
      },
    );
    fireEvent.click(within(widget).getByTestId('dashboard-widget-configure'));

    const map = within(widget).getByTestId('dashboard-widget-map');
    expect(map).toHaveAttribute('data-map-has-geojson', 'true');
  });

  it('binds the chart widget to a live aggregation result', async () => {
    aggMocks.aggregate.mockResolvedValue({
      data: [
        { group: { country: 'US' }, metrics: { count: 12 } },
        { group: { country: 'CN' }, metrics: { count: 8 } },
        { group: { country: 'IN' }, metrics: { count: 5 } },
      ],
    });
    render(<DashboardEditorPage />);
    fireEvent.click(screen.getByTestId('dashboard-widget-add-chart'));
    const widget = screen.getByTestId('dashboard-widget');
    fireEvent.click(within(widget).getByTestId('dashboard-widget-configure'));
    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-data-source-kind'),
      { target: { value: 'aggregation' } },
    );
    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-data-source-ontology'),
      { target: { value: 'crm' } },
    );
    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-data-source-object-type'),
      { target: { value: 'customer' } },
    );
    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-data-source-group-by'),
      { target: { value: 'country' } },
    );
    fireEvent.click(within(widget).getByTestId('dashboard-widget-configure'));

    await waitFor(() => {
      const chart = within(widget).getByTestId('dashboard-widget-chart');
      expect(chart).toHaveAttribute('data-chart-source', 'live');
      expect(chart).toHaveAttribute('data-chart-status', 'ready');
      expect(chart).toHaveAttribute('data-chart-values', '12,8,5');
    });
    expect(aggMocks.aggregate).toHaveBeenCalledWith(
      'crm',
      'customer',
      expect.objectContaining({
        aggregation: [expect.objectContaining({ type: 'count' })],
        groupBy: [{ field: 'country', type: 'exact' }],
      }),
    );
  });

  it('renders the chart error state when the aggregation request fails', async () => {
    aggMocks.aggregate.mockRejectedValue(new Error('boom'));
    render(<DashboardEditorPage />);
    fireEvent.click(screen.getByTestId('dashboard-widget-add-chart'));
    const widget = screen.getByTestId('dashboard-widget');
    fireEvent.click(within(widget).getByTestId('dashboard-widget-configure'));
    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-data-source-kind'),
      { target: { value: 'aggregation' } },
    );
    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-data-source-ontology'),
      { target: { value: 'crm' } },
    );
    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-data-source-object-type'),
      { target: { value: 'customer' } },
    );
    fireEvent.click(within(widget).getByTestId('dashboard-widget-configure'));

    await waitFor(() => {
      const chart = within(widget).getByTestId('dashboard-widget-chart');
      expect(chart).toHaveAttribute('data-chart-status', 'error');
    });
  });

  it('binds the stat widget to the latest aggregation bucket value', async () => {
    aggMocks.aggregate.mockResolvedValue({
      data: [
        { group: { day: '2026-04-30' }, metrics: { sum_revenue: 100 } },
        { group: { day: '2026-05-01' }, metrics: { sum_revenue: 150 } },
        { group: { day: '2026-05-02' }, metrics: { sum_revenue: 220 } },
      ],
    });
    render(<DashboardEditorPage />);
    fireEvent.click(screen.getByTestId('dashboard-widget-add-stat'));
    const widget = screen.getByTestId('dashboard-widget');
    fireEvent.click(within(widget).getByTestId('dashboard-widget-configure'));
    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-data-source-kind'),
      { target: { value: 'aggregation' } },
    );
    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-data-source-ontology'),
      { target: { value: 'crm' } },
    );
    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-data-source-object-type'),
      { target: { value: 'order' } },
    );
    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-data-source-metric'),
      { target: { value: 'sum' } },
    );
    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-data-source-property'),
      { target: { value: 'revenue' } },
    );
    fireEvent.change(
      within(widget).getByTestId('dashboard-widget-data-source-group-by'),
      { target: { value: 'day' } },
    );
    fireEvent.click(within(widget).getByTestId('dashboard-widget-configure'));

    await waitFor(() => {
      const stat = within(widget).getByTestId('dashboard-widget-stat');
      expect(stat).toHaveAttribute('data-stat-source', 'live');
      expect(within(stat).getByText('220')).toBeInTheDocument();
      expect(
        within(stat).getByTestId('dashboard-widget-stat-sparkline'),
      ).toHaveAttribute('data-spark-values', '100,150,220');
    });
  });
});
