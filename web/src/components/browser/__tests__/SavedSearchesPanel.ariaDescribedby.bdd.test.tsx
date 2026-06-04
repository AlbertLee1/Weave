import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createElement } from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { SavedSearchesPanel } from '../SavedSearchesPanel';
import * as api from '../../../api/savedSearches';
import type { SavedSearch } from '../../../api/savedSearches';

// BDD: the saved-search name field must be programmatically associated with
// its duplicate-name error so screen-reader users who are told the field is
// invalid (`aria-invalid`) can also reach the description of *why*.
//
// Given the Save dialog and an existing saved search named "Apples",
// When the operator types the colliding name "Apples",
// Then the name input exposes `aria-describedby` pointing at the alert <p>'s
//   stable id, and that <p> exists with the matching id.
// And Given a non-colliding name,
// Then the input exposes no `aria-describedby` (no dangling reference).

function makeWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
  return ({ children }: { children: React.ReactNode }) =>
    createElement(QueryClientProvider, { client: queryClient }, children);
}

function renderWithProviders(ui: React.ReactElement) {
  const Wrapper = makeWrapper();
  return render(ui, { wrapper: Wrapper });
}

const ROW: SavedSearch = {
  id: 'row-1',
  name: 'Apples',
  ontology: 'main',
  objectType: 'produce',
  createdBy: 'user:alice',
  definition: { searchText: 'apples' },
  createdAt: '2026-04-28T00:00:00Z',
  updatedAt: '2026-04-28T00:00:00Z',
};

describe('SavedSearchesPanel name field aria-describedby association', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(api, 'listSavedSearches').mockResolvedValue({
      savedSearches: [ROW],
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  async function openDialog(user: ReturnType<typeof userEvent.setup>) {
    renderWithProviders(
      <SavedSearchesPanel
        ontology="main"
        objectType="produce"
        currentDefinition={{ searchText: 'pears' }}
        hasCurrentState={true}
        onLoad={() => {}}
      />,
    );
    // Wait for the initial list load so existingNames is populated.
    await screen.findByTestId(`saved-search-${ROW.id}`);
    await user.click(screen.getByTestId('saved-searches-save'));
    return screen.findByTestId('saved-searches-name-input');
  }

  it('links the input to the duplicate warning via aria-describedby when names collide', async () => {
    const user = userEvent.setup();
    const input = await openDialog(user);

    // When: type the colliding name.
    await user.type(input, ROW.name);

    // Then: the duplicate warning is shown and carries a stable id.
    const warning = await screen.findByTestId(
      'saved-searches-duplicate-warning',
    );
    const warningId = warning.getAttribute('id');
    expect(warningId).toBeTruthy();
    expect(warning).toHaveAttribute('role', 'alert');

    // And: the input points at exactly that id, and is marked invalid.
    await waitFor(() => {
      expect(input).toHaveAttribute('aria-describedby', warningId as string);
    });
    expect(input).toHaveAttribute('aria-invalid', 'true');
  });

  it('exposes no aria-describedby when the name does not collide', async () => {
    const user = userEvent.setup();
    const input = await openDialog(user);

    // When: type a unique, non-colliding name.
    await user.type(input, 'Bananas');

    // Then: no duplicate warning and no dangling aria-describedby reference.
    expect(
      screen.queryByTestId('saved-searches-duplicate-warning'),
    ).not.toBeInTheDocument();
    expect(input).not.toHaveAttribute('aria-describedby');
    expect(input).not.toHaveAttribute('aria-invalid');
  });
});
