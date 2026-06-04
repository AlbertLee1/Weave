import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { SecurityPoliciesPage } from '../SecurityPoliciesPage';

// BDD: keyboard navigation for the two ARIA widgets on SecurityPoliciesPage.
//
//  1. The Row/Column/Cell `role="tablist"` follows the WAI-ARIA tabs pattern
//     (ArrowRight/Left + Home/End move and activate; roving tabindex).
//  2. The JSON / CEL `role="radiogroup"` inside the row-policy editor follows
//     the WAI-ARIA radio pattern (ArrowDown/Right + ArrowUp/Left move focus AND
//     selection — selection-follows-focus; roving tabindex; Space/Enter select).
//
// Both widgets are exercised end-to-end through the rendered page with the
// policy CRUD APIs mocked so the existing onClick / mode-switch behaviour is
// confirmed to survive alongside the new keyboard handlers.

const OBJECT_TYPE_RID = 'ri.ontology.main.object-type.order';

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
  http.get('/api/admin/row-policies', () =>
    HttpResponse.json({ policies: [] }),
  ),
  http.get('/api/admin/column-masks', () =>
    HttpResponse.json({ masks: [] }),
  ),
  http.get('/api/admin/cell-masks', () =>
    HttpResponse.json({ masks: [] }),
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

describe('BDD: SecurityPoliciesPage Row/Column/Cell tablist keyboard navigation (WAI-ARIA tabs)', () => {
  it('Given the Row tab is focused, When ArrowRight is pressed, Then focus and selection move to Column, then Cell, and wrap back to Row', async () => {
    const user = userEvent.setup();
    renderPage();

    const rowTab = await screen.findByRole('tab', { name: /row policies/i });
    const columnTab = screen.getByRole('tab', { name: /column masks/i });
    const cellTab = screen.getByRole('tab', { name: /cell masks/i });

    rowTab.focus();
    expect(rowTab).toHaveFocus();

    await user.keyboard('{ArrowRight}');
    expect(columnTab).toHaveFocus();
    expect(columnTab).toHaveAttribute('aria-selected', 'true');
    expect(await screen.findByTestId('column-masks-tab')).toBeTruthy();

    await user.keyboard('{ArrowRight}');
    expect(cellTab).toHaveFocus();
    expect(cellTab).toHaveAttribute('aria-selected', 'true');

    // Wrap-around from the last tab back to the first.
    await user.keyboard('{ArrowRight}');
    expect(rowTab).toHaveFocus();
    expect(rowTab).toHaveAttribute('aria-selected', 'true');
    expect(await screen.findByTestId('row-policies-tab')).toBeTruthy();
  });

  it('Given the Row tab is focused, When ArrowLeft is pressed, Then focus and selection wrap to the last tab (Cell) and move backwards', async () => {
    const user = userEvent.setup();
    renderPage();

    const rowTab = await screen.findByRole('tab', { name: /row policies/i });
    const columnTab = screen.getByRole('tab', { name: /column masks/i });
    const cellTab = screen.getByRole('tab', { name: /cell masks/i });

    rowTab.focus();

    await user.keyboard('{ArrowLeft}');
    expect(cellTab).toHaveFocus();
    expect(cellTab).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{ArrowLeft}');
    expect(columnTab).toHaveFocus();
    expect(columnTab).toHaveAttribute('aria-selected', 'true');
  });

  it('Given any tab is focused, When Home/End are pressed, Then focus and selection jump to the first/last tab', async () => {
    const user = userEvent.setup();
    renderPage();

    const rowTab = await screen.findByRole('tab', { name: /row policies/i });
    const cellTab = screen.getByRole('tab', { name: /cell masks/i });

    rowTab.focus();

    await user.keyboard('{End}');
    expect(cellTab).toHaveFocus();
    expect(cellTab).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{Home}');
    expect(rowTab).toHaveFocus();
    expect(rowTab).toHaveAttribute('aria-selected', 'true');
  });

  it('Given the tablist follows the roving tabindex pattern, Then only the selected tab is in the tab order', async () => {
    renderPage();

    const rowTab = await screen.findByRole('tab', { name: /row policies/i });
    const columnTab = screen.getByRole('tab', { name: /column masks/i });
    const cellTab = screen.getByRole('tab', { name: /cell masks/i });

    // Row is the default selection.
    expect(rowTab).toHaveAttribute('tabindex', '0');
    expect(columnTab).toHaveAttribute('tabindex', '-1');
    expect(cellTab).toHaveAttribute('tabindex', '-1');

    // Mouse click still works and updates roving tabindex.
    fireEvent.click(columnTab);
    expect(columnTab).toHaveAttribute('aria-selected', 'true');
    expect(columnTab).toHaveAttribute('tabindex', '0');
    expect(rowTab).toHaveAttribute('tabindex', '-1');
    expect(cellTab).toHaveAttribute('tabindex', '-1');
  });
});

describe('BDD: RowPolicyEditor JSON/CEL mode radiogroup keyboard navigation (WAI-ARIA radio)', () => {
  async function openEditor() {
    renderPage();
    fireEvent.click(await screen.findByTestId('row-policies-create-btn'));
    await screen.findByTestId('row-policy-editor');
  }

  it('Given the JSON radio is focused, When ArrowDown is pressed, Then focus and selection move to the CEL radio (selection-follows-focus)', async () => {
    const user = userEvent.setup();
    await openEditor();

    const jsonRadio = screen.getByRole('radio', { name: /json predicate/i });
    const celRadio = screen.getByRole('radio', { name: /cel expression/i });

    jsonRadio.focus();
    expect(jsonRadio).toHaveFocus();
    expect(jsonRadio).toHaveAttribute('aria-checked', 'true');

    await user.keyboard('{ArrowDown}');
    expect(celRadio).toHaveFocus();
    expect(celRadio).toHaveAttribute('aria-checked', 'true');
    expect(jsonRadio).toHaveAttribute('aria-checked', 'false');
    // The CEL textarea is now rendered, confirming the mode actually switched.
    expect(screen.getByTestId('row-policy-editor-cel')).toBeTruthy();
  });

  it('Given the CEL radio is focused, When ArrowUp is pressed, Then focus and selection move back to the JSON radio', async () => {
    const user = userEvent.setup();
    await openEditor();

    const jsonRadio = screen.getByRole('radio', { name: /json predicate/i });
    const celRadio = screen.getByRole('radio', { name: /cel expression/i });

    // Select CEL first via click (existing behaviour must survive).
    fireEvent.click(celRadio);
    celRadio.focus();
    expect(celRadio).toHaveAttribute('aria-checked', 'true');

    await user.keyboard('{ArrowUp}');
    expect(jsonRadio).toHaveFocus();
    expect(jsonRadio).toHaveAttribute('aria-checked', 'true');
    expect(celRadio).toHaveAttribute('aria-checked', 'false');
    expect(screen.getByTestId('row-policy-editor-predicate')).toBeTruthy();
  });

  it('Given the radio group, When ArrowRight/ArrowLeft are pressed, Then selection moves and wraps around', async () => {
    const user = userEvent.setup();
    await openEditor();

    const jsonRadio = screen.getByRole('radio', { name: /json predicate/i });
    const celRadio = screen.getByRole('radio', { name: /cel expression/i });

    jsonRadio.focus();

    await user.keyboard('{ArrowRight}');
    expect(celRadio).toHaveFocus();
    expect(celRadio).toHaveAttribute('aria-checked', 'true');

    // Wrap from last back to first.
    await user.keyboard('{ArrowRight}');
    expect(jsonRadio).toHaveFocus();
    expect(jsonRadio).toHaveAttribute('aria-checked', 'true');

    // ArrowLeft wraps from first to last.
    await user.keyboard('{ArrowLeft}');
    expect(celRadio).toHaveFocus();
    expect(celRadio).toHaveAttribute('aria-checked', 'true');
  });

  it('Given the radiogroup follows the roving tabindex pattern, Then only the checked radio is in the tab order', async () => {
    await openEditor();

    const jsonRadio = screen.getByRole('radio', { name: /json predicate/i });
    const celRadio = screen.getByRole('radio', { name: /cel expression/i });

    // JSON is the default checked radio.
    expect(jsonRadio).toHaveAttribute('tabindex', '0');
    expect(celRadio).toHaveAttribute('tabindex', '-1');

    fireEvent.click(celRadio);
    expect(celRadio).toHaveAttribute('tabindex', '0');
    expect(jsonRadio).toHaveAttribute('tabindex', '-1');
  });

  it('Given the CEL radio is focused but not checked, When Space is pressed, Then it becomes checked', async () => {
    const user = userEvent.setup();
    await openEditor();

    const jsonRadio = screen.getByRole('radio', { name: /json predicate/i });
    const celRadio = screen.getByRole('radio', { name: /cel expression/i });

    // Move focus to CEL and select it with Space (existing onClick untouched).
    celRadio.focus();
    await user.keyboard(' ');
    expect(celRadio).toHaveAttribute('aria-checked', 'true');
    expect(jsonRadio).toHaveAttribute('aria-checked', 'false');
  });
});
