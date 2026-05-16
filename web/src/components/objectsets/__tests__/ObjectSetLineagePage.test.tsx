import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { ObjectSetLineagePage } from '../ObjectSetLineagePage';
import { localStorageKey } from '../../../lib/objectSetBuilder';

const ONTOLOGY = 'test';

beforeEach(() => {
  window.localStorage.clear();
});

afterEach(() => {
  window.localStorage.clear();
});

function seed(initial: unknown[]): void {
  window.localStorage.setItem(
    localStorageKey(ONTOLOGY),
    JSON.stringify(initial),
  );
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={[`/objectsets/${ONTOLOGY}/lineage`]}>
      <Routes>
        <Route
          path="/objectsets/:ontology/lineage"
          element={<ObjectSetLineagePage />}
        />
      </Routes>
    </MemoryRouter>,
  );
}

describe('ObjectSetLineagePage', () => {
  it('renders the title and an empty state when no saved sets exist', () => {
    renderPage();
    expect(
      screen.getByRole('heading', { name: /object set lineage/i }),
    ).toBeInTheDocument();
    expect(screen.getByTestId('objectset-lineage-no-saved')).toBeInTheDocument();
  });

  it('asks the user to pick a saved set when sets exist but none is selected', () => {
    seed([
      {
        id: 'sa-1',
        name: 'Engineers',
        def: { type: 'base', objectType: 'Employee' },
        createdAt: '2026-01-01T00:00:00.000Z',
        activeVersionId: 'v-1',
        versions: [
          {
            versionId: 'v-1',
            def: { type: 'base', objectType: 'Employee' },
            createdAt: '2026-01-01T00:00:00.000Z',
          },
        ],
      },
    ]);
    renderPage();
    expect(
      screen.getByTestId('objectset-lineage-pending'),
    ).toBeInTheDocument();
  });

  it('renders a tree whose leaves are base nodes and root is the outer operation', () => {
    const composite = {
      type: 'filter',
      where: { field: 'age', op: 'gt', value: 30 },
      objectSet: {
        type: 'union',
        objectSets: [
          { type: 'base', objectType: 'Employee' },
          { type: 'base', objectType: 'Contractor' },
        ],
      },
    };
    seed([
      {
        id: 'sa-1',
        name: 'Adults',
        def: composite,
        createdAt: '2026-01-01T00:00:00.000Z',
        activeVersionId: 'v-1',
        versions: [
          {
            versionId: 'v-1',
            def: composite,
            createdAt: '2026-01-01T00:00:00.000Z',
          },
        ],
      },
    ]);
    renderPage();
    fireEvent.change(screen.getByLabelText(/saved object set/i), {
      target: { value: 'sa-1' },
    });

    const tree = screen.getByTestId('objectset-lineage-tree');
    expect(tree).toBeInTheDocument();

    // 1 filter + 1 union + 2 base = 4 nodes total.
    const counts = screen.getByTestId('objectset-lineage-counts');
    expect(counts).toHaveAttribute('data-node-count', '4');

    // The tree exposes a node per operation; the two leaves are base nodes.
    const nodes = screen.getAllByTestId('objectset-lineage-tree-node');
    expect(nodes.length).toBe(4);
    const leafNodes = nodes.filter(
      (n) => n.getAttribute('data-is-leaf') === 'true',
    );
    expect(leafNodes.length).toBe(2);
    expect(
      leafNodes.every((n) => n.getAttribute('data-node-type') === 'base'),
    ).toBe(true);

    // The where clause is rendered for the filter node.
    expect(
      screen.getByTestId('objectset-lineage-node-where'),
    ).toBeInTheDocument();
  });

  it('renders derived properties on withProperties nodes', () => {
    const wp = {
      type: 'withProperties',
      objectSet: { type: 'base', objectType: 'Order' },
      derivedProperties: [
        {
          name: 'totalItems',
          link: 'orderLines',
          metric: 'sum',
          field: 'qty',
        },
      ],
    };
    seed([
      {
        id: 'sa-2',
        name: 'OrdersWithCounts',
        def: wp,
        createdAt: '2026-01-01T00:00:00.000Z',
        activeVersionId: 'v-1',
        versions: [
          { versionId: 'v-1', def: wp, createdAt: '2026-01-01T00:00:00.000Z' },
        ],
      },
    ]);
    renderPage();
    fireEvent.change(screen.getByLabelText(/saved object set/i), {
      target: { value: 'sa-2' },
    });

    expect(
      screen.getByTestId('objectset-lineage-node-derived'),
    ).toBeInTheDocument();
  });
});
