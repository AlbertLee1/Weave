import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';

// US-399: built-in App template library. The picker is auto-shown in
// new-App mode (no rid prop) and disappears once an author either picks
// a template or hits "Start blank". Pre-Save authors can re-open it via
// the header "Templates" toggle. Post-Save the picker is gone for good
// (templates are a fresh-start affordance, not an undo).

const apiMocks = vi.hoisted(() => ({
  listApps: vi.fn(),
  getApp: vi.fn(),
  createApp: vi.fn(),
  updateApp: vi.fn(),
  deleteApp: vi.fn(),
  listAppVersions: vi.fn(),
  rollbackApp: vi.fn(),
}));

vi.mock('../../../api/apps', () => apiMocks);

import { AppEditorPage } from '../AppEditorPage';
import { APP_TEMPLATES, findTemplate } from '../templates';
import { decodeLayout, instancesToLayout } from '../layout';

beforeEach(() => {
  for (const m of Object.values(apiMocks)) m.mockReset();
  apiMocks.listApps.mockResolvedValue({ apps: [] });
  apiMocks.getApp.mockRejectedValue(new Error('not configured'));
});

describe('US-399 templates module', () => {
  it('exposes exactly three built-in templates (CRM / Approval / Object Browser)', () => {
    expect(APP_TEMPLATES).toHaveLength(3);
    const ids = APP_TEMPLATES.map((t) => t.id);
    expect(ids).toEqual(['crm-dashboard', 'approval-console', 'object-browser']);
  });

  it('every template has a non-empty name, description and default App name', () => {
    for (const tpl of APP_TEMPLATES) {
      expect(tpl.name.length).toBeGreaterThan(0);
      expect(tpl.description.length).toBeGreaterThan(0);
      expect(tpl.defaultAppName.length).toBeGreaterThan(0);
    }
  });

  it('every template carries a row-rooted layoutJson with at least one component', () => {
    for (const tpl of APP_TEMPLATES) {
      expect(tpl.layoutJson.type).toBe('row');
      const decoded = decodeLayout(tpl.layoutJson);
      expect(decoded.instances.length).toBeGreaterThan(0);
    }
  });

  it("every template's layoutJson columns sum to ≤ 12 (canonical wire constraint)", () => {
    for (const tpl of APP_TEMPLATES) {
      if (tpl.layoutJson.type !== 'row') throw new Error('unreachable');
      const total = tpl.layoutJson.children.reduce(
        (sum, col) => sum + col.width,
        0,
      );
      expect(total).toBeLessThanOrEqual(12);
      expect(total).toBeGreaterThan(0);
    }
  });

  it('round-trips template layouts through decode → encode without losing component identity', () => {
    for (const tpl of APP_TEMPLATES) {
      const decoded = decodeLayout(tpl.layoutJson);
      const encoded = instancesToLayout(decoded.instances, decoded.variables);
      expect(encoded.type).toBe('row');
      const reDecoded = decodeLayout(encoded);
      expect(reDecoded.instances.map((i) => i.componentType)).toEqual(
        decoded.instances.map((i) => i.componentType),
      );
      expect(reDecoded.variables).toEqual(decoded.variables);
    }
  });

  it('CRM Dashboard ships a chart, an Account table and a button', () => {
    const tpl = findTemplate('crm-dashboard');
    expect(tpl).toBeDefined();
    if (!tpl) return;
    const types = decodeLayout(tpl.layoutJson).instances.map(
      (i) => i.componentType,
    );
    expect(types).toContain('chart');
    expect(types).toContain('table');
    expect(types).toContain('button');
  });

  it('Approval Console pairs a status-filtered queue table with an inline form', () => {
    const tpl = findTemplate('approval-console');
    expect(tpl).toBeDefined();
    if (!tpl) return;
    const decoded = decodeLayout(tpl.layoutJson);
    const types = decoded.instances.map((i) => i.componentType);
    expect(types).toContain('table');
    expect(types).toContain('form');
    const table = decoded.instances.find((i) => i.componentType === 'table');
    expect(table?.props.filterField).toBe('status');
    expect(table?.props.filterValue).toBe('pending');
  });

  it('Object Browser drives card + table from the same {{objectType}} variable', () => {
    const tpl = findTemplate('object-browser');
    expect(tpl).toBeDefined();
    if (!tpl) return;
    const decoded = decodeLayout(tpl.layoutJson);
    const card = decoded.instances.find((i) => i.componentType === 'objectCard');
    const table = decoded.instances.find((i) => i.componentType === 'table');
    expect(card?.props.objectType).toBe('{{objectType}}');
    expect(table?.props.objectSet).toBe('{{objectType}}');
    expect(decoded.variables.map((v) => v.name)).toContain('objectType');
  });

  it('findTemplate returns undefined for unknown ids', () => {
    expect(findTemplate('crm-dashboard')).toBeDefined();
    expect(findTemplate('does-not-exist')).toBeUndefined();
    expect(findTemplate('')).toBeUndefined();
  });
});

