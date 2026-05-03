import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { renderHook, act } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createElement } from 'react';

// US-395: App ObjectSet 数据绑定 — Table component binds an ObjectSet
// (RID or base ObjectType API name) + columns and renders live data
// with pagination + sorting + filtering. The data path goes through a
// new useAppObjectSet hook that wraps the existing useLoadObjectSet
// query.

const apiMocks = vi.hoisted(() => ({
  listApps: vi.fn(),
  getApp: vi.fn(),
  createApp: vi.fn(),
  updateApp: vi.fn(),
  deleteApp: vi.fn(),
  listAppVersions: vi.fn(),
}));

const objectsetsMocks = vi.hoisted(() => ({
  loadObjectSet: vi.fn(),
  aggregateObjectSet: vi.fn(),
  createTemporaryObjectSet: vi.fn(),
  loadLinks: vi.fn(),
  getObjectSet: vi.fn(),
}));

const ontologyMocks = vi.hoisted(() => {
  const state: { selectedOntology: string | null } = {
    selectedOntology: 'northwind',
  };
  return {
    state,
    useOntologyStore: vi.fn((selector?: unknown) => {
      if (typeof selector === 'function') {
        return (selector as (s: typeof state) => unknown)(state);
      }
      return state;
    }),
  };
});

vi.mock('../../../api/apps', () => apiMocks);
vi.mock('../../../api/objectsets', () => objectsetsMocks);
vi.mock('../../../stores/ontologyStore', () => ({
  useOntologyStore: ontologyMocks.useOntologyStore,
}));

import { AppEditorPage } from '../AppEditorPage';
import {
  buildWhereClause,
  coercePageSize,
  readTableProps,
  resolveObjectSetDefinition,
  TABLE_FILTER_OPS,
  useAppObjectSet,
} from '../useAppObjectSet';

beforeEach(() => {
  for (const m of Object.values(apiMocks)) m.mockReset();
  apiMocks.listApps.mockResolvedValue({ apps: [] });
  apiMocks.getApp.mockRejectedValue(new Error('not configured'));
  for (const m of Object.values(objectsetsMocks)) m.mockReset();
  ontologyMocks.state.selectedOntology = 'northwind';
});

function makeWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return ({ children }: { children: React.ReactNode }) =>
    createElement(QueryClientProvider, { client: queryClient }, children);
}

