import {
  describe,
  it,
  expect,
  beforeAll,
  afterAll,
  afterEach,
  beforeEach,
  vi,
} from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BranchPicker } from '../BranchPicker';
import { useBranchStore } from '../../../stores/branchStore';

// BDD: when closing (deleting) a branch fails, the user must get visible
// feedback — mirroring the inline error the create-branch form already shows
// via createMutation.isError. The previous behaviour swallowed delete errors
// entirely: the confirm Modal closed (or appeared to do nothing) and the user
// was left with no indication the branch was still there. These scenarios lock
// in: (a) on failure the error is shown *inside* the confirmation Modal and the
// Modal stays open so the user can retry; (b) on success the Modal closes and
// the branch disappears; (c) reopening / cancelling clears any stale error.
const server = setupServer();
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => {
  server.resetHandlers();
  vi.restoreAllMocks();
});
afterAll(() => server.close());

beforeEach(() => {
  useBranchStore.setState({ selections: {} });
});

function branchObj(id: string, name: string) {
  return {
    id,
    ontologyRid: 'ri.ontology.foundry',
    name,
    baseVersion: 1,
    status: 'open' as const,
    createdBy: 'tester',
    createdAt: '2026-05-01T00:00:00Z',
    updatedAt: '2026-05-01T00:00:00Z',
  };
}

function renderPicker() {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchInterval: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={qc}>
      <BranchPicker ontologyApiName="foundry" />
    </QueryClientProvider>,
  );
}

describe('BranchPicker delete error feedback', () => {
  it('shows an inline error inside the Modal and keeps it open when DELETE fails', async () => {
    server.use(
      http.get('/api/v2/ontologies/foundry/branches', () =>
        HttpResponse.json({ data: [branchObj('br-feature-x', 'feature-x')] }),
      ),
      http.delete('/api/v2/ontologies/foundry/branches/br-feature-x', () =>
        HttpResponse.json({ error: 'boom' }, { status: 500 }),
      ),
    );

    const user = userEvent.setup();
    renderPicker();

    // Given the confirmation Modal is open for a branch
    await user.click(screen.getByTestId('branch-picker-trigger'));
    await screen.findByTestId('branch-picker-option-br-feature-x');
    await user.click(screen.getByTestId('branch-picker-delete-br-feature-x'));
    await screen.findByTestId('branch-picker-delete-confirm');

    // When the user confirms and the DELETE request fails
    await user.click(
      await screen.findByTestId('branch-picker-delete-confirm-btn'),
    );

    // Then an inline error appears inside the still-open Modal
    expect(
      await screen.findByTestId('branch-picker-delete-error'),
    ).toBeInTheDocument();
    expect(screen.getByTestId('branch-picker-delete-confirm')).toBeInTheDocument();
    expect(screen.getByTestId('modal-overlay')).toBeInTheDocument();
  });

  it('clears the error and closes the Modal once a retried DELETE succeeds', async () => {
    let branches = [branchObj('br-feature-x', 'feature-x')];
    let shouldFail = true;

    server.use(
      http.get('/api/v2/ontologies/foundry/branches', () =>
        HttpResponse.json({ data: branches }),
      ),
      http.delete('/api/v2/ontologies/foundry/branches/br-feature-x', () => {
        if (shouldFail) {
          shouldFail = false;
          return HttpResponse.json({ error: 'boom' }, { status: 500 });
        }
        branches = branches.filter((b) => b.id !== 'br-feature-x');
        return new HttpResponse(null, { status: 204 });
      }),
    );

    const user = userEvent.setup();
    renderPicker();

    await user.click(screen.getByTestId('branch-picker-trigger'));
    await screen.findByTestId('branch-picker-option-br-feature-x');
    await user.click(screen.getByTestId('branch-picker-delete-br-feature-x'));

    // First attempt fails and surfaces the inline error.
    await user.click(
      await screen.findByTestId('branch-picker-delete-confirm-btn'),
    );
    await screen.findByTestId('branch-picker-delete-error');

    // Retrying succeeds: the error clears, the Modal closes, the branch is gone.
    await user.click(screen.getByTestId('branch-picker-delete-confirm-btn'));

    await waitFor(() => {
      expect(screen.queryByTestId('modal-overlay')).toBeNull();
    });
    expect(screen.queryByTestId('branch-picker-delete-error')).toBeNull();
    await user.click(screen.getByTestId('branch-picker-trigger'));
    await waitFor(() => {
      expect(
        screen.queryByTestId('branch-picker-option-br-feature-x'),
      ).toBeNull();
    });
  });

  it('clears a stale error when the delete dialog is cancelled and reopened', async () => {
    server.use(
      http.get('/api/v2/ontologies/foundry/branches', () =>
        HttpResponse.json({ data: [branchObj('br-feature-x', 'feature-x')] }),
      ),
      http.delete('/api/v2/ontologies/foundry/branches/br-feature-x', () =>
        HttpResponse.json({ error: 'boom' }, { status: 500 }),
      ),
    );

    const user = userEvent.setup();
    renderPicker();

    await user.click(screen.getByTestId('branch-picker-trigger'));
    await screen.findByTestId('branch-picker-option-br-feature-x');
    await user.click(screen.getByTestId('branch-picker-delete-br-feature-x'));
    await user.click(
      await screen.findByTestId('branch-picker-delete-confirm-btn'),
    );
    await screen.findByTestId('branch-picker-delete-error');

    // Cancel clears the stale error.
    await user.click(screen.getByTestId('branch-picker-delete-cancel'));
    await waitFor(() => {
      expect(screen.queryByTestId('modal-overlay')).toBeNull();
    });

    // Reopening the dialog shows no leftover error.
    await user.click(screen.getByTestId('branch-picker-trigger'));
    await screen.findByTestId('branch-picker-option-br-feature-x');
    await user.click(screen.getByTestId('branch-picker-delete-br-feature-x'));
    await screen.findByTestId('branch-picker-delete-confirm');
    expect(screen.queryByTestId('branch-picker-delete-error')).toBeNull();
  });
});
