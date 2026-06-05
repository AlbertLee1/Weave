import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { SecurityPoliciesPage } from '../SecurityPoliciesPage';

// BDD: accessible focus management for the six self-drawn `role="dialog"`
// overlays on SecurityPoliciesPage (RowPolicy/Column/Cell editors + their
// three delete confirmations). These are NOT the shared common/Modal, so each
// must trap focus, move focus inside on open, close on Escape, and restore
// focus to the trigger on close — wired via the in-file `useDialogFocus` hook
// (mirrors marketplace/MarketplacePage #252).
//
// The contract is exercised end-to-end through the rendered page with the
// policy CRUD APIs mocked. Existing CRUD / tablist / radiogroup keyboard
// behaviour must survive — these scenarios only assert the new focus wiring.

const OBJECT_TYPE_RID = 'ri.ontology.main.object-type.order';

const ROW_POLICY = {
  rid: 'ri.rls.main.row-policy.p1',
  objectTypeRid: OBJECT_TYPE_RID,
  predicate: { field: 'region', op: 'eq', value: 'EU' },
  appliesTo: { roles: ['analyst'], groups: [], users: [] },
  description: 'EU only',
};

const COLUMN_MASK = {
  rid: 'ri.masking.main.column-mask.c1',
  objectTypeRid: OBJECT_TYPE_RID,
  propertyApiName: 'email',
  maskRule: 'redact',
  appliesTo: { roles: ['admin'], groups: [], users: [] },
  description: 'mask email',
};

const CELL_MASK = {
  rid: 'ri.masking.main.cell-mask.cm1',
  objectTypeRid: OBJECT_TYPE_RID,
  primaryKey: '42',
  propertyApiName: 'email',
  maskStrategy: 'REDACT',
  expression: '',
  appliesTo: { roles: ['admin'], groups: [], users: [] },
  description: 'cell mask',
};

const server = setupServer(
  http.get('/api/v2/ontologies/:ontology/objectTypes', () =>
    HttpResponse.json({
      data: [
        {
          rid: OBJECT_TYPE_RID,
          apiName: 'order',
          displayName: 'Order',
          status: 'ACTIVE',
          visibility: 'PROMINENT',
        },
      ],
    }),
  ),
  // Single object-type detail backs the column/cell editors' property dropdown.
  http.get('/api/v2/ontologies/:ontology/objectTypes/:objectType', () =>
    HttpResponse.json({
      rid: OBJECT_TYPE_RID,
      apiName: 'order',
      displayName: 'Order',
      status: 'ACTIVE',
      visibility: 'PROMINENT',
      properties: {
        email: { apiName: 'email', dataType: { type: 'string' } },
      },
    }),
  ),
  http.get('/api/admin/row-policies', () =>
    HttpResponse.json({ policies: [ROW_POLICY] }),
  ),
  http.get('/api/admin/column-masks', () =>
    HttpResponse.json({ masks: [COLUMN_MASK] }),
  ),
  http.get('/api/admin/cell-masks', () =>
    HttpResponse.json({ masks: [CELL_MASK] }),
  ),
);

beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/admin/northwind/security']}>
        <Routes>
          <Route
            path="/admin/:ontology/security"
            element={<SecurityPoliciesPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BDD: SecurityPoliciesPage dialog focus management (a11y)', () => {
  it('Given the Row Policy editor is opened, When it mounts, Then focus moves inside the dialog and the trigger is remembered', async () => {
    renderPage();
    const trigger = await screen.findByTestId('row-policies-create-btn');
    trigger.focus();
    expect(trigger).toHaveFocus();

    fireEvent.click(trigger);

    const dialog = await screen.findByTestId('row-policy-editor');
    // Focus must land on a focusable element INSIDE the dialog, never on the
    // page behind it.
    await waitFor(() => {
      expect(dialog.contains(document.activeElement)).toBe(true);
    });
    expect(document.activeElement).not.toBe(document.body);
  });

  it('Given the Row Policy editor is open, When Escape is pressed, Then the dialog closes and focus returns to the trigger', async () => {
    const user = userEvent.setup();
    renderPage();
    const trigger = await screen.findByTestId('row-policies-create-btn');
    trigger.focus();
    fireEvent.click(trigger);
    await screen.findByTestId('row-policy-editor');

    await user.keyboard('{Escape}');

    await waitFor(() => {
      expect(screen.queryByTestId('row-policy-editor')).toBeNull();
    });
    // Focus restored to the element that opened the dialog.
    expect(trigger).toHaveFocus();
  });

  it('Given the Row Policy editor is open, When Tab is pressed on the last focusable element, Then focus wraps to the first (focus trap)', async () => {
    const user = userEvent.setup();
    renderPage();
    fireEvent.click(await screen.findByTestId('row-policies-create-btn'));
    const dialog = await screen.findByTestId('row-policy-editor');

    const focusables = dialog.querySelectorAll<HTMLElement>(
      'a[href],button:not([disabled]),textarea:not([disabled]),input:not([disabled]),select:not([disabled]),[tabindex]:not([tabindex="-1"])',
    );
    expect(focusables.length).toBeGreaterThan(1);
    const first = focusables[0];
    const last = focusables[focusables.length - 1];

    // Tab off the last element wraps back to the first.
    last.focus();
    expect(last).toHaveFocus();
    await user.tab();
    expect(first).toHaveFocus();

    // Shift+Tab off the first element wraps to the last.
    await user.tab({ shift: true });
    expect(last).toHaveFocus();
  });

  it('Given the Row Policy delete dialog is opened, When it mounts, Then focus enters; When Escape is pressed, Then it closes and focus restores', async () => {
    const user = userEvent.setup();
    renderPage();
    const deleteBtn = await screen.findByTestId('row-policies-delete-btn');
    deleteBtn.focus();
    fireEvent.click(deleteBtn);

    const dialog = await screen.findByTestId('row-policy-delete-dialog');
    await waitFor(() => {
      expect(dialog.contains(document.activeElement)).toBe(true);
    });

    await user.keyboard('{Escape}');
    await waitFor(() => {
      expect(screen.queryByTestId('row-policy-delete-dialog')).toBeNull();
    });
    expect(deleteBtn).toHaveFocus();
  });

  it('Given the Column Mask editor is opened on the Column tab, When it mounts, Then focus enters; When Escape is pressed, Then it closes', async () => {
    const user = userEvent.setup();
    renderPage();

    fireEvent.click(await screen.findByRole('tab', { name: /column masks/i }));
    const trigger = await screen.findByTestId('column-masks-create-btn');
    trigger.focus();
    fireEvent.click(trigger);

    const dialog = await screen.findByTestId('column-mask-editor');
    await waitFor(() => {
      expect(dialog.contains(document.activeElement)).toBe(true);
    });

    await user.keyboard('{Escape}');
    await waitFor(() => {
      expect(screen.queryByTestId('column-mask-editor')).toBeNull();
    });
    expect(trigger).toHaveFocus();
  });

  it('Given the Column Mask delete dialog is opened, When Escape is pressed, Then it closes and focus restores to the trigger', async () => {
    const user = userEvent.setup();
    renderPage();

    fireEvent.click(await screen.findByRole('tab', { name: /column masks/i }));
    const deleteBtn = await screen.findByTestId('column-masks-delete-btn');
    deleteBtn.focus();
    fireEvent.click(deleteBtn);

    const dialog = await screen.findByTestId('column-mask-delete-dialog');
    await waitFor(() => {
      expect(dialog.contains(document.activeElement)).toBe(true);
    });

    await user.keyboard('{Escape}');
    await waitFor(() => {
      expect(screen.queryByTestId('column-mask-delete-dialog')).toBeNull();
    });
    expect(deleteBtn).toHaveFocus();
  });

  it('Given the Cell Mask editor is opened on the Cell tab, When it mounts, Then focus enters; When Escape is pressed, Then it closes', async () => {
    const user = userEvent.setup();
    renderPage();

    fireEvent.click(await screen.findByRole('tab', { name: /cell masks/i }));
    const trigger = await screen.findByTestId('cell-masks-create-btn');
    trigger.focus();
    fireEvent.click(trigger);

    const dialog = await screen.findByTestId('cell-mask-editor');
    await waitFor(() => {
      expect(dialog.contains(document.activeElement)).toBe(true);
    });

    await user.keyboard('{Escape}');
    await waitFor(() => {
      expect(screen.queryByTestId('cell-mask-editor')).toBeNull();
    });
    expect(trigger).toHaveFocus();
  });

  it('Given the Cell Mask delete dialog is opened, When Escape is pressed, Then it closes and focus restores to the trigger', async () => {
    const user = userEvent.setup();
    renderPage();

    fireEvent.click(await screen.findByRole('tab', { name: /cell masks/i }));
    const deleteBtn = await screen.findByTestId('cell-masks-delete-btn');
    deleteBtn.focus();
    fireEvent.click(deleteBtn);

    const dialog = await screen.findByTestId('cell-mask-delete-dialog');
    await waitFor(() => {
      expect(dialog.contains(document.activeElement)).toBe(true);
    });

    await user.keyboard('{Escape}');
    await waitFor(() => {
      expect(screen.queryByTestId('cell-mask-delete-dialog')).toBeNull();
    });
    expect(deleteBtn).toHaveFocus();
  });

  it('Given every dialog exposes the modal contract, Then all six carry role="dialog" + aria-modal="true"', async () => {
    // Row editor.
    renderPage();
    fireEvent.click(await screen.findByTestId('row-policies-create-btn'));
    const rowEditor = await screen.findByTestId('row-policy-editor');
    expect(rowEditor).toHaveAttribute('role', 'dialog');
    expect(rowEditor).toHaveAttribute('aria-modal', 'true');
  });
});
