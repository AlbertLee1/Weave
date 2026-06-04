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
import { changeLocale } from '../../../i18n';

// BDD: each non-default branch row in the picker surfaces its pending change
// count (BranchDetailResponse.changeCount from GET /branches/{id}) as a badge.
const server = setupServer();
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => {
  server.resetHandlers();
  vi.restoreAllMocks();
});
afterAll(async () => {
  server.close();
  await changeLocale('zh-CN');
});

beforeEach(async () => {
  useBranchStore.setState({ selections: {} });
  // Pin English so the "N pending" badge copy is deterministic.
  await changeLocale('en');
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

describe('BranchPicker pending change count badge', () => {
  it('Given a branch with N pending changes, Then its row shows an "N pending" badge', async () => {
    server.use(
      http.get('/api/v2/ontologies/foundry/branches', () =>
        HttpResponse.json({ data: [branchObj('br-feature', 'feature')] }),
      ),
      http.get('/api/v2/ontologies/foundry/branches/br-feature', () =>
        HttpResponse.json({
          ...branchObj('br-feature', 'feature'),
          changeCount: 3,
        }),
      ),
    );

    const user = userEvent.setup();
    renderPicker();

    await user.click(screen.getByTestId('branch-picker-trigger'));
    await screen.findByTestId('branch-picker-option-br-feature');

    const badge = await screen.findByTestId(
      'branch-picker-change-count-br-feature',
    );
    expect(badge).toHaveTextContent('3');
    expect(badge).toHaveTextContent(/pending/i);
  });

  it('Given a branch with zero pending changes, Then no count badge is rendered', async () => {
    server.use(
      http.get('/api/v2/ontologies/foundry/branches', () =>
        HttpResponse.json({ data: [branchObj('br-empty', 'empty')] }),
      ),
      http.get('/api/v2/ontologies/foundry/branches/br-empty', () =>
        HttpResponse.json({
          ...branchObj('br-empty', 'empty'),
          changeCount: 0,
        }),
      ),
    );

    const user = userEvent.setup();
    renderPicker();

    await user.click(screen.getByTestId('branch-picker-trigger'));
    await screen.findByTestId('branch-picker-option-br-empty');

    // Give the per-branch detail query time to resolve, then assert no badge.
    await waitFor(() => {
      expect(
        screen.queryByTestId('branch-picker-change-count-br-empty'),
      ).toBeNull();
    });
  });

  it('does not fetch a change count for the synthetic default (main) row', async () => {
    let mainDetailCalled = false;
    server.use(
      http.get('/api/v2/ontologies/foundry/branches', () =>
        HttpResponse.json({ data: [branchObj('br-feature', 'feature')] }),
      ),
      http.get('/api/v2/ontologies/foundry/branches/main', () => {
        mainDetailCalled = true;
        return HttpResponse.json({
          ...branchObj('main', 'main'),
          changeCount: 9,
        });
      }),
      http.get('/api/v2/ontologies/foundry/branches/br-feature', () =>
        HttpResponse.json({
          ...branchObj('br-feature', 'feature'),
          changeCount: 2,
        }),
      ),
    );

    const user = userEvent.setup();
    renderPicker();

    await user.click(screen.getByTestId('branch-picker-trigger'));
    await screen.findByTestId('branch-picker-change-count-br-feature');

    expect(mainDetailCalled).toBe(false);
    expect(
      screen.queryByTestId('branch-picker-change-count-main'),
    ).toBeNull();
  });
});
