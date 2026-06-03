import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { ActionConsolePage } from '../ActionConsolePage';
import * as ontologiesApi from '../../../api/ontologies';
import * as actionsApi from '../../../api/actions';
import * as objectsApi from '../../../api/objects';
import type { ActionType, ActionBatchApplyRequest } from '../../../api/types';

type BatchOptions = NonNullable<ActionBatchApplyRequest['options']>;

// Unit 9 — Action returnEdits selector + batch option type narrowing.
//
// Backend contract (pkg/actions/handlers.go):
//   - single Apply  accepts returnEdits ∈ {ALL, ALL_V2_WITH_DELETIONS, NONE}
//                   and mode ∈ {VALIDATE_ONLY, VALIDATE_AND_EXECUTE}
//   - ApplyBatch    rejects ALL_V2_WITH_DELETIONS with 400 (allowed: ALL, NONE)
//
// These scenarios prove the console wires the selectors into the single-apply
// request `options`, and a compile-time assurance proves the batch option type
// cannot carry ALL_V2_WITH_DELETIONS.

const fakeAction: ActionType = {
  rid: 'ri.action.main.action-type.update-emp',
  apiName: 'updateEmployee',
  displayName: 'Update Employee',
  description: 'Change employee info',
  status: 'ACTIVE',
  parameters: {
    newName: {
      dataType: { type: 'string' },
      required: true,
    },
  },
  operations: [],
} as unknown as ActionType;

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/actions/default']}>
        <Routes>
          <Route path="/actions/:ontology" element={<ActionConsolePage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function fillRequiredParam(value = 'Alice') {
  fireEvent.change(screen.getByLabelText(/^newName/i), { target: { value } });
}

describe('BDD: ActionConsolePage returnEdits + mode selector (single-apply)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(ontologiesApi, 'listActionTypes').mockResolvedValue([fakeAction]);
    vi.spyOn(objectsApi, 'getObjectActivity').mockResolvedValue({ data: [] });
  });

  // Given an action is selected
  // When the user picks "Validate only" and executes
  // Then the apply request carries options.mode = 'VALIDATE_ONLY'
  it('sends options.mode VALIDATE_ONLY when validate-only is selected', async () => {
    const applySpy = vi
      .spyOn(actionsApi, 'applyAction')
      .mockResolvedValue({ edits: undefined });

    renderPage();
    fireEvent.click(await screen.findByText('updateEmployee'));
    fillRequiredParam();

    fireEvent.change(screen.getByTestId('apply-mode-select'), {
      target: { value: 'VALIDATE_ONLY' },
    });
    fireEvent.click(screen.getByText('Execute Action'));

    await waitFor(() => expect(applySpy).toHaveBeenCalled());
    const [, , payload] = applySpy.mock.calls[0];
    expect(payload.options).toEqual(
      expect.objectContaining({ mode: 'VALIDATE_ONLY' }),
    );
  });

  // Given an action is selected
  // When the user picks returnEdits = ALL_V2_WITH_DELETIONS and executes
  // Then the apply request carries options.returnEdits = 'ALL_V2_WITH_DELETIONS'
  it('sends options.returnEdits when a returnEdits value is selected', async () => {
    const applySpy = vi
      .spyOn(actionsApi, 'applyAction')
      .mockResolvedValue({ edits: undefined });

    renderPage();
    fireEvent.click(await screen.findByText('updateEmployee'));
    fillRequiredParam();

    fireEvent.change(screen.getByTestId('apply-return-edits-select'), {
      target: { value: 'ALL_V2_WITH_DELETIONS' },
    });
    fireEvent.click(screen.getByText('Execute Action'));

    await waitFor(() => expect(applySpy).toHaveBeenCalled());
    const [, , payload] = applySpy.mock.calls[0];
    expect(payload.options).toEqual(
      expect.objectContaining({ returnEdits: 'ALL_V2_WITH_DELETIONS' }),
    );
  });

  // Given the user keeps the defaults (validate-and-execute, returnEdits ALL)
  // When executing
  // Then no mode override leaks and returnEdits defaults are not forced
  // (the request still applies — server defaults govern omitted fields).
  it('does not send VALIDATE_ONLY by default (validate-and-execute is the default)', async () => {
    const applySpy = vi
      .spyOn(actionsApi, 'applyAction')
      .mockResolvedValue({ edits: undefined });

    renderPage();
    fireEvent.click(await screen.findByText('updateEmployee'));
    fillRequiredParam();
    fireEvent.click(screen.getByText('Execute Action'));

    await waitFor(() => expect(applySpy).toHaveBeenCalled());
    const [, , payload] = applySpy.mock.calls[0];
    expect(payload.options?.mode).not.toBe('VALIDATE_ONLY');
  });
});

describe('BDD: batch apply options cannot carry ALL_V2_WITH_DELETIONS', () => {
  // Compile-time assurance: the batch options type narrows returnEdits to
  // ALL | NONE. If a future edit widens it to include ALL_V2_WITH_DELETIONS,
  // the `@ts-expect-error` below becomes a *non-error* and tsc fails the build
  // (caught by `npm run build`'s `tsc -b`). The runtime assertion keeps the
  // test green and documents the intent for readers running vitest alone.
  it('rejects ALL_V2_WITH_DELETIONS in batch options at the type level', () => {
    const legalAll: BatchOptions = { returnEdits: 'ALL' };
    const legalNone: BatchOptions = { returnEdits: 'NONE' };
    expect(legalAll.returnEdits).toBe('ALL');
    expect(legalNone.returnEdits).toBe('NONE');

    // Compile-time guard: assigning the single-apply-only mode to a batch
    // option object must error. If the batch type ever widens to include it,
    // this directive becomes "unused" and `tsc -b` (run by `npm run build`)
    // fails — pinning the narrowing in CI.
    const invalid: BatchOptions = {
      // @ts-expect-error ALL_V2_WITH_DELETIONS is single-apply only
      returnEdits: 'ALL_V2_WITH_DELETIONS',
    };
    // Runtime sanity: at runtime the forced sentinel is not a legal value.
    expect(['ALL', 'NONE']).not.toContain(invalid.returnEdits);
  });
});
