import {
  describe,
  it,
  expect,
  vi,
  beforeAll,
  afterAll,
  afterEach,
} from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { FormProvider, useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { ParameterForm } from '../ParameterForm';
import type { ActionParameterV2 } from '../../../api/types';
import type { ValidateActionResponse } from '../../../api/actions';
import {
  buildParameterDefaults,
  buildParameterZodSchema,
} from '../parameterSchema';

// BDD coverage for the real-time, field-level Actions validation surface.
//
// Wire contract (verified against pkg/actions/handlers.go::Validate and
// pkg/actions/executor.go::ValidateActionResponse):
//
//   POST /api/v2/ontologies/{ontology}/actions/{action}/validate
//   body: { parameters }
//   200 ALWAYS, even on INVALID — the envelope is a validation report:
//     {
//       result: 'VALID' | 'INVALID',
//       submissionCriteria: [{ result, configuredFailureMessage }],
//       parameters: { [id]: { result, required, evaluatedConstraints } },
//     }
//
// The form must:
//   (1) debounce onChange so we don't POST on every keystroke,
//   (2) red-line the specific field whose parameters[id].result === 'INVALID',
//   (3) surface submissionCriteria[].configuredFailureMessage as a form-level
//       banner,
//   (4) clear both once a subsequent value validates VALID.

const ONTOLOGY = 'acme';
const ACTION = 'promoteEmployee';
const VALIDATE_PATH = `/api/v2/ontologies/${ONTOLOGY}/actions/${ACTION}/validate`;

// The handler red-lines a parameter only when the submitted value is the
// "bad" sentinel. This lets a single MSW handler model both the failing and
// the recovering keystroke without re-registering handlers mid-test.
const BAD_VALUE = 'BAD';
const FAILURE_MESSAGE = 'newTitle must not be empty';

let lastBody: { parameters: Record<string, unknown> } | null = null;
let requestCount = 0;

const server = setupServer(
  http.post(VALIDATE_PATH, async ({ request }) => {
    requestCount += 1;
    const body = (await request.json()) as { parameters: Record<string, unknown> };
    lastBody = body;
    const newTitle = body.parameters?.newTitle;
    const isBad = newTitle === BAD_VALUE || newTitle === '' || newTitle === undefined;
    if (isBad) {
      const resp: ValidateActionResponse = {
        result: 'INVALID',
        submissionCriteria: [
          { result: 'INVALID', configuredFailureMessage: FAILURE_MESSAGE },
        ],
        parameters: {
          newTitle: {
            result: 'INVALID',
            required: true,
            evaluatedConstraints: [{ type: 'required', result: 'INVALID' }],
          },
        },
      };
      return HttpResponse.json(resp);
    }
    const resp: ValidateActionResponse = {
      result: 'VALID',
      submissionCriteria: [],
      parameters: {
        newTitle: { result: 'VALID', required: true, evaluatedConstraints: [] },
      },
    };
    return HttpResponse.json(resp);
  }),
);

beforeAll(() => server.listen());
afterEach(() => {
  vi.restoreAllMocks();
  vi.useRealTimers();
  server.resetHandlers();
  lastBody = null;
  requestCount = 0;
});
afterAll(() => server.close());

const params: Record<string, ActionParameterV2> = {
  newTitle: {
    dataType: { type: 'string' },
    required: true,
    description: 'The new job title',
  },
};

function Harness({
  ontologyApiName,
  actionApiName,
}: {
  ontologyApiName?: string;
  actionApiName?: string;
}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const form = useForm<Record<string, unknown>>({
    resolver: zodResolver(buildParameterZodSchema(params)),
    defaultValues: buildParameterDefaults(params),
    mode: 'onBlur',
  });
  return (
    <QueryClientProvider client={queryClient}>
      <FormProvider {...form}>
        <form noValidate>
          <ParameterForm
            parameters={params}
            ontologyApiName={ontologyApiName}
            actionApiName={actionApiName}
          />
        </form>
      </FormProvider>
    </QueryClientProvider>
  );
}

describe('BDD: ParameterForm real-time field-level validation', () => {
  it('Given a value that fails a constraint (after debounce), Then the field is red-lined and the failure banner shows; When a valid value is entered, both clear', async () => {
    render(<Harness ontologyApiName={ONTOLOGY} actionApiName={ACTION} />);

    const input = screen.getByTestId('param-newTitle') as HTMLInputElement;

    // When: the user types a value the server rejects.
    fireEvent.change(input, { target: { value: BAD_VALUE } });

    // Then: after the debounce window, the field shows the per-parameter
    // INVALID error and the form-level banner shows the submissionCriteria
    // configuredFailureMessage.
    await waitFor(
      () => {
        expect(
          screen.getByTestId('action-validate-banner'),
        ).toHaveTextContent(FAILURE_MESSAGE);
      },
      { timeout: 2000 },
    );
    expect(input).toHaveAttribute('aria-invalid', 'true');
    expect(
      screen.getByTestId('param-newTitle-validate-error'),
    ).toBeInTheDocument();

    // And: the POST body carried the typed value (action name is in the URL).
    expect(lastBody?.parameters?.newTitle).toBe(BAD_VALUE);

    // When: the user corrects the value to something valid.
    fireEvent.change(input, { target: { value: 'Senior Engineer' } });

    // Then: the inline error and the banner both clear.
    await waitFor(
      () => {
        expect(
          screen.queryByTestId('action-validate-banner'),
        ).not.toBeInTheDocument();
      },
      { timeout: 2000 },
    );
    expect(
      screen.queryByTestId('param-newTitle-validate-error'),
    ).not.toBeInTheDocument();
    expect(input).toHaveAttribute('aria-invalid', 'false');
  });

  it('Given multiple rapid keystrokes, Then validation is debounced (fewer requests than keystrokes)', async () => {
    render(<Harness ontologyApiName={ONTOLOGY} actionApiName={ACTION} />);
    const input = screen.getByTestId('param-newTitle') as HTMLInputElement;

    // Five rapid keystrokes inside the debounce window.
    fireEvent.change(input, { target: { value: 'a' } });
    fireEvent.change(input, { target: { value: 'ab' } });
    fireEvent.change(input, { target: { value: 'abc' } });
    fireEvent.change(input, { target: { value: 'abcd' } });
    fireEvent.change(input, { target: { value: 'abcde' } });

    // Let the debounce settle and one request fire.
    await waitFor(() => expect(requestCount).toBeGreaterThanOrEqual(1), {
      timeout: 2000,
    });
    // Give any stray timers a beat to NOT fire.
    await new Promise((r) => setTimeout(r, 250));
    // Far fewer than the five keystrokes — debounce coalesced them.
    expect(requestCount).toBeLessThan(5);
    expect(lastBody?.parameters?.newTitle).toBe('abcde');
  });

  it('Given no ontology/action wiring, Then no validate request is ever issued (opt-in)', async () => {
    render(<Harness />);
    const input = screen.getByTestId('param-newTitle') as HTMLInputElement;
    fireEvent.change(input, { target: { value: BAD_VALUE } });

    // Wait past the debounce window — nothing should have been requested,
    // and no banner should appear.
    await new Promise((r) => setTimeout(r, 600));
    expect(requestCount).toBe(0);
    expect(
      screen.queryByTestId('action-validate-banner'),
    ).not.toBeInTheDocument();
  });

  it('Given the form just mounted (wired but untouched), Then no validate request fires and required fields are NOT pre-red-lined', async () => {
    // Regression guard: validation must be gated on user interaction. An
    // untouched default form (required string === '') would otherwise be POSTed
    // on mount and the server's INVALID verdict would red-line every required
    // field and show the failure banner before the user has typed anything.
    render(<Harness ontologyApiName={ONTOLOGY} actionApiName={ACTION} />);
    const input = screen.getByTestId('param-newTitle') as HTMLInputElement;

    // No fireEvent.change — the form is pristine. Wait past the debounce window.
    await new Promise((r) => setTimeout(r, 600));

    expect(requestCount).toBe(0);
    expect(input).toHaveAttribute('aria-invalid', 'false');
    expect(
      screen.queryByTestId('action-validate-banner'),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByTestId('param-newTitle-validate-error'),
    ).not.toBeInTheDocument();
  });
});
