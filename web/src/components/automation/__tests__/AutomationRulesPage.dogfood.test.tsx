import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { AutomationRulesPage } from '../AutomationRulesPage';
import * as automationApi from '../../../api/automationRules';

function renderPage(initial = '/automation/default') {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
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

describe('AutomationRulesPage empty-state CTA (dogfood #6)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders a "New rule" CTA inside the EmptyState when no rules exist', async () => {
    vi.spyOn(automationApi, 'listAutomationRules').mockResolvedValue({ data: [] });

    renderPage();

    const emptyBlock = await screen.findByTestId('automation-rules-empty');
    // The CTA must live inside the empty state so users can act without
    // hunting for the header button.
    const cta = within(emptyBlock).getByRole('button', { name: /new rule/i });
    expect(cta).toBeInTheDocument();
  });
});