describe('US-395 helpers', () => {
  describe('resolveObjectSetDefinition', () => {
    it('returns null for blank input', () => {
      expect(resolveObjectSetDefinition('')).toBeNull();
      expect(resolveObjectSetDefinition('   ')).toBeNull();
      expect(resolveObjectSetDefinition(undefined)).toBeNull();
    });

    it('treats ri.objectSet.* as a reference', () => {
      expect(resolveObjectSetDefinition('ri.objectSet.x')).toEqual({
        type: 'reference',
        reference: 'ri.objectSet.x',
      });
    });

    it('treats plain strings as base ObjectType', () => {
      expect(resolveObjectSetDefinition('Customer')).toEqual({
        type: 'base',
        objectType: 'Customer',
      });
    });
  });

  describe('coercePageSize', () => {
    it('falls back to 25 on garbage', () => {
      expect(coercePageSize(undefined)).toBe(25);
      expect(coercePageSize('')).toBe(25);
      expect(coercePageSize('abc')).toBe(25);
      expect(coercePageSize(0)).toBe(25);
      expect(coercePageSize(-1)).toBe(25);
    });

    it('accepts numbers and stringified numbers', () => {
      expect(coercePageSize(50)).toBe(50);
      expect(coercePageSize('100')).toBe(100);
      expect(coercePageSize(7.7)).toBe(7);
    });
  });

  describe('buildWhereClause', () => {
    it('drops empty filters', () => {
      expect(buildWhereClause(undefined, 'eq', 'x')).toBeNull();
      expect(buildWhereClause('name', 'eq', undefined)).toBeNull();
      expect(buildWhereClause('name', 'eq', '')).toBeNull();
    });

    it('builds the standard wire shape', () => {
      expect(buildWhereClause('name', 'contains', 'foo')).toEqual({
        type: 'contains',
        field: 'name',
        value: 'foo',
      });
    });

    it('exposes the canonical op set', () => {
      expect(TABLE_FILTER_OPS).toEqual([
        'eq',
        'neq',
        'gt',
        'gte',
        'lt',
        'lte',
        'contains',
      ]);
    });
  });

  describe('readTableProps', () => {
    it('coerces missing fields to safe defaults', () => {
      const out = readTableProps({});
      expect(out.objectSet).toBeUndefined();
      expect(out.columns).toBeUndefined();
      expect(out.orderByDirection).toBe('asc');
      expect(out.filterOp).toBe('eq');
    });

    it('reads valid props through verbatim', () => {
      const out = readTableProps({
        objectSet: 'Customer',
        columns: ['name', 'email'],
        pageSize: 50,
        orderByField: 'name',
        orderByDirection: 'desc',
        filterField: 'country',
        filterOp: 'eq',
        filterValue: 'US',
      });
      expect(out.objectSet).toBe('Customer');
      expect(out.columns).toEqual(['name', 'email']);
      expect(out.pageSize).toBe(50);
      expect(out.orderByField).toBe('name');
      expect(out.orderByDirection).toBe('desc');
      expect(out.filterField).toBe('country');
      expect(out.filterOp).toBe('eq');
      expect(out.filterValue).toBe('US');
    });

    it('drops blank columns from the column list', () => {
      const out = readTableProps({ columns: ['name', '', 'email', 42] });
      expect(out.columns).toEqual(['name', 'email']);
    });

    it('falls back to eq for unknown filter ops', () => {
      const out = readTableProps({ filterOp: 'matches' });
      expect(out.filterOp).toBe('eq');
    });
  });
});

