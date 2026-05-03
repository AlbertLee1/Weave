import { describe, it, expect, beforeAll, afterAll, afterEach, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BranchPicker } from '../BranchPicker';
import { useBranchStore } from '../../../stores/branchStore';

const server = setupServer();

beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

beforeEach(() => {
  useBranchStore.setState({ selections: {} });
});

function renderPicker(ontologyApiName: string | null) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, refetchInterval: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <BranchPicker ontologyApiName={ontologyApiName} />
    </QueryClientProvider>,
  );
}

describe('BranchPicker', () => {
  it('renders nothing when no ontology is active', () => {
    const { container } = renderPicker(null);
    expect(container.firstChild).toBeNull();
  });

  it('shows the default branch label "main" before opening', () => {
    renderPicker('foundry');
    expect(screen.getByTestId('branch-picker-active')).toHaveTextContent('main');
  });

  it('lists the default branch plus server-supplied branches when opened', async () => {
    server.use(
      http.get('/api/v2/ontologies/foundry/branches', () =>
        HttpResponse.json({
          data: [
            {
              id: 'br-feature-x',
              ontologyRid: 'ri.ontology.foundry',
              name: 'feature-x',
              baseVersion: 1,
              status: 'open',
              createdBy: 'tester',
              createdAt: '2026-05-01T00:00:00Z',
              updatedAt: '2026-05-01T00:00:00Z',
            },
          ],
        }),
      ),
    );
    const user = userEvent.setup();
    renderPicker('foundry');
    await user.click(screen.getByTestId('branch-picker-trigger'));
    await waitFor(() => {
      expect(screen.getByTestId('branch-picker-option-main')).toBeInTheDocument();
      expect(
        screen.getByTestId('branch-picker-option-br-feature-x'),
      ).toBeInTheDocument();
    });
  });

  it('selecting a non-default branch updates the store', async () => {
    server.use(
      http.get('/api/v2/ontologies/foundry/branches', () =>
        HttpResponse.json({
          data: [
            {
              id: 'br-feature-x',
              ontologyRid: 'ri.ontology.foundry',
              name: 'feature-x',
              baseVersion: 1,
              status: 'open',
              createdBy: 'tester',
              createdAt: '2026-05-01T00:00:00Z',
              updatedAt: '2026-05-01T00:00:00Z',
            },
          ],
        }),
      ),
    );
    const user = userEvent.setup();
    renderPicker('foundry');
    await user.click(screen.getByTestId('branch-picker-trigger'));
    await user.click(await screen.findByTestId('branch-picker-option-br-feature-x'));
    expect(useBranchStore.getState().selections.foundry).toBe('br-feature-x');
  });

  it('selecting "main" clears the store entry (no per-ontology override)', async () => {
    useBranchStore.getState().setBranch('foundry', 'br-feature-x');
    server.use(
      http.get('/api/v2/ontologies/foundry/branches', () =>
        HttpResponse.json({ data: [] }),
      ),
    );
    const user = userEvent.setup();
    renderPicker('foundry');
    await user.click(screen.getByTestId('branch-picker-trigger'));
    await user.click(await screen.findByTestId('branch-picker-option-main'));
    expect(useBranchStore.getState().selections.foundry).toBeUndefined();
  });
});
