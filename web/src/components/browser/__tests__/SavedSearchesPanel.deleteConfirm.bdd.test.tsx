import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createElement } from 'react';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { SavedSearchesPanel } from '../SavedSearchesPanel';
import * as api from '../../../api/savedSearches';
import type { SavedSearch } from '../../../api/savedSearches';

// BDD: deleting a saved search must go through the styled shared Modal
// confirmation dialog — NOT the native, unstyleable window.confirm.
//
// Given the Saved Searches panel listing saved entries,
// When the operator clicks a row's "Delete" (×) button,
// Then window.confirm is NOT invoked and a styled Modal confirmation appears.
//   - Clicking "Cancel" closes the Modal and deletes nothing.
//   - Only after clicking the Modal's destructive "Delete" button is the
//     delete API called and the row removed from the list.

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

describe('SavedSearchesPanel delete confirmation (styled Modal, no window.confirm)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('opens a styled Modal instead of calling window.confirm when Delete is clicked', async () => {
    const user = userEvent.setup();
    // Spy on window.confirm so we can prove it is never invoked.
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    vi.spyOn(api, 'listSavedSearches').mockResolvedValue({
      savedSearches: [ROW],
    });
    const deleteSpy = vi
      .spyOn(api, 'deleteSavedSearch')
      .mockResolvedValue(undefined as void);

    renderWithProviders(
      <SavedSearchesPanel
        ontology="main"
        objectType="produce"
        currentDefinition={{}}
        hasCurrentState={false}
        onLoad={() => {}}
      />,
    );

    const delBtn = await screen.findByTestId(`saved-search-delete-${ROW.id}`);
    await user.click(delBtn);

    // Then: native confirm is never used, and a styled Modal is shown.
    expect(confirmSpy).not.toHaveBeenCalled();
    const overlay = await screen.findByTestId('modal-overlay');
    expect(within(overlay).getByText(/cannot be undone/i)).toBeInTheDocument();
    // The row's name is surfaced in the confirmation copy.
    expect(within(overlay).getByText(/Apples/)).toBeInTheDocument();
    // The destructive confirm action is present in the dialog.
    expect(
      within(overlay).getByTestId('confirm-delete-saved-search'),
    ).toBeInTheDocument();
    // No delete has happened yet — confirmation is still pending.
    expect(deleteSpy).not.toHaveBeenCalled();
  });

  it('cancels without deleting when the Modal Cancel button is clicked', async () => {
    const user = userEvent.setup();
    vi.spyOn(api, 'listSavedSearches').mockResolvedValue({
      savedSearches: [ROW],
    });
    const deleteSpy = vi
      .spyOn(api, 'deleteSavedSearch')
      .mockResolvedValue(undefined as void);

    renderWithProviders(
      <SavedSearchesPanel
        ontology="main"
        objectType="produce"
        currentDefinition={{}}
        hasCurrentState={false}
        onLoad={() => {}}
      />,
    );

    const delBtn = await screen.findByTestId(`saved-search-delete-${ROW.id}`);
    await user.click(delBtn);

    const overlay = await screen.findByTestId('modal-overlay');
    await user.click(within(overlay).getByRole('button', { name: /cancel/i }));

    // Modal closes, nothing deleted, the row is still present.
    await waitFor(() => {
      expect(screen.queryByTestId('modal-overlay')).not.toBeInTheDocument();
    });
    expect(deleteSpy).not.toHaveBeenCalled();
    expect(screen.getByTestId(`saved-search-${ROW.id}`)).toBeInTheDocument();
  });

  it('deletes the saved search only after confirming in the Modal', async () => {
    const user = userEvent.setup();
    // First load returns the row; after delete the list refetch returns empty.
    const listSpy = vi
      .spyOn(api, 'listSavedSearches')
      .mockResolvedValueOnce({ savedSearches: [ROW] })
      .mockResolvedValue({ savedSearches: [] });
    const deleteSpy = vi
      .spyOn(api, 'deleteSavedSearch')
      .mockResolvedValue(undefined as void);

    renderWithProviders(
      <SavedSearchesPanel
        ontology="main"
        objectType="produce"
        currentDefinition={{}}
        hasCurrentState={false}
        onLoad={() => {}}
      />,
    );

    const delBtn = await screen.findByTestId(`saved-search-delete-${ROW.id}`);
    await user.click(delBtn);

    const overlay = await screen.findByTestId('modal-overlay');
    await user.click(
      within(overlay).getByTestId('confirm-delete-saved-search'),
    );

    // The delete API is called with the chosen row id.
    await waitFor(() => {
      expect(deleteSpy).toHaveBeenCalledWith(ROW.id);
    });
    // Modal closes after a successful delete.
    await waitFor(() => {
      expect(screen.queryByTestId('modal-overlay')).not.toBeInTheDocument();
    });
    // The list refetches and the deleted row disappears.
    await waitFor(() => {
      expect(listSpy.mock.calls.length).toBeGreaterThanOrEqual(2);
    });
    await waitFor(() => {
      expect(
        screen.queryByTestId(`saved-search-${ROW.id}`),
      ).not.toBeInTheDocument();
    });
  });
});
