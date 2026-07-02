import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { ActionConsolePage } from '../ActionConsolePage';
import * as ontologiesApi from '../../../api/ontologies';
import * as actionsApi from '../../../api/actions';
import * as objectsApi from '../../../api/objects';
import type { ActionType } from '../../../api/types';
import type { ActionJob } from '../../../api/actions';

// Unit — Async single-apply with job polling.
//
// Backend contract (pkg/actions/handlers.go US-240):
//   - POST .../actions/{action}/apply?async=true returns 202 {jobId, status}
//     when an ActionJobStore is wired, runs the Apply in a detached goroutine.
//   - GET  .../actions/jobs/{jobId} returns the persisted ActionJob row with
//     status ∈ {PENDING, RUNNING, SUCCEEDED, FAILED, CANCELED}, progress 0..100,
//     and (on SUCCEEDED) a `result` payload mirroring the sync apply envelope.
//
// Scenario: Given a single action form, When the user enables async and
// applies, Then the request is sent via the async path (?async=true), a jobId
// is captured, the page polls the job status endpoint until it reaches a
// terminal state, and the result/progress is surfaced.

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

function job(overrides: Partial<ActionJob>): ActionJob {
  return {
    jobId: 'job-async-1',
    ontologyApiName: 'default',
    actionType: 'updateEmployee',
    status: 'PENDING',
    progress: 0,
    createdAt: '',
    updatedAt: '',
    ...overrides,
  };
}

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

describe('BDD: ActionConsolePage async single-apply with job polling', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.spyOn(ontologiesApi, 'listActionTypes').mockResolvedValue([fakeAction]);
    vi.spyOn(objectsApi, 'getObjectActivity').mockResolvedValue({ data: [] });
  });

  afterEach(() => {
    vi.runOnlyPendingTimers();
    vi.useRealTimers();
  });

  // Given the async toggle is OFF (default)
  // When the user executes
  // Then the synchronous applyAction path is used (no jobId polling)
  it('uses the synchronous apply path when async is disabled', async () => {
    const syncSpy = vi
      .spyOn(actionsApi, 'applyAction')
      .mockResolvedValue({ edits: undefined });
    const asyncSpy = vi.spyOn(actionsApi, 'applyActionAsync');

    renderPage();
    fireEvent.click(await screen.findByText('updateEmployee'));
    fillRequiredParam();
    fireEvent.click(screen.getByText('Execute Action'));

    await waitFor(() => expect(syncSpy).toHaveBeenCalled());
    expect(asyncSpy).not.toHaveBeenCalled();
  });

  // Given the user enables the async toggle
  // When the user executes the action
  // Then the request goes through applyActionAsync (?async=true), a jobId is
  // captured, the job-status endpoint is polled until SUCCEEDED, and the
  // result is surfaced.
  it('applies asynchronously, polls the job until terminal, and shows the result', async () => {
    const asyncSpy = vi
      .spyOn(actionsApi, 'applyActionAsync')
      .mockResolvedValue({ jobId: 'job-async-1', status: 'PENDING' });

    // PENDING → RUNNING → SUCCEEDED across successive polls.
    const getSpy = vi
      .spyOn(actionsApi, 'getActionJob')
      .mockResolvedValueOnce(job({ status: 'RUNNING', progress: 40 }))
      .mockResolvedValue(
        job({
          status: 'SUCCEEDED',
          progress: 100,
          result: {
            edits: {
              type: 'edits',
              addedObjectCount: 0,
              modifiedObjectsCount: 1,
              deletedObjectsCount: 0,
              addedLinksCount: 0,
              deletedLinksCount: 0,
            },
          },
        }),
      );

    renderPage();
    fireEvent.click(await screen.findByText('updateEmployee'));
    fillRequiredParam();

    // Enable async mode.
    fireEvent.click(screen.getByTestId('async-apply-toggle'));
    fireEvent.click(screen.getByText('Execute Action'));

    // The async path is taken.
    await waitFor(() => expect(asyncSpy).toHaveBeenCalled());

    // Polling kicks off against the captured jobId.
    await waitFor(() => expect(getSpy).toHaveBeenCalled());
    expect(getSpy.mock.calls[0][1]).toBe('job-async-1');

    // The job-progress panel renders.
    expect(await screen.findByTestId('async-job-progress')).toBeInTheDocument();

    // Polling continues until terminal; success surfaces the result.
    await waitFor(() => {
      expect(screen.getByTestId('async-job-status')).toHaveTextContent(/succeeded/i);
    });
    await waitFor(() => {
      expect(screen.getByText(/1 edit applied/i)).toBeInTheDocument();
    });
  });

  // Given a degraded-mode server (no ActionJobStore wired) that ignores
  // ?async=true and returns the synchronous SyncApplyActionResponseV2 envelope
  // (no jobId) at HTTP 200
  // When the user applies with async enabled
  // Then the result is surfaced like a sync apply (not lost) and no job poll
  // is attempted.
  it('surfaces a synchronous result when the server falls through (no jobId)', async () => {
    vi.spyOn(actionsApi, 'applyActionAsync').mockResolvedValue({
      // Degraded fallthrough: server returns SyncApplyActionResponseV2 shape.
      operationId: 'op-1',
      edits: {
        type: 'edits',
        addedObjectCount: 0,
        modifiedObjectsCount: 1,
        deletedObjectsCount: 0,
        addedLinksCount: 0,
        deletedLinksCount: 0,
      },
    } as unknown as actionsApi.AsyncApplyResponse);
    const getSpy = vi.spyOn(actionsApi, 'getActionJob');

    renderPage();
    fireEvent.click(await screen.findByText('updateEmployee'));
    fillRequiredParam();
    fireEvent.click(screen.getByTestId('async-apply-toggle'));
    fireEvent.click(screen.getByText('Execute Action'));

    // The result is shown — not silently dropped.
    await waitFor(() => {
      expect(screen.getByText(/1 edit applied/i)).toBeInTheDocument();
    });
    // No job poll is attempted (there is no jobId), and no stuck progress panel.
    expect(getSpy).not.toHaveBeenCalled();
    expect(screen.queryByTestId('async-job-progress')).toBeNull();
  });

  // Given an async apply that fails server-side
  // When polling reaches FAILED
  // Then the failure is surfaced and polling stops (no infinite loop)
  it('surfaces a FAILED async job and stops polling', async () => {
    vi
      .spyOn(actionsApi, 'applyActionAsync')
      .mockResolvedValue({ jobId: 'job-async-1', status: 'PENDING' });
    const getSpy = vi
      .spyOn(actionsApi, 'getActionJob')
      .mockResolvedValue(
        job({ status: 'FAILED', progress: 30, errorMessage: 'boom' }),
      );

    renderPage();
    fireEvent.click(await screen.findByText('updateEmployee'));
    fillRequiredParam();
    fireEvent.click(screen.getByTestId('async-apply-toggle'));
    fireEvent.click(screen.getByText('Execute Action'));

    await waitFor(() => {
      expect(screen.getByTestId('async-job-status')).toHaveTextContent(/failed/i);
    });
    expect(screen.getByTestId('async-job-error')).toHaveTextContent(/boom/i);

    // Polling must stop on terminal status: record the call count, advance a
    // few intervals, and assert it does not keep climbing.
    const callsAtTerminal = getSpy.mock.calls.length;
    await vi.advanceTimersByTimeAsync(10_000);
    expect(getSpy.mock.calls.length).toBe(callsAtTerminal);
  });
});
