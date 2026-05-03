import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';

// US-393: Variables panel + Events config + Preview runtime tests.
// Mocks mirror the AppEditorPage.test.tsx setup so the editor doesn't
// hit the network.

const apiMocks = vi.hoisted(() => ({
  listApps: vi.fn(),
  getApp: vi.fn(),
  createApp: vi.fn(),
  updateApp: vi.fn(),
  deleteApp: vi.fn(),
  listAppVersions: vi.fn(),
}));

vi.mock('../../../api/apps', () => apiMocks);

import { AppEditorPage } from '../AppEditorPage';
import {
  coerceVariableValue,
  decodeLayout,
  instancesToLayout,
  makeEvent,
  makeInstance,
  makeVariable,
  substituteVariables,
} from '../layout';
import {
  dispatchEvent,
  initialVariableState,
  type RuntimeContext,
} from '../runtime';
import type { LayoutRow } from '../../../api/apps';

beforeEach(() => {
  for (const m of Object.values(apiMocks)) m.mockReset();
  apiMocks.listApps.mockResolvedValue({ apps: [] });
  apiMocks.getApp.mockRejectedValue(new Error('not configured'));
});

describe('layout helpers - variables + events', () => {
  it('makeVariable defaults to type=string and empty default', () => {
    expect(makeVariable('x')).toEqual({ name: 'x', type: 'string', default: '' });
    expect(makeVariable('n', 'number', '42')).toEqual({
      name: 'n',
      type: 'number',
      default: '42',
    });
  });

  it('coerceVariableValue routes by declared type', () => {
    expect(coerceVariableValue('hi', 'string')).toBe('hi');
    expect(coerceVariableValue('42', 'number')).toBe(42);
    expect(coerceVariableValue('not a number', 'number')).toBe(0);
    expect(coerceVariableValue('true', 'boolean')).toBe(true);
    expect(coerceVariableValue('YES', 'boolean')).toBe(true);
    expect(coerceVariableValue('false', 'boolean')).toBe(false);
    expect(coerceVariableValue('', 'boolean')).toBe(false);
  });

  it('substituteVariables expands {{ name }} and leaves unknown refs alone', () => {
    const state = { user: 'Ada', count: 7 };
    expect(substituteVariables('hello {{user}}', state)).toBe('hello Ada');
    expect(substituteVariables('{{ user }} : {{count}}', state)).toBe('Ada : 7');
    expect(substituteVariables('hello {{missing}}', state)).toBe(
      'hello {{missing}}',
    );
    // No-template fast path
    expect(substituteVariables('plain', state)).toBe('plain');
  });

  it('instancesToLayout encodes variables on the root row', () => {
    const insts = [makeInstance('text')];
    const vars = [makeVariable('userId', 'string', 'u-1')];
    const layout = instancesToLayout(insts, vars);
    expect(layout.type).toBe('row');
    if (layout.type !== 'row') throw new Error('unreachable');
    expect(layout.variables).toEqual([
      { name: 'userId', type: 'string', default: 'u-1' },
    ]);
  });

  it('instancesToLayout omits variables when none declared', () => {
    const layout = instancesToLayout([makeInstance('text')]) as LayoutRow;
    expect(layout.type).toBe('row');
    expect(layout.variables).toBeUndefined();
  });

  it('instancesToLayout copies events onto component nodes', () => {
    const inst = makeInstance('button');
    inst.events = {
      onClick: { kind: 'setVariable', name: 'count', value: '1' },
    };
    const layout = instancesToLayout([inst]) as LayoutRow;
    const child = layout.children[0].child;
    expect(child.type).toBe('component');
    if (child.type !== 'component') throw new Error('unreachable');
    expect(child.events?.onClick).toEqual({
      kind: 'setVariable',
      name: 'count',
      value: '1',
    });
  });

  it('decodeLayout round-trips variables and events', () => {
    const inst = makeInstance('button');
    inst.events = { onClick: { kind: 'navigate', to: '/x' } };
    const vars = [makeVariable('flag', 'boolean', 'true')];
    const layout = instancesToLayout([inst], vars);
    const decoded = decodeLayout(layout);
    expect(decoded.variables).toEqual(vars);
    expect(decoded.instances[0].events?.onClick).toEqual({
      kind: 'navigate',
      to: '/x',
    });
  });

  it('makeEvent defaults each kind with empty fields', () => {
    expect(makeEvent('setVariable')).toEqual({
      kind: 'setVariable',
      name: '',
      value: '',
    });
    expect(makeEvent('runAction')).toEqual({
      kind: 'runAction',
      actionType: '',
      params: {},
    });
    expect(makeEvent('navigate')).toEqual({ kind: 'navigate', to: '' });
  });
});

