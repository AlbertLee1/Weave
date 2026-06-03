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
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AutomationRulesPage } from '../AutomationRulesPage';
import type { AutomationRule } from '../../../api/automationRules';

// Unit 11 — Automation rule "disabled" state UX fix.
//
// The pause/resume toggle previously fired `resume` for ANY non-active
// rule, including `status: 'disabled'`. The backend resume handler
// (pkg/oms/handlers_automation.go) does NOT block 'disabled' and
// unconditionally flips status to 'active', silently re-enabling a rule
// the operator intentionally disabled. There is no separate "enable"
// backend path, so the honest fix is: disable the toggle for disabled
// rules + explain why, and never let a click reach the resume endpoint.

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
let pauseCalls: string[] = [];
let resumeCalls: string[] = [];

const server = setupServer(
  http.get('/api/v2/ontologies/:ontology/automationRules', () =>
    HttpResponse.json({ data: rules }),
  ),
  http.post(
    '/api/v2/ontologies/:ontology/automationRules/:ruleId/pause',
    ({ params }) => {
      const ruleId = String(params.ruleId);
      pauseCalls.push(ruleId);
      const rule = rules.find((r) => r.id === ruleId);
      return HttpResponse.json({ ...(rule ?? ruleFixture({ id: ruleId })), status: 'paused' });
    },
  ),
  http.post(
    '/api/v2/ontologies/:ontology/automationRules/:ruleId/resume',
    ({ params }) => {
      const ruleId = String(params.ruleId);
      resumeCalls.push(ruleId);
      const rule = rules.find((r) => r.id === ruleId);
      return HttpResponse.json({ ...(rule ?? ruleFixture({ id: ruleId })), status: 'active' });
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

describe('AutomationRulesPage disabled-rule toggle UX (Unit 11)', () => {
  beforeAll(() => server.listen());
  beforeEach(() => {
    rules = [];
    pauseCalls = [];
    resumeCalls = [];
  });
  afterEach(() => {
    server.resetHandlers();
    vi.restoreAllMocks();
  });
  afterAll(() => server.close());

  it('Given a disabled rule, the toggle is disabled, labelled "Disabled", and clicking it does NOT call resume', async () => {
    rules = [ruleFixture({ id: 'rule-disabled', name: 'Archived job', status: 'disabled' })];

    renderPage();

    const row = await findRow('rule-disabled');
    const toggle = within(row).getByTestId('automation-rule-toggle-btn');

    // The button must be disabled so a click can never fire resume.
    expect(toggle).toBeDisabled();
    // The label must NOT mislead the operator with "Resume".
    expect(toggle).not.toHaveTextContent(/resume/i);
    expect(toggle).toHaveTextContent(/disabled/i);
    // An explanatory tooltip/title clarifies why it cannot be toggled.
    expect(toggle).toHaveAttribute('title', expect.stringMatching(/disabled/i));

    // Attempting to click must not reach the resume endpoint.
    fireEvent.click(toggle);
    await new Promise((r) => setTimeout(r, 30));
    expect(resumeCalls).toEqual([]);
    expect(pauseCalls).toEqual([]);
  });

  it('Given an active rule, the toggle is labelled "Pause" and clicking it calls pause', async () => {
    rules = [ruleFixture({ id: 'rule-active', name: 'Live rule', status: 'active' })];

    renderPage();

    const row = await findRow('rule-active');
    const toggle = within(row).getByTestId('automation-rule-toggle-btn');

    expect(toggle).toBeEnabled();
    expect(toggle).toHaveTextContent(/pause/i);

    fireEvent.click(toggle);
    await waitFor(() => expect(pauseCalls).toEqual(['rule-active']));
    expect(resumeCalls).toEqual([]);
  });

  it('Given a paused rule, the toggle is labelled "Resume" and clicking it calls resume', async () => {
    rules = [ruleFixture({ id: 'rule-paused', name: 'Snoozed rule', status: 'paused' })];

    renderPage();

    const row = await findRow('rule-paused');
    const toggle = within(row).getByTestId('automation-rule-toggle-btn');

    expect(toggle).toBeEnabled();
    expect(toggle).toHaveTextContent(/resume/i);

    fireEvent.click(toggle);
    await waitFor(() => expect(resumeCalls).toEqual(['rule-paused']));
    expect(pauseCalls).toEqual([]);
  });
});
