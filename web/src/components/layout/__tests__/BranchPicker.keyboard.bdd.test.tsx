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

// a11y: WAI-ARIA menu keyboard contract for the BranchPicker dropdown.
// The container is role="menu" with role="menuitemradio" items, so screen
// readers announce a "menu" and users expect Arrow/Escape navigation.
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

function useMultiBranch() {
  server.use(
    http.get('/api/v2/ontologies/foundry/branches', () =>
      HttpResponse.json({
        data: [branchObj('br-alpha', 'alpha'), branchObj('br-beta', 'beta')],
      }),
    ),
    // Lazy change-count fetch for non-default rows.
    http.get('/api/v2/ontologies/foundry/branches/:id', ({ params }) =>
      HttpResponse.json({
        ...branchObj(String(params.id), String(params.id)),
        changeCount: 0,
      }),
    ),
  );
}

describe('BranchPicker keyboard navigation (a11y)', () => {
  it('Given an open menu, When ArrowDown is pressed, Then focus moves to the next branch item', async () => {
    useMultiBranch();
    const user = userEvent.setup();
    renderPicker();

    await user.click(screen.getByTestId('branch-picker-trigger'));

    // Items: main, br-alpha, br-beta (sorted: alpha, beta after main).
    const main = await screen.findByTestId('branch-picker-option-main');
    const alpha = await screen.findByTestId('branch-picker-option-br-alpha');
    const beta = await screen.findByTestId('branch-picker-option-br-beta');

    // On open, focus lands on the first (or active) item.
    await waitFor(() => expect(main).toHaveFocus());

    await user.keyboard('{ArrowDown}');
    expect(alpha).toHaveFocus();

    await user.keyboard('{ArrowDown}');
    expect(beta).toHaveFocus();

    // Wraps around back to the first item.
    await user.keyboard('{ArrowDown}');
    expect(main).toHaveFocus();
  });

  it('Given an open menu, When ArrowUp is pressed, Then focus moves to the previous branch item (wrapping)', async () => {
    useMultiBranch();
    const user = userEvent.setup();
    renderPicker();

    await user.click(screen.getByTestId('branch-picker-trigger'));

    const main = await screen.findByTestId('branch-picker-option-main');
    const beta = await screen.findByTestId('branch-picker-option-br-beta');

    await waitFor(() => expect(main).toHaveFocus());

    // ArrowUp from the first item wraps to the last.
    await user.keyboard('{ArrowUp}');
    expect(beta).toHaveFocus();
  });

  it('Given an open menu, When Escape is pressed, Then the menu closes and focus returns to the trigger', async () => {
    useMultiBranch();
    const user = userEvent.setup();
    renderPicker();

    const trigger = screen.getByTestId('branch-picker-trigger');
    await user.click(trigger);

    await screen.findByTestId('branch-picker-menu');

    await user.keyboard('{Escape}');

    await waitFor(() => {
      expect(screen.queryByTestId('branch-picker-menu')).toBeNull();
    });
    expect(trigger).toHaveFocus();
  });
});
