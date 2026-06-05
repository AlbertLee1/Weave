// VTX-027 a11y BDD — focus management for the self-drawn Vertex "+ Add
// objects" dialog (web/src/vertex/VertexAddObjectsDialog.tsx).
//
// This dialog is a plain absolutely-positioned Tailwind overlay with
// role="dialog" — NOT the shared common/Modal (which already has focus trap +
// restore). So focus management lives in the dialog itself. These scenarios
// pin the keyboard-user contract from the outside (what a keyboard /
// screen-reader user can observe), mirroring VertexShareLinkPanel.focus.bdd:
//
//   Given an open add-objects dialog
//     Then it is announced as a modal dialog (aria-modal="true")
//     And initial focus lands inside the dialog (not on the page behind it)
//   Given focus on the last focusable element, When Tab is pressed
//     Then focus wraps to the first (stays trapped — never escapes to background)
//   Given focus on the first focusable element, When Shift+Tab is pressed
//     Then focus wraps to the last (stays trapped)
//   Given an open dialog, When Escape is pressed
//     Then the dialog closes (onClose fires)
//   Given a dialog opened from a trigger button, When it closes
//     Then focus returns to the trigger element
//
// The objectTypes + search endpoints are stubbed at the fetch boundary (same
// contract as VertexAddObjectsDialog.test.tsx) so the dialog's controls render
// as focusable targets.

import { useState } from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import userEvent from '@testing-library/user-event';

import { VertexAddObjectsDialog } from '../VertexAddObjectsDialog';

type Handler = {
  match: (url: string, init?: RequestInit) => boolean;
  body: unknown;
  status?: number;
};

let handlers: Handler[] = [];

function setupFetch() {
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url =
      typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
    for (const h of handlers) {
      if (h.match(url, init)) {
        const status = h.status ?? 200;
        return {
          ok: status >= 200 && status < 300,
          status,
          statusText: 'OK',
          headers: new Headers({ 'content-type': 'application/json' }),
          text: async () => JSON.stringify(h.body),
          json: async () => h.body,
        } as Response;
      }
    }
    throw new Error(`unmocked fetch: ${url}`);
  }) as unknown as typeof fetch;
}

const realFetch = globalThis.fetch;

const objectTypesBody = {
  data: [
    {
      rid: 'ri.ontology.main.object-type.airport',
      apiName: 'Airport',
      displayName: 'Airport',
      primaryKey: 'icao',
      titleProperty: 'name',
      status: 'ACTIVE',
      visibility: 'NORMAL',
    },
  ],
};

beforeEach(() => {
  handlers = [
    { match: (u) => u.includes('/objectTypes'), body: objectTypesBody },
    { match: (u) => u.includes('/Airport/search'), body: { data: [] } },
  ];
  setupFetch();
});

afterEach(() => {
  globalThis.fetch = realFetch;
  vi.clearAllMocks();
});

function withQueryClient(node: React.ReactNode) {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchInterval: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
  return <QueryClientProvider client={qc}>{node}</QueryClientProvider>;
}

// Same focusable set the dialog's trap computes — notably excludes the
// disabled Add button (nothing checked yet) and the result-row checkboxes
// only appear when search returns rows. Used to assert real traversal order.
const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'textarea:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

function getFocusables(dialog: HTMLElement): HTMLElement[] {
  return Array.from(dialog.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR));
}

// Wait until the dialog has rendered its focusable controls.
async function waitForDialog() {
  await screen.findByTestId('vertex-add-objects-dialog');
  await waitFor(() => {
    expect((screen.getByTestId('vertex-add-objects-type') as HTMLSelectElement).value).toBe(
      'Airport',
    );
  });
}

