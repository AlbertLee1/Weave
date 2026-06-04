import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { ActionConsolePage } from '../ActionConsolePage';
import * as ontologiesApi from '../../../api/ontologies';
import * as objectsApi from '../../../api/objects';
import type { ActionType } from '../../../api/types';

// a11y BDD — the Action Console is a standalone route page and must expose
// exactly one page-level <h1> for screen-reader landmark/heading navigation,
// matching ExplorerPage / ThreadsPage. The visible panel headings stay <h2>;
// the <h1> is visually hidden (sr-only) so the layout is unchanged.

const fakeAction: ActionType = {
  rid: 'ri.action.main.action-type.update-emp',
  apiName: 'updateEmployee',
  displayName: 'Update Employee',
  description: 'Change employee info',
  status: 'ACTIVE',
  parameters: {
    newName: {
      dataType: { type: 'string' },
      required: true,
    },
  },
  operations: [],
} as unknown as ActionType;

function renderPage(initialEntry = '/actions/default') {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialEntry]}>
        <Routes>
          <Route path="/actions/:ontology" element={<ActionConsolePage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BDD: ActionConsolePage page-level h1 (a11y)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(ontologiesApi, 'listActionTypes').mockResolvedValue([fakeAction]);
    vi.spyOn(objectsApi, 'getObjectActivity').mockResolvedValue({ data: [] });
  });

  // Given the Action Console route is rendered (no action selected yet)
  // When the page mounts
  // Then it exposes exactly one level-1 heading named "Action Console"
  it('renders exactly one h1 named /action console/i on mount', async () => {
    renderPage();

    const headings = await screen.findAllByRole('heading', { level: 1 });
    expect(headings).toHaveLength(1);
    expect(headings[0]).toHaveAccessibleName(/action console/i);
  });

  // Given an action is selected (the right panel switches to the form view)
  // When the form view is shown
  // Then there is still exactly one page-level h1 (the panel headings are h2)
  it('keeps exactly one h1 after an action is selected', async () => {
    renderPage();

    fireEvent.click(await screen.findByText('updateEmployee'));

    const headings = screen.getAllByRole('heading', { level: 1 });
    expect(headings).toHaveLength(1);
    expect(headings[0]).toHaveAccessibleName(/action console/i);
  });
});
