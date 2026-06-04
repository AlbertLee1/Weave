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
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AutomationRulesPage } from '../AutomationRulesPage';
import type { AutomationRule } from '../../../api/automationRules';

// UX consistency — automation-rule deletion confirm.
//
// The page previously gated DELETE behind a native `window.confirm`,
// which does not honour the dark theme, breaks visually with the rest of
// the app's styled Modal dialogs, and is awkward to assert against. The
// codebase has standardised on the shared `common/Modal` (see
// DashboardEditorPage's note about deliberately avoiding the
// unstylable window.confirm). This contract pins the styled two-step
// confirm flow: click Delete → styled Modal naming the rule → Cancel
// aborts (no DELETE), Confirm deletes and removes the row. The native
// window.confirm must never be invoked.

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
let deleteCalls: string[] = [];

const server = setupServer(
  http.get('/api/v2/ontologies/:ontology/automationRules', () =>
    HttpResponse.json({ data: rules }),
  ),
  http.delete(
    '/api/v2/ontologies/:ontology/automationRules/:ruleId',
    ({ params }) => {
      const ruleId = String(params.ruleId);
      deleteCalls.push(ruleId);
      rules = rules.filter((r) => r.id !== ruleId);
      return new HttpResponse(null, { status: 204 });
    },
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
          <Route path="/automation/:ontology" element={<AutomationRulesPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

async function findRow(ruleId: string): Promise<HTMLElement> {
  const list = await screen.findByTestId('automation-rules-list');
  const row = within(list)
    .getAllByTestId('automation-rule-row')
    .find((el) => el.getAttribute('data-rule-id') === ruleId);
  if (!row) throw new Error(`row for ${ruleId} not found`);
  return row as HTMLElement;
}

describe('AutomationRulesPage styled delete-confirm Modal (UX consistency)', () => {
  beforeAll(() => server.listen());
  beforeEach(() => {
    rules = [];
    deleteCalls = [];
  });
  afterEach(() => {
    server.resetHandlers();
    vi.restoreAllMocks();
  });
  afterAll(() => server.close());

  it('Given a rule, When Delete is clicked, Then window.confirm is NOT called and a styled Modal naming the rule appears', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm');
    rules = [ruleFixture({ id: 'rule-a', name: 'Nightly sync' })];

    renderPage();
    const user = userEvent.setup();

    const row = await findRow('rule-a');
    await user.click(within(row).getByTestId('automation-rule-delete-btn'));

    // No native, unstylable confirm.
    expect(confirmSpy).not.toHaveBeenCalled();

    // A styled shared-Modal dialog appears, naming the rule.
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText(/Nightly sync/)).toBeInTheDocument();

    // Nothing deleted just by opening the confirm.
    expect(deleteCalls).toEqual([]);
  });

  it('Given the confirm Modal is open, When Cancel is clicked, Then the rule is NOT deleted and the Modal closes', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm');
    rules = [ruleFixture({ id: 'rule-a', name: 'Nightly sync' })];

    renderPage();
    const user = userEvent.setup();

    const row = await findRow('rule-a');
    await user.click(within(row).getByTestId('automation-rule-delete-btn'));

    const dialog = await screen.findByRole('dialog');
    await user.click(within(dialog).getByRole('button', { name: /cancel/i }));

    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument(),
    );
    expect(confirmSpy).not.toHaveBeenCalled();
    expect(deleteCalls).toEqual([]);
    // Row still present.
    expect(await findRow('rule-a')).toBeInTheDocument();
  });

  it('Given the confirm Modal is open, When the destructive Delete is clicked, Then the rule is deleted and disappears', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm');
    rules = [
      ruleFixture({ id: 'rule-a', name: 'Nightly sync' }),
      ruleFixture({ id: 'rule-b', name: 'Hourly report' }),
    ];

    renderPage();
    const user = userEvent.setup();

    const row = await findRow('rule-a');
    await user.click(within(row).getByTestId('automation-rule-delete-btn'));

    const dialog = await screen.findByRole('dialog');
    // The destructive confirm button (distinct from the Cancel button).
    await user.click(
      within(dialog).getByTestId('automation-rule-delete-confirm-btn'),
    );

    // Real DELETE fired for the chosen rule only.
    await waitFor(() => expect(deleteCalls).toEqual(['rule-a']));
    expect(confirmSpy).not.toHaveBeenCalled();

    // Row gone after the list refetches; the other rule survives.
    await waitFor(() => {
      const list = screen.getByTestId('automation-rules-list');
      const ids = within(list)
        .getAllByTestId('automation-rule-row')
        .map((el) => el.getAttribute('data-rule-id'));
      expect(ids).toEqual(['rule-b']);
    });

    // Modal closed.
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });
});
