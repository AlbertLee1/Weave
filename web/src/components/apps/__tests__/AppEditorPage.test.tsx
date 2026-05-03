import { describe, it, expect, vi, beforeEach } from 'vitest';
import {
  render,
  screen,
  fireEvent,
  within,
  createEvent,
  waitFor,
} from '@testing-library/react';

// Mock the apps API module so the editor doesn't issue real network
// requests in tests. The default impls match degraded mode (empty list,
// 404 on Get).
const apiMocks = vi.hoisted(() => ({
  listApps: vi.fn(),
  getApp: vi.fn(),
  createApp: vi.fn(),
  updateApp: vi.fn(),
  deleteApp: vi.fn(),
  listAppVersions: vi.fn(),
}));

vi.mock('../../../api/apps', () => apiMocks);

import { AppEditorPage } from '../AppEditorPage';
import {
  COMPONENT_TYPES,
  MAX_COLUMNS,
  PALETTE_DND_MIME,
  distributeWidths,
  instancesFromLayout,
  instancesToLayout,
} from '../layout';

// jsdom doesn't fully model DataTransfer; supply a minimal stub so drag
// events round-trip a payload (same recipe as the dashboard editor
// tests).
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

function fireDragWithCoords(
  target: HTMLElement,
  type: 'dragStart' | 'dragOver' | 'drop',
  dataTransfer: ReturnType<typeof makeDataTransfer>,
  clientX = 0,
  clientY = 0,
) {
  const ev = createEvent[type](target, { dataTransfer });
  Object.defineProperty(ev, 'clientX', { value: clientX });
  Object.defineProperty(ev, 'clientY', { value: clientY });
  fireEvent(target, ev);
}

function dragPaletteToCanvas(
  paletteItem: HTMLElement,
  canvas: HTMLElement,
  paletteType: string,
) {
  const dt = makeDataTransfer();
  fireDragWithCoords(paletteItem, 'dragStart', dt);
  // After dragStart the dataTransfer carries our mime type — surface it
  // to the canvas's dragover/drop handlers.
  dt.types = [PALETTE_DND_MIME];
  // Manually populate the type since our makeDataTransfer setter doesn't
  // also update types.
  dt.setData(PALETTE_DND_MIME, paletteType);
  fireDragWithCoords(canvas, 'dragOver', dt);
  fireDragWithCoords(canvas, 'drop', dt);
}

function getCanvasInstances() {
  return screen.queryAllByTestId('app-canvas-instance');
}

beforeEach(() => {
  for (const m of Object.values(apiMocks)) m.mockReset();
  apiMocks.listApps.mockResolvedValue({ apps: [] });
  apiMocks.getApp.mockRejectedValue(new Error('not configured'));
});

