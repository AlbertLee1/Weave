import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MetricSelector } from '../MetricSelector';

describe('MetricSelector', () => {
  it('renders existing metrics', () => {
    render(
      <MetricSelector
        metrics={[{ type: 'count' }]}
        onChange={() => {}}
        availableFields={['name', 'age']}
      />,
    );
    expect(screen.getByText('count')).toBeInTheDocument();
  });

  it('calls onChange when add button is clicked', () => {
    const onChange = vi.fn();
    render(
      <MetricSelector
        metrics={[]}
        onChange={onChange}
        availableFields={['name', 'age']}
      />,
    );

    fireEvent.click(screen.getByText(/add metric/i));
    expect(onChange).toHaveBeenCalled();
  });

  it('calls onChange when remove button is clicked', () => {
    const onChange = vi.fn();
    render(
      <MetricSelector
        metrics={[{ type: 'count' }, { type: 'sum', field: 'age' }]}
        onChange={onChange}
        availableFields={['name', 'age']}
      />,
    );

    const removeButtons = screen.getAllByRole('button', { name: /remove/i });
    fireEvent.click(removeButtons[0]);
    expect(onChange).toHaveBeenCalledWith([{ type: 'sum', field: 'age' }]);
  });
});
