import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { SavedObjectSetsPage } from '../SavedObjectSetsPage';
import { localStorageKey } from '../../../lib/objectSetBuilder';
import {
  OBJECT_SET_URL_PARAM,
  parseDefinitionFromSearch,
} from '../../../lib/objectSetUrl';
import type { ObjectSetDefinition } from '../../../api/types';

const ONTOLOGY = 'test';

beforeEach(() => {
  window.localStorage.clear();
});

function seed(initial: unknown[]): void {
  window.localStorage.setItem(
    localStorageKey(ONTOLOGY),
    JSON.stringify(initial),
  );
}

function readPersisted(): Array<{
  id: string;
  versions: { versionId: string; def: ObjectSetDefinition }[];
  activeVersionId: string;
}> {
  const raw = window.localStorage.getItem(localStorageKey(ONTOLOGY));
  return raw ? (JSON.parse(raw) as never) : [];
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={[`/objectsets/${ONTOLOGY}/saved`]}>
      <Routes>
        <Route
          path="/objectsets/:ontology/saved"
          element={<SavedObjectSetsPage />}
        />
        <Route path="/objectsets/:ontology" element={<div>composer</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

const baseA: ObjectSetDefinition = { type: 'base', objectType: 'Employee' };
const baseB: ObjectSetDefinition = { type: 'base', objectType: 'Department' };

describe('SavedObjectSetsPage', () => {
  it('renders empty state when nothing is saved', () => {
    renderPage();
    expect(
      screen.getByText(/No saved Object Sets/i),
    ).toBeInTheDocument();
  });

  it('lists saved sets and expands to show versions', () => {
    seed([
      {
        id: 'sid-1',
        name: 'Engineers',
        def: baseB,
        createdAt: '2026-01-01T00:00:00.000Z',
        activeVersionId: 'v-2',
        versions: [
          { versionId: 'v-2', def: baseB, createdAt: '2026-02-01T00:00:00.000Z', note: 'departments' },
          { versionId: 'v-1', def: baseA, createdAt: '2026-01-01T00:00:00.000Z' },
        ],
      },
    ]);
    renderPage();

    expect(screen.getByText('Engineers')).toBeInTheDocument();
    expect(screen.getByText(/2 versions/)).toBeInTheDocument();

    fireEvent.click(screen.getByLabelText('Expand'));

    const versionsList = screen.getByTestId('versions-sid-1');
    const items = within(versionsList).getAllByRole('listitem');
    expect(items).toHaveLength(2);
    expect(within(items[0]).getByText('active')).toBeInTheDocument();
    expect(within(items[1]).getByLabelText(/switch to version v-1/)).toBeInTheDocument();
  });

  it('switches the active version on click', () => {
    seed([
      {
        id: 'sid-1',
        name: 'Q',
        def: baseB,
        createdAt: '2026-01-01T00:00:00.000Z',
        activeVersionId: 'v-2',
        versions: [
          { versionId: 'v-2', def: baseB, createdAt: '2026-02-01T00:00:00.000Z' },
          { versionId: 'v-1', def: baseA, createdAt: '2026-01-01T00:00:00.000Z' },
        ],
      },
    ]);
    renderPage();

    fireEvent.click(screen.getByLabelText('Expand'));
    fireEvent.click(screen.getByLabelText('switch to version v-1'));

    const persisted = readPersisted();
    expect(persisted[0].activeVersionId).toBe('v-1');
    expect(persisted[0].versions[1].def).toEqual(baseA);

    // The newly-active version row no longer has a Switch button
    expect(
      screen.queryByLabelText('switch to version v-1'),
    ).not.toBeInTheDocument();
    expect(
      screen.getByLabelText('switch to version v-2'),
    ).toBeInTheDocument();
  });

  it('opens the composer with the saved definition pre-encoded in the URL', () => {
    seed([
      {
        id: 'sid-1',
        name: 'Q',
        def: baseA,
        createdAt: '2026-01-01T00:00:00.000Z',
        activeVersionId: 'v-1',
        versions: [
          { versionId: 'v-1', def: baseA, createdAt: '2026-01-01T00:00:00.000Z' },
        ],
      },
    ]);
    renderPage();

    const link = screen.getByLabelText('open Q');
    const href = link.getAttribute('href') ?? '';
    expect(href.startsWith(`/objectsets/${ONTOLOGY}?${OBJECT_SET_URL_PARAM}=`)).toBe(
      true,
    );
    const search = href.slice(href.indexOf('?'));
    expect(parseDefinitionFromSearch(search)).toEqual(baseA);
  });

  it('deletes a single version via the confirm modal', () => {
    seed([
      {
        id: 'sid-1',
        name: 'Q',
        def: baseB,
        createdAt: '2026-01-01T00:00:00.000Z',
        activeVersionId: 'v-2',
        versions: [
          { versionId: 'v-2', def: baseB, createdAt: '2026-02-01T00:00:00.000Z' },
          { versionId: 'v-1', def: baseA, createdAt: '2026-01-01T00:00:00.000Z' },
        ],
      },
    ]);
    renderPage();

    fireEvent.click(screen.getByLabelText('Expand'));
    fireEvent.click(screen.getByLabelText('delete version v-1'));
    expect(screen.getByText(/Delete version/i)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));

    const persisted = readPersisted();
    expect(persisted[0].versions).toHaveLength(1);
    expect(persisted[0].versions[0].versionId).toBe('v-2');
  });

  it('deletes the whole saved set via the confirm modal', () => {
    seed([
      {
        id: 'sid-1',
        name: 'Q',
        def: baseA,
        createdAt: '2026-01-01T00:00:00.000Z',
        activeVersionId: 'v-1',
        versions: [
          { versionId: 'v-1', def: baseA, createdAt: '2026-01-01T00:00:00.000Z' },
        ],
      },
    ]);
    renderPage();

    fireEvent.click(screen.getByLabelText('delete Q'));
    expect(
      screen.getByText(/Delete saved Object Set/i),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));

    expect(readPersisted()).toHaveLength(0);
  });

  it('disables version-delete when only one version remains', () => {
    seed([
      {
        id: 'sid-1',
        name: 'Q',
        def: baseA,
        createdAt: '2026-01-01T00:00:00.000Z',
        activeVersionId: 'v-1',
        versions: [
          { versionId: 'v-1', def: baseA, createdAt: '2026-01-01T00:00:00.000Z' },
        ],
      },
    ]);
    renderPage();
    fireEvent.click(screen.getByLabelText('Expand'));
    const btn = screen.getByLabelText('delete version v-1');
    expect(btn).toBeDisabled();
  });
});