describe('US-395 useAppObjectSet', () => {
  it('disables the query when ontology is null', async () => {
    ontologyMocks.state.selectedOntology = null;
    const { result } = renderHook(
      () =>
        useAppObjectSet({
          ontologyApiName: null,
          props: { objectSet: 'Customer', columns: ['name'] },
          state: {},
        }),
      { wrapper: makeWrapper() },
    );
    expect(result.current.enabled).toBe(false);
    expect(objectsetsMocks.loadObjectSet).not.toHaveBeenCalled();
  });

  it('disables the query when columns are empty', async () => {
    const { result } = renderHook(
      () =>
        useAppObjectSet({
          ontologyApiName: 'northwind',
          props: { objectSet: 'Customer', columns: [] },
          state: {},
        }),
      { wrapper: makeWrapper() },
    );
    expect(result.current.enabled).toBe(true);
    // The query is gated on a non-empty select list, so still no fetch.
    expect(objectsetsMocks.loadObjectSet).not.toHaveBeenCalled();
  });

  it('builds a base ObjectSet definition for plain strings', async () => {
    objectsetsMocks.loadObjectSet.mockResolvedValue({
      data: [
        { __rid: 'ri.1', __primaryKey: '1', __apiName: 'Customer', name: 'Acme' },
      ],
      totalCount: '1',
    });
    const { result } = renderHook(
      () =>
        useAppObjectSet({
          ontologyApiName: 'northwind',
          props: { objectSet: 'Customer', columns: ['name'] },
          state: {},
        }),
      { wrapper: makeWrapper() },
    );
    await waitFor(() => expect(result.current.data).toHaveLength(1));
    expect(objectsetsMocks.loadObjectSet).toHaveBeenCalledWith(
      'northwind',
      expect.objectContaining({
        objectSet: { type: 'base', objectType: 'Customer' },
        select: ['name'],
        pageSize: 25,
      }),
    );
  });

  it('wraps a FilterObjectSet around the base when filterField + value present', async () => {
    objectsetsMocks.loadObjectSet.mockResolvedValue({
      data: [],
      totalCount: '0',
    });
    renderHook(
      () =>
        useAppObjectSet({
          ontologyApiName: 'northwind',
          props: {
            objectSet: 'Customer',
            columns: ['name'],
            filterField: 'country',
            filterOp: 'eq',
            filterValue: 'US',
          },
          state: {},
        }),
      { wrapper: makeWrapper() },
    );
    await waitFor(() =>
      expect(objectsetsMocks.loadObjectSet).toHaveBeenCalled(),
    );
    const call = objectsetsMocks.loadObjectSet.mock.calls[0][1];
    expect(call.objectSet).toEqual({
      type: 'filter',
      objectSet: { type: 'base', objectType: 'Customer' },
      where: { type: 'eq', field: 'country', value: 'US' },
    });
  });

  it('substitutes {{var}} references in filterValue at runtime', async () => {
    objectsetsMocks.loadObjectSet.mockResolvedValue({
      data: [],
      totalCount: '0',
    });
    renderHook(
      () =>
        useAppObjectSet({
          ontologyApiName: 'northwind',
          props: {
            objectSet: 'Customer',
            columns: ['name'],
            filterField: 'country',
            filterOp: 'eq',
            filterValue: '{{country}}',
          },
          state: { country: 'JP' },
        }),
      { wrapper: makeWrapper() },
    );
    await waitFor(() =>
      expect(objectsetsMocks.loadObjectSet).toHaveBeenCalled(),
    );
    const call = objectsetsMocks.loadObjectSet.mock.calls[0][1];
    expect(call.objectSet.where.value).toBe('JP');
  });

  it('threads orderBy from author config', async () => {
    objectsetsMocks.loadObjectSet.mockResolvedValue({
      data: [],
      totalCount: '0',
    });
    renderHook(
      () =>
        useAppObjectSet({
          ontologyApiName: 'northwind',
          props: {
            objectSet: 'Customer',
            columns: ['name'],
            orderByField: 'name',
            orderByDirection: 'desc',
          },
          state: {},
        }),
      { wrapper: makeWrapper() },
    );
    await waitFor(() =>
      expect(objectsetsMocks.loadObjectSet).toHaveBeenCalled(),
    );
    const call = objectsetsMocks.loadObjectSet.mock.calls[0][1];
    expect(call.orderBy).toEqual({
      fields: [{ field: 'name', direction: 'desc' }],
    });
  });

  it('runtime sortOverride wins over author config', async () => {
    objectsetsMocks.loadObjectSet.mockResolvedValue({
      data: [],
      totalCount: '0',
    });
    renderHook(
      () =>
        useAppObjectSet({
          ontologyApiName: 'northwind',
          props: {
            objectSet: 'Customer',
            columns: ['name', 'email'],
            orderByField: 'name',
            orderByDirection: 'asc',
          },
          state: {},
          sortOverride: { field: 'email', direction: 'desc' },
        }),
      { wrapper: makeWrapper() },
    );
    await waitFor(() =>
      expect(objectsetsMocks.loadObjectSet).toHaveBeenCalled(),
    );
    const call = objectsetsMocks.loadObjectSet.mock.calls[0][1];
    expect(call.orderBy).toEqual({
      fields: [{ field: 'email', direction: 'desc' }],
    });
  });

  it('paginates: goNext appends server pageToken; goPrev pops', async () => {
    let call = 0;
    objectsetsMocks.loadObjectSet.mockImplementation(async () => {
      call += 1;
      if (call === 1) {
        return {
          data: [
            {
              __rid: 'ri.1',
              __primaryKey: '1',
              __apiName: 'Customer',
              name: 'A',
            },
          ],
          nextPageToken: 'tok-2',
          totalCount: '3',
        };
      }
      if (call === 2) {
        return {
          data: [
            {
              __rid: 'ri.2',
              __primaryKey: '2',
              __apiName: 'Customer',
              name: 'B',
            },
          ],
          nextPageToken: undefined,
          totalCount: '3',
        };
      }
      return { data: [], totalCount: '3' };
    });

    const { result } = renderHook(
      () =>
        useAppObjectSet({
          ontologyApiName: 'northwind',
          props: { objectSet: 'Customer', columns: ['name'], pageSize: 1 },
          state: {},
        }),
      { wrapper: makeWrapper() },
    );

    await waitFor(() => expect(result.current.data).toHaveLength(1));
    expect(result.current.pageIndex).toBe(0);
    expect(result.current.hasNextPage).toBe(true);
    expect(result.current.hasPrevPage).toBe(false);
    expect(result.current.totalCount).toBe(3);

    await act(async () => {
      result.current.goNext();
    });
    await waitFor(() => expect(result.current.pageIndex).toBe(1));
    expect(objectsetsMocks.loadObjectSet).toHaveBeenLastCalledWith(
      'northwind',
      expect.objectContaining({ pageToken: 'tok-2' }),
    );
    expect(result.current.hasPrevPage).toBe(true);
    expect(result.current.hasNextPage).toBe(false);

    await act(async () => {
      result.current.goPrev();
    });
    await waitFor(() => expect(result.current.pageIndex).toBe(0));
    expect(result.current.hasPrevPage).toBe(false);
  });
});

