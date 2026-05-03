import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';

// US-398 — App version rollback UI. The Versions drawer is gated on a
// saved rid (no drawer for unsaved drafts), lists every history row
// newest-first, marks the live version with a badge + disabled button,
// and triggers POST /api/v2/apps/{rid}/versions/{n}/rollback when the
// rollback button fires. After a successful rollback the canvas swaps
// to the restored layout and the drawer reloads its list.

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

const liveLayout = {
  type: 'row',
  children: [
    {
      type: 'col',
      width: 12,
      child: { type: 'component', componentType: 'chart' },
    },
  ],
};

const v1Layout = {
  type: 'row',
  children: [
    {
      type: 'col',
      width: 12,
      child: {
        type: 'component',
        componentType: 'text',
        props: { content: 'first' },
      },
    },
  ],
};

beforeEach(() => {
  for (const m of Object.values(apiMocks)) m.mockReset();
  apiMocks.listApps.mockResolvedValue({ apps: [] });
  apiMocks.getApp.mockResolvedValue({
    rid: 'ri.app.main.app.42',
    name: 'Console',
    ownerId: 'u1',
    layoutJson: liveLayout,
    version: 2,
    createdAt: '2026-05-03T00:00:00Z',
    updatedAt: '2026-05-04T00:00:00Z',
  });
  apiMocks.listAppVersions.mockResolvedValue({
    versions: [
      {
        appRid: 'ri.app.main.app.42',
        version: 2,
        name: 'Console',
        layoutJson: liveLayout,
        createdAt: '2026-05-04T00:00:00Z',
        createdBy: 'user:alice',
      },
      {
        appRid: 'ri.app.main.app.42',
        version: 1,
        name: 'Console',
        layoutJson: v1Layout,
        createdAt: '2026-05-03T00:00:00Z',
        createdBy: 'user:alice',
      },
    ],
  });
});

describe('US-398 versions toggle gating', () => {
  it('does not render the toggle until an rid is saved', () => {
    render(<AppEditorPage />);
    expect(screen.queryByTestId('app-versions-toggle')).toBeNull();
  });

  it('renders the toggle when rid is supplied (existing App)', async () => {
    render(<AppEditorPage rid="ri.app.main.app.42" />);
    await waitFor(() => {
      expect(screen.getByTestId('app-versions-toggle')).toBeInTheDocument();
    });
  });
});

