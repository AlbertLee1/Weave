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
          <Route path="/actions/:ontology" element={<ActionConsolePage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
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
