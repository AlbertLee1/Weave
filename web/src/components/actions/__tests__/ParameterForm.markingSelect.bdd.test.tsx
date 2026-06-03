import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { FormProvider, useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { ParameterForm } from '../ParameterForm';
import type { ActionParameterV2 } from '../../../api/types';
import {
  buildParameterDefaults,
  buildParameterZodSchema,
} from '../parameterSchema';

// BDD coverage for marking-typed action-parameter selectors.
//
// Wire-format contract (verified against the Go side):
//   - `marking` is a base type (pkg/types/basetype.go) with no dedicated
//     Coerce/Validate case — it falls through to the pass-through `default`.
//   - A marking is identified everywhere by its NAME string: the grant API
//     body carries `marking` (the name), and the policy engine
//     (pkg/security/policy_engine.go RuleTypeMarkingSubset) compares marking
//     NAME strings against the object's marking set.
// Therefore the form value the backend expects for a marking param is the
// marking's `name` string (scalar) or an array of names (array-of-marking).

const MARKINGS_PATH = '/api/admin/markings';

const sampleMarkings = [
  { name: 'PII', displayName: 'Personally Identifiable Information', description: '', color: '#f00' },
  { name: 'SECRET', displayName: 'Top Secret', description: '', color: '#00f' },
];

const server = setupServer(
  http.get(MARKINGS_PATH, () => HttpResponse.json({ markings: sampleMarkings })),
);

beforeAll(() => server.listen());
afterEach(() => {
  vi.restoreAllMocks();
  server.resetHandlers();
});
afterAll(() => server.close());

function Harness({
  parameters,
  onSubmit = vi.fn(),
}: {
  parameters: Record<string, ActionParameterV2>;
  onSubmit?: (values: Record<string, unknown>) => void;
}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const form = useForm<Record<string, unknown>>({
    resolver: zodResolver(buildParameterZodSchema(parameters)),
    defaultValues: buildParameterDefaults(parameters),
    mode: 'onBlur',
  });
  return (
    <QueryClientProvider client={queryClient}>
      <FormProvider {...form}>
        <form onSubmit={form.handleSubmit(onSubmit)} noValidate>
          <ParameterForm parameters={parameters} />
          <button type="submit">Submit</button>
        </form>
      </FormProvider>
    </QueryClientProvider>
  );
}

describe('BDD: ParameterForm marking selector (scalar)', () => {
  const markingParams: Record<string, ActionParameterV2> = {
    classification: {
      dataType: { type: 'marking' },
      required: true,
      description: 'Marking to apply',
    },
  };

  it('Given a marking param, Then it renders a select populated from listMarkings (not free-text)', async () => {
    render(<Harness parameters={markingParams} />);
    const select = (await screen.findByTestId('param-classification')) as HTMLSelectElement;
    expect(select.tagName).toBe('SELECT');
    // Human display names are shown; one option per marking + a placeholder.
    await waitFor(() =>
      expect(
        screen.getByRole('option', { name: /Personally Identifiable Information/ }),
      ).toBeInTheDocument(),
    );
    expect(
      screen.getByRole('option', { name: /Top Secret/ }),
    ).toBeInTheDocument();
  });

  it('When a marking is chosen, Then applying sends the marking NAME (not the display name)', async () => {
    const onSubmit = vi.fn();
    render(<Harness parameters={markingParams} onSubmit={onSubmit} />);

    const select = (await screen.findByTestId('param-classification')) as HTMLSelectElement;
    await waitFor(() =>
      expect(
        screen.getByRole('option', { name: /Top Secret/ }),
      ).toBeInTheDocument(),
    );
    fireEvent.change(select, { target: { value: 'SECRET' } });
    fireEvent.click(screen.getByText('Submit'));

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    const submitted = onSubmit.mock.calls[0][0] as Record<string, unknown>;
    expect(submitted.classification).toBe('SECRET');
  });

  it('When a required marking is left unselected, Then apply is blocked', async () => {
    const onSubmit = vi.fn();
    render(<Harness parameters={markingParams} onSubmit={onSubmit} />);
    await screen.findByTestId('param-classification');

    fireEvent.click(screen.getByText('Submit'));
    await waitFor(() =>
      expect(screen.getByTestId('param-classification')).toHaveAttribute(
        'aria-invalid',
        'true',
      ),
    );
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('Given an optional marking left unselected, Then the field is omitted from the payload', async () => {
    const onSubmit = vi.fn();
    const optionalParams: Record<string, ActionParameterV2> = {
      classification: { dataType: { type: 'marking' }, required: false },
    };
    render(<Harness parameters={optionalParams} onSubmit={onSubmit} />);
    await screen.findByTestId('param-classification');

    fireEvent.click(screen.getByText('Submit'));
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    const submitted = onSubmit.mock.calls[0][0] as Record<string, unknown>;
    expect(submitted.classification).toBeUndefined();
  });
});

describe('BDD: ParameterForm marking selector (array → multi-select)', () => {
  const arrayMarkingParams: Record<string, ActionParameterV2> = {
    markings: {
      dataType: { type: 'array', itemType: { type: 'marking' } },
      required: true,
      description: 'Markings to apply',
    },
  };

  it('Given an array-of-marking param, Then it renders a multi-select populated from listMarkings', async () => {
    render(<Harness parameters={arrayMarkingParams} />);
    const select = (await screen.findByTestId('param-markings')) as HTMLSelectElement;
    expect(select.tagName).toBe('SELECT');
    expect(select.multiple).toBe(true);
    await waitFor(() =>
      expect(
        screen.getByRole('option', { name: /Personally Identifiable Information/ }),
      ).toBeInTheDocument(),
    );
  });

  it('When multiple markings are chosen, Then applying sends an array of NAMES', async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(<Harness parameters={arrayMarkingParams} onSubmit={onSubmit} />);

    await waitFor(() =>
      expect(
        screen.getByRole('option', { name: /Top Secret/ }),
      ).toBeInTheDocument(),
    );
    const select = screen.getByTestId('param-markings') as HTMLSelectElement;
    // Select both options (the catalog has populated the multi-select by now).
    await user.selectOptions(select, ['PII', 'SECRET']);
    fireEvent.click(screen.getByText('Submit'));

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    const submitted = onSubmit.mock.calls[0][0] as Record<string, unknown>;
    expect(submitted.markings).toEqual(['PII', 'SECRET']);
  });
});

describe('BDD: ParameterForm marking selector degraded states', () => {
  const markingParams: Record<string, ActionParameterV2> = {
    classification: { dataType: { type: 'marking' }, required: false },
  };

  it('When the markings list cannot load, Then it degrades to a usable text input', async () => {
    server.use(
      http.get(MARKINGS_PATH, () =>
        HttpResponse.json(
          { errorCode: 'INTERNAL', errorName: 'MarkingStoreError', errorInstanceId: 'x' },
          { status: 500 },
        ),
      ),
    );
    const onSubmit = vi.fn();
    render(<Harness parameters={markingParams} onSubmit={onSubmit} />);

    // The field stays usable: a text input keyed by the canonical testid lets
    // the user hand-type a marking name even when the catalog is unavailable.
    // (A disabled <select> shell shows during the transient loading window, so
    // we wait for the query to settle into the text-input fallback.)
    await waitFor(() =>
      expect(screen.getByTestId('param-classification').tagName).toBe('INPUT'),
    );
    const input = screen.getByTestId('param-classification') as HTMLInputElement;
    fireEvent.change(input, { target: { value: 'PII' } });
    fireEvent.click(screen.getByText('Submit'));

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    const submitted = onSubmit.mock.calls[0][0] as Record<string, unknown>;
    expect(submitted.classification).toBe('PII');
  });

  it('When the markings list is empty, Then it degrades to a usable text input', async () => {
    server.use(http.get(MARKINGS_PATH, () => HttpResponse.json({ markings: [] })));
    const onSubmit = vi.fn();
    render(<Harness parameters={markingParams} onSubmit={onSubmit} />);

    await waitFor(() =>
      expect(screen.getByTestId('param-classification').tagName).toBe('INPUT'),
    );
    const input = screen.getByTestId('param-classification') as HTMLInputElement;
    fireEvent.change(input, { target: { value: 'SECRET' } });
    fireEvent.click(screen.getByText('Submit'));

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    const submitted = onSubmit.mock.calls[0][0] as Record<string, unknown>;
    expect(submitted.classification).toBe('SECRET');
  });
});