describe('runtime engine', () => {
  it('initialVariableState coerces defaults via declared types', () => {
    const state = initialVariableState([
      makeVariable('s', 'string', 'hi'),
      makeVariable('n', 'number', '5'),
      makeVariable('b', 'boolean', 'true'),
    ]);
    expect(state).toEqual({ s: 'hi', n: 5, b: true });
  });

  it('dispatchEvent setVariable mutates state with type coercion', async () => {
    let state: Record<string, string | number | boolean> = { count: 0 };
    const ctx: RuntimeContext = {
      variables: [makeVariable('count', 'number', '0')],
      state,
      setState: (updater) => {
        state =
          typeof updater === 'function'
            ? (updater as (prev: typeof state) => typeof state)(state)
            : updater;
      },
      navigate: vi.fn(),
      runAction: vi.fn(),
    };
    await dispatchEvent(
      { kind: 'setVariable', name: 'count', value: '12' },
      ctx,
    );
    expect(state.count).toBe(12);
  });

  it('dispatchEvent setVariable expands {{var}} refs in the value', async () => {
    let state: Record<string, string | number | boolean> = { src: 'abc', dst: '' };
    const ctx: RuntimeContext = {
      variables: [
        makeVariable('src', 'string', 'abc'),
        makeVariable('dst', 'string', ''),
      ],
      state,
      setState: (updater) => {
        state =
          typeof updater === 'function'
            ? (updater as (prev: typeof state) => typeof state)(state)
            : updater;
      },
      navigate: vi.fn(),
      runAction: vi.fn(),
    };
    await dispatchEvent(
      { kind: 'setVariable', name: 'dst', value: 'echo:{{src}}' },
      ctx,
    );
    expect(state.dst).toBe('echo:abc');
  });

  it('dispatchEvent navigate calls the navigate callback with substituted path', async () => {
    const navigate = vi.fn();
    const state = { id: 'r-42' };
    await dispatchEvent(
      { kind: 'navigate', to: '/objects/{{id}}' },
      {
        variables: [makeVariable('id', 'string', 'r-42')],
        state,
        setState: vi.fn(),
        navigate,
        runAction: vi.fn(),
      },
    );
    expect(navigate).toHaveBeenCalledWith('/objects/r-42');
  });

  it('dispatchEvent runAction passes substituted params to the runner', async () => {
    const runAction = vi.fn().mockResolvedValue(undefined);
    const state = { name: 'Ada' };
    await dispatchEvent(
      {
        kind: 'runAction',
        actionType: 'createUser',
        params: { displayName: '{{name}}' },
      },
      {
        variables: [makeVariable('name', 'string', 'Ada')],
        state,
        setState: vi.fn(),
        navigate: vi.fn(),
        runAction,
      },
    );
    expect(runAction).toHaveBeenCalledWith('createUser', {
      displayName: 'Ada',
    });
  });
});