describe('US-395 Table runtime + property panel', () => {
  function selectTableComponent() {
    fireEvent.click(screen.getByTestId('app-palette-item-table'));
  }

  function configureTable(overrides: Record<string, string> = {}) {
    fireEvent.change(screen.getByTestId('prop-table-objectSet'), {
      target: { value: overrides.objectSet ?? 'Customer' },
    });
    fireEvent.change(screen.getByTestId('prop-table-columns'), {
      target: { value: overrides.columns ?? 'name, email' },
    });
  }

  function renderEditor() {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false, refetchInterval: false } },
    });
    return render(
      <QueryClientProvider client={qc}>
        <AppEditorPage />
      </QueryClientProvider>,
    );
  }

  it('property panel exposes pageSize / sort / filter inputs', () => {
    renderEditor();
    selectTableComponent();
    expect(screen.getByTestId('prop-table-pageSize')).toBeInTheDocument();
    expect(screen.getByTestId('prop-table-orderByField')).toBeInTheDocument();
    expect(
      screen.getByTestId('prop-table-orderByDirection'),
    ).toBeInTheDocument();
    expect(screen.getByTestId('prop-table-filterField')).toBeInTheDocument();
    expect(screen.getByTestId('prop-table-filterOp')).toBeInTheDocument();
    expect(screen.getByTestId('prop-table-filterValue')).toBeInTheDocument();
  });

  it('renders unbound state when ObjectSet is blank', () => {
    renderEditor();
    selectTableComponent();
    fireEvent.click(screen.getByTestId('app-mode-toggle'));
    expect(
      screen.getByTestId('app-runtime-table'),
    ).toHaveAttribute('data-table-state', 'unbound');
  });

  it('Preview mode fetches data via loadObjectSet and renders rows + pagination', async () => {
    objectsetsMocks.loadObjectSet.mockResolvedValue({
      data: [
        {
          __rid: 'ri.1',
          __primaryKey: '1',
          __apiName: 'Customer',
          name: 'Acme',
          email: 'a@a',
        },
        {
          __rid: 'ri.2',
          __primaryKey: '2',
          __apiName: 'Customer',
          name: 'Beta',
          email: 'b@b',
        },
      ],
      totalCount: '2',
    });
    renderEditor();
    selectTableComponent();
    configureTable();
    fireEvent.click(screen.getByTestId('app-mode-toggle'));

    await waitFor(() =>
      expect(screen.getAllByTestId('app-runtime-table-row')).toHaveLength(2),
    );
    const nameCells = screen.getAllByTestId('app-runtime-table-cell-name');
    expect(nameCells[0]).toHaveTextContent('Acme');
    expect(nameCells[1]).toHaveTextContent('Beta');
    expect(screen.getByTestId('app-runtime-table-page-label')).toHaveTextContent(
      'Page 1',
    );
    expect(screen.getByTestId('app-runtime-table-total')).toHaveTextContent(
      'Total 2',
    );
    // Prev disabled at page 1; Next disabled when no nextPageToken.
    expect(screen.getByTestId('app-runtime-table-prev')).toBeDisabled();
    expect(screen.getByTestId('app-runtime-table-next')).toBeDisabled();
  });

  it('column header click toggles sort direction and refetches with orderBy', async () => {
    objectsetsMocks.loadObjectSet.mockResolvedValue({
      data: [],
      totalCount: '0',
    });
    renderEditor();
    selectTableComponent();
    configureTable();
    fireEvent.click(screen.getByTestId('app-mode-toggle'));

    await waitFor(() =>
      expect(screen.getByTestId('app-runtime-table-header-name')).toBeInTheDocument(),
    );
    objectsetsMocks.loadObjectSet.mockClear();

    fireEvent.click(screen.getByTestId('app-runtime-table-header-name'));
    await waitFor(() =>
      expect(objectsetsMocks.loadObjectSet).toHaveBeenCalled(),
    );
    let lastCall =
      objectsetsMocks.loadObjectSet.mock.calls[
        objectsetsMocks.loadObjectSet.mock.calls.length - 1
      ][1];
    expect(lastCall.orderBy).toEqual({
      fields: [{ field: 'name', direction: 'asc' }],
    });

    objectsetsMocks.loadObjectSet.mockClear();
    fireEvent.click(screen.getByTestId('app-runtime-table-header-name'));
    await waitFor(() =>
      expect(objectsetsMocks.loadObjectSet).toHaveBeenCalled(),
    );
    lastCall =
      objectsetsMocks.loadObjectSet.mock.calls[
        objectsetsMocks.loadObjectSet.mock.calls.length - 1
      ][1];
    expect(lastCall.orderBy).toEqual({
      fields: [{ field: 'name', direction: 'desc' }],
    });
  });

  it('renders a clear hint when no ontology is selected', () => {
    ontologyMocks.state.selectedOntology = null;
    renderEditor();
    selectTableComponent();
    configureTable();
    fireEvent.click(screen.getByTestId('app-mode-toggle'));
    expect(screen.getByTestId('app-runtime-table')).toHaveAttribute(
      'data-table-state',
      'no-ontology',
    );
  });

  it('next page button uses returned pageToken', async () => {
    let call = 0;
    objectsetsMocks.loadObjectSet.mockImplementation(async () => {
      call += 1;
      if (call === 1) {
        return {
          data: [
            {
              __rid: 'ri.1',
              __primaryKey: '1',
              __apiName: 'Customer',
              name: 'A',
              email: 'a@a',
            },
          ],
          nextPageToken: 'PAGE-2',
          totalCount: '2',
        };
      }
      return {
        data: [
          {
            __rid: 'ri.2',
            __primaryKey: '2',
            __apiName: 'Customer',
            name: 'B',
            email: 'b@b',
          },
        ],
        totalCount: '2',
      };
    });
    renderEditor();
    selectTableComponent();
    configureTable();
    fireEvent.change(screen.getByTestId('prop-table-pageSize'), {
      target: { value: '1' },
    });
    fireEvent.click(screen.getByTestId('app-mode-toggle'));

    await waitFor(() =>
      expect(screen.getAllByTestId('app-runtime-table-row')).toHaveLength(1),
    );
    expect(screen.getByTestId('app-runtime-table-next')).not.toBeDisabled();

    fireEvent.click(screen.getByTestId('app-runtime-table-next'));

    await waitFor(() =>
      expect(screen.getByTestId('app-runtime-table-page-label')).toHaveTextContent(
        'Page 2',
      ),
    );
    expect(objectsetsMocks.loadObjectSet).toHaveBeenLastCalledWith(
      'northwind',
      expect.objectContaining({ pageToken: 'PAGE-2' }),
    );
  });
});
