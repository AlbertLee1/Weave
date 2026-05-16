import { describe, it, expect, beforeAll, afterAll, afterEach, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { ObjectSetSnapshotsPage } from '../ObjectSetSnapshotsPage';
import { localStorageKey } from '../../../lib/objectSetBuilder';
import { snapshotsLocalStorageKey } from '../../../lib/objectSetSnapshots';

const ONTOLOGY = 'test';

const server = setupServer();
beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

beforeEach(() => {
  window.localStorage.clear();
  // Seed a saved object set so the page has something to snapshot.
  const saved = [
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
  ];
  window.localStorage.setItem(localStorageKey(ONTOLOGY), JSON.stringify(saved));
});

afterEach(() => {
  window.localStorage.clear();
});

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[`/objectsets/${ONTOLOGY}/snapshots`]}>
        <Routes>
          <Route
            path="/objectsets/:ontology/snapshots"
            element={<ObjectSetSnapshotsPage />}
          />
          <Route
            path="/objectsets/:ontology"
            element={<div data-testid="composer-page">composer</div>}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('ObjectSetSnapshotsPage', () => {
  it('renders page title and an empty list', () => {
    renderPage();
    expect(
      screen.getByRole('heading', { name: /object set snapshots/i }),
    ).toBeInTheDocument();
    expect(screen.getByText(/no snapshots/i)).toBeInTheDocument();
    expect(
      screen.getByTestId('objectset-snapshots-create-btn'),
    ).toBeDisabled();
  });

  it('creates a snapshot via createTemporary + snapshot endpoints and lists it', async () => {
    server.use(
      http.post(
        '/api/v2/ontologies/test/objectSets/createTemporary',
        () =>
          HttpResponse.json({
            objectSetRid: 'ri.objectset.weave.main.tempObjectSet.123',
          }),
      ),
      http.post(
        '/api/v2/ontologies/test/objectSets/ri.objectset.weave.main.tempObjectSet.123/snapshot',
        () =>
          HttpResponse.json({
            snapshotRid: 'ri.objectsets.main.snapshot.snap1',
            objectType: 'Employee',
            primaryKeys: ['1', '2'],
            totalCount: '2',
            createdAt: '2026-05-17T00:00:00.000Z',
            definitionHash: 'sha256-abc',
            snapshotAt: 1000,
            isImmutable: true,
          }),
      ),
    );
    renderPage();

    fireEvent.change(screen.getByLabelText(/saved object set/i), {
      target: { value: 'sa-1' },
    });
    fireEvent.click(screen.getByTestId('objectset-snapshots-create-btn'));

    await waitFor(() => {
      expect(screen.getByTestId('objectset-snapshots-list')).toBeInTheDocument();
    });

    // The row carries the snapshot rid and metadata.
    expect(
      screen.getByTestId(
        'objectset-snapshot-row-ri.objectsets.main.snapshot.snap1',
      ),
    ).toBeInTheDocument();
    const row = screen.getByTestId(
      'objectset-snapshot-row-ri.objectsets.main.snapshot.snap1',
    );
    expect(row.textContent).toMatch(/Engineers/);
    expect(row.textContent).toMatch(/2 rows/);

    // The Restore link routes back to the composer with the original
    // Definition encoded in the URL so the user can re-load it.
    const restoreLink = screen.getByTestId(
      'objectset-snapshot-restore-ri.objectsets.main.snapshot.snap1',
    );
    expect(restoreLink).toHaveAttribute(
      'href',
      expect.stringContaining(`/objectsets/${ONTOLOGY}?`),
    );

    // The snapshot is persisted in localStorage so a page refresh
    // preserves the list.
    const raw = window.localStorage.getItem(snapshotsLocalStorageKey(ONTOLOGY));
    expect(raw).not.toBeNull();
    const parsed = raw ? (JSON.parse(raw) as Array<{ snapshotRid: string }>) : [];
    expect(parsed.length).toBe(1);
    expect(parsed[0].snapshotRid).toBe(
      'ri.objectsets.main.snapshot.snap1',
    );
  });

  it('forgets a snapshot row from the local index when clicked', () => {
    window.localStorage.setItem(
      snapshotsLocalStorageKey(ONTOLOGY),
      JSON.stringify([
        {
          snapshotRid: 'ri.snap.1',
          ontologyApiName: ONTOLOGY,
          objectType: 'Employee',
          savedSetId: 'sa-1',
          savedSetName: 'Engineers',
          def: { type: 'base', objectType: 'Employee' },
          createdAt: '2026-05-17T00:00:00.000Z',
          totalCount: 5,
        },
      ]),
    );

    renderPage();
    expect(
      screen.getByTestId('objectset-snapshot-row-ri.snap.1'),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByTestId('objectset-snapshot-forget-ri.snap.1'));
    expect(screen.queryByTestId('objectset-snapshot-row-ri.snap.1')).toBeNull();
    expect(screen.getByText(/no snapshots/i)).toBeInTheDocument();
  });

  it('surfaces an error if the snapshot endpoint fails', async () => {
    server.use(
      http.post(
        '/api/v2/ontologies/test/objectSets/createTemporary',
        () =>
          HttpResponse.json({
            objectSetRid: 'ri.objectset.weave.main.tempObjectSet.123',
          }),
      ),
      http.post(
        '/api/v2/ontologies/test/objectSets/ri.objectset.weave.main.tempObjectSet.123/snapshot',
        () =>
          HttpResponse.json(
            {
              errorCode: 'INVALID',
              errorName: 'SnapshotsUnavailable',
              errorInstanceId: 'abc',
              statusCode: 400,
            },
            { status: 400 },
          ),
      ),
    );
    renderPage();
    fireEvent.change(screen.getByLabelText(/saved object set/i), {
      target: { value: 'sa-1' },
    });
    fireEvent.click(screen.getByTestId('objectset-snapshots-create-btn'));

    await waitFor(() => {
      expect(
        screen.getByTestId('objectset-snapshots-error'),
      ).toBeInTheDocument();
    });
  });
});
