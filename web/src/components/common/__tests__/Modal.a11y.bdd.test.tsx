import { describe, it, expect, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Modal } from '../Modal';

// BDD: accessibility & focus-management contract for the shared Modal.
// These scenarios describe what a screen-reader / keyboard user can observe,
// independent of the visual styling (which must stay untouched).
describe('BDD: Modal accessibility & focus trap', () => {
  it('Given an open Modal, Then it exposes a dialog role labelled by its title', () => {
    // Given a Modal is opened with a title
    render(
      <Modal open={true} onClose={() => {}} title="Edit Object Type">
        <button>First</button>
      </Modal>,
    );

    // Then the panel is announced as a modal dialog
    const dialog = screen.getByRole('dialog');
    expect(dialog).toHaveAttribute('aria-modal', 'true');

    // And the dialog is labelled by the visible heading
    const labelledBy = dialog.getAttribute('aria-labelledby');
    expect(labelledBy).toBeTruthy();
    const heading = document.getElementById(labelledBy as string);
    expect(heading).not.toBeNull();
    expect(heading).toHaveTextContent('Edit Object Type');

    // And the accessible name resolves to the title (no dangling labelledby)
    expect(dialog).toHaveAccessibleName('Edit Object Type');
  });

  it('Given an open Modal, Then initial focus lands inside the dialog', () => {
    render(
      <Modal open={true} onClose={() => {}} title="Confirm">
        <button>Confirm action</button>
      </Modal>,
    );

    const dialog = screen.getByRole('dialog');
    // Then focus is moved into the dialog (not left on document.body)
    expect(dialog.contains(document.activeElement)).toBe(true);
    expect(document.activeElement).not.toBe(document.body);
  });

  it('Given focus on the last focusable element, When Tab is pressed, Then focus wraps to the first (stays trapped)', async () => {
    const user = userEvent.setup();
    render(
      <Modal open={true} onClose={() => {}} title="Trap">
        <button data-testid="first-btn">First</button>
        <button data-testid="last-btn">Last</button>
      </Modal>,
    );

    const dialog = screen.getByRole('dialog');
    // Focus order inside the dialog: [First, Last, Close button].
    const focusables = within(dialog).getAllByRole('button');
    const first = focusables[0];
    const last = focusables[focusables.length - 1];

    // Given focus is on the last focusable element
    last.focus();
    expect(document.activeElement).toBe(last);

    // When Tab is pressed from the last element
    await user.tab();

    // Then focus stays within the dialog and wraps back to the first element
    expect(dialog.contains(document.activeElement)).toBe(true);
    expect(document.activeElement).toBe(first);
  });

  it('Given focus on the first focusable element, When Shift+Tab is pressed, Then focus wraps to the last (stays trapped)', async () => {
    const user = userEvent.setup();
    render(
      <Modal open={true} onClose={() => {}} title="Trap">
        <button data-testid="first-btn">First</button>
        <button data-testid="last-btn">Last</button>
      </Modal>,
    );

    const dialog = screen.getByRole('dialog');
    const focusables = within(dialog).getAllByRole('button');
    const first = focusables[0];
    const last = focusables[focusables.length - 1];

    // Given focus is on the first focusable element
    first.focus();
    expect(document.activeElement).toBe(first);

    // When Shift+Tab is pressed from the first element
    await user.tab({ shift: true });

    // Then focus stays within the dialog and wraps to the last element
    expect(dialog.contains(document.activeElement)).toBe(true);
    expect(document.activeElement).toBe(last);
  });

  it('Given a Modal with no interactive children, Then it still renders and traps focus on the close button', async () => {
    const user = userEvent.setup();
    // Degenerate case: only the built-in close button is focusable.
    render(
      <Modal open={true} onClose={() => {}} title="Empty">
        <p>No interactive content here.</p>
      </Modal>,
    );

    const dialog = screen.getByRole('dialog');
    expect(dialog.contains(document.activeElement)).toBe(true);

    // Tabbing must not throw or escape the dialog.
    await user.tab();
    expect(dialog.contains(document.activeElement)).toBe(true);
  });

  it('Given an open Modal, Then Escape and overlay-click still close it (no regression)', async () => {
    const onClose = vi.fn();
    render(
      <Modal open={true} onClose={onClose} title="Closable">
        <button>Content</button>
      </Modal>,
    );

    // Escape closes
    await userEvent.keyboard('{Escape}');
    expect(onClose).toHaveBeenCalledTimes(1);

    // Overlay click closes
    await userEvent.click(screen.getByTestId('modal-overlay'));
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it('Given two stacked open Modals, Then each dialog is labelled by its own unique title id', () => {
    // Given two Modals are open at the same time (e.g. a confirm dialog
    // stacked on top of an edit form), each with a distinct title.
    render(
      <>
        <Modal open={true} onClose={() => {}} title="Edit Object Type">
          <button>Save</button>
        </Modal>
        <Modal open={true} onClose={() => {}} title="Discard changes?">
          <button>Discard</button>
        </Modal>
      </>,
    );

    const dialogs = screen.getAllByRole('dialog');
    expect(dialogs).toHaveLength(2);

    const labelledByIds = dialogs.map((d) => d.getAttribute('aria-labelledby'));
    // Both must actually be wired up...
    labelledByIds.forEach((id) => expect(id).toBeTruthy());
    // ...and crucially the two ids must be DIFFERENT, so a screen reader can
    // never confuse the top dialog with the one beneath it (the bug with a
    // shared module-level "modal-title" constant).
    expect(labelledByIds[0]).not.toBe(labelledByIds[1]);

    // And each dialog's labelledby resolves through the DOM to its OWN visible
    // heading text (relationship-based, not a hardcoded id value).
    const headingTexts = labelledByIds.map(
      (id) => document.getElementById(id as string)?.textContent,
    );
    expect(headingTexts).toEqual(
      expect.arrayContaining(['Edit Object Type', 'Discard changes?']),
    );

    // And each dialog exposes its own title as its accessible name.
    const accessibleNames = dialogs.map((d) => d.getAttribute('aria-label') ?? null);
    expect(accessibleNames).toEqual([null, null]); // labelled by id, not aria-label
  });
});
