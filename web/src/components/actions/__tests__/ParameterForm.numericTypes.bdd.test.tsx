import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { FormProvider, useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { ParameterForm } from '../ParameterForm';
import type { ActionParameterV2 } from '../../../api/types';
import {
  buildParameterDefaults,
  buildParameterZodSchema,
} from '../parameterSchema';

// BDD coverage for the full numeric base-type family on action parameters.
//
// Real bug (not just UX): ParameterForm's numeric branch only handled
// `integer` / `double`. The other numeric base types — `short`, `long`,
// `float`, `byte`, `decimal` — fell through to the DEFAULT TEXT branch and so
// emitted a STRING. The Go coercion (pkg/types/coerce.go) is inconsistent:
//   - coerceShort has NO `case string` → a `short` submitted as a string fails
//     with "cannot coerce string to short". So `short` params were UNUSABLE
//     from the UI. THIS IS THE LOAD-BEARING REGRESSION.
//   - coerceLong / coerceInteger / coerceFloat DO accept strings, so those
//     "worked" by luck but still sent a string instead of a number.
//   - `byte` / `decimal` are not in the Coerce switch (fall to `default:
//     return value`) and then Validate runs: Byte accepts float64; Decimal
//     accepts float64 (and string). A JSON number is accepted by ALL of them.
//
// Fix contract (verified against pkg/types/coerce.go + validate.go): every
// numeric type renders <input type="number"> and emits a JSON *number*:
//   - integer-family (integer, short, long, byte): step="1", parseInt.
//   - float-family   (double, float, decimal):     step="any", parseFloat.
// The key regression assertion is `typeof value === 'number'` (NOT a string),
// which is exactly what was broken for `short`.

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

const integerFamily: Array<{ type: string; step: string; example: string; expected: number }> = [
  { type: 'integer', step: '1', example: '42', expected: 42 },
  { type: 'short', step: '1', example: '7', expected: 7 },
  { type: 'long', step: '1', example: '9000000000', expected: 9000000000 },
  { type: 'byte', step: '1', example: '120', expected: 120 },
];

const floatFamily: Array<{ type: string; step: string; example: string; expected: number }> = [
  { type: 'double', step: 'any', example: '3.14', expected: 3.14 },
  { type: 'float', step: 'any', example: '2.5', expected: 2.5 },
  { type: 'decimal', step: 'any', example: '19.99', expected: 19.99 },
];

describe('BDD: ParameterForm numeric-family params render number inputs', () => {
  for (const { type, step } of [...integerFamily, ...floatFamily]) {
    it(`Given a ${type} param, Then it renders a native number input with step="${step}"`, () => {
      const params: Record<string, ActionParameterV2> = {
        qty: { dataType: { type }, required: true, description: `a ${type}` },
      };
      render(<Harness parameters={params} />);
      const input = screen.getByTestId('param-qty') as HTMLInputElement;
      expect(input).toBeInTheDocument();
      expect(input).toHaveAttribute('type', 'number');
      expect(input).toHaveAttribute('step', step);
    });
  }
});

describe('BDD: ParameterForm numeric-family params emit a JSON number (not a string)', () => {
  for (const { type, example, expected } of [...integerFamily, ...floatFamily]) {
    it(`When a ${type} value is entered, Then applying sends a JSON number`, async () => {
      const onSubmit = vi.fn();
      const params: Record<string, ActionParameterV2> = {
        qty: { dataType: { type }, required: true },
      };
      render(<Harness parameters={params} onSubmit={onSubmit} />);

      fireEvent.change(screen.getByTestId('param-qty'), {
        target: { value: example },
      });
      fireEvent.click(screen.getByText('Submit'));

      await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
      const submitted = onSubmit.mock.calls[0][0] as Record<string, unknown>;
      // The load-bearing contract: the wire value is a NUMBER, never a string.
      // This is what was broken for `short` (coerceShort has no `case string`).
      expect(typeof submitted.qty).toBe('number');
      expect(submitted.qty).not.toBe(String(example));
      expect(submitted.qty).toBe(expected);
    });
  }
});

describe('BDD: short param (the real regression) is usable end to end', () => {
  it('Given a short param, When applied, Then the value is a number coercible by coerceShort', async () => {
    const onSubmit = vi.fn();
    const params: Record<string, ActionParameterV2> = {
      seatCount: { dataType: { type: 'short' }, required: true, description: 'seats' },
    };
    render(<Harness parameters={params} onSubmit={onSubmit} />);

    const input = screen.getByTestId('param-seatCount') as HTMLInputElement;
    expect(input).toHaveAttribute('type', 'number');
    fireEvent.change(input, { target: { value: '32' } });
    fireEvent.click(screen.getByText('Submit'));

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    const submitted = onSubmit.mock.calls[0][0] as Record<string, unknown>;
    // Regression guard: NOT a string. coerceShort would reject "32".
    expect(typeof submitted.seatCount).toBe('number');
    expect(submitted.seatCount).toBe(32);
  });
});

describe('BDD: numeric-family optional/required handling matches integer/double', () => {
  for (const { type } of [...integerFamily, ...floatFamily]) {
    it(`Given an optional ${type} left empty, Then it is omitted from the payload`, async () => {
      const onSubmit = vi.fn();
      const params: Record<string, ActionParameterV2> = {
        qty: { dataType: { type }, required: false },
      };
      render(<Harness parameters={params} onSubmit={onSubmit} />);

      fireEvent.click(screen.getByText('Submit'));
      await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
      const submitted = onSubmit.mock.calls[0][0] as Record<string, unknown>;
      expect(submitted.qty).toBeUndefined();
    });

    it(`When a required ${type} is left empty, Then apply is blocked`, async () => {
      const onSubmit = vi.fn();
      const params: Record<string, ActionParameterV2> = {
        qty: { dataType: { type }, required: true },
      };
      render(<Harness parameters={params} onSubmit={onSubmit} />);

      fireEvent.click(screen.getByText('Submit'));
      await waitFor(() =>
        expect(screen.getByTestId('param-qty')).toHaveAttribute('aria-invalid', 'true'),
      );
      expect(onSubmit).not.toHaveBeenCalled();
    });
  }
});

describe('BDD: integer-family rejects a fractional value (coerce* requires whole)', () => {
  // coerceShort/coerceLong/coerceInteger reject a float64 with a fractional part
  // ("has fractional part"). The Zod schema mirrors that with .int().
  for (const { type } of integerFamily) {
    it(`When a fractional value is entered for ${type}, Then apply is blocked`, async () => {
      const onSubmit = vi.fn();
      const params: Record<string, ActionParameterV2> = {
        qty: { dataType: { type }, required: true },
      };
      render(<Harness parameters={params} onSubmit={onSubmit} />);

      fireEvent.change(screen.getByTestId('param-qty'), { target: { value: '1.5' } });
      fireEvent.click(screen.getByText('Submit'));

      await waitFor(() =>
        expect(screen.getByTestId('param-qty')).toHaveAttribute('aria-invalid', 'true'),
      );
      expect(onSubmit).not.toHaveBeenCalled();
    });
  }
});
