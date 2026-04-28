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
import { ObjectActivityPanel } from '../ObjectActivityPanel';
import * as api from '../../../api/objects';
import type {
  ObjectActivityEntry,
  ObjectActivityResponse,
} from '../../../api/types';

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

function event(
  version: number,
  editType: ObjectActivityEntry['editType'] = 'MODIFY',
  overrides: Partial<ObjectActivityEntry> = {},
): ObjectActivityEntry {
  return {
    id: `id-${version}`,
    objectTypeRid: 'ri.ontology.main.object-type.employee',
    primaryKey: 'emp1',
    version,
    editType,
    userId: `user-${version}`,
    source: 'user',
    recordedAt: '2026-04-28T10:00:00Z',
    ...overrides,
  };
}

describe('ObjectActivityPanel', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders empty state when there is no activity', async () => {
    vi.spyOn(api, 'getObjectActivity').mockResolvedValue({
      data: [],
    });

    renderWithProviders(
      <ObjectActivityPanel
        ontologyApiName="main"
        objectType="employee"
        primaryKey="emp1"
      />,
    );

    await waitFor(() =>
      expect(screen.getByTestId('activity-empty')).toBeInTheDocument(),
    );
  });

  it('renders rows ordered by version DESC', async () => {
    const rows = [event(3, 'MODIFY'), event(2, 'CREATE'), event(1, 'CREATE')];
    vi.spyOn(api, 'getObjectActivity').mockResolvedValue({
      data: rows,
    });

    renderWithProviders(
      <ObjectActivityPanel
        ontologyApiName="main"
        objectType="employee"
        primaryKey="emp1"
      />,
    );

    const renderedRows = await screen.findAllByTestId('activity-row');
    expect(renderedRows).toHaveLength(3);
    // First row should reference v3
    expect(renderedRows[0].textContent).toContain('v3');
    expect(renderedRows[2].textContent).toContain('v1');
  });

  it('shows surface badge appropriate to edit type', async () => {
    vi.spyOn(api, 'getObjectActivity').mockResolvedValue({
      data: [event(2, 'DELETE')],
    });

    renderWithProviders(
      <ObjectActivityPanel
        ontologyApiName="main"
        objectType="employee"
        primaryKey="emp1"
      />,
    );

    await waitFor(() => screen.getByTestId('activity-row'));
    expect(screen.getByText('DELETE')).toBeInTheDocument();
  });

  it('paginates with Load more when nextPageToken is set', async () => {
    const page1: ObjectActivityResponse = {
      data: [event(5), event(4)],
      nextPageToken: 'NA==',
    };
    const page2: ObjectActivityResponse = {
      data: [event(3), event(2)],
    };
    const spy = vi
      .spyOn(api, 'getObjectActivity')
      .mockResolvedValueOnce(page1)
      .mockResolvedValueOnce(page2);

    renderWithProviders(
      <ObjectActivityPanel
        ontologyApiName="main"
        objectType="employee"
        primaryKey="emp1"
      />,
    );

    // First page renders, Load more button is visible.
    const loadMore = await screen.findByTestId('activity-load-more');
    await act(async () => {
      fireEvent.click(loadMore);
    });

    await waitFor(() =>
      expect(screen.getAllByTestId('activity-row')).toHaveLength(4),
    );
    expect(spy).toHaveBeenCalledTimes(2);
    // Second call must forward the cursor from page1.
    expect(spy.mock.calls[1][0]).toMatchObject({
      pageToken: 'NA==',
    });
    // After the final page renders, Load more must disappear.
    expect(screen.queryByTestId('activity-load-more')).toBeNull();
  });

  it('renders error state when the API rejects', async () => {
    vi.spyOn(api, 'getObjectActivity').mockRejectedValue(new Error('boom'));
    renderWithProviders(
      <ObjectActivityPanel
        ontologyApiName="main"
        objectType="employee"
        primaryKey="emp1"
      />,
    );
    await waitFor(() =>
      expect(screen.getByTestId('activity-error')).toBeInTheDocument(),
    );
  });
});
