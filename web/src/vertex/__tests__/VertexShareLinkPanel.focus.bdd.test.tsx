// VTX-013 a11y BDD — focus management for the self-drawn Vertex share-links
// dialog (web/src/vertex/VertexShareLinkPanel.tsx).
//
// This panel is a plain Tailwind popover with role="dialog" — NOT the shared
// common/Modal (which already has focus trap + restore). So focus management
// lives in the panel itself. These scenarios pin the keyboard-user contract
// from the outside (what a keyboard / screen-reader user can observe):
//
//   Given an open share-links dialog
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
// The three share-link endpoints are stubbed at the fetch boundary (same
// contract as VertexShareLinks.bdd.test.tsx) so the panel's own data effect
// resolves and the create/list/revoke buttons render as focusable targets.

import { useState } from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { VertexShareLinkPanel } from '../VertexShareLinkPanel';

const RID = 'ri.vertex.main.graph.demo';

interface StubLink {
  tokenSuffix: string;
  graphRid: string;
  createdBy: string;
  createdAt: string;
  revoked: boolean;
}

let serverLinks: StubLink[];

function jsonResponse(status: number, body: unknown): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: '',
    text: async () => (body === undefined ? '' : JSON.stringify(body)),
    json: async () => body,
  } as unknown as Response;
}

function installFetchStub() {
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString();
    const method = (init?.method ?? 'GET').toUpperCase();

    if (url.endsWith(`/graphs/${RID}/share-links`) && method === 'GET') {
      return jsonResponse(200, { shareLinks: serverLinks });
    }
    if (url.endsWith(`/graphs/${RID}/share-links`) && method === 'POST') {
      return jsonResponse(201, {
        token: 'TOKENabcdefABCDEF12345678',
        graphRid: RID,
        createdBy: 'alice',
        createdAt: '2026-06-04T00:00:00Z',
      });
    }
    const revokeMatch = url.match(/\/share-links\/([^/?#]+)$/);
    if (revokeMatch && method === 'DELETE') {
      return jsonResponse(204, undefined);
    }
    throw new Error(`unexpected fetch: ${method} ${url}`);
  }) as unknown as typeof fetch;
}

const realFetch = globalThis.fetch;

beforeEach(() => {
  serverLinks = [
    {
      tokenSuffix: 'deadbeef',
      graphRid: RID,
      createdBy: 'alice',
      createdAt: '2026-06-04T00:00:00Z',
      revoked: false,
    },
  ];
  installFetchStub();
});

afterEach(() => {
  globalThis.fetch = realFetch;
  vi.restoreAllMocks();
});

// Wait for the panel's initial data effect to settle so all focusable
// controls (create + revoke) are rendered before we assert focus order.
async function waitForLoaded() {
  await waitFor(() =>
    expect(screen.queryByTestId('vertex-share-loading')).not.toBeInTheDocument(),
  );
}

// Harness mirroring the parent ShareMenu: a trigger button that conditionally
// mounts the panel, so we can observe focus restoration to the trigger on close.
function ShareHarness() {
  const [open, setOpen] = useState(false);
  return (
    <div>
      <button data-testid="trigger" onClick={() => setOpen(true)}>
        Share
      </button>
      {open && <VertexShareLinkPanel graphRid={RID} onClose={() => setOpen(false)} />}
    </div>
  );
}

describe('BDD: Vertex share-links dialog focus management', () => {
  it('Given an open dialog, Then it is announced as a modal dialog', async () => {
    render(<VertexShareLinkPanel graphRid={RID} onClose={() => {}} />);
    await waitForLoaded();

    const dialog = screen.getByRole('dialog', { name: 'Share links' });
    expect(dialog).toHaveAttribute('aria-modal', 'true');
  });

  it('Given an open dialog, Then initial focus lands inside the dialog', async () => {
    render(<VertexShareLinkPanel graphRid={RID} onClose={() => {}} />);
    await waitForLoaded();

    const dialog = screen.getByRole('dialog', { name: 'Share links' });
    expect(dialog.contains(document.activeElement)).toBe(true);
    expect(document.activeElement).not.toBe(document.body);
  });

  it('Given focus on the last focusable element, When Tab is pressed, Then focus wraps to the first (stays trapped)', async () => {
    const user = userEvent.setup();
    render(<VertexShareLinkPanel graphRid={RID} onClose={() => {}} />);
    await waitForLoaded();

    const dialog = screen.getByRole('dialog', { name: 'Share links' });
    const focusables = within(dialog).getAllByRole('button');
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
    render(<VertexShareLinkPanel graphRid={RID} onClose={() => {}} />);
    await waitForLoaded();

    const dialog = screen.getByRole('dialog', { name: 'Share links' });
    const focusables = within(dialog).getAllByRole('button');
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
    render(<VertexShareLinkPanel graphRid={RID} onClose={onClose} />);
    await waitForLoaded();

    await userEvent.keyboard('{Escape}');
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('Given a dialog opened from a trigger, When it closes via Escape, Then focus returns to the trigger', async () => {
    const user = userEvent.setup();
    render(<ShareHarness />);

    const trigger = screen.getByTestId('trigger');
    trigger.focus();
    await user.click(trigger);

    // The dialog opened and took focus.
    await waitForLoaded();
    const dialog = screen.getByRole('dialog', { name: 'Share links' });
    expect(dialog.contains(document.activeElement)).toBe(true);

    // Closing it returns focus to the element that opened it.
    await user.keyboard('{Escape}');
    await waitFor(() =>
      expect(screen.queryByRole('dialog', { name: 'Share links' })).not.toBeInTheDocument(),
    );
    expect(document.activeElement).toBe(trigger);
  });
});
