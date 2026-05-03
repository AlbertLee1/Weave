import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';

// US-394: App Action 集成 — Button → ActionType apply with variable → param
// mapping. The runAction event leaves the preview-only stub behind and
// actually POSTs to /api/v2/ontologies/{ontology}/actions/{action}/apply
// when the editor is in preview mode. Result counts (or error) surface in
// the existing runtime message strip ("toast").

const apiMocks = vi.hoisted(() => ({
  listApps: vi.fn(),
  getApp: vi.fn(),
  createApp: vi.fn(),
  updateApp: vi.fn(),
  deleteApp: vi.fn(),
  listAppVersions: vi.fn(),
}));

const actionsMocks = vi.hoisted(() => ({
  applyAction: vi.fn(),
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
vi.mock('../../../api/actions', () => actionsMocks);
vi.mock('../../../stores/ontologyStore', () => ({
  useOntologyStore: ontologyMocks.useOntologyStore,
}));

import { AppEditorPage } from '../AppEditorPage';

beforeEach(() => {
  for (const m of Object.values(apiMocks)) m.mockReset();
  apiMocks.listApps.mockResolvedValue({ apps: [] });
  apiMocks.getApp.mockRejectedValue(new Error('not configured'));
  actionsMocks.applyAction.mockReset();
  ontologyMocks.state.selectedOntology = 'northwind';
});

function selectButtonAndPickRunAction(actionType: string) {
  fireEvent.click(screen.getByTestId('app-palette-item-button'));
  fireEvent.change(screen.getByTestId('app-event-onclick-kind'), {
    target: { value: 'runAction' },
  });
  fireEvent.change(
    screen.getByTestId('app-event-onclick-runaction-actiontype'),
    { target: { value: actionType } },
  );
}

describe('US-394 runAction params editor', () => {
  it('Add Param row appends a fresh blank entry', () => {
    render(<AppEditorPage />);
    selectButtonAndPickRunAction('createCustomer');
    expect(
      screen.queryByTestId('app-event-onclick-runaction-param-row-0'),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('app-event-onclick-runaction-add-param'));
    expect(
      screen.getByTestId('app-event-onclick-runaction-param-row-0'),
    ).toBeInTheDocument();
  });

  it('user can name a param and bind its value to a {{var}} reference', () => {
    render(<AppEditorPage />);
    fireEvent.click(screen.getByTestId('app-variables-add'));
    fireEvent.change(screen.getByTestId('app-variable-name-0'), {
      target: { value: 'displayName' },
    });
    selectButtonAndPickRunAction('createCustomer');
    fireEvent.click(screen.getByTestId('app-event-onclick-runaction-add-param'));
    fireEvent.change(
      screen.getByTestId('app-event-onclick-runaction-param-key-0'),
      { target: { value: 'name' } },
    );
    fireEvent.change(
      screen.getByTestId('app-event-onclick-runaction-param-value-0'),
      { target: { value: '{{displayName}}' } },
    );
    expect(
      (
        screen.getByTestId(
          'app-event-onclick-runaction-param-key-0',
        ) as HTMLInputElement
      ).value,
    ).toBe('name');
    expect(
      (
        screen.getByTestId(
          'app-event-onclick-runaction-param-value-0',
        ) as HTMLInputElement
      ).value,
    ).toBe('{{displayName}}');
  });

  it('Remove drops the param row', () => {
    render(<AppEditorPage />);
    selectButtonAndPickRunAction('a');
    fireEvent.click(screen.getByTestId('app-event-onclick-runaction-add-param'));
    fireEvent.click(
      screen.getByTestId('app-event-onclick-runaction-param-remove-0'),
    );
    expect(
      screen.queryByTestId('app-event-onclick-runaction-param-row-0'),
    ).not.toBeInTheDocument();
  });

  it('switching kind away from runAction clears the params editor', () => {
    render(<AppEditorPage />);
    selectButtonAndPickRunAction('a');
    fireEvent.click(screen.getByTestId('app-event-onclick-runaction-add-param'));
    fireEvent.change(screen.getByTestId('app-event-onclick-kind'), {
      target: { value: 'navigate' },
    });
    expect(
      screen.queryByTestId('app-event-onclick-runaction-add-param'),
    ).not.toBeInTheDocument();
  });
});

describe('US-394 preview runAction → applyAction', () => {
  it('button onClick=runAction invokes applyAction with substituted params', async () => {
    actionsMocks.applyAction.mockResolvedValue({
      operationId: 'ri.action.batch.42',
      edits: {
        type: 'edits',
        addedObjectCount: 1,
        modifiedObjectCount: 0,
        deletedObjectCount: 0,
        addedLinksCount: 0,
        deletedLinksCount: 0,
      },
    });
    render(<AppEditorPage />);
    fireEvent.click(screen.getByTestId('app-variables-add'));
    fireEvent.change(screen.getByTestId('app-variable-name-0'), {
      target: { value: 'name' },
    });
    fireEvent.change(screen.getByTestId('app-variable-default-0'), {
      target: { value: 'Ada' },
    });
    selectButtonAndPickRunAction('createCustomer');
    fireEvent.click(screen.getByTestId('app-event-onclick-runaction-add-param'));
    fireEvent.change(
      screen.getByTestId('app-event-onclick-runaction-param-key-0'),
      { target: { value: 'displayName' } },
    );
    fireEvent.change(
      screen.getByTestId('app-event-onclick-runaction-param-value-0'),
      { target: { value: '{{name}}' } },
    );
    fireEvent.click(screen.getByTestId('app-mode-toggle'));
    fireEvent.click(screen.getByTestId('app-runtime-button'));
    await waitFor(() => {
      expect(actionsMocks.applyAction).toHaveBeenCalledTimes(1);
    });
    expect(actionsMocks.applyAction).toHaveBeenCalledWith(
      'northwind',
      'createCustomer',
      { parameters: { displayName: 'Ada' } },
    );
    await waitFor(() => {
      expect(screen.getByTestId('app-runtime-message')).toHaveTextContent(
        '+1',
      );
    });
    expect(screen.getByTestId('app-runtime-message')).toHaveTextContent(
      'createCustomer',
    );
  });

  it('runAction surfaces the error message when applyAction rejects', async () => {
    actionsMocks.applyAction.mockRejectedValue(new Error('boom'));
    render(<AppEditorPage />);
    selectButtonAndPickRunAction('doThing');
    fireEvent.click(screen.getByTestId('app-mode-toggle'));
    fireEvent.click(screen.getByTestId('app-runtime-button'));
    await waitFor(() => {
      expect(screen.getByTestId('app-runtime-message')).toHaveTextContent(
        'error',
      );
    });
    expect(screen.getByTestId('app-runtime-message')).toHaveTextContent(
      'boom',
    );
  });

  it('runAction without an ontology selected shows a clear hint and does not call applyAction', async () => {
    ontologyMocks.state.selectedOntology = null;
    render(<AppEditorPage />);
    selectButtonAndPickRunAction('doThing');
    fireEvent.click(screen.getByTestId('app-mode-toggle'));
    fireEvent.click(screen.getByTestId('app-runtime-button'));
    await waitFor(() => {
      expect(screen.getByTestId('app-runtime-message')).toBeInTheDocument();
    });
    expect(screen.getByTestId('app-runtime-message')).toHaveTextContent(
      'no ontology selected',
    );
    expect(actionsMocks.applyAction).not.toHaveBeenCalled();
  });
});
