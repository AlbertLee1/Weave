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

// BDD: deleting a branch from the picker must go through the shared styled
// Modal confirmation dialog — never the native window.confirm() that the row
// previously relied on. We deliberately do NOT mock window.confirm so that any
// regression back to the native prompt would surface as an unhandled call
// (jsdom's window.confirm returns false by default, which would silently block
// the delete and fail the "confirm deletes" scenario below).
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

describe('BranchPicker delete confirmation Modal (UX consistency)', () => {
  it('opens a styled Modal instead of window.confirm when Delete is clicked', async () => {
    // Spy without stubbing the return so a regression that still *calls*
    // window.confirm is caught by the assertion below.
    const confirmSpy = vi.spyOn(window, 'confirm');
    server.use(
      http.get('/api/v2/ontologies/foundry/branches', () =>
        HttpResponse.json({ data: [branchObj('br-feature-x', 'feature-x')] }),
      ),
    );

    const user = userEvent.setup();
    renderPicker();

    // Given the picker dropdown is open and lists the branch
    await user.click(screen.getByTestId('branch-picker-trigger'));
    await screen.findByTestId('branch-picker-option-br-feature-x');

    // When the row's Delete button is clicked
    await user.click(screen.getByTestId('branch-picker-delete-br-feature-x'));

    // Then the shared styled Modal overlay appears and window.confirm is never
    // used as the confirmation surface.
    expect(await screen.findByTestId('modal-overlay')).toBeInTheDocument();
    expect(confirmSpy).not.toHaveBeenCalled();
  });

  it('does not call DELETE when the Modal is cancelled', async () => {
    let deleteCount = 0;
    server.use(
      http.get('/api/v2/ontologies/foundry/branches', () =>
        HttpResponse.json({ data: [branchObj('br-feature-x', 'feature-x')] }),
      ),
      http.delete('/api/v2/ontologies/foundry/branches/br-feature-x', () => {
        deleteCount += 1;
        return new HttpResponse(null, { status: 204 });
      }),
    );

    const user = userEvent.setup();
    renderPicker();

    await user.click(screen.getByTestId('branch-picker-trigger'));
    await screen.findByTestId('branch-picker-option-br-feature-x');
    await user.click(screen.getByTestId('branch-picker-delete-br-feature-x'));

    // Cancel inside the Modal
    await user.click(await screen.findByTestId('branch-picker-delete-cancel'));

    // The Modal closes and no DELETE request is issued.
    await waitFor(() => {
      expect(screen.queryByTestId('modal-overlay')).toBeNull();
    });
    await new Promise((r) => setTimeout(r, 30));
    expect(deleteCount).toBe(0);
  });

  it('calls DELETE and removes the branch once the Modal is confirmed', async () => {
    let branches = [
      branchObj('br-feature-x', 'feature-x'),
      branchObj('br-other', 'other'),
    ];
    let deleted: string | null = null;

    server.use(
      http.get('/api/v2/ontologies/foundry/branches', () =>
        HttpResponse.json({ data: branches }),
      ),
      http.delete('/api/v2/ontologies/foundry/branches/br-feature-x', () => {
        deleted = 'br-feature-x';
        branches = branches.filter((b) => b.id !== 'br-feature-x');
        return new HttpResponse(null, { status: 204 });
      }),
    );

    const user = userEvent.setup();
    renderPicker();

    await user.click(screen.getByTestId('branch-picker-trigger'));
    await screen.findByTestId('branch-picker-option-br-feature-x');
    await user.click(screen.getByTestId('branch-picker-delete-br-feature-x'));

    // Confirm inside the Modal triggers the DELETE.
    await user.click(
      await screen.findByTestId('branch-picker-delete-confirm-btn'),
    );

    await waitFor(() => {
      expect(deleted).toBe('br-feature-x');
    });
    await waitFor(() => {
      expect(
        screen.queryByTestId('branch-picker-option-br-feature-x'),
      ).toBeNull();
    });
  });
});
