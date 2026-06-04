import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import type { Dashboard } from '../../../api/dashboards';

// Mock the dashboards API so the editor exercises the delete wire contract
// without real network. Mirrors the hoisted-mock pattern in
// DashboardEditorPage.duplicate.bdd.test.tsx, exercising deleteDashboard.
const apiMocks = vi.hoisted(() => ({
  listDashboards: vi.fn(),
  getDashboard: vi.fn(),
  createDashboard: vi.fn(),
  updateDashboard: vi.fn(),
  deleteDashboard: vi.fn(),
  duplicateDashboard: vi.fn(),
}));
vi.mock('../../../api/dashboards', () => apiMocks);
vi.mock('../../../api/aggregation', () => ({ aggregate: vi.fn() }));
vi.mock('../widgets/MapViewLeaflet', () => ({ default: () => null }));

import { DashboardEditorPage } from '../DashboardEditorPage';

const source: Dashboard = {
  id: 'd-1',
  name: 'Sales',
  createdBy: 'user:me',
  isPublic: false,
  definition: {
    widgets: [
      {
        id: 'w-1',
        type: 'text',
        title: 'Hello',
        content: 'hi',
        x: 0,
        y: 0,
        w: 4,
        h: 2,
      },
    ],
  },
  createdAt: '2026-05-01T00:00:00Z',
  updatedAt: '2026-05-01T00:00:00Z',
};

beforeEach(() => {
  vi.clearAllMocks();
  apiMocks.getDashboard.mockResolvedValue(source);
  apiMocks.listDashboards.mockResolvedValue({ dashboards: [source] });
  apiMocks.deleteDashboard.mockResolvedValue(undefined);
});

describe('BDD: DashboardEditorPage delete (Foundry "Delete")', () => {
  it('Given a saved dashboard, When Delete is clicked and confirmed, Then it DELETEs and resets the editor', async () => {
    const onSaved = vi.fn();
    render(<DashboardEditorPage id="d-1" onSaved={onSaved} />);

    // Wait until the saved dashboard is loaded (its id surfaces on the page).
    await waitFor(() =>
      expect(
        screen
          .getByTestId('dashboard-editor-page')
          .getAttribute('data-dashboard-id'),
      ).toBe('d-1'),
    );
    // The loaded widget is rendered before delete.
    expect(screen.getByTestId('dashboard-widget')).toBeTruthy();

    // Click Delete — this opens a confirmation prompt; it does NOT fire the
    // request yet.
    fireEvent.click(screen.getByTestId('dashboard-delete'));
    expect(apiMocks.deleteDashboard).not.toHaveBeenCalled();

    // A confirmation dialog appears with an explicit confirm action.
    const confirm = await screen.findByTestId('dashboard-delete-confirm');
    fireEvent.click(confirm);

    await waitFor(() =>
      expect(apiMocks.deleteDashboard).toHaveBeenCalledWith('d-1'),
    );

    // Editor state resets: savedId cleared, name back to default, widgets gone.
    await waitFor(() =>
      expect(
        screen
          .getByTestId('dashboard-editor-page')
          .getAttribute('data-dashboard-id'),
      ).toBe(''),
    );
    expect(
      (screen.getByTestId('dashboard-name-input') as HTMLInputElement).value,
    ).toBe('Untitled Dashboard');
    expect(screen.queryByTestId('dashboard-widget')).toBeNull();

    // The navigate-away / onSaved hook is invoked so the route wrapper can
    // leave the now-deleted dashboard's URL.
    await waitFor(() => expect(onSaved).toHaveBeenCalledWith(''));

    // Delete is no longer available on the now-unsaved editor.
    expect(screen.queryByTestId('dashboard-delete')).toBeNull();
  });

  it('Given a saved dashboard, When Delete is clicked then Cancelled, Then nothing is deleted', async () => {
    render(<DashboardEditorPage id="d-1" onSaved={vi.fn()} />);

    await waitFor(() =>
      expect(
        screen
          .getByTestId('dashboard-editor-page')
          .getAttribute('data-dashboard-id'),
      ).toBe('d-1'),
    );

    fireEvent.click(screen.getByTestId('dashboard-delete'));
    const cancel = await screen.findByTestId('dashboard-delete-cancel');
    fireEvent.click(cancel);

    expect(apiMocks.deleteDashboard).not.toHaveBeenCalled();
    // Editor still holds the dashboard.
    expect(
      screen
        .getByTestId('dashboard-editor-page')
        .getAttribute('data-dashboard-id'),
    ).toBe('d-1');
    expect(screen.queryByTestId('dashboard-delete-confirm')).toBeNull();
  });

  it('Given an unsaved (new) dashboard, Then no Delete button is shown', async () => {
    apiMocks.getDashboard.mockRejectedValue(new Error('not configured'));
    render(<DashboardEditorPage onSaved={vi.fn()} />);

    await screen.findByTestId('dashboard-save');
    expect(screen.queryByTestId('dashboard-delete')).toBeNull();
  });
});
