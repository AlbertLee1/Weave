import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';

// US-397: App 移动响应式 — Tailwind sm/md/lg breakpoint switching plus a
// preview-mode viewport toggle (Desktop / Mobile) that wraps the runtime
// canvas in a 375px frame and forces single-column rendering even on a
// desktop browser.

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

beforeEach(() => {
  for (const m of Object.values(apiMocks)) m.mockReset();
  apiMocks.listApps.mockResolvedValue({ apps: [] });
  apiMocks.getApp.mockRejectedValue(new Error('not configured'));
});

function enterPreview() {
  fireEvent.click(screen.getByTestId('app-mode-toggle'));
}

describe('US-397 edit-mode chrome is responsive (Tailwind sm/md/lg)', () => {
  it('outer grid uses grid-cols-1 with lg:grid-cols-12 so it stacks on small viewports', () => {
    const { container } = render(<AppEditorPage />);
    // The edit-mode wrapper is the only direct grid sibling of the
    // header — find it via its class signature so we don't need a
    // dedicated testid for the layout primitive itself.
    const wrapper = container.querySelector('.grid.grid-cols-1.lg\\:grid-cols-12');
    expect(wrapper).not.toBeNull();
  });

  it('palette and property-panel use lg:col-span-* so they only sidebar on lg+', () => {
    render(<AppEditorPage />);
    const palette = screen.getByTestId('app-palette');
    expect(palette.className).toMatch(/lg:col-span-3/);
    const props = screen.getByTestId('app-property-panel');
    expect(props.className).toMatch(/lg:col-span-2/);
    // Neither side rail should declare a non-responsive col-span (which
    // would force the side-by-side layout on phones).
    expect(palette.className).not.toMatch(/(?:^| )col-span-3(?: |$)/);
    expect(props.className).not.toMatch(/(?:^| )col-span-2(?: |$)/);
  });
});

