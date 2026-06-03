import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import type { Dashboard } from '../../../api/dashboards';

// Mock the dashboards API so the editor exercises the duplicate wire
// contract without real network. Mirrors the hoisted-mock pattern in
// DashboardEditorPage.test.tsx, adding duplicateDashboard.
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
  definition: { widgets: [] },
  createdAt: '2026-05-01T00:00:00Z',
  updatedAt: '2026-05-01T00:00:00Z',
};

const copy: Dashboard = {
  ...source,
  id: 'd-2',
  name: 'Sales (copy)',
};

beforeEach(() => {
  vi.clearAllMocks();
  apiMocks.getDashboard.mockResolvedValue(source);
  apiMocks.listDashboards.mockResolvedValue({ dashboards: [source] });
  apiMocks.duplicateDashboard.mockResolvedValue(copy);
});

describe('BDD: DashboardEditorPage duplicate (Foundry "Duplicate")', () => {
  it('Given a saved dashboard, When Duplicate is clicked, Then it POSTs duplicate and loads the copy', async () => {
    const onSaved = vi.fn();
    render(<DashboardEditorPage id="d-1" onSaved={onSaved} />);

    const btn = await screen.findByTestId('dashboard-duplicate');
    fireEvent.click(btn);

    await waitFor(() =>
      expect(apiMocks.duplicateDashboard).toHaveBeenCalledWith('d-1'),
    );
    // The copy is loaded by navigating to its id (same path as Save-new).
    await waitFor(() => expect(onSaved).toHaveBeenCalledWith('d-2'));
  });

  it('Given an unsaved (new) dashboard, Then no Duplicate button is shown', async () => {
    apiMocks.getDashboard.mockRejectedValue(new Error('not configured'));
    render(<DashboardEditorPage onSaved={vi.fn()} />);

    await screen.findByTestId('dashboard-save');
    expect(screen.queryByTestId('dashboard-duplicate')).toBeNull();
  });
});
