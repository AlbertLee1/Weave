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

  it.each([
    ['containsAllTerms', 'contains all terms'],
    ['containsAllTermsInOrder', 'phrase (in order)'],
  ])(
    'Given %s is selected, When the user enters terms, Then the generated WhereClause carries a string value',
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

      fireEvent.change(screen.getByTestId('filter-op-select'), {
        target: { value: operator },
      });
      fireEvent.change(screen.getByTestId('filter-value-input'), {
        target: { value: 'machine learning' },
      });
      fireEvent.click(screen.getByTestId('filter-add-btn'));

      const nextFilters = onFiltersChange.mock.calls[0][0];
      expect(nextFilters).toEqual([
        { field: 'title', op: operator, value: 'machine learning' },
      ]);
      expect(buildWhereClause(nextFilters)).toEqual({
        type: operator,
        field: 'title',
        value: 'machine learning',
      });
    },
  );

  it('Given isNull is selected, When the user picks true, Then the WhereClause carries a boolean value (not a string)', () => {
    const onFiltersChange = vi.fn();
    render(
      <FilterBuilder
        properties={properties}
        filters={[]}
        onFiltersChange={onFiltersChange}
      />,
    );

    expect(screen.getByRole('option', { name: 'is null' })).toHaveValue('isNull');

    fireEvent.change(screen.getByTestId('filter-op-select'), {
      target: { value: 'isNull' },
    });

    // isNull must surface a boolean selector, not a free-text input.
    const boolSelect = screen.getByTestId('filter-boolean-value-select');
    expect(screen.queryByTestId('filter-value-input')).not.toBeInTheDocument();
    fireEvent.change(boolSelect, { target: { value: 'true' } });
    fireEvent.click(screen.getByTestId('filter-add-btn'));

    const nextFilters = onFiltersChange.mock.calls[0][0];
    expect(nextFilters).toEqual([{ field: 'title', op: 'isNull', value: true }]);
    expect(buildWhereClause(nextFilters)).toEqual({
      type: 'isNull',
      field: 'title',
      value: true,
    });
  });

  it('Given isNull was selected, When the user switches to a text operator, Then the free-text input is not pre-filled with a stale true/false', () => {
    render(
      <FilterBuilder
        properties={properties}
        filters={[]}
        onFiltersChange={vi.fn()}
      />,
    );

    fireEvent.change(screen.getByTestId('filter-op-select'), {
      target: { value: 'isNull' },
    });
    // The boolean selector is showing 'true'.
    fireEvent.change(screen.getByTestId('filter-op-select'), {
      target: { value: 'containsAllTerms' },
    });

    const input = screen.getByTestId('filter-value-input') as HTMLInputElement;
    expect(input.value).toBe('');
  });

  it('Given isNull is selected, When the user picks false, Then the WhereClause carries boolean false', () => {
    const onFiltersChange = vi.fn();
    render(
      <FilterBuilder
        properties={properties}
        filters={[]}
        onFiltersChange={onFiltersChange}
      />,
    );

    fireEvent.change(screen.getByTestId('filter-op-select'), {
      target: { value: 'isNull' },
    });
    fireEvent.change(screen.getByTestId('filter-boolean-value-select'), {
      target: { value: 'false' },
    });
    fireEvent.click(screen.getByTestId('filter-add-btn'));

    const nextFilters = onFiltersChange.mock.calls[0][0];
    expect(nextFilters).toEqual([{ field: 'title', op: 'isNull', value: false }]);
    expect(buildWhereClause(nextFilters)).toEqual({
      type: 'isNull',
      field: 'title',
      value: false,
    });
  });
});
