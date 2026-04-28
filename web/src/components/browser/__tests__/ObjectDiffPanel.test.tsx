import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createElement } from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

// Mock the diff viewer so jsdom doesn't have to render emotion + measurement
// internals; the test only cares about which strings WE feed in.
vi.mock('react-diff-viewer-continued', () => ({
  default: (props: {
    oldValue: string;
    newValue: string;
    leftTitle?: string;
    rightTitle?: string;
  }) =>
    createElement(
      'div',
      { 'data-testid': 'rdv-mock' },
      createElement(
        'div',
        { 'data-testid': 'rdv-titles' },
        `${props.leftTitle ?? ''}|${props.rightTitle ?? ''}`,
      ),
      createElement(
        'pre',
        { 'data-testid': 'rdv-old' },
        props.oldValue,
      ),
      createElement(
        'pre',
        { 'data-testid': 'rdv-new' },
        props.newValue,
      ),
    ),
  DiffMethod: { LINES: 'diffLines' },
}));

// Imports that depend on the mock have to come AFTER the mock registration.
import { ObjectDiffPanel } from '../ObjectDiffPanel';
import * as api from '../../../api/objects';
import type { ObjectActivityEntry } from '../../../api/types';

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
  newState: Record<string, unknown> | null,
  editType: ObjectActivityEntry['editType'] = 'MODIFY',
): ObjectActivityEntry {
  return {
    id: `id-${version}`,
    objectTypeRid: 'ri.ontology.main.object-type.employee',
    primaryKey: 'emp1',
    version,
    editType,
    newState,
    recordedAt: '2026-04-28T10:00:00Z',
  };
}

describe('ObjectDiffPanel', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders insufficient state when fewer than 2 versions exist', async () => {
    vi.spyOn(api, 'getObjectActivity').mockResolvedValue({
      data: [event(1, { name: 'Alice' }, 'CREATE')],
    });

    renderWithProviders(
      <ObjectDiffPanel
        ontologyApiName="main"
        objectType="employee"
        primaryKey="emp1"
      />,
    );

    await waitFor(() =>
      expect(screen.getByTestId('diff-insufficient')).toBeInTheDocument(),
    );
  });

  it('renders the diff between the latest two versions by default', async () => {
    vi.spyOn(api, 'getObjectActivity').mockResolvedValue({
      data: [
        event(2, { name: 'Bob' }, 'MODIFY'),
        event(1, { name: 'Alice' }, 'CREATE'),
      ],
    });

    renderWithProviders(
      <ObjectDiffPanel
        ontologyApiName="main"
        objectType="employee"
        primaryKey="emp1"
      />,
    );

    await waitFor(() => screen.getByTestId('diff-viewer'));
    const oldPane = screen.getByTestId('rdv-old');
    const newPane = screen.getByTestId('rdv-new');
    expect(oldPane.textContent).toContain('"Alice"');
    expect(newPane.textContent).toContain('"Bob"');

    // Default: left=v1, right=v2; titles match.
    expect(screen.getByTestId('rdv-titles').textContent).toBe(
      'v1 · CREATE|v2 · MODIFY',
    );
  });

  it('canonicalises object keys so JSON shape diffs but key order does not', async () => {
    vi.spyOn(api, 'getObjectActivity').mockResolvedValue({
      data: [
        event(2, { z: 1, a: 2 }, 'MODIFY'),
        event(1, { a: 2, z: 1 }, 'CREATE'),
      ],
    });

    renderWithProviders(
      <ObjectDiffPanel
        ontologyApiName="main"
        objectType="employee"
        primaryKey="emp1"
      />,
    );

    await waitFor(() => screen.getByTestId('diff-viewer'));
    // Both panes should serialise with keys in the same alphabetical order
    // so a key-order shuffle is NOT mistaken for a real diff.
    expect(screen.getByTestId('rdv-old').textContent).toEqual(
      screen.getByTestId('rdv-new').textContent,
    );
  });

  it('shows a hint when both selectors point at the same version', async () => {
    vi.spyOn(api, 'getObjectActivity').mockResolvedValue({
      data: [
        event(2, { name: 'Bob' }, 'MODIFY'),
        event(1, { name: 'Alice' }, 'CREATE'),
      ],
    });

    renderWithProviders(
      <ObjectDiffPanel
        ontologyApiName="main"
        objectType="employee"
        primaryKey="emp1"
      />,
    );

    await waitFor(() => screen.getByTestId('diff-left-select'));
    fireEvent.change(screen.getByTestId('diff-left-select'), {
      target: { value: '2' },
    });
    await waitFor(() =>
      expect(screen.getByTestId('diff-same-version')).toBeInTheDocument(),
    );
    expect(screen.queryByTestId('diff-viewer')).toBeNull();
  });

  it('renders empty old value when comparing against a CREATE prevState', async () => {
    vi.spyOn(api, 'getObjectActivity').mockResolvedValue({
      data: [
        event(2, { name: 'Bob' }, 'MODIFY'),
        event(1, null, 'CREATE'),
      ],
    });

    renderWithProviders(
      <ObjectDiffPanel
        ontologyApiName="main"
        objectType="employee"
        primaryKey="emp1"
      />,
    );

    await waitFor(() => screen.getByTestId('diff-viewer'));
    expect(screen.getByTestId('rdv-old').textContent).toBe('');
    expect(screen.getByTestId('rdv-new').textContent).toContain('"Bob"');
  });
});
