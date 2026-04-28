import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { FormProvider, useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { ParameterForm } from '../ParameterForm';
import type { ActionParameterV2 } from '../../../api/types';
import {
  buildParameterDefaults,
  buildParameterZodSchema,
} from '../parameterSchema';

const mockParameters: Record<string, ActionParameterV2> = {
  name: { dataType: { type: 'string' }, required: true, description: 'Employee name' },
  age: { dataType: { type: 'integer' }, required: false, description: 'Employee age' },
  active: { dataType: { type: 'boolean' }, required: false, description: 'Is active' },
};

function Harness({
  parameters = mockParameters,
  onSubmit = vi.fn(),
}: {
  parameters?: Record<string, ActionParameterV2>;
  onSubmit?: (values: Record<string, unknown>) => void;
}) {
  const form = useForm<Record<string, unknown>>({
    resolver: zodResolver(buildParameterZodSchema(parameters)),
    defaultValues: buildParameterDefaults(parameters),
    mode: 'onBlur',
  });
  return (
    <FormProvider {...form}>
      <form onSubmit={form.handleSubmit(onSubmit)} noValidate>
        <ParameterForm parameters={parameters} />
        <button type="submit">Submit</button>
      </form>
    </FormProvider>
  );
}

describe('ParameterForm', () => {
  it('renders text input for string parameters', () => {
    render(<Harness />);
    const input = screen.getByPlaceholderText('Employee name');
    expect(input).toBeInTheDocument();
    expect(input).toHaveAttribute('type', 'text');
  });

  it('renders number input for integer parameters', () => {
    render(<Harness />);
    const input = screen.getByPlaceholderText('integer');
    expect(input).toBeInTheDocument();
    expect(input).toHaveAttribute('type', 'number');
  });

  it('renders checkbox for boolean parameters', () => {
    render(<Harness />);
    const checkbox = screen.getByRole('checkbox');
    expect(checkbox).toBeInTheDocument();
  });

  it('submits typed values when inputs are filled', async () => {
    const onSubmit = vi.fn();
    render(<Harness onSubmit={onSubmit} />);

    fireEvent.change(screen.getByPlaceholderText('Employee name'), {
      target: { value: 'Alice' },
    });
    fireEvent.change(screen.getByPlaceholderText('integer'), {
      target: { value: '42' },
    });
    fireEvent.click(screen.getByRole('checkbox'));
    fireEvent.click(screen.getByText('Submit'));

    await new Promise((r) => setTimeout(r, 0));

    expect(onSubmit).toHaveBeenCalledTimes(1);
    const submitted = onSubmit.mock.calls[0][0] as Record<string, unknown>;
    expect(submitted).toMatchObject({ name: 'Alice', age: 42, active: true });
  });

  it('shows a Required field error when a required input is empty on submit', async () => {
    const onSubmit = vi.fn();
    render(<Harness onSubmit={onSubmit} />);

    fireEvent.click(screen.getByText('Submit'));

    const error = await screen.findByRole('alert');
    expect(error).toHaveTextContent(/required/i);
    expect(onSubmit).not.toHaveBeenCalled();
  });
});
