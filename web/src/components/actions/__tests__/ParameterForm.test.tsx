import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ParameterForm } from '../ParameterForm';

const mockParameters = {
  name: { type: 'string', description: 'Employee name' },
  age: { type: 'integer', description: 'Employee age' },
  active: { type: 'boolean', description: 'Is active' },
};

describe('ParameterForm', () => {
  it('renders text input for string parameters', () => {
    render(
      <ParameterForm
        parameters={mockParameters}
        values={{}}
        onChange={() => {}}
      />,
    );
    const input = screen.getByPlaceholderText('Employee name');
    expect(input).toBeInTheDocument();
    expect(input).toHaveAttribute('type', 'text');
  });

  it('renders number input for integer parameters', () => {
    render(
      <ParameterForm
        parameters={mockParameters}
        values={{}}
        onChange={() => {}}
      />,
    );
    const input = screen.getByPlaceholderText('integer');
    expect(input).toBeInTheDocument();
    expect(input).toHaveAttribute('type', 'number');
  });

  it('renders checkbox for boolean parameters', () => {
    render(
      <ParameterForm
        parameters={mockParameters}
        values={{}}
        onChange={() => {}}
      />,
    );
    const checkbox = screen.getByRole('checkbox');
    expect(checkbox).toBeInTheDocument();
  });

  it('calls onChange when input value changes', () => {
    const onChange = vi.fn();
    render(
      <ParameterForm
        parameters={mockParameters}
        values={{}}
        onChange={onChange}
      />,
    );

    fireEvent.change(screen.getByPlaceholderText('Employee name'), {
      target: { value: 'Alice' },
    });
    expect(onChange).toHaveBeenCalledWith({ name: 'Alice' });
  });
});
