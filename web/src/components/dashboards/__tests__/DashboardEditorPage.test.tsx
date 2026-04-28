import { describe, it, expect } from 'vitest';
import {
  render,
  screen,
  fireEvent,
  within,
  createEvent,
} from '@testing-library/react';
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
