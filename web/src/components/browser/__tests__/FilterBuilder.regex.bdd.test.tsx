import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { FilterBuilder } from '../FilterBuilder';
import { buildWhereClause } from '../../../lib/whereBuilder';

const properties = {
  title: { dataType: { type: 'string' }, rid: 'ri.property.title' },
  rank: { dataType: { type: 'number' }, rid: 'ri.property.rank' },
};

describe('BDD: Browser FilterBuilder exposes the backend regex operator', () => {
  it('Given the FilterBuilder, When the user opens the operator menu, Then a "regex" option is offered', () => {
    render(
      <FilterBuilder
        properties={properties}
        filters={[]}
        onFiltersChange={vi.fn()}
      />,
    );

    // The operator <select> must carry a regex choice mapped to the backend
    // where-clause type "regex" (pkg/oss/where/converter.go case "regex").
    expect(screen.getByRole('option', { name: 'regex' })).toHaveValue('regex');
  });

  it('Given regex is selected, When the user types a pattern and adds the filter, Then a regex FilterCondition with a raw string pattern is emitted', () => {
    const onFiltersChange = vi.fn();
    render(
      <FilterBuilder
        properties={properties}
        filters={[]}
        onFiltersChange={onFiltersChange}
      />,
    );

    fireEvent.change(screen.getByTestId('filter-op-select'), {
      target: { value: 'regex' },
    });
    fireEvent.change(screen.getByTestId('filter-value-input'), {
      target: { value: '^a.*' },
    });
    fireEvent.click(screen.getByTestId('filter-add-btn'));

    const nextFilters = onFiltersChange.mock.calls[0][0];
    // The pattern must reach the wire untouched (no whitespace splitting, no
    // numeric coercion) so the Bleve RegexpQuery receives the literal pattern.
    expect(nextFilters).toEqual([
      { field: 'title', op: 'regex', value: '^a.*' },
    ]);
  });

  it('Given a regex FilterCondition, When buildWhereClause maps it, Then the WhereClause matches the backend regex shape', () => {
    // Backend contract (pkg/oss/where/converter.go convertRegex + regex_test.go):
    //   { type: "regex", field: <fieldName>, value: <pattern string> }
    const where = buildWhereClause([
      { field: 'name', op: 'regex', value: '^a.*' },
    ]);

    expect(where).toEqual({
      type: 'regex',
      field: 'name',
      value: '^a.*',
    });
  });

  it('Given regex is the active operator, When the user adds it, Then end-to-end the generated WhereClause is the backend regex shape', () => {
    const onFiltersChange = vi.fn();
    render(
      <FilterBuilder
        properties={properties}
        filters={[]}
        onFiltersChange={onFiltersChange}
      />,
    );

    fireEvent.change(screen.getByTestId('filter-op-select'), {
      target: { value: 'regex' },
    });
    fireEvent.change(screen.getByTestId('filter-value-input'), {
      target: { value: '.*li.*' },
    });
    fireEvent.click(screen.getByTestId('filter-add-btn'));

    const nextFilters = onFiltersChange.mock.calls[0][0];
    expect(buildWhereClause(nextFilters)).toEqual({
      type: 'regex',
      field: 'title',
      value: '.*li.*',
    });
  });
});