describe('US-397 preview viewport toggle', () => {
  it('Preview mode renders Desktop and Mobile viewport buttons', () => {
    render(<AppEditorPage />);
    enterPreview();
    expect(screen.getByTestId('app-runtime-viewport-toolbar')).toBeInTheDocument();
    expect(screen.getByTestId('app-viewport-desktop')).toBeInTheDocument();
    expect(screen.getByTestId('app-viewport-mobile')).toBeInTheDocument();
  });

  it('Desktop is the default and the runtime view advertises viewport=desktop', () => {
    render(<AppEditorPage />);
    enterPreview();
    const view = screen.getByTestId('app-runtime-view');
    expect(view.getAttribute('data-viewport')).toBe('desktop');
    expect(
      screen.getByTestId('app-viewport-desktop').getAttribute('data-active'),
    ).toBe('true');
    expect(
      screen.getByTestId('app-viewport-mobile').getAttribute('data-active'),
    ).toBe('false');
    // Default canvas keeps the 12-track grid for sm+
    expect(
      screen.getByTestId('app-runtime-canvas').getAttribute('data-cols'),
    ).toBe('12');
    // No mobile frame present
    expect(
      screen.getByTestId('app-runtime-frame').getAttribute('data-frame-mode'),
    ).toBe('desktop');
  });

  it('Clicking Mobile flips viewport, frame mode, and collapses the canvas to 1 col', () => {
    render(<AppEditorPage />);
    enterPreview();
    fireEvent.click(screen.getByTestId('app-viewport-mobile'));
    const view = screen.getByTestId('app-runtime-view');
    expect(view.getAttribute('data-viewport')).toBe('mobile');
    expect(
      screen.getByTestId('app-viewport-mobile').getAttribute('data-active'),
    ).toBe('true');
    expect(
      screen.getByTestId('app-viewport-desktop').getAttribute('data-active'),
    ).toBe('false');
    expect(
      screen.getByTestId('app-runtime-canvas').getAttribute('data-cols'),
    ).toBe('1');
    const frame = screen.getByTestId('app-runtime-frame');
    expect(frame.getAttribute('data-frame-mode')).toBe('mobile');
    expect(frame.className).toMatch(/max-w-\[375px\]/);
  });

  it('Clicking Desktop after Mobile restores the 12-track layout', () => {
    render(<AppEditorPage />);
    enterPreview();
    fireEvent.click(screen.getByTestId('app-viewport-mobile'));
    fireEvent.click(screen.getByTestId('app-viewport-desktop'));
    expect(
      screen.getByTestId('app-runtime-view').getAttribute('data-viewport'),
    ).toBe('desktop');
    expect(
      screen.getByTestId('app-runtime-canvas').getAttribute('data-cols'),
    ).toBe('12');
    expect(
      screen.getByTestId('app-runtime-frame').getAttribute('data-frame-mode'),
    ).toBe('desktop');
  });

  it('In Mobile mode runtime instances flag data-mobile-frame=true and drop their inline col-span style', async () => {
    render(<AppEditorPage />);
    // Add two text components so we exercise the per-instance span path.
    fireEvent.click(screen.getByTestId('app-palette-item-text'));
    fireEvent.click(screen.getByTestId('app-palette-item-text'));
    enterPreview();
    fireEvent.click(screen.getByTestId('app-viewport-mobile'));
    const insts = await screen.findAllByTestId('app-runtime-instance');
    expect(insts).toHaveLength(2);
    for (const node of insts) {
      expect(node.getAttribute('data-mobile-frame')).toBe('true');
      // No inline gridColumn span — the parent grid is grid-cols-1.
      expect((node as HTMLElement).style.gridColumn).toBe('');
    }
  });

  it('In Desktop mode runtime instances keep their authored col-span via inline style', async () => {
    render(<AppEditorPage />);
    fireEvent.click(screen.getByTestId('app-palette-item-text'));
    fireEvent.click(screen.getByTestId('app-palette-item-text'));
    enterPreview();
    const insts = await screen.findAllByTestId('app-runtime-instance');
    expect(insts).toHaveLength(2);
    for (const node of insts) {
      expect(node.getAttribute('data-mobile-frame')).toBe('false');
      // Two text components → distributeWidths(2) === [6, 6]
      expect((node as HTMLElement).style.gridColumn).toBe('span 6');
    }
  });

  it('Viewport selection survives toggling out of preview and back', async () => {
    render(<AppEditorPage />);
    enterPreview();
    fireEvent.click(screen.getByTestId('app-viewport-mobile'));
    // Back to Edit
    fireEvent.click(screen.getByTestId('app-mode-toggle'));
    expect(screen.queryByTestId('app-runtime-view')).not.toBeInTheDocument();
    // Re-enter Preview — viewport state persists.
    fireEvent.click(screen.getByTestId('app-mode-toggle'));
    await waitFor(() => {
      expect(
        screen.getByTestId('app-runtime-view').getAttribute('data-viewport'),
      ).toBe('mobile');
    });
  });
});

describe('US-397 runtime canvas grid classes', () => {
  it('Desktop canvas declares responsive grid-cols-1 sm:grid-cols-12', () => {
    render(<AppEditorPage />);
    enterPreview();
    const canvas = screen.getByTestId('app-runtime-canvas');
    expect(canvas.className).toMatch(/grid-cols-1/);
    expect(canvas.className).toMatch(/sm:grid-cols-12/);
  });

  it('Mobile canvas declares grid-cols-1 only — no sm:grid-cols-12 escape hatch', () => {
    render(<AppEditorPage />);
    enterPreview();
    fireEvent.click(screen.getByTestId('app-viewport-mobile'));
    const canvas = screen.getByTestId('app-runtime-canvas');
    expect(canvas.className).toMatch(/grid-cols-1/);
    expect(canvas.className).not.toMatch(/sm:grid-cols-12/);
  });
});
