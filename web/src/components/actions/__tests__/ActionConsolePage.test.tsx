import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { ActionConsolePage } from '../ActionConsolePage';
import * as ontologiesApi from '../../../api/ontologies';
import * as actionsApi from '../../../api/actions';
import * as objectsApi from '../../../api/objects';
import { ApiRequestError } from '../../../api/client';
import type { ActionType } from '../../../api/types';
import { Toaster } from '../../common/Toaster';
import { useToastStore } from '../../../stores/toastStore';

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
      <MemoryRouter initialEntries={["/actions/default"]}>
        <Routes>
          <Route
            path="/actions/:ontology"
            element={
              <>
                <ActionConsolePage />
                <Toaster />
              </>
            }
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

// fillRequiredParam fills the lone required `newName` text input on the
// fake action so handleSubmit clears the Zod resolver and downstream
// applyMutation actually fires.
function fillRequiredParam(value = 'Alice') {
  fireEvent.change(screen.getByLabelText(/^newName/i), {
    target: { value },
  });
}

describe('ActionConsolePage — optimistic concurrency (US-024)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(ontologiesApi, 'listActionTypes').mockResolvedValue([fakeAction]);
  });

  it('loads object version and passes expectedVersion in apply options', async () => {
    vi.spyOn(objectsApi, 'getObjectHistory').mockResolvedValue({
      history: [],
      totalVersions: 3,
    });
    const applySpy = vi
      .spyOn(actionsApi, 'applyAction')
      .mockResolvedValue({ edits: undefined });

    renderPage();

    // Pick the action type
    const actionBtn = await screen.findByText('updateEmployee');
    fireEvent.click(actionBtn);

    // Enter target object
    fireEvent.change(screen.getByLabelText(/target object type/i), {
      target: { value: 'Employee' },
    });
    fireEvent.change(screen.getByLabelText(/target primary key/i), {
      target: { value: 'E1' },
    });

    // Wait for version to load
    await waitFor(() =>
      expect(screen.getByTestId('object-version')).toHaveTextContent('3'),
    );

    fillRequiredParam();

    // Execute
    fireEvent.click(screen.getByText('Execute Action'));

    await waitFor(() => expect(applySpy).toHaveBeenCalled());
    const [, , payload] = applySpy.mock.calls[0];
    expect(payload.options).toEqual(
      expect.objectContaining({ expectedVersion: 3 }),
    );
  });

  it('shows stale-object banner with Reload button on 409', async () => {
    const historySpy = vi
      .spyOn(objectsApi, 'getObjectHistory')
      .mockResolvedValueOnce({ history: [], totalVersions: 2 })
      .mockResolvedValueOnce({ history: [], totalVersions: 9 });

    const staleError = new ApiRequestError({
      errorCode: 'CONFLICT',
      errorName: 'StaleObject',
      errorInstanceId: 'instance-1',
      parameters: {
        expectedVersion: '2',
        currentVersion: '9',
        objectType: 'Employee',
        primaryKey: 'E1',
      },
      statusCode: 409,
    });
    vi.spyOn(actionsApi, 'applyAction').mockRejectedValue(staleError);

    renderPage();

    fireEvent.click(await screen.findByText('updateEmployee'));
    fireEvent.change(screen.getByLabelText(/target object type/i), {
      target: { value: 'Employee' },
    });
    fireEvent.change(screen.getByLabelText(/target primary key/i), {
      target: { value: 'E1' },
    });

    await waitFor(() =>
      expect(screen.getByTestId('object-version')).toHaveTextContent('2'),
    );

    fillRequiredParam();

    fireEvent.click(screen.getByText('Execute Action'));

    await waitFor(() =>
      expect(
        screen.getByText(/This object was updated elsewhere/i),
      ).toBeInTheDocument(),
    );
    expect(screen.getByText(/Reload to continue/i)).toBeInTheDocument();

    const reloadBtn = screen.getByRole('button', { name: /reload/i });
    fireEvent.click(reloadBtn);

    await waitFor(() =>
      expect(screen.getByTestId('object-version')).toHaveTextContent('9'),
    );
    expect(historySpy).toHaveBeenCalledTimes(2);
  });
});

