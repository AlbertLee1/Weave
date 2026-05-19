import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { FilterBuilder } from '../FilterBuilder';
import { buildWhereClause } from '../../../lib/whereBuilder';

const properties = {
  title: { dataType: { type: 'string' }, rid: 'ri.property.title' },
  rank: { dataType: { type: 'number' }, rid: 'ri.property.rank' },
};

describe('BDD: Browser FilterBuilder exposes backend-supported Where operators (SELF-444)', () => {
  it.each([
    ['gte', '>='],
    ['lte', '<='],
  ])(
    'Given %s is selected, When the user enters a numeric value, Then the generated WhereClause keeps a numeric value',
    (operator, label) => {
      const onFiltersChange = vi.fn();
      render(
        <FilterBuilder
          properties={properties}
          filters={[]}
          onFiltersChange={onFiltersChange}
        />,
      );

      expect(screen.getByRole('option', { name: label })).toHaveValue(operator);

      fireEvent.change(screen.getByTestId('filter-field-select'), {
        target: { value: 'rank' },
      });
      fireEvent.change(screen.getByTestId('filter-op-select'), {
        target: { value: operator },
      });
      fireEvent.change(screen.getByTestId('filter-value-input'), {
        target: { value: '42' },
      });
      fireEvent.click(screen.getByTestId('filter-add-btn'));

      const nextFilters = onFiltersChange.mock.calls[0][0];
      expect(nextFilters).toEqual([
        { field: 'rank', op: operator, value: 42 },
      ]);
      expect(buildWhereClause(nextFilters)).toEqual({
        type: operator,
        field: 'rank',
        value: 42,
      });
    },
  );

  it('Given startsWith is selected, When the user enters a prefix, Then the generated WhereClause uses a string prefix', () => {
    const onFiltersChange = vi.fn();
    render(
      <FilterBuilder
        properties={properties}
        filters={[]}
        onFiltersChange={onFiltersChange}
      />,
    );

    expect(screen.getByRole('option', { name: 'starts with' })).toHaveValue(
      'startsWith',
    );

    fireEvent.change(screen.getByTestId('filter-op-select'), {
      target: { value: 'startsWith' },
    });
    fireEvent.change(screen.getByTestId('filter-value-input'), {
      target: { value: 'Open' },
    });
    fireEvent.click(screen.getByTestId('filter-add-btn'));

    const nextFilters = onFiltersChange.mock.calls[0][0];
    expect(nextFilters).toEqual([
      { field: 'title', op: 'startsWith', value: 'Open' },
    ]);
    expect(buildWhereClause(nextFilters)).toEqual({
      type: 'startsWith',
      field: 'title',
      value: 'Open',
    });
  });
});
