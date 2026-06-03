import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { FormProvider, useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { ParameterForm } from '../ParameterForm';
import type { ActionParameterV2 } from '../../../api/types';
import {
  buildParameterDefaults,
  buildParameterZodSchema,
} from '../parameterSchema';

// BDD coverage for date / timestamp action-parameter pickers.
//
// Wire-format contract (verified against the Go coercion path):
//   pkg/types/coerce.go::coerceDate     -> time.Parse("2006-01-02", v)  => "YYYY-MM-DD"
//   pkg/types/coerce.go::coerceTimestamp-> time.Parse(time.RFC3339, v)  => RFC3339
// The HTML pickers emit:
//   <input type="date">           -> "YYYY-MM-DD"            (already canonical)
//   <input type="datetime-local"> -> "YYYY-MM-DDTHH:mm"      (needs ":00Z" appended)
// so the widget must normalise the datetime-local value to RFC3339 before it
// reaches the form value / wire payload.

function Harness({
  parameters,
  onSubmit = vi.fn(),
}: {
  parameters: Record<string, ActionParameterV2>;
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

describe('BDD: ParameterForm date picker', () => {
  const dateParams: Record<string, ActionParameterV2> = {
    dueDate: {
      dataType: { type: 'date' },
      required: true,
      description: 'When the task is due',
    },
  };

  it('Given a date param, Then it renders a native date input (not free-text)', () => {
    render(<Harness parameters={dateParams} />);
    const input = screen.getByTestId('param-dueDate') as HTMLInputElement;
    expect(input).toBeInTheDocument();
    expect(input).toHaveAttribute('type', 'date');
  });

  it('When a date is chosen, Then applying sends the canonical YYYY-MM-DD string', async () => {
    const onSubmit = vi.fn();
    render(<Harness parameters={dateParams} onSubmit={onSubmit} />);

    const input = screen.getByTestId('param-dueDate') as HTMLInputElement;
    fireEvent.change(input, { target: { value: '2026-03-15' } });
    fireEvent.click(screen.getByText('Submit'));

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    const submitted = onSubmit.mock.calls[0][0] as Record<string, unknown>;
    expect(submitted.dueDate).toBe('2026-03-15');
  });

  it('When a required date is left empty, Then apply is blocked', async () => {
    const onSubmit = vi.fn();
    render(<Harness parameters={dateParams} onSubmit={onSubmit} />);

    fireEvent.click(screen.getByText('Submit'));

    await waitFor(() =>
      expect(screen.getByTestId('param-dueDate')).toHaveAttribute(
        'aria-invalid',
        'true',
      ),
    );
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('Given an optional date left empty, Then the field is omitted from the payload', async () => {
    const onSubmit = vi.fn();
    const optionalParams: Record<string, ActionParameterV2> = {
      effectiveDate: { dataType: { type: 'date' }, required: false },
    };
    render(<Harness parameters={optionalParams} onSubmit={onSubmit} />);

    fireEvent.click(screen.getByText('Submit'));
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    const submitted = onSubmit.mock.calls[0][0] as Record<string, unknown>;
    expect(submitted.effectiveDate).toBeUndefined();
  });
});

describe('BDD: ParameterForm timestamp picker', () => {
  const tsParams: Record<string, ActionParameterV2> = {
    occurredAt: {
      dataType: { type: 'timestamp' },
      required: true,
      description: 'Event instant',
    },
  };

  it('Given a timestamp param, Then it renders a datetime-local input', () => {
    render(<Harness parameters={tsParams} />);
    const input = screen.getByTestId('param-occurredAt') as HTMLInputElement;
    expect(input).toBeInTheDocument();
    expect(input).toHaveAttribute('type', 'datetime-local');
  });

  it('When a datetime is chosen, Then applying sends an RFC3339 string the backend parses', async () => {
    const onSubmit = vi.fn();
    render(<Harness parameters={tsParams} onSubmit={onSubmit} />);

    const input = screen.getByTestId('param-occurredAt') as HTMLInputElement;
    // datetime-local yields "YYYY-MM-DDTHH:mm" (no seconds / zone).
    fireEvent.change(input, { target: { value: '2026-03-15T09:30' } });
    fireEvent.click(screen.getByText('Submit'));

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    const submitted = onSubmit.mock.calls[0][0] as Record<string, unknown>;
    const value = submitted.occurredAt as string;
    // Must be RFC3339 (Go: 2006-01-02T15:04:05Z07:00). Seconds + zone present.
    expect(value).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(Z|[+-]\d{2}:\d{2})$/);
    // Deterministic: the chosen wall-clock time is preserved as a UTC instant.
    expect(value).toBe('2026-03-15T09:30:00Z');
  });

  it('When a datetime that already carries seconds is chosen, Then it round-trips to RFC3339', async () => {
    const onSubmit = vi.fn();
    render(<Harness parameters={tsParams} onSubmit={onSubmit} />);

    const input = screen.getByTestId('param-occurredAt') as HTMLInputElement;
    fireEvent.change(input, { target: { value: '2026-03-15T09:30:45' } });
    fireEvent.click(screen.getByText('Submit'));

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    const submitted = onSubmit.mock.calls[0][0] as Record<string, unknown>;
    expect(submitted.occurredAt).toBe('2026-03-15T09:30:45Z');
  });

  it('When a required timestamp is left empty, Then apply is blocked', async () => {
    const onSubmit = vi.fn();
    render(<Harness parameters={tsParams} onSubmit={onSubmit} />);

    fireEvent.click(screen.getByText('Submit'));
    await waitFor(() =>
      expect(screen.getByTestId('param-occurredAt')).toHaveAttribute(
        'aria-invalid',
        'true',
      ),
    );
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('Given an optional timestamp left empty, Then the field is omitted from the payload', async () => {
    const onSubmit = vi.fn();
    const optionalParams: Record<string, ActionParameterV2> = {
      seenAt: { dataType: { type: 'timestamp' }, required: false },
    };
    render(<Harness parameters={optionalParams} onSubmit={onSubmit} />);

    fireEvent.click(screen.getByText('Submit'));
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    const submitted = onSubmit.mock.calls[0][0] as Record<string, unknown>;
    expect(submitted.seenAt).toBeUndefined();
  });
});

describe('BDD: ParameterForm date/timestamp reflect existing form value', () => {
  beforeEach(() => vi.restoreAllMocks());

  it('Given a prefilled timestamp value, Then the picker shows it without seconds/zone', () => {
    const tsParams: Record<string, ActionParameterV2> = {
      occurredAt: { dataType: { type: 'timestamp' }, required: false },
    };
    function Prefilled() {
      const form = useForm<Record<string, unknown>>({
        resolver: zodResolver(buildParameterZodSchema(tsParams)),
        defaultValues: { occurredAt: '2026-03-15T09:30:00Z' },
        mode: 'onBlur',
      });
      return (
        <FormProvider {...form}>
          <ParameterForm parameters={tsParams} />
        </FormProvider>
      );
    }
    render(<Prefilled />);
    const input = screen.getByTestId('param-occurredAt') as HTMLInputElement;
    // datetime-local control value must be "YYYY-MM-DDTHH:mm".
    expect(input.value).toBe('2026-03-15T09:30');
  });
});