describe('ActionConsolePage Undo toast (US-319)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useToastStore.getState().clear();
    vi.spyOn(ontologiesApi, 'listActionTypes').mockResolvedValue([fakeAction]);
    vi.spyOn(objectsApi, 'getObjectHistory').mockResolvedValue({
      history: [],
      totalVersions: 0,
    });
  });

  it('shows an Undo toast carrying actionLogId after a successful apply', async () => {
    vi.spyOn(actionsApi, 'applyAction').mockResolvedValue({
      operationId: 'op-1',
      actionLogId: 42,
      edits: undefined,
    });

    renderPage();

    fireEvent.click(await screen.findByText('updateEmployee'));
    fillRequiredParam();
    fireEvent.click(screen.getByText('Execute Action'));

    const toastAction = await screen.findByTestId('toast-action');
    expect(toastAction).toHaveTextContent('Undo');
    expect(screen.getByText(/Action "Update Employee" applied/)).toBeInTheDocument();
  });

  it('does NOT push an Undo toast when actionLogId is absent (validate-only / no-op)', async () => {
    vi.spyOn(actionsApi, 'applyAction').mockResolvedValue({
      operationId: 'op-noop',
      edits: undefined,
    });

    renderPage();

    fireEvent.click(await screen.findByText('updateEmployee'));
    fillRequiredParam();
    fireEvent.click(screen.getByText('Execute Action'));

    // Wait until the result section actually renders, then assert no toast.
    await waitFor(() =>
      expect(screen.queryByText('Execute Action')?.hasAttribute('disabled')).toBe(
        false,
      ),
    );
    expect(screen.queryByTestId('toast-action')).not.toBeInTheDocument();
  });

  it('clicking Undo on the toast invokes revertActionLog and replaces the toast with a "reverted" message', async () => {
    vi.spyOn(actionsApi, 'applyAction').mockResolvedValue({
      operationId: 'op-1',
      actionLogId: 99,
      edits: undefined,
    });
    const revertSpy = vi
      .spyOn(actionsApi, 'revertActionLog')
      .mockResolvedValue({ operationId: 'reverse-batch' });

    renderPage();

    fireEvent.click(await screen.findByText('updateEmployee'));
    fillRequiredParam();
    fireEvent.click(screen.getByText('Execute Action'));

    const undoBtn = await screen.findByTestId('toast-action');
    fireEvent.click(undoBtn);

    await waitFor(() =>
      expect(revertSpy).toHaveBeenCalledWith('default', 99),
    );
    await waitFor(() =>
      expect(
        screen.getByText(/Action "Update Employee" reverted/),
      ).toBeInTheDocument(),
    );
  });
});

describe('ActionConsolePage — dynamic form validation (US-322)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(ontologiesApi, 'listActionTypes').mockResolvedValue([fakeAction]);
    vi.spyOn(objectsApi, 'getObjectHistory').mockResolvedValue({
      history: [],
      totalVersions: 0,
    });
  });

  it('blocks Execute and shows a field-level Required error when newName is empty', async () => {
    const applySpy = vi
      .spyOn(actionsApi, 'applyAction')
      .mockResolvedValue({ edits: undefined });

    renderPage();

    fireEvent.click(await screen.findByText('updateEmployee'));
    fireEvent.click(screen.getByText('Execute Action'));

    const error = await screen.findByRole('alert');
    expect(error).toHaveTextContent(/required/i);
    expect(applySpy).not.toHaveBeenCalled();
  });

  it('maps a 422 WEAVE_VALIDATION_SCHEMA response onto the corresponding field error', async () => {
    const violations = [
      { field: 'newName', reason: 'must be at least 3 characters', keyword: 'minLength' },
    ];
    const schemaError = new ApiRequestError({
      errorCode: 'WEAVE_VALIDATION_SCHEMA',
      errorName: 'ParameterSchemaViolation',
      errorInstanceId: 'instance-2',
      parameters: {
        field: 'newName',
        reason: 'must be at least 3 characters',
        keyword: 'minLength',
        violations: JSON.stringify(violations),
      },
      statusCode: 422,
    });
    vi.spyOn(actionsApi, 'applyAction').mockRejectedValue(schemaError);

    renderPage();

    fireEvent.click(await screen.findByText('updateEmployee'));
    fillRequiredParam('Al');
    fireEvent.click(screen.getByText('Execute Action'));

    const error = await screen.findByRole('alert');
    expect(error).toHaveTextContent(/at least 3 characters/i);
  });
});