describe('AppEditorPage Variables panel', () => {
  it('renders an empty hint and Add button on first mount', () => {
    render(<AppEditorPage />);
    expect(screen.getByTestId('app-variables-panel')).toBeInTheDocument();
    expect(screen.getByTestId('app-variables-empty')).toBeInTheDocument();
  });

  it('Add button creates a fresh variable row with type=string default=""', () => {
    render(<AppEditorPage />);
    fireEvent.click(screen.getByTestId('app-variables-add'));
    const row = screen.getByTestId('app-variable-row');
    expect(row.getAttribute('data-variable-name')).toBe('var1');
    expect(row.getAttribute('data-variable-type')).toBe('string');
  });

  it('user can rename, retype, and set default of a variable', () => {
    render(<AppEditorPage />);
    fireEvent.click(screen.getByTestId('app-variables-add'));
    fireEvent.change(screen.getByTestId('app-variable-name-0'), {
      target: { value: 'count' },
    });
    fireEvent.change(screen.getByTestId('app-variable-type-0'), {
      target: { value: 'number' },
    });
    fireEvent.change(screen.getByTestId('app-variable-default-0'), {
      target: { value: '7' },
    });
    const row = screen.getByTestId('app-variable-row');
    expect(row.getAttribute('data-variable-name')).toBe('count');
    expect(row.getAttribute('data-variable-type')).toBe('number');
  });

  it('flags invalid variable names with the error hint', () => {
    render(<AppEditorPage />);
    fireEvent.click(screen.getByTestId('app-variables-add'));
    fireEvent.change(screen.getByTestId('app-variable-name-0'), {
      target: { value: '1bad' },
    });
    const row = screen.getByTestId('app-variable-row');
    expect(row.getAttribute('data-variable-invalid')).toBe('true');
    expect(screen.getByTestId('app-variable-error-0')).toBeInTheDocument();
  });

  it('flags duplicate variable names', () => {
    render(<AppEditorPage />);
    fireEvent.click(screen.getByTestId('app-variables-add'));
    fireEvent.click(screen.getByTestId('app-variables-add'));
    fireEvent.change(screen.getByTestId('app-variable-name-1'), {
      target: { value: 'var1' },
    });
    expect(screen.getByTestId('app-variable-dup-0')).toBeInTheDocument();
    expect(screen.getByTestId('app-variable-dup-1')).toBeInTheDocument();
  });

  it('Remove button drops the variable', () => {
    render(<AppEditorPage />);
    fireEvent.click(screen.getByTestId('app-variables-add'));
    fireEvent.click(screen.getByTestId('app-variable-remove-0'));
    expect(screen.queryByTestId('app-variable-row')).not.toBeInTheDocument();
    expect(screen.getByTestId('app-variables-empty')).toBeInTheDocument();
  });

  it('save flow includes variables on the layout root', async () => {
    apiMocks.createApp.mockResolvedValue({
      rid: 'ri.app.main.app.99',
      name: 'A',
      ownerId: 'u',
      layoutJson: { type: 'row', children: [] },
      version: 1,
      createdAt: '2026-05-04T00:00:00Z',
      updatedAt: '2026-05-04T00:00:00Z',
    });
    render(<AppEditorPage />);
    fireEvent.click(screen.getByTestId('app-palette-item-text'));
    fireEvent.click(screen.getByTestId('app-variables-add'));
    fireEvent.change(screen.getByTestId('app-variable-name-0'), {
      target: { value: 'userId' },
    });
    fireEvent.click(screen.getByTestId('app-save'));
    await waitFor(() => {
      expect(apiMocks.createApp).toHaveBeenCalledTimes(1);
    });
    const layoutJson = apiMocks.createApp.mock.calls[0][0].layoutJson;
    expect(layoutJson.variables).toEqual([
      { name: 'userId', type: 'string', default: '' },
    ]);
  });
});

describe('AppEditorPage Events editor', () => {
  it('shows "no handler" by default for a button', () => {
    render(<AppEditorPage />);
    fireEvent.click(screen.getByTestId('app-palette-item-button'));
    expect(
      screen.getByTestId('app-events-editor').getAttribute('data-onclick-kind'),
    ).toBe('none');
  });

  it('selecting setVariable surfaces target selector + value field', () => {
    render(<AppEditorPage />);
    fireEvent.click(screen.getByTestId('app-palette-item-button'));
    fireEvent.click(screen.getByTestId('app-variables-add'));
    fireEvent.change(screen.getByTestId('app-variable-name-0'), {
      target: { value: 'opened' },
    });
    fireEvent.change(screen.getByTestId('app-event-onclick-kind'), {
      target: { value: 'setVariable' },
    });
    fireEvent.change(
      screen.getByTestId('app-event-onclick-setvariable-name'),
      {
        target: { value: 'opened' },
      },
    );
    fireEvent.change(
      screen.getByTestId('app-event-onclick-setvariable-value'),
      {
        target: { value: 'true' },
      },
    );
    expect(
      screen.getByTestId('app-events-editor').getAttribute('data-onclick-kind'),
    ).toBe('setVariable');
  });

  it('selecting navigate exposes the path field', () => {
    render(<AppEditorPage />);
    fireEvent.click(screen.getByTestId('app-palette-item-button'));
    fireEvent.change(screen.getByTestId('app-event-onclick-kind'), {
      target: { value: 'navigate' },
    });
    fireEvent.change(screen.getByTestId('app-event-onclick-navigate-to'), {
      target: { value: '/objects/42' },
    });
    expect(
      screen.getByTestId('app-events-editor').getAttribute('data-onclick-kind'),
    ).toBe('navigate');
  });

  it('switching back to "none" clears the handler', () => {
    render(<AppEditorPage />);
    fireEvent.click(screen.getByTestId('app-palette-item-button'));
    fireEvent.change(screen.getByTestId('app-event-onclick-kind'), {
      target: { value: 'navigate' },
    });
    fireEvent.change(screen.getByTestId('app-event-onclick-kind'), {
      target: { value: 'none' },
    });
    expect(
      screen.getByTestId('app-events-editor').getAttribute('data-onclick-kind'),
    ).toBe('none');
    expect(
      screen.queryByTestId('app-event-onclick-navigate-to'),
    ).not.toBeInTheDocument();
  });
});

