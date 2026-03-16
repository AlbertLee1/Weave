import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { FilterChip } from '../FilterChip';

describe('FilterChip', () => {
  it('renders label and value', () => {
    render(<FilterChip label="name" value="Alice" onRemove={() => {}} />);
    expect(screen.getByText('name:')).toBeInTheDocument();
    expect(screen.getByText('Alice')).toBeInTheDocument();
  });

  it('calls onRemove when close button is clicked', () => {
    const onRemove = vi.fn();
    render(<FilterChip label="name" value="Alice" onRemove={onRemove} />);

    fireEvent.click(screen.getByRole('button', { name: /remove filter/i }));
    expect(onRemove).toHaveBeenCalled();
  });
});
