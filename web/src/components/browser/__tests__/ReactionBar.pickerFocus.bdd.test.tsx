import {
  describe,
  it,
  expect,
  vi,
  beforeAll,
  afterAll,
  beforeEach,
  afterEach,
} from 'vitest';
import { createElement } from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AuthProvider } from '../../../auth/AuthContext';
import { ReactionBar } from '../ReactionBar';

// BDD (a11y): the self-drawn reaction picker popover (role="dialog") must
// implement standard focus management, mirroring VertexShareLinkPanel (#229):
//   - opening moves focus inside the picker (first focusable element),
//   - Escape closes the picker,
//   - Tab / Shift+Tab cycle within the picker (focus trap, degrades safely),
//   - closing restores focus to the trigger ("Add reaction") button.
// Previously the picker had none of this: it was an absolute z-20 popover with
// role="dialog" but no Escape, no focus trap, no focus move on open, and no
// focus restore on close — leaving keyboard users stranded behind the popover.
const server = setupServer();
beforeAll(() => server.listen({ onUnhandledRequest: 'bypass' }));
afterEach(() => {
  server.resetHandlers();
  vi.clearAllMocks();
});
afterAll(() => server.close());

const targetRid = 'ri.ontology.main.object.emp1';

function meHandler() {
  return http.get('/api/v2/me', () =>
    HttpResponse.json({
      id: 'alice',
      email: 'alice@example.test',
      name: 'alice',
      roles: ['viewer'],
      ontologyRoles: {},
      permissions: ['object.read'],
    }),
  );
}

function makeWrapper() {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
  return ({ children }: { children: React.ReactNode }) =>
    createElement(
      QueryClientProvider,
      { client },
      createElement(AuthProvider, null, children),
    );
}

describe('ReactionBar picker focus management (BDD, a11y)', () => {
  beforeEach(() => {
    server.use(
      meHandler(),
      http.get('/api/v2/reactions', () =>
        HttpResponse.json({ targetRid, emojis: [] }),
      ),
    );
  });

  it('Given the picker is opened, Then focus moves to the first focusable element inside the picker', async () => {
    const user = userEvent.setup();
    render(<ReactionBar targetRid={targetRid} />, { wrapper: makeWrapper() });

    const addButton = await screen.findByTestId('reaction-add-button');
    await waitFor(() => expect(addButton).not.toBeDisabled());

    await user.click(addButton);

    const picker = await screen.findByTestId('reaction-picker');
    await waitFor(() => {
      // Focus should be somewhere inside the picker, not left on the page body
      // or the trigger button.
      expect(picker.contains(document.activeElement)).toBe(true);
    });
    expect(document.activeElement).not.toBe(addButton);
  });

  it('Given the picker is open, When the user presses Escape, Then the picker closes and focus returns to the trigger', async () => {
    const user = userEvent.setup();
    render(<ReactionBar targetRid={targetRid} />, { wrapper: makeWrapper() });

    const addButton = await screen.findByTestId('reaction-add-button');
    await waitFor(() => expect(addButton).not.toBeDisabled());

    await user.click(addButton);
    await screen.findByTestId('reaction-picker');

    await user.keyboard('{Escape}');

    await waitFor(() => {
      expect(screen.queryByTestId('reaction-picker')).toBeNull();
    });
    await waitFor(() => {
      expect(document.activeElement).toBe(addButton);
    });
  });

  it('Given the picker is open, When focus is on the last focusable element and the user presses Tab, Then focus wraps to the first focusable element (focus trap)', async () => {
    const user = userEvent.setup();
    render(<ReactionBar targetRid={targetRid} />, { wrapper: makeWrapper() });

    const addButton = await screen.findByTestId('reaction-add-button');
    await waitFor(() => expect(addButton).not.toBeDisabled());

    await user.click(addButton);
    const picker = await screen.findByTestId('reaction-picker');

    const focusables = Array.from(
      picker.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])',
      ),
    );
    expect(focusables.length).toBeGreaterThan(1);
    const first = focusables[0];
    const last = focusables[focusables.length - 1];

    last.focus();
    expect(document.activeElement).toBe(last);

    await user.tab();
    await waitFor(() => {
      expect(document.activeElement).toBe(first);
    });
  });

  it('Given the picker is open, When focus is on the first focusable element and the user presses Shift+Tab, Then focus wraps to the last focusable element (focus trap)', async () => {
    const user = userEvent.setup();
    render(<ReactionBar targetRid={targetRid} />, { wrapper: makeWrapper() });

    const addButton = await screen.findByTestId('reaction-add-button');
    await waitFor(() => expect(addButton).not.toBeDisabled());

    await user.click(addButton);
    const picker = await screen.findByTestId('reaction-picker');

    const focusables = Array.from(
      picker.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])',
      ),
    );
    expect(focusables.length).toBeGreaterThan(1);
    const first = focusables[0];
    const last = focusables[focusables.length - 1];

    first.focus();
    expect(document.activeElement).toBe(first);

    await user.tab({ shift: true });
    await waitFor(() => {
      expect(document.activeElement).toBe(last);
    });
  });
});