describe('AppEditorPage Preview mode', () => {
  it('switches to preview, renders runtime canvas, and back to edit', () => {
    render(<AppEditorPage />);
    fireEvent.click(screen.getByTestId('app-palette-item-text'));
    fireEvent.click(screen.getByTestId('app-mode-toggle'));
    expect(screen.getByTestId('app-runtime-view')).toBeInTheDocument();
    expect(
      screen.getByTestId('app-editor-page').getAttribute('data-mode'),
    ).toBe('preview');
    fireEvent.click(screen.getByTestId('app-mode-toggle'));
    expect(
      screen.getByTestId('app-editor-page').getAttribute('data-mode'),
    ).toBe('edit');
  });

  it('preview substitutes {{var}} references in text content', () => {
    render(<AppEditorPage />);
    // Add a variable and a text component referencing it.
    fireEvent.click(screen.getByTestId('app-variables-add'));
    fireEvent.change(screen.getByTestId('app-variable-name-0'), {
      target: { value: 'user' },
    });
    fireEvent.change(screen.getByTestId('app-variable-default-0'), {
      target: { value: 'Ada' },
    });
    fireEvent.click(screen.getByTestId('app-palette-item-text'));
    fireEvent.change(screen.getByTestId('prop-text-content'), {
      target: { value: 'Hello {{user}}' },
    });
    fireEvent.click(screen.getByTestId('app-mode-toggle'));
    expect(screen.getByTestId('app-runtime-text')).toHaveTextContent(
      'Hello Ada',
    );
  });

  it('button onClick=setVariable updates runtime state and re-renders text', () => {
    render(<AppEditorPage />);
    // Variable: count : number = 0
    fireEvent.click(screen.getByTestId('app-variables-add'));
    fireEvent.change(screen.getByTestId('app-variable-name-0'), {
      target: { value: 'count' },
    });
    fireEvent.change(screen.getByTestId('app-variable-type-0'), {
      target: { value: 'number' },
    });
    fireEvent.change(screen.getByTestId('app-variable-default-0'), {
      target: { value: '0' },
    });
    // Component 1: button → setVariable count = 7
    fireEvent.click(screen.getByTestId('app-palette-item-button'));
    fireEvent.change(screen.getByTestId('app-event-onclick-kind'), {
      target: { value: 'setVariable' },
    });
    fireEvent.change(
      screen.getByTestId('app-event-onclick-setvariable-name'),
      {
        target: { value: 'count' },
      },
    );
    fireEvent.change(
      screen.getByTestId('app-event-onclick-setvariable-value'),
      {
        target: { value: '7' },
      },
    );
    // Component 2: text reading {{count}}
    fireEvent.click(screen.getByTestId('app-palette-item-text'));
    fireEvent.change(screen.getByTestId('prop-text-content'), {
      target: { value: 'value: {{count}}' },
    });
    // Enter preview
    fireEvent.click(screen.getByTestId('app-mode-toggle'));
    expect(screen.getByTestId('app-runtime-text')).toHaveTextContent('value: 0');
    // Click the runtime button — count should flip to 7.
    fireEvent.click(screen.getByTestId('app-runtime-button'));
    expect(screen.getByTestId('app-runtime-text')).toHaveTextContent('value: 7');
    expect(
      screen
        .getByTestId('app-runtime-var-count')
        .getAttribute('data-variable-value'),
    ).toBe('7');
  });

  it('button onClick=navigate surfaces a runtime message', () => {
    render(<AppEditorPage />);
    fireEvent.click(screen.getByTestId('app-palette-item-button'));
    fireEvent.change(screen.getByTestId('app-event-onclick-kind'), {
      target: { value: 'navigate' },
    });
    fireEvent.change(screen.getByTestId('app-event-onclick-navigate-to'), {
      target: { value: '/inbox' },
    });
    fireEvent.click(screen.getByTestId('app-mode-toggle'));
    fireEvent.click(screen.getByTestId('app-runtime-button'));
    expect(screen.getByTestId('app-runtime-message')).toHaveTextContent(
      'navigate → /inbox',
    );
  });

  it('button onClick=runAction surfaces a runtime message', () => {
    render(<AppEditorPage />);
    fireEvent.click(screen.getByTestId('app-palette-item-button'));
    fireEvent.change(screen.getByTestId('app-event-onclick-kind'), {
      target: { value: 'runAction' },
    });
    fireEvent.change(
      screen.getByTestId('app-event-onclick-runaction-actiontype'),
      {
        target: { value: 'doThing' },
      },
    );
    fireEvent.click(screen.getByTestId('app-mode-toggle'));
    fireEvent.click(screen.getByTestId('app-runtime-button'));
    expect(screen.getByTestId('app-runtime-message')).toHaveTextContent(
      'runAction doThing',
    );
  });
});
