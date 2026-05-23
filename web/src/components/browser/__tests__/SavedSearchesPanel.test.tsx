import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createElement } from 'react';
import {
  render,
  screen,
  fireEvent,
  waitFor,
  act,
} from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { SavedSearchesPanel } from '../SavedSearchesPanel';
import * as api from '../../../api/savedSearches';
import type { SavedSearch } from '../../../api/savedSearches';

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

describe('SavedSearchesPanel', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('shows empty state when there are no saved searches', async () => {
    vi.spyOn(api, 'listSavedSearches').mockResolvedValue({
      savedSearches: [],
    });

    renderWithProviders(
      <SavedSearchesPanel
        ontology="main"
        objectType="produce"
        currentDefinition={{}}
        hasCurrentState={false}
        onLoad={() => {}}
      />,
    );

    await waitFor(() =>
      expect(screen.getByTestId('saved-searches-empty')).toBeInTheDocument(),
    );
  });

  it('renders saved rows and applies definition on click', async () => {
    vi.spyOn(api, 'listSavedSearches').mockResolvedValue({
      savedSearches: [ROW],
    });
    const onLoad = vi.fn();
    renderWithProviders(
      <SavedSearchesPanel
        ontology="main"
        objectType="produce"
        currentDefinition={{}}
        hasCurrentState={false}
        onLoad={onLoad}
      />,
    );

    const loadBtn = await screen.findByTestId(`saved-search-load-${ROW.id}`);
    fireEvent.click(loadBtn);
    expect(onLoad).toHaveBeenCalledWith(ROW);
  });

  it('marks the activeId row aria-current and renders the active badge', async () => {
    vi.spyOn(api, 'listSavedSearches').mockResolvedValue({
      savedSearches: [ROW],
    });
    renderWithProviders(
      <SavedSearchesPanel
        ontology="main"
        objectType="produce"
        currentDefinition={{}}
        hasCurrentState={false}
        onLoad={() => {}}
        activeId={ROW.id}
      />,
    );

    const row = await screen.findByTestId(`saved-search-${ROW.id}`);
    expect(row).toHaveAttribute('aria-current', 'true');
    expect(
      screen.getByTestId(`saved-search-active-badge-${ROW.id}`),
    ).toBeInTheDocument();
  });

  it('save button is disabled when there is no current state', async () => {
    vi.spyOn(api, 'listSavedSearches').mockResolvedValue({
      savedSearches: [],
    });
    renderWithProviders(
      <SavedSearchesPanel
        ontology="main"
        objectType="produce"
        currentDefinition={{}}
        hasCurrentState={false}
        onLoad={() => {}}
      />,
    );
    const btn = await screen.findByTestId('saved-searches-save');
    expect(btn).toBeDisabled();
  });

  it('save flow opens dialog, posts to API and closes', async () => {
    vi.spyOn(api, 'listSavedSearches').mockResolvedValue({
      savedSearches: [],
    });
    const created: SavedSearch = { ...ROW, id: 'row-2', name: 'My View' };
    const createSpy = vi
      .spyOn(api, 'createSavedSearch')
      .mockResolvedValue(created);

    renderWithProviders(
      <SavedSearchesPanel
        ontology="main"
        objectType="produce"
        currentDefinition={{ searchText: 'fresh' }}
        hasCurrentState={true}
        onLoad={() => {}}
      />,
    );

    const saveBtn = await screen.findByTestId('saved-searches-save');
    fireEvent.click(saveBtn);

    const input = await screen.findByTestId('saved-searches-name-input');
    await act(async () => {
      fireEvent.change(input, { target: { value: 'My View' } });
    });

    const confirm = screen.getByTestId('saved-searches-confirm');
    await act(async () => {
      fireEvent.click(confirm);
    });

    await waitFor(() => expect(createSpy).toHaveBeenCalled());
    expect(createSpy.mock.calls[0][0]).toMatchObject({
      name: 'My View',
      ontology: 'main',
      objectType: 'produce',
      definition: { searchText: 'fresh' },
    });
  });

  it('delete button calls API after window.confirm true', async () => {
    vi.spyOn(api, 'listSavedSearches').mockResolvedValue({
      savedSearches: [ROW],
    });
    const delSpy = vi
      .spyOn(api, 'deleteSavedSearch')
      .mockResolvedValue(undefined as void);
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);

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
    await act(async () => {
      fireEvent.click(delBtn);
    });
    expect(confirmSpy).toHaveBeenCalled();
    await waitFor(() => expect(delSpy).toHaveBeenCalledWith(ROW.id));
  });
});