describe('AppEditorPage layout helpers', () => {
  it('distributeWidths sums to exactly 12 across any count up to MAX_COLUMNS', () => {
    for (let n = 1; n <= MAX_COLUMNS; n++) {
      const widths = distributeWidths(n);
      expect(widths).toHaveLength(n);
      expect(widths.reduce((s, w) => s + w, 0)).toBe(12);
      for (const w of widths) {
        expect(w).toBeGreaterThanOrEqual(2);
        expect(w).toBeLessThanOrEqual(12);
      }
    }
    expect(distributeWidths(0)).toEqual([]);
  });

  it('instancesToLayout encodes a placeholder for an empty canvas (passes ValidateLayout)', () => {
    const layout = instancesToLayout([]);
    expect(layout.type).toBe('row');
    if (layout.type !== 'row') throw new Error('unreachable');
    expect(layout.children).toHaveLength(1);
    expect(layout.children[0].width).toBe(12);
    expect(layout.children[0].child.type).toBe('component');
  });

  it('instancesToLayout encodes 3 components as a row of 3 cols summing to 12', () => {
    const layout = instancesToLayout([
      { id: 'a', componentType: 'table', props: {} },
      { id: 'b', componentType: 'chart', props: {} },
      { id: 'c', componentType: 'text', props: { content: 'hi' } },
    ]);
    if (layout.type !== 'row') throw new Error('unreachable');
    expect(layout.children).toHaveLength(3);
    expect(layout.children.map((c) => c.width).reduce((s, w) => s + w, 0)).toBe(
      12,
    );
    expect(layout.children[2].child.type).toBe('component');
    if (layout.children[2].child.type === 'component') {
      expect(layout.children[2].child.componentType).toBe('text');
      expect(layout.children[2].child.props).toEqual({ content: 'hi' });
    }
  });

  it('instancesFromLayout flattens nested rows back to a list of components', () => {
    const layout = {
      type: 'row' as const,
      children: [
        {
          type: 'col' as const,
          width: 12,
          child: {
            type: 'row' as const,
            children: [
              {
                type: 'col' as const,
                width: 6,
                child: {
                  type: 'component' as const,
                  componentType: 'table',
                  props: { objectSet: 'ri.x' },
                },
              },
              {
                type: 'col' as const,
                width: 6,
                child: {
                  type: 'component' as const,
                  componentType: 'chart',
                },
              },
            ],
          },
        },
      ],
    };
    const insts = instancesFromLayout(layout);
    expect(insts).toHaveLength(2);
    expect(insts[0].componentType).toBe('table');
    expect(insts[0].props).toEqual({ objectSet: 'ri.x' });
    expect(insts[1].componentType).toBe('chart');
  });
});

describe('AppEditorPage palette', () => {
  it('renders all 6 palette items on first mount', () => {
    render(<AppEditorPage />);
    for (const meta of COMPONENT_TYPES) {
      expect(
        screen.getByTestId(`app-palette-item-${meta.type}`),
      ).toBeInTheDocument();
    }
    expect(screen.getByTestId('app-canvas')).toBeInTheDocument();
    expect(screen.getByTestId('app-canvas-empty')).toBeInTheDocument();
  });

  it('clicking a palette item appends the component to the canvas and selects it', () => {
    render(<AppEditorPage />);
    fireEvent.click(screen.getByTestId('app-palette-item-table'));
    const insts = getCanvasInstances();
    expect(insts).toHaveLength(1);
    expect(insts[0].getAttribute('data-component-type')).toBe('table');
    expect(insts[0].getAttribute('data-selected')).toBe('true');
    expect(screen.queryByTestId('app-canvas-empty')).not.toBeInTheDocument();
  });

  it('caps the canvas at MAX_COLUMNS components and disables further palette clicks', () => {
    render(<AppEditorPage />);
    const types: string[] = [
      'table',
      'form',
      'chart',
      'button',
      'objectCard',
      'text',
    ];
    for (const t of types) {
      fireEvent.click(screen.getByTestId(`app-palette-item-${t}`));
    }
    expect(getCanvasInstances()).toHaveLength(MAX_COLUMNS);
    expect(screen.getByTestId('app-palette-full')).toBeInTheDocument();

    // Buttons should be disabled — further clicks must not exceed the
    // grid cap.
    fireEvent.click(screen.getByTestId('app-palette-item-table'));
    expect(getCanvasInstances()).toHaveLength(MAX_COLUMNS);
  });

  it('drag-from-palette drops a new component onto the canvas via the DnD MIME', () => {
    render(<AppEditorPage />);
    const palette = screen.getByTestId('app-palette-item-chart');
    const canvas = screen.getByTestId('app-canvas');
    dragPaletteToCanvas(palette, canvas, 'chart');
    const insts = getCanvasInstances();
    expect(insts).toHaveLength(1);
    expect(insts[0].getAttribute('data-component-type')).toBe('chart');
  });

  it('flags the canvas as a drop target while a palette drag is in flight', () => {
    render(<AppEditorPage />);
    const palette = screen.getByTestId('app-palette-item-table');
    const canvas = screen.getByTestId('app-canvas');
    expect(canvas.getAttribute('data-drop-target')).toBe('false');

    const dt = makeDataTransfer();
    fireDragWithCoords(palette, 'dragStart', dt);
    dt.types = [PALETTE_DND_MIME];
    fireDragWithCoords(canvas, 'dragOver', dt);
    expect(canvas.getAttribute('data-drop-target')).toBe('true');

    fireEvent.dragLeave(canvas);
    expect(canvas.getAttribute('data-drop-target')).toBe('false');
  });
});

