import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { Dashboard } from '../../../api/dashboards';

// a11y BDD: the inline "Confirm delete dashboard" popover is a self-drawn
// role="dialog" (NOT the shared common/Modal). This suite pins its keyboard /
// focus contract, mirroring the focus management shipped for
// VertexShareLinkPanel (#229): focus moves into the dialog on open, Tab/Shift
// +Tab cycle within (focus trap), Escape closes via the existing cancel
// callback, and focus returns to the Delete trigger on close.
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

async function openDeleteDialog() {
  const user = userEvent.setup();
  render(<DashboardEditorPage id="d-1" onSaved={vi.fn()} />);

  await waitFor(() =>
    expect(
      screen
        .getByTestId('dashboard-editor-page')
        .getAttribute('data-dashboard-id'),
    ).toBe('d-1'),
  );

  const trigger = screen.getByTestId('dashboard-delete') as HTMLButtonElement;
  // Focus the trigger like a keyboard user would before activating it, so the
  // focus-restore-on-close behaviour has a deterministic element to return to.
  trigger.focus();
  await user.click(trigger);

  const dialog = await screen.findByTestId('dashboard-delete-confirm-dialog');
  return { user, trigger, dialog };
}

describe('BDD: DashboardEditorPage delete dialog focus management (a11y)', () => {
  it('Given the delete dialog is opened, Then it is marked aria-modal and focus moves inside it', async () => {
    const { dialog } = await openDeleteDialog();

    // The self-drawn dialog must announce itself as modal to assistive tech.
    expect(dialog.getAttribute('aria-modal')).toBe('true');

    // Focus must land on the first focusable control inside the dialog
    // (Confirm Delete), not stay on the page behind it.
    await waitFor(() =>
      expect(dialog.contains(document.activeElement)).toBe(true),
    );
  });

  it('Given the delete dialog is open, When Tab is pressed past the last control, Then focus cycles back inside (no escape)', async () => {
    const { user, dialog } = await openDeleteDialog();

    await waitFor(() =>
      expect(dialog.contains(document.activeElement)).toBe(true),
    );

    // Tab forward several times — more than the count of focusable controls —
    // and assert focus never leaves the dialog (focus trap).
    for (let i = 0; i < 4; i += 1) {
      await user.tab();
      expect(dialog.contains(document.activeElement)).toBe(true);
    }

    // Shift+Tab backwards likewise stays trapped within the dialog.
    for (let i = 0; i < 4; i += 1) {
      await user.tab({ shift: true });
      expect(dialog.contains(document.activeElement)).toBe(true);
    }
  });

  it('Given the delete dialog is open, When Escape is pressed, Then it closes without deleting and focus returns to the Delete trigger', async () => {
    const { user } = await openDeleteDialog();

    await user.keyboard('{Escape}');

    // Escape routes through the existing cancel callback — the dialog closes
    // and nothing is deleted.
    await waitFor(() =>
      expect(
        screen.queryByTestId('dashboard-delete-confirm-dialog'),
      ).toBeNull(),
    );
    expect(apiMocks.deleteDashboard).not.toHaveBeenCalled();

    // Focus is restored to the Delete control that opened the dialog. The
    // Delete button swaps itself out for the dialog while open, so on close it
    // is re-rendered as a fresh node — assert by querying the live button
    // rather than holding the pre-open reference.
    await waitFor(() =>
      expect(document.activeElement).toBe(
        screen.getByTestId('dashboard-delete'),
      ),
    );
  });
});
