import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ObjectSetBuilder } from '../ObjectSetBuilder';
import { ObjectSetComposer } from '../ObjectSetComposer';
import type {
  NearestNeighborsObjectSet,
  ObjectSetDefinition,
  SampleObjectSet,
} from '../../../api/types';

describe('BDD: nearestNeighbors multi-column + fusion (A5)', () => {
  const objectTypes = ['Incident', 'Device'];

  it('Given a single-column NN, When the user adds a column, Then propertyIdentifiers is emitted with both columns', () => {
    const onChange = vi.fn();
    const value: NearestNeighborsObjectSet = {
      type: 'nearestNeighbors',
      objectSet: { type: 'base', objectType: 'Incident' },
      propertyIdentifiers: [{ property: { apiName: 'embedding' } }],
      numNeighbors: 5,
      query: { text: { value: 'q' } },
    };

    render(
      <ObjectSetBuilder
        objectTypes={objectTypes}
        value={value}
        onChange={onChange}
      />,
    );

    fireEvent.click(screen.getByRole('button', { name: /add column/i }));

    const next = onChange.mock.calls[0][0] as NearestNeighborsObjectSet;
    expect(next.propertyIdentifiers).toEqual([
      { property: { apiName: 'embedding' } },
      { property: { apiName: '' } },
    ]);
  });

  it('Given two columns, When the user edits the second column apiName, Then only that entry changes', () => {
    const onChange = vi.fn();
    const value: NearestNeighborsObjectSet = {
      type: 'nearestNeighbors',
      objectSet: { type: 'base', objectType: 'Incident' },
      propertyIdentifiers: [
        { property: { apiName: 'title_vec' } },
        { property: { apiName: '' } },
      ],
      numNeighbors: 5,
      query: { text: { value: 'q' } },
    };

    render(
      <ObjectSetBuilder
        objectTypes={objectTypes}
        value={value}
        onChange={onChange}
      />,
    );

    const cols = screen.getAllByLabelText(/embedding property/i);
    expect(cols).toHaveLength(2);
    fireEvent.change(cols[1], { target: { value: 'body_vec' } });

    const next = onChange.mock.calls[0][0] as NearestNeighborsObjectSet;
    expect(next.propertyIdentifiers).toEqual([
      { property: { apiName: 'title_vec' } },
      { property: { apiName: 'body_vec' } },
    ]);
  });

  it('Given one column, When rendered, Then no fusionStrategy select is shown', () => {
    render(
      <ObjectSetBuilder
        objectTypes={objectTypes}
        value={{
          type: 'nearestNeighbors',
          objectSet: { type: 'base', objectType: 'Incident' },
          propertyIdentifiers: [{ property: { apiName: 'embedding' } }],
          numNeighbors: 5,
          query: { text: { value: 'q' } },
        }}
        onChange={vi.fn()}
      />,
    );
    expect(
      screen.queryByRole('combobox', { name: /fusion strategy/i }),
    ).not.toBeInTheDocument();
  });

  it('Given two columns, When the user picks rrf fusion, Then fusionStrategy=rrf is emitted', () => {
    const onChange = vi.fn();
    render(
      <ObjectSetBuilder
        objectTypes={objectTypes}
        value={{
          type: 'nearestNeighbors',
          objectSet: { type: 'base', objectType: 'Incident' },
          propertyIdentifiers: [
            { property: { apiName: 'title_vec' } },
            { property: { apiName: 'body_vec' } },
          ],
          numNeighbors: 5,
          query: { text: { value: 'q' } },
        }}
        onChange={onChange}
      />,
    );

    fireEvent.change(
      screen.getByRole('combobox', { name: /fusion strategy/i }),
      { target: { value: 'rrf' } },
    );

    const next = onChange.mock.calls[0][0] as NearestNeighborsObjectSet;
    expect(next.fusionStrategy).toBe('rrf');
  });

  it('Given a multi-column NN definition, When loaded in the composer, Then it is editable and executable', () => {
    const value: ObjectSetDefinition = {
      type: 'nearestNeighbors',
      objectSet: { type: 'base', objectType: 'Incident' },
      propertyIdentifiers: [
        { property: { apiName: 'title_vec' } },
        { property: { apiName: 'body_vec' } },
      ],
      fusionStrategy: 'min',
      numNeighbors: 3,
      query: { text: { value: 'find similar' } },
    };

    render(
      <ObjectSetComposer
        objectTypes={objectTypes}
        value={value}
        onChange={vi.fn()}
        onExecute={vi.fn()}
        onSaveAs={() => {}}
        savedObjectSets={[]}
        onLoadSaved={() => {}}
        onDeleteSaved={() => {}}
      />,
    );

    expect(screen.getByRole('button', { name: /execute/i })).toBeEnabled();
    expect(
      screen.getByRole('combobox', { name: /fusion strategy/i }),
    ).toHaveValue('min');
  });
});