describe('US-398 versions drawer renders history newest-first', () => {
  it('clicking the toggle fetches versions and lists them with the live row marked', async () => {
    render(<AppEditorPage rid="ri.app.main.app.42" />);
    await waitFor(() => {
      expect(screen.getByTestId('app-versions-toggle')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTestId('app-versions-toggle'));

    await waitFor(() => {
      expect(apiMocks.listAppVersions).toHaveBeenCalledWith(
        'ri.app.main.app.42',
      );
    });
    await waitFor(() => {
      expect(screen.getByTestId('app-versions-list')).toBeInTheDocument();
    });
    const rows = screen.getAllByTestId(/^app-versions-row-/);
    expect(rows).toHaveLength(2);
    // Newest-first: first row is v2, second is v1.
    expect(rows[0].getAttribute('data-version')).toBe('2');
    expect(rows[1].getAttribute('data-version')).toBe('1');
    // Live badge sits on the v2 row only.
    expect(screen.getByTestId('app-versions-live-badge-2')).toBeInTheDocument();
    expect(screen.queryByTestId('app-versions-live-badge-1')).toBeNull();
    // The live-row's rollback button is disabled (rolling back to live
    // is a no-op from the user's perspective). The v1 button is
    // enabled.
    expect(
      (screen.getByTestId('app-versions-rollback-2') as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    expect(
      (screen.getByTestId('app-versions-rollback-1') as HTMLButtonElement)
        .disabled,
    ).toBe(false);
  });

  it('Hide Versions closes the drawer', async () => {
    render(<AppEditorPage rid="ri.app.main.app.42" />);
    await waitFor(() => {
      expect(screen.getByTestId('app-versions-toggle')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTestId('app-versions-toggle'));
    await waitFor(() => {
      expect(screen.getByTestId('app-versions-panel')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTestId('app-versions-toggle'));
    expect(screen.queryByTestId('app-versions-panel')).toBeNull();
  });

  it('a failed listAppVersions surfaces an error row', async () => {
    apiMocks.listAppVersions.mockRejectedValueOnce(new Error('boom'));
    render(<AppEditorPage rid="ri.app.main.app.42" />);
    await waitFor(() => {
      expect(screen.getByTestId('app-versions-toggle')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTestId('app-versions-toggle'));
    await waitFor(() => {
      expect(screen.getByTestId('app-versions-error')).toBeInTheDocument();
    });
    expect(screen.getByTestId('app-versions-error').textContent).toMatch(
      /boom/i,
    );
  });
});

describe('US-398 one-click rollback', () => {
  it('clicking Rollback issues POST and swaps editor state to the rolled-back layout', async () => {
    apiMocks.rollbackApp.mockResolvedValue({
      rid: 'ri.app.main.app.42',
      name: 'Console',
      ownerId: 'u1',
      layoutJson: v1Layout,
      version: 3,
      createdAt: '2026-05-03T00:00:00Z',
      updatedAt: '2026-05-04T01:00:00Z',
    });
    // Second listAppVersions call after rollback returns v3 + v2 + v1.
    apiMocks.listAppVersions
      .mockResolvedValueOnce({
        versions: [
          {
            appRid: 'ri.app.main.app.42',
            version: 2,
            name: 'Console',
            layoutJson: liveLayout,
            createdAt: '2026-05-04T00:00:00Z',
            createdBy: 'user:alice',
          },
          {
            appRid: 'ri.app.main.app.42',
            version: 1,
            name: 'Console',
            layoutJson: v1Layout,
            createdAt: '2026-05-03T00:00:00Z',
            createdBy: 'user:alice',
          },
        ],
      })
      .mockResolvedValueOnce({
        versions: [
          {
            appRid: 'ri.app.main.app.42',
            version: 3,
            name: 'Console',
            layoutJson: v1Layout,
            createdAt: '2026-05-04T01:00:00Z',
            createdBy: 'user:alice',
          },
          {
            appRid: 'ri.app.main.app.42',
            version: 2,
            name: 'Console',
            layoutJson: liveLayout,
            createdAt: '2026-05-04T00:00:00Z',
            createdBy: 'user:alice',
          },
          {
            appRid: 'ri.app.main.app.42',
            version: 1,
            name: 'Console',
            layoutJson: v1Layout,
            createdAt: '2026-05-03T00:00:00Z',
            createdBy: 'user:alice',
          },
        ],
      });

    render(<AppEditorPage rid="ri.app.main.app.42" />);
    await waitFor(() => {
      expect(screen.getByTestId('app-versions-toggle')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTestId('app-versions-toggle'));
    await waitFor(() => {
      expect(screen.getByTestId('app-versions-rollback-1')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('app-versions-rollback-1'));

    await waitFor(() => {
      expect(apiMocks.rollbackApp).toHaveBeenCalledWith(
        'ri.app.main.app.42',
        1,
      );
    });

    // Canvas should now reflect v1Layout: the only rendered instance is
    // a "text" component (chart had been the live one). Reading
    // data-component-type off the canvas instances is the cleanest
    // assertion.
    await waitFor(() => {
      const insts = screen.queryAllByTestId('app-canvas-instance');
      expect(insts).toHaveLength(1);
      expect(insts[0].getAttribute('data-component-type')).toBe('text');
    });

    // Drawer reloaded after rollback (v3 row now exists with live
    // badge).
    await waitFor(() => {
      expect(
        screen.getByTestId('app-versions-live-badge-3'),
      ).toBeInTheDocument();
    });
  });

  it('a failed rollback surfaces an error and leaves editor state untouched', async () => {
    apiMocks.rollbackApp.mockRejectedValue(new Error('rollback failed'));
    render(<AppEditorPage rid="ri.app.main.app.42" />);
    await waitFor(() => {
      expect(screen.getByTestId('app-versions-toggle')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTestId('app-versions-toggle'));
    await waitFor(() => {
      expect(screen.getByTestId('app-versions-rollback-1')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTestId('app-versions-rollback-1'));
    await waitFor(() => {
      expect(
        screen.getByTestId('app-versions-rollback-error'),
      ).toBeInTheDocument();
    });
    // Canvas still shows the live (chart) layout.
    const insts = screen.queryAllByTestId('app-canvas-instance');
    expect(insts).toHaveLength(1);
    expect(insts[0].getAttribute('data-component-type')).toBe('chart');
  });
});
