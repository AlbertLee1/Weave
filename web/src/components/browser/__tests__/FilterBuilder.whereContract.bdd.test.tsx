import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { FilterBuilder } from '../FilterBuilder';
import { buildWhereClause, type FilterCondition } from '../../../lib/whereBuilder';

const properties = {
  title: { dataType: { type: 'string' }, rid: 'ri.property.title' },
  rank: { dataType: { type: 'number' }, rid: 'ri.property.rank' },
};

describe('BDD: Browser FilterBuilder emits canonical WhereClause values (SELF-443)', () => {
  it('Given contains any term is selected, When the user adds OpenAI Codex, Then the filter stores a string value', () => {
    const onFiltersChange = vi.fn();
    render(
      <FilterBuilder
        properties={properties}
        filters={[]}
        onFiltersChange={onFiltersChange}
      />,
    );

    fireEvent.change(screen.getByTestId('filter-op-select'), {
      target: { value: 'containsAnyTerm' },
    });
    fireEvent.change(screen.getByTestId('filter-value-input'), {
      target: { value: '  OpenAI   Codex  ' },
    });
    fireEvent.click(screen.getByTestId('filter-add-btn'));

    expect(onFiltersChange).toHaveBeenCalledWith([
      { field: 'title', op: 'containsAnyTerm', value: 'OpenAI Codex' },
    ]);
  });

  it('Given multiple filters include containsAnyTerm, When serialized, Then the and group keeps a string value', () => {
    const filters: FilterCondition[] = [
      { field: 'title', op: 'containsAnyTerm', value: 'OpenAI Codex' },
      { field: 'rank', op: 'gt', value: 10 },
    ];

    expect(buildWhereClause(filters)).toEqual({
      type: 'and',
      value: [
        { type: 'containsAnyTerm', field: 'title', value: 'OpenAI Codex' },
        { type: 'gt', field: 'rank', value: 10 },
      ],
    });
  });

  it('Given a canonical containsAnyTerm filter is active, When rendered, Then the chip shows the readable value', () => {
    render(
      <FilterBuilder
        properties={properties}
        filters={[
          { field: 'title', op: 'containsAnyTerm', value: 'OpenAI Codex' },
        ]}
        onFiltersChange={vi.fn()}
      />,
    );

    expect(screen.getByText('title contains any term:')).toBeInTheDocument();
    expect(screen.getByText('OpenAI Codex')).toBeInTheDocument();
  });
});