describe('BDD: nearestNeighbors raw vector query (A6)', () => {
  const objectTypes = ['Incident'];

  it('Given text query mode, When the user switches to vector mode, Then a textarea appears and text is cleared', () => {
    const onChange = vi.fn();
    render(
      <ObjectSetBuilder
        objectTypes={objectTypes}
        value={{
          type: 'nearestNeighbors',
          objectSet: { type: 'base', objectType: 'Incident' },
          propertyIdentifier: { property: { apiName: 'embedding' } },
          numNeighbors: 5,
          query: { text: { value: 'hello' } },
        }}
        onChange={onChange}
      />,
    );

    fireEvent.change(
      screen.getByRole('combobox', { name: /query mode/i }),
      { target: { value: 'vector' } },
    );

    const next = onChange.mock.calls[0][0] as NearestNeighborsObjectSet;
    expect(next.query).toEqual({ vector: { value: [] } });
  });

  it('Given vector mode, When the user types comma-separated floats, Then query.vector.value is parsed to numbers', () => {
    const onChange = vi.fn();
    render(
      <ObjectSetBuilder
        objectTypes={objectTypes}
        value={{
          type: 'nearestNeighbors',
          objectSet: { type: 'base', objectType: 'Incident' },
          propertyIdentifier: { property: { apiName: 'embedding' } },
          numNeighbors: 5,
          query: { vector: { value: [] } },
        }}
        onChange={onChange}
      />,
    );

    fireEvent.change(screen.getByLabelText(/query vector/i), {
      target: { value: '0.1, 0.2, -0.3' },
    });

    const next = onChange.mock.calls[0][0] as NearestNeighborsObjectSet;
    expect(next.query).toEqual({ vector: { value: [0.1, 0.2, -0.3] } });
  });

  it('Given a vector-query NN definition, When loaded, Then the textarea shows the floats', () => {
    render(
      <ObjectSetBuilder
        objectTypes={objectTypes}
        value={{
          type: 'nearestNeighbors',
          objectSet: { type: 'base', objectType: 'Incident' },
          propertyIdentifier: { property: { apiName: 'embedding' } },
          numNeighbors: 5,
          query: { vector: { value: [0.5, 0.25] } },
        }}
        onChange={vi.fn()}
      />,
    );

    expect(screen.getByLabelText(/query vector/i)).toHaveValue('0.5, 0.25');
    expect(
      screen.queryByLabelText(/query text/i),
    ).not.toBeInTheDocument();
  });
});

describe('BDD: sample ObjectSet (A7)', () => {
  const objectTypes = ['Incident', 'Device'];

  it('Given a user switches to sample, When the type changes, Then a backend-compatible definition is started', () => {
    const onChange = vi.fn();
    render(
      <ObjectSetBuilder
        objectTypes={objectTypes}
        value={{ type: 'base', objectType: 'Incident' }}
        onChange={onChange}
      />,
    );

    fireEvent.change(
      screen.getByRole('combobox', { name: /objectset type/i }),
      { target: { value: 'sample' } },
    );

    expect(onChange).toHaveBeenCalledWith({
      type: 'sample',
      objectSet: { type: 'base', objectType: 'Incident' },
      size: 10,
    });
  });

  it('Given a sample definition, When the user edits the size, Then size is emitted as a number', () => {
    const onChange = vi.fn();
    const value: SampleObjectSet = {
      type: 'sample',
      objectSet: { type: 'base', objectType: 'Incident' },
      size: 10,
    };
    render(
      <ObjectSetBuilder
        objectTypes={objectTypes}
        value={value}
        onChange={onChange}
      />,
    );

    fireEvent.change(screen.getByLabelText(/sample size/i), {
      target: { value: '25' },
    });

    const next = onChange.mock.calls[0][0] as SampleObjectSet;
    expect(next.size).toBe(25);
  });

  it('Given a sample definition, When the user sets a seed, Then seed is emitted as a number', () => {
    const onChange = vi.fn();
    const value: SampleObjectSet = {
      type: 'sample',
      objectSet: { type: 'base', objectType: 'Incident' },
      size: 10,
    };
    render(
      <ObjectSetBuilder
        objectTypes={objectTypes}
        value={value}
        onChange={onChange}
      />,
    );

    fireEvent.change(screen.getByLabelText(/sample seed/i), {
      target: { value: '42' },
    });

    const next = onChange.mock.calls[0][0] as SampleObjectSet;
    expect(next.seed).toBe(42);
  });

  it('Given a sample definition, When loaded in the composer, Then it is editable and executable', () => {
    const value: ObjectSetDefinition = {
      type: 'sample',
      objectSet: { type: 'base', objectType: 'Incident' },
      size: 5,
      seed: 7,
    };
    render(
      <ObjectSetComposer
        objectTypes={objectTypes}
        value={value}
        onChange={vi.fn()}
        onExecute={vi.fn()}
        onSaveAs={() => {}}
        savedObjectSets={[]}
        onLoadSaved={() => {}}
        onDeleteSaved={() => {}}
      />,
    );

    expect(
      screen.queryByTestId('objectset-readonly-unsupported'),
    ).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /execute/i })).toBeEnabled();
    expect(screen.getByLabelText(/sample size/i)).toHaveValue(5);
    expect(screen.getByLabelText(/sample seed/i)).toHaveValue(7);
  });

  it('Given a sample with size <= 0, When validated, Then it is not executable', () => {
    const value: ObjectSetDefinition = {
      type: 'sample',
      objectSet: { type: 'base', objectType: 'Incident' },
      size: 0,
    };
    render(
      <ObjectSetComposer
        objectTypes={objectTypes}
        value={value}
        onChange={vi.fn()}
        onExecute={vi.fn()}
        onSaveAs={() => {}}
        savedObjectSets={[]}
        onLoadSaved={() => {}}
        onDeleteSaved={() => {}}
      />,
    );

    expect(screen.getByRole('button', { name: /execute/i })).toBeDisabled();
  });
});