describe('AppEditorPage canvas instances', () => {
  it('removes a component when the × control is clicked', () => {
    render(<AppEditorPage />);
    fireEvent.click(screen.getByTestId('app-palette-item-table'));
    fireEvent.click(screen.getByTestId('app-palette-item-chart'));
    expect(getCanvasInstances()).toHaveLength(2);
    const remove = within(getCanvasInstances()[0]).getByTestId(
      'app-canvas-instance-remove',
    );
    fireEvent.click(remove);
    const remaining = getCanvasInstances();
    expect(remaining).toHaveLength(1);
    expect(remaining[0].getAttribute('data-component-type')).toBe('chart');
  });

  it('clicking an instance selects it and updates the property panel', () => {
    render(<AppEditorPage />);
    fireEvent.click(screen.getByTestId('app-palette-item-table'));
    fireEvent.click(screen.getByTestId('app-palette-item-chart'));
    const [first, second] = getCanvasInstances();
    fireEvent.click(first);
    expect(first.getAttribute('data-selected')).toBe('true');
    expect(second.getAttribute('data-selected')).toBe('false');
    expect(screen.getByTestId('app-property-panel')).toHaveAttribute(
      'data-component-type',
      'table',
    );

    fireEvent.click(second);
    expect(second.getAttribute('data-selected')).toBe('true');
    expect(screen.getByTestId('app-property-panel')).toHaveAttribute(
      'data-component-type',
      'chart',
    );
  });

  it('distributes widths so the canvas always sums to 12 grid columns', () => {
    render(<AppEditorPage />);
    fireEvent.click(screen.getByTestId('app-palette-item-table'));
    fireEvent.click(screen.getByTestId('app-palette-item-chart'));
    fireEvent.click(screen.getByTestId('app-palette-item-text'));
    const widths = getCanvasInstances().map((el) =>
      Number(el.getAttribute('data-instance-width')),
    );
    expect(widths.reduce((s, w) => s + w, 0)).toBe(12);
  });
});

describe('AppEditorPage property panel', () => {
  it('renders an empty hint when no component is selected', () => {
    render(<AppEditorPage />);
    const panel = screen.getByTestId('app-property-panel');
    expect(panel.getAttribute('data-empty')).toBe('true');
  });

  it('Table panel persists ObjectSet RID and columns into the instance preview', () => {
    render(<AppEditorPage />);
    fireEvent.click(screen.getByTestId('app-palette-item-table'));
    const objectSet = screen.getByTestId('prop-table-objectSet');
    fireEvent.change(objectSet, { target: { value: 'ri.foo' } });
    const columns = screen.getByTestId('prop-table-columns');
    fireEvent.change(columns, { target: { value: 'name, status, owner' } });
    expect(screen.getByTestId('app-preview-table')).toHaveTextContent('ri.foo');
  });

  it('Chart panel persists chartType and title', () => {
    render(<AppEditorPage />);
    fireEvent.click(screen.getByTestId('app-palette-item-chart'));
    const select = screen.getByTestId('prop-chart-type');
    fireEvent.change(select, { target: { value: 'pie' } });
    fireEvent.change(screen.getByTestId('prop-chart-title'), {
      target: { value: 'Revenue' },
    });
    const preview = screen.getByTestId('app-preview-chart');
    expect(preview.getAttribute('data-chart-type')).toBe('pie');
    expect(preview).toHaveTextContent('Revenue');
  });

  it('Text panel persists content', () => {
    render(<AppEditorPage />);
    fireEvent.click(screen.getByTestId('app-palette-item-text'));
    fireEvent.change(screen.getByTestId('prop-text-content'), {
      target: { value: 'Welcome to the dashboard' },
    });
    expect(screen.getByTestId('app-preview-text')).toHaveTextContent(
      'Welcome to the dashboard',
    );
  });
});

