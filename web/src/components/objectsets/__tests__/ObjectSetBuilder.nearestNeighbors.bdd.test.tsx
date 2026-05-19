import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ObjectSetBuilder } from '../ObjectSetBuilder';
import { ObjectSetComposer } from '../ObjectSetComposer';
import type { ObjectSetDefinition } from '../../../api/types';

describe('BDD: nearestNeighbors ObjectSet composer support', () => {
  const objectTypes = ['Incident', 'Device'];

  it('Given a user switches to nearestNeighbors, When the type changes, Then a backend-compatible definition is started', () => {
    const onChange = vi.fn();

    render(
      <ObjectSetBuilder
        objectTypes={objectTypes}
        value={{ type: 'base', objectType: 'Incident' }}
        onChange={onChange}
      />,
    );

    fireEvent.change(screen.getByRole('combobox', { name: /objectset type/i }), {
      target: { value: 'nearestNeighbors' },
    });

    expect(onChange).toHaveBeenCalledWith({
      type: 'nearestNeighbors',
      objectSet: { type: 'base', objectType: 'Incident' },
      propertyIdentifier: { property: { apiName: '' } },
      numNeighbors: 10,
      query: { text: { value: '' } },
    });
  });

  it('Given a saved nearestNeighbors definition, When rendered in the composer, Then it is editable and executable', () => {
    const onChange = vi.fn();
    const onExecute = vi.fn();
    const value: ObjectSetDefinition = {
      type: 'nearestNeighbors',
      objectSet: { type: 'base', objectType: 'Incident' },
      propertyIdentifier: { property: { apiName: 'embedding' } },
      numNeighbors: 3,
      query: { text: { value: 'find similar incidents' } },
    };

    render(
      <ObjectSetComposer
        objectTypes={objectTypes}
        value={value}
        onChange={onChange}
        onExecute={onExecute}
        onSaveAs={() => {}}
        savedObjectSets={[]}
        onLoadSaved={() => {}}
        onDeleteSaved={() => {}}
      />,
    );

    expect(screen.queryByText(/nearestNeighbors.*not.*supported/i)).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /execute/i })).toBeEnabled();
    expect(screen.getByLabelText(/embedding property/i)).toHaveValue('embedding');
    expect(screen.getByLabelText(/neighbors/i)).toHaveValue(3);
    expect(screen.getByLabelText(/query text/i)).toHaveValue('find similar incidents');

    fireEvent.change(screen.getByLabelText(/query text/i), {
      target: { value: 'network outage incidents' },
    });

    expect(onChange).toHaveBeenCalledWith({
      ...value,
      query: { text: { value: 'network outage incidents' } },
    });
  });
});
