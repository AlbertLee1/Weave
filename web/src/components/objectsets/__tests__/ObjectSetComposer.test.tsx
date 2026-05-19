import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ObjectSetComposer } from '../ObjectSetComposer';
import type { ObjectSetDefinition } from '../../../api/types';

describe('ObjectSetComposer', () => {
  const baseDef: ObjectSetDefinition = {
    type: 'base',
    objectType: 'Employee',
  };

  it('renders the embedded tree builder', () => {
    render(
      <ObjectSetComposer
        objectTypes={['Employee', 'Department']}
        value={baseDef}
        onChange={() => {}}
        onExecute={() => {}}
        onSaveAs={() => {}}
        savedObjectSets={[]}
        onLoadSaved={() => {}}
        onDeleteSaved={() => {}}
      />,
    );
    expect(screen.getByText('base')).toBeInTheDocument();
  });

  it('calls onExecute when Execute is clicked', () => {
    const onExecute = vi.fn();
    render(
      <ObjectSetComposer
        objectTypes={['Employee']}
        value={baseDef}
        onChange={() => {}}
        onExecute={onExecute}
        onSaveAs={() => {}}
        savedObjectSets={[]}
        onLoadSaved={() => {}}
        onDeleteSaved={() => {}}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: /execute/i }));
    expect(onExecute).toHaveBeenCalled();
  });

  it('disables Execute when validation errors exist', () => {
    const onExecute = vi.fn();
    render(
      <ObjectSetComposer
        objectTypes={['Employee']}
        value={{ type: 'base', objectType: '' }}
        onChange={() => {}}
        onExecute={onExecute}
        onSaveAs={() => {}}
        savedObjectSets={[]}
        onLoadSaved={() => {}}
        onDeleteSaved={() => {}}
      />,
    );
    const btn = screen.getByRole('button', { name: /execute/i });
    expect(btn).toBeDisabled();
  });

  it('calls onSaveAs with current def', () => {
    const onSaveAs = vi.fn();
    render(
      <ObjectSetComposer
        objectTypes={['Employee']}
        value={baseDef}
        onChange={() => {}}
        onExecute={() => {}}
        onSaveAs={onSaveAs}
        savedObjectSets={[]}
        onLoadSaved={() => {}}
        onDeleteSaved={() => {}}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: /save as/i }));
    expect(onSaveAs).toHaveBeenCalled();
  });

  it('renders saved object sets', () => {
    render(
      <ObjectSetComposer
        objectTypes={['Employee']}
        value={baseDef}
        onChange={() => {}}
        onExecute={() => {}}
        onSaveAs={() => {}}
        savedObjectSets={[
          {
            id: 's1',
            name: 'My saved query',
            def: baseDef,
            createdAt: '2026-04-06T00:00:00Z',
            versions: [
              {
                versionId: 'v1',
                def: baseDef,
                createdAt: '2026-04-06T00:00:00Z',
              },
            ],
            activeVersionId: 'v1',
          },
        ]}
        onLoadSaved={() => {}}
        onDeleteSaved={() => {}}
      />,
    );
    expect(screen.getByText('My saved query')).toBeInTheDocument();
  });

  it('shows union/intersect/subtract type-mismatch warning when branches differ', () => {
    render(
      <ObjectSetComposer
        objectTypes={['Employee', 'Department']}
        value={{
          type: 'union',
          objectSets: [
            { type: 'base', objectType: 'Employee' },
            { type: 'base', objectType: 'Department' },
          ],
        }}
        onChange={() => {}}
        onExecute={() => {}}
        onSaveAs={() => {}}
        savedObjectSets={[]}
        onLoadSaved={() => {}}
        onDeleteSaved={() => {}}
      />,
    );
    expect(
      screen.getByText(/branch.*differ|different.*types|mixing.*types/i),
    ).toBeInTheDocument();
  });

  it('renders a valid static object set with execute enabled', () => {
    render(
      <ObjectSetComposer
        objectTypes={['Employee', 'Department']}
        value={{
          type: 'static',
          objectType: 'Employee',
          primaryKeys: ['emp-1', 'emp-2'],
        }}
        onChange={() => {}}
        onExecute={() => {}}
        onSaveAs={() => {}}
        savedObjectSets={[]}
        onLoadSaved={() => {}}
        onDeleteSaved={() => {}}
      />,
    );

    expect(screen.getByRole('combobox', { name: /objectset type/i })).toHaveValue('static');
    expect(screen.getByLabelText(/primary keys/i)).toHaveValue('emp-1\nemp-2');
    expect(screen.getByRole('button', { name: /execute/i })).toBeEnabled();
    expect(screen.queryByText(/unsupported|not yet supported/i)).not.toBeInTheDocument();
  });
});
