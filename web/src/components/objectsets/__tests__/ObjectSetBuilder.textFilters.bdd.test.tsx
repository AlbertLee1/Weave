import { useState } from 'react';
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ObjectSetBuilder } from '../ObjectSetBuilder';
import { ObjectSetComposer } from '../ObjectSetComposer';
import type { ObjectSetDefinition } from '../../../api/types';

const textOperators = [
  'contains',
  'containsAllTerms',
  'containsAllTermsInOrder',
] as const;

function StatefulBuilder({
  initial,
  onValue,
}: {
  initial: ObjectSetDefinition;
  onValue: (value: ObjectSetDefinition) => void;
}) {
  const [value, setValue] = useState(initial);
  return (
    <ObjectSetBuilder
      objectTypes={['Employee']}
      value={value}
      onChange={(next) => {
        setValue(next);
        onValue(next);
      }}
    />
  );
}

function StatefulComposer({
  initial,
}: {
  initial: ObjectSetDefinition;
}) {
  const [value, setValue] = useState(initial);
  return (
    <ObjectSetComposer
      objectTypes={['Employee']}
      value={value}
      onChange={setValue}
      onExecute={vi.fn()}
      onSaveAs={() => {}}
      savedObjectSets={[]}
      onLoadSaved={() => {}}
      onDeleteSaved={() => {}}
    />
  );
}

describe('BDD: text-search Where operators in ObjectSet composer (SELF-442)', () => {
  it.each(textOperators)(
    'Given the user selects %s, When they enter a field and text value, Then the wire WhereClause uses that backend text operator',
    (operator) => {
      const onValue = vi.fn();
      render(
        <StatefulBuilder
          initial={{
            type: 'filter',
            objectSet: { type: 'base', objectType: 'Employee' },
            where: { type: 'eq', field: '', value: '' },
          }}
          onValue={onValue}
        />,
      );

      fireEvent.change(screen.getByRole('combobox', { name: /where type/i }), {
        target: { value: operator },
      });
      fireEvent.change(screen.getByLabelText(/where field/i), {
        target: { value: 'description' },
      });
      fireEvent.change(screen.getByLabelText(/where value/i), {
        target: { value: 'critical outage' },
      });

      expect(onValue).toHaveBeenLastCalledWith({
        type: 'filter',
        objectSet: { type: 'base', objectType: 'Employee' },
        where: {
          type: operator,
          field: 'description',
          value: 'critical outage',
        },
      });
    },
  );

  it('Given the user selects startsWith, When they enter a prefix, Then validation enables Execute', () => {
    render(
      <StatefulComposer
        initial={{
          type: 'filter',
          objectSet: { type: 'base', objectType: 'Employee' },
          where: { type: 'eq', field: '', value: '' },
        }}
      />,
    );

    fireEvent.change(screen.getByRole('combobox', { name: /where type/i }), {
      target: { value: 'startsWith' },
    });
    fireEvent.change(screen.getByLabelText(/where field/i), {
      target: { value: 'name' },
    });
    fireEvent.change(screen.getByLabelText(/where value/i), {
      target: { value: 'Acme' },
    });

    expect(screen.getByRole('combobox', { name: /where type/i })).toHaveValue(
      'startsWith',
    );
    expect(screen.getByRole('button', { name: /execute/i })).toBeEnabled();
    expect(screen.queryByText(/validation/i)).not.toBeInTheDocument();
  });

  it('Given a shared definition already uses a text-search operator, When loaded in the composer, Then the operator is editable', () => {
    render(
      <StatefulComposer
        initial={{
          type: 'filter',
          objectSet: { type: 'base', objectType: 'Employee' },
          where: {
            type: 'containsAllTerms',
            field: 'description',
            value: 'critical outage',
          },
        }}
      />,
    );

    expect(screen.getByRole('combobox', { name: /where type/i })).toHaveValue(
      'containsAllTerms',
    );
    expect(screen.getByLabelText(/where field/i)).toHaveValue('description');
    expect(screen.getByLabelText(/where value/i)).toHaveValue('critical outage');
    expect(screen.queryByText(/unsupported/i)).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /execute/i })).toBeEnabled();
  });
});
