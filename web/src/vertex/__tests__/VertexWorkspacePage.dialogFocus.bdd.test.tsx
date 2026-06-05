// a11y BDD — focus management for the self-drawn Vertex LayoutMenu dialog
// inside web/src/vertex/VertexWorkspacePage.tsx (the "Layout options" popover
// with role="dialog" opened from the TopBar "Layout" button).
//
// That popover is a plain Tailwind popover with role="dialog" — NOT the shared
// common/Modal (which already traps + restores focus), and NOT the sibling
// VertexAddObjectsDialog / VertexShareLinkPanel (which already manage their own
// focus). So focus management lives in the LayoutMenu itself. These scenarios
// pin the keyboard-user contract from the outside (what a keyboard /
// screen-reader user can observe), mirroring VertexShareLinkPanel.focus.bdd:
//
//   Given an open Layout dialog
//     Then it is announced as a modal dialog (aria-modal="true")
//     And initial focus lands inside the dialog (not on the page behind it)
//   Given focus on the last focusable element, When Tab is pressed
//     Then focus wraps to the first (stays trapped — never escapes to background)
//   Given focus on the first focusable element, When Shift+Tab is pressed
//     Then focus wraps to the last (stays trapped)
//   Given an open dialog, When Escape is pressed
//     Then the dialog closes
//   Given the dialog opened from the Layout button, When it closes via Escape
//     Then focus returns to the Layout trigger button
//
// The Sigma WebGL stack is stubbed exactly as in VertexWorkspacePage.test.tsx
// (jsdom can't WebGL) so the page mounts and the TopBar renders.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import Graph from 'graphology';

// --- Sigma stubs (mirrors VertexWorkspacePage.test.tsx) ---------------------
let loadedGraph: Graph | null = null;
let afterRenderHandler: (() => void) | null = null;
const captureLoad = (g: Graph) => {
  loadedGraph = g;
  if (afterRenderHandler) {
    queueMicrotask(afterRenderHandler);
  }
};
const emptyGraphStub = {
  hasNode: () => false,
  getNodeAttribute: () => undefined,
  forEachNode: () => {},
  setNodeAttribute: () => {},
  nodes: () => [] as string[],
};
const sigmaContainer = document.createElement('div');
Object.defineProperty(sigmaContainer, 'getBoundingClientRect', {
  value: () => ({ left: 0, top: 0, width: 800, height: 600, right: 800, bottom: 600 }),
  configurable: true,
});
const sigmaStub = {
  graphToViewport: (p: { x: number; y: number }) => ({ x: p.x, y: p.y }),
  viewportToGraph: (p: { x: number; y: number }) => ({ x: p.x, y: p.y }),
  getDimensions: () => ({ width: 800, height: 600 }),
  getGraph: () => loadedGraph ?? emptyGraphStub,
  getContainer: () => sigmaContainer,
  refresh: () => {},
};
let capturedHandlers: Record<string, ((p: unknown) => void) | undefined> = {};
vi.mock('@react-sigma/core', () => ({
  SigmaContainer: ({
    children,
    style,
  }: {
    children?: React.ReactNode;
    style?: React.CSSProperties;
  }) => (
    <div data-testid="vertex-canvas-mock" style={style}>
      {children}
    </div>
  ),
  useLoadGraph: () => captureLoad,
  useSigma: () => sigmaStub,
  useRegisterEvents:
    () => (handlers: Record<string, ((p: unknown) => void) | undefined>) => {
      Object.assign(capturedHandlers, handlers);
      if (typeof handlers.afterRender === 'function') {
        afterRenderHandler = handlers.afterRender as () => void;
      }
    },
}));

import { VertexWorkspacePage } from '../VertexWorkspacePage';

const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'textarea:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

function focusablesOf(el: HTMLElement): HTMLElement[] {
  return Array.from(el.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR));
}

function renderAt(path: string) {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchInterval: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/vertex/:rid" element={<VertexWorkspacePage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const realFetch = globalThis.fetch;

beforeEach(() => {
  loadedGraph = null;
  afterRenderHandler = null;
  capturedHandlers = {};
  globalThis.fetch = vi.fn() as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = realFetch;
  vi.clearAllMocks();
});

// Open the Layout popover by clicking the TopBar trigger; return both the
// trigger button and the opened dialog.
async function openLayoutDialog(user: ReturnType<typeof userEvent.setup>) {
  const trigger = await screen.findByTestId('vertex-topbar-layout');
  trigger.focus();
  await user.click(trigger);
  const dialog = await screen.findByTestId('vertex-layout-popover');
  return { trigger, dialog };
}

describe('BDD: Vertex Layout dialog focus management', () => {
  it('Given an open Layout dialog, Then it is announced as a modal dialog', async () => {
    const user = userEvent.setup();
    renderAt('/vertex/new');
    const { dialog } = await openLayoutDialog(user);

    expect(dialog).toHaveAttribute('role', 'dialog');
    expect(dialog).toHaveAttribute('aria-modal', 'true');
  });

  it('Given an open Layout dialog, Then initial focus lands inside the dialog', async () => {
    const user = userEvent.setup();
    renderAt('/vertex/new');
    const { dialog } = await openLayoutDialog(user);

    await waitFor(() => expect(dialog.contains(document.activeElement)).toBe(true));
    expect(document.activeElement).not.toBe(document.body);
  });

  it('Given focus on the last focusable element, When Tab is pressed, Then focus wraps to the first (stays trapped)', async () => {
    const user = userEvent.setup();
    renderAt('/vertex/new');
    const { dialog } = await openLayoutDialog(user);

    const list = focusablesOf(dialog);
    expect(list.length).toBeGreaterThan(0);
    const first = list[0];
    const last = list[list.length - 1];

    last.focus();
    expect(document.activeElement).toBe(last);

    await user.tab();

    expect(dialog.contains(document.activeElement)).toBe(true);
    expect(document.activeElement).toBe(first);
  });

  it('Given focus on the first focusable element, When Shift+Tab is pressed, Then focus wraps to the last (stays trapped)', async () => {
    const user = userEvent.setup();
    renderAt('/vertex/new');
    const { dialog } = await openLayoutDialog(user);

    const list = focusablesOf(dialog);
    expect(list.length).toBeGreaterThan(0);
    const first = list[0];
    const last = list[list.length - 1];

    first.focus();
    expect(document.activeElement).toBe(first);

    await user.tab({ shift: true });

    expect(dialog.contains(document.activeElement)).toBe(true);
    expect(document.activeElement).toBe(last);
  });

  it('Given an open Layout dialog, When Escape is pressed, Then the dialog closes', async () => {
    const user = userEvent.setup();
    renderAt('/vertex/new');
    await openLayoutDialog(user);

    await user.keyboard('{Escape}');
    await waitFor(() =>
      expect(screen.queryByTestId('vertex-layout-popover')).not.toBeInTheDocument(),
    );
  });

  it('Given the dialog opened from the Layout button, When it closes via Escape, Then focus returns to the trigger', async () => {
    const user = userEvent.setup();
    renderAt('/vertex/new');
    const { trigger } = await openLayoutDialog(user);

    await user.keyboard('{Escape}');
    await waitFor(() =>
      expect(screen.queryByTestId('vertex-layout-popover')).not.toBeInTheDocument(),
    );
    expect(document.activeElement).toBe(trigger);
  });
});