// Harness mirroring the parent VertexWorkspacePage: a trigger button that
// opens the dialog, so we can observe focus restoration to the trigger on close.
function AddObjectsHarness() {
  const [open, setOpen] = useState(false);
  return (
    <div>
      <button data-testid="trigger" onClick={() => setOpen(true)}>
        Add objects
      </button>
      {open && (
        <VertexAddObjectsDialog
          open
          ontologyApiName="main"
          onClose={() => setOpen(false)}
          onAdd={() => {}}
        />
      )}
    </div>
  );
}

describe('BDD: Vertex add-objects dialog focus management', () => {
  it('Given an open dialog, Then it is announced as a modal dialog', async () => {
    render(
      withQueryClient(
        <VertexAddObjectsDialog open ontologyApiName="main" onClose={() => {}} onAdd={() => {}} />,
      ),
    );
    await waitForDialog();

    const dialog = screen.getByRole('dialog', { name: 'Add objects' });
    expect(dialog).toHaveAttribute('aria-modal', 'true');
  });

  it('Given an open dialog, Then initial focus lands inside the dialog', async () => {
    render(
      withQueryClient(
        <VertexAddObjectsDialog open ontologyApiName="main" onClose={() => {}} onAdd={() => {}} />,
      ),
    );
    await waitForDialog();

    const dialog = screen.getByRole('dialog', { name: 'Add objects' });
    expect(dialog.contains(document.activeElement)).toBe(true);
    expect(document.activeElement).not.toBe(document.body);
  });

  it('Given focus on the last focusable element, When Tab is pressed, Then focus wraps to the first (stays trapped)', async () => {
    const user = userEvent.setup();
    render(
      withQueryClient(
        <VertexAddObjectsDialog open ontologyApiName="main" onClose={() => {}} onAdd={() => {}} />,
      ),
    );
    await waitForDialog();

    const dialog = screen.getByRole('dialog', { name: 'Add objects' });
    const focusables = getFocusables(dialog);
    const first = focusables[0];
    const last = focusables[focusables.length - 1];

    last.focus();
    expect(document.activeElement).toBe(last);

    await user.tab();

    expect(dialog.contains(document.activeElement)).toBe(true);
    expect(document.activeElement).toBe(first);
  });

  it('Given focus on the first focusable element, When Shift+Tab is pressed, Then focus wraps to the last (stays trapped)', async () => {
    const user = userEvent.setup();
    render(
      withQueryClient(
        <VertexAddObjectsDialog open ontologyApiName="main" onClose={() => {}} onAdd={() => {}} />,
      ),
    );
    await waitForDialog();

    const dialog = screen.getByRole('dialog', { name: 'Add objects' });
    const focusables = getFocusables(dialog);
    const first = focusables[0];
    const last = focusables[focusables.length - 1];

    first.focus();
    expect(document.activeElement).toBe(first);

    await user.tab({ shift: true });

    expect(dialog.contains(document.activeElement)).toBe(true);
    expect(document.activeElement).toBe(last);
  });

  it('Given an open dialog, When Escape is pressed, Then onClose fires', async () => {
    const onClose = vi.fn();
    render(
      withQueryClient(
        <VertexAddObjectsDialog open ontologyApiName="main" onClose={onClose} onAdd={() => {}} />,
      ),
    );
    await waitForDialog();

    await userEvent.keyboard('{Escape}');
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('Given a dialog opened from a trigger, When it closes via Escape, Then focus returns to the trigger', async () => {
    const user = userEvent.setup();
    render(withQueryClient(<AddObjectsHarness />));

    const trigger = screen.getByTestId('trigger');
    trigger.focus();
    await user.click(trigger);

    // The dialog opened and took focus.
    await waitForDialog();
    const dialog = screen.getByRole('dialog', { name: 'Add objects' });
    expect(dialog.contains(document.activeElement)).toBe(true);

    // Closing it returns focus to the element that opened it.
    await user.keyboard('{Escape}');
    await waitFor(() =>
      expect(screen.queryByRole('dialog', { name: 'Add objects' })).not.toBeInTheDocument(),
    );
    expect(document.activeElement).toBe(trigger);
  });
});
