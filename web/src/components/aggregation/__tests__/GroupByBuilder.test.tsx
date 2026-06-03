import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { GroupByBuilder } from '../GroupByBuilder';

describe('GroupByBuilder', () => {
  it('shows the width input only for fixedWidth grouping', () => {
    const { rerender } = render(
      <GroupByBuilder
        groupBy={[{ field: 'freight', type: 'exact' }]}
        onChange={() => {}}
        availableFields={['freight']}
      />,
    );
    expect(screen.queryByTestId('groupby-0-fixedWidth')).toBeNull();

    rerender(
      <GroupByBuilder
        groupBy={[{ field: 'freight', type: 'fixedWidth', fixedWidth: 10 }]}
        onChange={() => {}}
        availableFields={['freight']}
      />,
    );
    expect(screen.getByTestId('groupby-0-fixedWidth')).toHaveValue(10);
  });

  it('updates the width as a number', () => {
    const onChange = vi.fn();
    render(
      <GroupByBuilder
        groupBy={[{ field: 'freight', type: 'fixedWidth' }]}
        onChange={onChange}
        availableFields={['freight']}
      />,
    );
    fireEvent.change(screen.getByTestId('groupby-0-fixedWidth'), {
      target: { value: '25' },
    });
    expect(onChange).toHaveBeenCalledWith([
      { field: 'freight', type: 'fixedWidth', fixedWidth: 25 },
    ]);
  });

  it('drops the width when switching away from fixedWidth', () => {
    const onChange = vi.fn();
    render(
      <GroupByBuilder
        groupBy={[{ field: 'freight', type: 'fixedWidth', fixedWidth: 10 }]}
        onChange={onChange}
        availableFields={['freight']}
      />,
    );
    fireEvent.change(screen.getByTestId('groupby-0-type'), {
      target: { value: 'exact' },
    });
    expect(onChange).toHaveBeenCalledWith([{ field: 'freight', type: 'exact' }]);
  });

  it('clears the width to undefined when the input is emptied', () => {
    const onChange = vi.fn();
    render(
      <GroupByBuilder
        groupBy={[{ field: 'freight', type: 'fixedWidth', fixedWidth: 10 }]}
        onChange={onChange}
        availableFields={['freight']}
      />,
    );
    fireEvent.change(screen.getByTestId('groupby-0-fixedWidth'), {
      target: { value: '' },
    });
    expect(onChange).toHaveBeenCalledWith([
      { field: 'freight', type: 'fixedWidth', fixedWidth: undefined },
    ]);
  });
});
