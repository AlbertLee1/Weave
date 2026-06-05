// a11y BDD — focus management for the self-drawn DrawerShell used by
// SagaJobsPage (web/src/components/sagaJobs/SagaJobsPage.tsx).
//
// DrawerShell is the reusable role="dialog" aria-modal drawer wrapper behind
// the Saga-detail drawer and the DLQ drawer. It is NOT the shared common/Modal
// (which already traps + restores focus), so focus management lives in the
// shell itself. These scenarios pin the keyboard-user contract from the
// outside (what a keyboard / screen-reader user can observe), mirroring the
// VertexShareLinkPanel focus contract (#229):
//
//   Given an open drawer
//     Then it is announced as a modal dialog (aria-modal="true")
//     And initial focus lands inside the drawer (not on the page behind it)
//   Given focus on the last focusable element, When Tab is pressed
//     Then focus wraps to the first (stays trapped — never escapes to background)
//   Given focus on the first focusable element, When Shift+Tab is pressed
//     Then focus wraps to the last (stays trapped)
//   Given an open drawer, When Escape is pressed
//     Then the drawer closes (onClose fires — the drawer unmounts)
//   Given a drawer opened from a trigger button, When it closes
//     Then focus returns to the trigger element
//
// We drive the DLQ drawer because it is reachable from a stable trigger
// (saga-jobs-open-dlq-btn) without needing a saga row, and its list is stubbed
// empty so the shell renders deterministically.

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { SagaJobsPage } from '../SagaJobsPage';
import * as sagaJobsApi from '../../../api/sagaJobs';
import * as sagaDLQApi from '../../../api/sagaDLQ';

function renderPage(initial = '/actions/default/jobs') {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initial]}>
        <Routes>
          <Route path="/actions/:ontology/jobs" element={<SagaJobsPage />} />
          <Route path="/actions/jobs" element={<SagaJobsPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

// Open the DLQ drawer (a DrawerShell) and return its dialog element.
async function openDlqDrawer(user: ReturnType<typeof userEvent.setup>) {
  const openBtn = await screen.findByTestId('saga-jobs-open-dlq-btn');
  await user.click(openBtn);
  return screen.findByTestId('saga-dlq-drawer');
}

// The tab stops inside the drawer, in DOM order. We exclude tabindex="-1"
// elements (e.g. the roving-tabindex status tabs that are not the current
// selection) because they are intentionally NOT Tab targets — the focus trap
// must wrap among real tab stops only.
function tabStops(drawer: HTMLElement): HTMLElement[] {
  const selector = [
    'a[href]',
    'button:not([disabled])',
    'textarea:not([disabled])',
    'input:not([disabled])',
    'select:not([disabled])',
    '[tabindex]:not([tabindex="-1"])',
  ].join(',');
  return Array.from(drawer.querySelectorAll<HTMLElement>(selector));
}

describe('BDD: SagaJobsPage DrawerShell focus management', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(sagaJobsApi, 'listSagas').mockResolvedValue({ data: [] });
    vi.spyOn(sagaDLQApi, 'listSagaDLQ').mockResolvedValue({ entries: [] });
  });

  it('Given an open drawer, Then it is announced as a modal dialog', async () => {
    const user = userEvent.setup();
    renderPage();
    const drawer = await openDlqDrawer(user);

    expect(drawer).toHaveAttribute('role', 'dialog');
    expect(drawer).toHaveAttribute('aria-modal', 'true');
  });

  it('Given an open drawer, Then initial focus lands inside the drawer', async () => {
    const user = userEvent.setup();
    renderPage();
    const drawer = await openDlqDrawer(user);

    await waitFor(() => {
      expect(drawer.contains(document.activeElement)).toBe(true);
    });
    expect(document.activeElement).not.toBe(document.body);
  });

  it('Given focus on the last focusable element, When Tab is pressed, Then focus wraps to the first (stays trapped)', async () => {
    const user = userEvent.setup();
    renderPage();
    const drawer = await openDlqDrawer(user);

    const focusables = tabStops(drawer);
    expect(focusables.length).toBeGreaterThan(1);
    const first = focusables[0];
    const last = focusables[focusables.length - 1];

    last.focus();
    expect(document.activeElement).toBe(last);

    await user.tab();

    expect(drawer.contains(document.activeElement)).toBe(true);
    expect(document.activeElement).toBe(first);
  });

  it('Given focus on the first focusable element, When Shift+Tab is pressed, Then focus wraps to the last (stays trapped)', async () => {
    const user = userEvent.setup();
    renderPage();
    const drawer = await openDlqDrawer(user);

    const focusables = tabStops(drawer);
    expect(focusables.length).toBeGreaterThan(1);
    const first = focusables[0];
    const last = focusables[focusables.length - 1];

    first.focus();
    expect(document.activeElement).toBe(first);

    await user.tab({ shift: true });

    expect(drawer.contains(document.activeElement)).toBe(true);
    expect(document.activeElement).toBe(last);
  });

  it('Given an open drawer, When Escape is pressed, Then the drawer closes', async () => {
    const user = userEvent.setup();
    renderPage();
    await openDlqDrawer(user);

    await user.keyboard('{Escape}');

    await waitFor(() =>
      expect(screen.queryByTestId('saga-dlq-drawer')).not.toBeInTheDocument(),
    );
  });

  it('Given a drawer opened from a trigger, When it closes via Escape, Then focus returns to the trigger', async () => {
    const user = userEvent.setup();
    renderPage();

    const openBtn = await screen.findByTestId('saga-jobs-open-dlq-btn');
    openBtn.focus();
    await user.click(openBtn);

    // The drawer opened and took focus.
    const drawer = await screen.findByTestId('saga-dlq-drawer');
    await waitFor(() => {
      expect(drawer.contains(document.activeElement)).toBe(true);
    });

    // Closing it returns focus to the element that opened it.
    await user.keyboard('{Escape}');
    await waitFor(() =>
      expect(screen.queryByTestId('saga-dlq-drawer')).not.toBeInTheDocument(),
    );
    expect(document.activeElement).toBe(openBtn);
  });
});