describe('US-399 template picker UI in new-App flow', () => {
  it('auto-shows the template picker on first mount when no rid is supplied', async () => {
    render(<AppEditorPage />);
    await waitFor(() => {
      expect(screen.getByTestId('app-template-picker')).toBeInTheDocument();
    });
    expect(screen.getByTestId('app-template-picker-list')).toBeInTheDocument();
    expect(
      screen.getByTestId('app-template-picker').getAttribute('data-template-count'),
    ).toBe('3');
  });

  it('renders one card per built-in template with name + description visible', async () => {
    render(<AppEditorPage />);
    await waitFor(() => {
      expect(screen.getByTestId('app-template-picker')).toBeInTheDocument();
    });
    for (const tpl of APP_TEMPLATES) {
      const card = screen.getByTestId(`app-template-card-${tpl.id}`);
      expect(card).toHaveTextContent(tpl.name);
      expect(card).toHaveTextContent(tpl.description);
      expect(
        screen.getByTestId(`app-template-use-${tpl.id}`),
      ).toBeInTheDocument();
    }
  });

  it('"Start blank" dismisses the picker without populating instances', async () => {
    render(<AppEditorPage />);
    await waitFor(() => {
      expect(screen.getByTestId('app-template-picker')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTestId('app-template-picker-blank'));
    expect(screen.queryByTestId('app-template-picker')).not.toBeInTheDocument();
    // Canvas remains in the empty state — start-blank is a no-op against
    // the canvas itself.
    expect(screen.getByTestId('app-canvas-empty')).toBeInTheDocument();
  });

  it('selecting a template populates the canvas, name, and any declared variables', async () => {
    render(<AppEditorPage />);
    await waitFor(() => {
      expect(screen.getByTestId('app-template-picker')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTestId('app-template-use-crm-dashboard'));

    // Picker hides on selection.
    expect(screen.queryByTestId('app-template-picker')).not.toBeInTheDocument();

    // Canvas now carries the template's components.
    const insts = screen.getAllByTestId('app-canvas-instance');
    expect(insts.length).toBeGreaterThanOrEqual(3);
    const componentTypes = insts.map((el) =>
      el.getAttribute('data-component-type'),
    );
    expect(componentTypes).toEqual(
      expect.arrayContaining(['chart', 'table', 'button']),
    );

    // App name swaps to the template's default.
    const nameInput = screen.getByTestId('app-name-input') as HTMLInputElement;
    expect(nameInput.value).toBe('CRM Dashboard');

    // Variables panel reflects the template's declared variables.
    expect(
      screen.getByTestId('app-variables-panel').getAttribute(
        'data-variable-count',
      ),
    ).toBe('1');
  });

  it('Approval Console template populates form + table and renames the App', async () => {
    render(<AppEditorPage />);
    await waitFor(() => {
      expect(screen.getByTestId('app-template-picker')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTestId('app-template-use-approval-console'));
    const types = screen
      .getAllByTestId('app-canvas-instance')
      .map((el) => el.getAttribute('data-component-type'));
    expect(types).toEqual(expect.arrayContaining(['table', 'form']));
    const nameInput = screen.getByTestId('app-name-input') as HTMLInputElement;
    expect(nameInput.value).toBe('Approval Console');
  });

  it('Object Browser template populates objectCard + table + text', async () => {
    render(<AppEditorPage />);
    await waitFor(() => {
      expect(screen.getByTestId('app-template-picker')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTestId('app-template-use-object-browser'));
    const types = screen
      .getAllByTestId('app-canvas-instance')
      .map((el) => el.getAttribute('data-component-type'));
    expect(types).toEqual(
      expect.arrayContaining(['objectCard', 'table', 'text']),
    );
    const nameInput = screen.getByTestId('app-name-input') as HTMLInputElement;
    expect(nameInput.value).toBe('Object Browser');
  });

  it('header Templates toggle re-opens the picker after Start blank', async () => {
    render(<AppEditorPage />);
    await waitFor(() => {
      expect(screen.getByTestId('app-template-picker')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTestId('app-template-picker-blank'));
    expect(screen.queryByTestId('app-template-picker')).not.toBeInTheDocument();
    expect(screen.getByTestId('app-templates-toggle')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('app-templates-toggle'));
    expect(screen.getByTestId('app-template-picker')).toBeInTheDocument();
    // Toggle button hides itself while the picker is visible to keep the
    // header chrome compact.
    expect(screen.queryByTestId('app-templates-toggle')).not.toBeInTheDocument();
  });

  it('header Templates toggle does NOT appear when an existing rid is loaded', async () => {
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
      createdAt: '2026-05-04T00:00:00Z',
      updatedAt: '2026-05-04T00:00:00Z',
    });
    render(<AppEditorPage rid="ri.app.main.app.42" />);
    await waitFor(() => {
      expect(screen.getAllByTestId('app-canvas-instance').length).toBe(1);
    });
    expect(screen.queryByTestId('app-template-picker')).not.toBeInTheDocument();
    expect(screen.queryByTestId('app-templates-toggle')).not.toBeInTheDocument();
  });

  it('template picker is hidden in preview mode even before Save', async () => {
    render(<AppEditorPage />);
    await waitFor(() => {
      expect(screen.getByTestId('app-template-picker')).toBeInTheDocument();
    });
    // Drop a component so Preview has something to render then enter
    // preview — picker should disappear because preview is a runtime
    // affordance, not an editing affordance.
    fireEvent.click(screen.getByTestId('app-palette-item-text'));
    fireEvent.click(screen.getByTestId('app-mode-toggle'));
    expect(screen.queryByTestId('app-template-picker')).not.toBeInTheDocument();
  });
});
