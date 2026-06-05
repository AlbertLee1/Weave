import {
  describe,
  it,
  expect,
  beforeAll,
  afterAll,
  afterEach,
  beforeEach,
  vi,
} from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AutomationRulesPage } from '../AutomationRulesPage';
import type { AutomationRule } from '../../../api/automationRules';

// a11y — focus management for the self-drawn DrawerShell.
//
// The automation editor / executions drawers are NOT the shared
// common/Modal (which already traps + restores focus). This contract pins
// the standard focus management for the self-drawn DrawerShell, mirroring
// SagaJobsPage's DrawerShell (#244) and VertexShareLinkPanel (#229):
//   - opening the drawer moves DOM focus inside it;
//   - Tab / Shift+Tab cycle within the drawer (focus trap);
//   - Escape closes the drawer;
//   - closing restores focus to the element that opened it.
//
// Without this, keyboard / screen-reader users land behind the overlay,
// can tab into the inert background page, and have no keyboard way out.

const FOCUSABLE_SELECTOR =
  'a[href],button:not([disabled]),textarea:not([disabled]),input:not([disabled]),select:not([disabled]),[tabindex]:not([tabindex="-1"])';

function ruleFixture(overrides: Partial<AutomationRule>): AutomationRule {
  return {
    id: 'rule-x',
    ontologyRid: 'ri.ontology.main.ontology.default',
    name: 'Nightly sync',
    description: 'Runs at midnight',
    status: 'active',
    triggerType: 'schedule',
    triggerConfig: { condition: 'true' },
    effects: [],
    retryPolicy: undefined,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

let rules: AutomationRule[] = [];

const server = setupServer(
  http.get('/api/v2/ontologies/:ontology/automationRules', () =>
    HttpResponse.json({ data: rules }),
  ),
  http.get(
    '/api/v2/ontologies/:ontology/automationRules/:ruleId/executions',
    () => HttpResponse.json({ data: [] }),
  ),
);

function renderPage(initial = '/automation/default') {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initial]}>
        <Routes>
          <Route
            path="/automation/:ontology"
            element={<AutomationRulesPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('AutomationRulesPage DrawerShell focus management (a11y)', () => {
  beforeAll(() => server.listen());
  beforeEach(() => {
    rules = [];
  });
  afterEach(() => {
    server.resetHandlers();
    vi.restoreAllMocks();
  });
  afterAll(() => server.close());

  it('Given the editor drawer opens, Then DOM focus moves inside it', async () => {
    rules = [ruleFixture({ id: 'rule-a', name: 'Nightly sync' })];
    renderPage();
    const user = userEvent.setup();

    const createBtn = await screen.findByTestId('automation-rules-create-btn');
    await user.click(createBtn);

    const drawer = await screen.findByTestId('automation-rule-editor-drawer');
    await waitFor(() => {
      expect(drawer.contains(document.activeElement)).toBe(true);
    });
  });

  it('Given the editor drawer is open, When Escape is pressed, Then it closes', async () => {
    rules = [ruleFixture({ id: 'rule-a', name: 'Nightly sync' })];
    renderPage();
    const user = userEvent.setup();

    const createBtn = await screen.findByTestId('automation-rules-create-btn');
    await user.click(createBtn);
    await screen.findByTestId('automation-rule-editor-drawer');

    await user.keyboard('{Escape}');

    await waitFor(() =>
      expect(
        screen.queryByTestId('automation-rule-editor-drawer'),
      ).not.toBeInTheDocument(),
    );
  });

  it('Given the editor drawer is open, When Tab reaches the last element, Then focus wraps back into the drawer (trap)', async () => {
    rules = [ruleFixture({ id: 'rule-a', name: 'Nightly sync' })];
    renderPage();
    const user = userEvent.setup();

    const createBtn = await screen.findByTestId('automation-rules-create-btn');
    await user.click(createBtn);

    const drawer = await screen.findByTestId('automation-rule-editor-drawer');
    const focusables = Array.from(
      drawer.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR),
    );
    expect(focusables.length).toBeGreaterThan(1);

    // Drive focus to the last focusable element, then Tab — it must wrap
    // back into the drawer (not escape to the background page).
    const last = focusables[focusables.length - 1];
    last.focus();
    expect(document.activeElement).toBe(last);

    await user.tab();

    expect(drawer.contains(document.activeElement)).toBe(true);
  });

  it('Given the editor drawer is open, When Shift+Tab on the first element, Then focus wraps to the last (trap)', async () => {
    rules = [ruleFixture({ id: 'rule-a', name: 'Nightly sync' })];
    renderPage();
    const user = userEvent.setup();

    const createBtn = await screen.findByTestId('automation-rules-create-btn');
    await user.click(createBtn);

    const drawer = await screen.findByTestId('automation-rule-editor-drawer');
    const focusables = Array.from(
      drawer.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR),
    );
    expect(focusables.length).toBeGreaterThan(1);

    const first = focusables[0];
    first.focus();
    expect(document.activeElement).toBe(first);

    await user.tab({ shift: true });

    expect(drawer.contains(document.activeElement)).toBe(true);
  });

  it('Given a trigger opened the drawer, When the drawer closes, Then focus returns to the trigger', async () => {
    rules = [ruleFixture({ id: 'rule-a', name: 'Nightly sync' })];
    renderPage();
    const user = userEvent.setup();

    const createBtn = await screen.findByTestId('automation-rules-create-btn');
    createBtn.focus();
    await user.click(createBtn);
    await screen.findByTestId('automation-rule-editor-drawer');

    await user.keyboard('{Escape}');

    await waitFor(() =>
      expect(
        screen.queryByTestId('automation-rule-editor-drawer'),
      ).not.toBeInTheDocument(),
    );
    await waitFor(() =>
      expect(document.activeElement).toBe(
        screen.getByTestId('automation-rules-create-btn'),
      ),
    );
  });
});