describe('AppEditorPage save flow', () => {
  it('POSTs a new App and surfaces the saved status on first save', async () => {
    apiMocks.createApp.mockResolvedValue({
      rid: 'ri.app.main.app.123',
      name: 'My App',
      ownerId: 'u1',
      layoutJson: { type: 'row', children: [] },
      version: 1,
      createdAt: '2026-05-03T00:00:00Z',
      updatedAt: '2026-05-03T00:00:00Z',
    });
    const onSaved = vi.fn();
    render(<AppEditorPage onSaved={onSaved} />);
    fireEvent.click(screen.getByTestId('app-palette-item-table'));
    fireEvent.change(screen.getByTestId('app-name-input'), {
      target: { value: 'My App' },
    });
    fireEvent.click(screen.getByTestId('app-save'));
    await waitFor(() => {
      expect(apiMocks.createApp).toHaveBeenCalledTimes(1);
    });
    const callArg = apiMocks.createApp.mock.calls[0][0];
    expect(callArg.name).toBe('My App');
    expect(callArg.layoutJson.type).toBe('row');
    expect(callArg.layoutJson.children).toHaveLength(1);
    await waitFor(() => {
      expect(screen.getByTestId('app-save-status').getAttribute('data-save-status')).toBe(
        'saved',
      );
    });
    expect(onSaved).toHaveBeenCalledWith('ri.app.main.app.123');
  });

  it('PUTs an existing App when rid is supplied', async () => {
    apiMocks.getApp.mockResolvedValue({
      rid: 'ri.app.main.app.42',
      name: 'Existing',
      ownerId: 'u1',
      layoutJson: {
        type: 'row',
        children: [
          {
            type: 'col',
            width: 12,
            child: { type: 'component', componentType: 'chart' },
          },
        ],
      },
      version: 3,
      createdAt: '2026-05-03T00:00:00Z',
      updatedAt: '2026-05-03T00:00:00Z',
    });
    apiMocks.updateApp.mockResolvedValue({
      rid: 'ri.app.main.app.42',
      name: 'Existing',
      ownerId: 'u1',
      layoutJson: { type: 'row', children: [] },
      version: 4,
      createdAt: '2026-05-03T00:00:00Z',
      updatedAt: '2026-05-03T00:00:00Z',
    });
    render(<AppEditorPage rid="ri.app.main.app.42" />);
    await waitFor(() => {
      expect(getCanvasInstances()).toHaveLength(1);
    });
    fireEvent.click(screen.getByTestId('app-save'));
    await waitFor(() => {
      expect(apiMocks.updateApp).toHaveBeenCalledTimes(1);
    });
    const callArg = apiMocks.updateApp.mock.calls[0][0];
    expect(callArg.rid).toBe('ri.app.main.app.42');
    expect(callArg.name).toBe('Existing');
    expect(callArg.layoutJson.type).toBe('row');
  });

  it('surfaces a save failure status when the API rejects', async () => {
    apiMocks.createApp.mockRejectedValue(new Error('boom'));
    render(<AppEditorPage />);
    fireEvent.click(screen.getByTestId('app-palette-item-table'));
    fireEvent.click(screen.getByTestId('app-save'));
    await waitFor(() => {
      expect(screen.getByTestId('app-save-status').getAttribute('data-save-status')).toBe(
        'error',
      );
    });
  });
});
