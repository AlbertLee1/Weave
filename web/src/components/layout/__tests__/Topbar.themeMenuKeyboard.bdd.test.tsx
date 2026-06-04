import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Topbar } from '../Topbar';

// a11y: WAI-ARIA menu keyboard contract for the Topbar *theme* menu.
// The trigger advertises aria-haspopup="menu" and the dropdown is a
// role="menu" container with role="menuitemradio" options — so screen
// readers announce a "menu" and keyboard users expect Arrow / Home / End /
// Escape navigation. This scenario locks that contract in.
//
// Scope note: the Topbar also renders a BranchPicker which has its own
// keyboard navigation (#184). These scenarios only exercise the theme menu.

function stubNotifications() {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.includes('/api/v2/notifications/unread-count')) {
        return new Response(JSON.stringify({ count: 0 }), { status: 200 });
      }
      if (url.includes('/api/v2/notifications')) {
        return new Response(JSON.stringify({ data: [] }), { status: 200 });
      }
      return new Response('{}', { status: 200 });
    }),
  );
}

function renderTopbar() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, refetchInterval: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <Topbar />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('Topbar theme menu keyboard navigation (a11y)', () => {
  beforeEach(() => {
    window.localStorage.clear();
    document.documentElement.classList.remove('dark', 'light');
    stubNotifications();
  });

  afterEach(() => {
    window.localStorage.clear();
    document.documentElement.classList.remove('dark', 'light');
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('Given an open menu with no checked option, Then focus lands on the checked default option', async () => {
    // No stored preference resolves to the "dark" preference (useTheme
    // default), so the dark option is the checked one and gets focus.
    const user = userEvent.setup();
    renderTopbar();

    await user.click(screen.getByTestId('theme-menu-trigger'));

    await waitFor(() =>
      expect(screen.getByTestId('theme-option-dark')).toHaveFocus(),
    );
  });

  it('Given an open menu, When opened with an active preference, Then focus lands on the checked option', async () => {
    window.localStorage.setItem('weave:theme', 'system');
    const user = userEvent.setup();
    renderTopbar();

    await user.click(screen.getByTestId('theme-menu-trigger'));

    await waitFor(() =>
      expect(screen.getByTestId('theme-option-system')).toHaveFocus(),
    );
  });

  it('Given an open menu, When ArrowDown is pressed, Then focus moves to the next option (wrapping)', async () => {
    // Pin the preference to "light" so the first option is the focus anchor,
    // making the roving order deterministic regardless of system defaults.
    window.localStorage.setItem('weave:theme', 'light');
    const user = userEvent.setup();
    renderTopbar();

    await user.click(screen.getByTestId('theme-menu-trigger'));

    const light = screen.getByTestId('theme-option-light');
    const dark = screen.getByTestId('theme-option-dark');
    const system = screen.getByTestId('theme-option-system');

    await waitFor(() => expect(light).toHaveFocus());

    await user.keyboard('{ArrowDown}');
    expect(dark).toHaveFocus();

    await user.keyboard('{ArrowDown}');
    expect(system).toHaveFocus();

    // Wraps back to the first option.
    await user.keyboard('{ArrowDown}');
    expect(light).toHaveFocus();
  });

  it('Given an open menu, When ArrowUp is pressed, Then focus moves to the previous option (wrapping)', async () => {
    window.localStorage.setItem('weave:theme', 'light');
    const user = userEvent.setup();
    renderTopbar();

    await user.click(screen.getByTestId('theme-menu-trigger'));

    const light = screen.getByTestId('theme-option-light');
    const system = screen.getByTestId('theme-option-system');

    await waitFor(() => expect(light).toHaveFocus());

    // ArrowUp from the first option wraps to the last.
    await user.keyboard('{ArrowUp}');
    expect(system).toHaveFocus();
  });

  it('Given an open menu, When Home / End are pressed, Then focus jumps to the first / last option', async () => {
    window.localStorage.setItem('weave:theme', 'light');
    const user = userEvent.setup();
    renderTopbar();

    await user.click(screen.getByTestId('theme-menu-trigger'));

    const light = screen.getByTestId('theme-option-light');
    const system = screen.getByTestId('theme-option-system');

    await waitFor(() => expect(light).toHaveFocus());

    await user.keyboard('{End}');
    expect(system).toHaveFocus();

    await user.keyboard('{Home}');
    expect(light).toHaveFocus();
  });

  it('Given an open menu, When Escape is pressed, Then the menu closes and focus returns to the trigger', async () => {
    const user = userEvent.setup();
    renderTopbar();

    const trigger = screen.getByTestId('theme-menu-trigger');
    await user.click(trigger);

    await screen.findByTestId('theme-menu');

    await user.keyboard('{Escape}');

    await waitFor(() => {
      expect(screen.queryByTestId('theme-menu')).toBeNull();
    });
    expect(trigger).toHaveFocus();
  });

  it('does not break mouse selection of a theme option', async () => {
    const user = userEvent.setup();
    renderTopbar();
    // Default (no stored preference) resolves to dark.
    expect(document.documentElement.classList.contains('dark')).toBe(true);

    await user.click(screen.getByTestId('theme-menu-trigger'));
    await user.click(screen.getByTestId('theme-option-light'));

    expect(window.localStorage.getItem('weave:theme')).toBe('light');
    expect(document.documentElement.classList.contains('light')).toBe(true);
    // Menu closes after selection.
    expect(screen.queryByTestId('theme-menu')).toBeNull();
  });
});
