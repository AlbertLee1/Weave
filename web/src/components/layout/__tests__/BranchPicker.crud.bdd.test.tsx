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

// US-113 / US-116 branch create + close from the picker.
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

describe('BranchPicker create branch (US-113)', () => {
  it('posts the typed name and the new branch appears in the list', async () => {
    let branches = [branchObj('br-existing', 'existing')];
    let postedBody: unknown = null;

    server.use(
      http.get('/api/v2/ontologies/foundry/branches', () =>
        HttpResponse.json({ data: branches }),
      ),
      http.post(
        '/api/v2/ontologies/foundry/branches',
        async ({ request }) => {
          postedBody = await request.json();
          const created = branchObj('br-new', 'release-1');
          branches = [...branches, created];
          return HttpResponse.json(created, { status: 201 });
        },
      ),
    );

    const user = userEvent.setup();
    renderPicker();

    await user.click(screen.getByTestId('branch-picker-trigger'));
    await user.click(await screen.findByTestId('branch-picker-create-toggle'));

    const input = await screen.findByTestId('branch-picker-create-name');
    await user.type(input, 'release-1');
    await user.click(screen.getByTestId('branch-picker-create-submit'));

    await waitFor(() => {
      expect(postedBody).toMatchObject({ name: 'release-1' });
    });
    await waitFor(() => {
      expect(
        screen.getByTestId('branch-picker-option-br-new'),
      ).toBeInTheDocument();
    });
  });

  it('blocks submission of an empty branch name', async () => {
    let postCount = 0;
    server.use(
      http.get('/api/v2/ontologies/foundry/branches', () =>
        HttpResponse.json({ data: [] }),
      ),
      http.post('/api/v2/ontologies/foundry/branches', () => {
        postCount += 1;
        return HttpResponse.json(branchObj('br-x', 'x'), { status: 201 });
      }),
    );

    const user = userEvent.setup();
    renderPicker();

    await user.click(screen.getByTestId('branch-picker-trigger'));
    await user.click(await screen.findByTestId('branch-picker-create-toggle'));
    const submit = await screen.findByTestId('branch-picker-create-submit');
    expect(submit).toBeDisabled();

    await new Promise((r) => setTimeout(r, 20));
    expect(postCount).toBe(0);
  });
});

describe('BranchPicker close branch (US-116)', () => {
  it('calls DELETE and removes the branch from the list', async () => {
    let branches = [
      branchObj('br-feature-x', 'feature-x'),
      branchObj('br-other', 'other'),
    ];
    let deleted: string | null = null;

    server.use(
      http.get('/api/v2/ontologies/foundry/branches', () =>
        HttpResponse.json({ data: branches }),
      ),
      http.delete(
        '/api/v2/ontologies/foundry/branches/br-feature-x',
        () => {
          deleted = 'br-feature-x';
          branches = branches.filter((b) => b.id !== 'br-feature-x');
          return new HttpResponse(null, { status: 204 });
        },
      ),
    );
    vi.spyOn(window, 'confirm').mockReturnValue(true);

    const user = userEvent.setup();
    renderPicker();

    await user.click(screen.getByTestId('branch-picker-trigger'));
    await screen.findByTestId('branch-picker-option-br-feature-x');

    await user.click(screen.getByTestId('branch-picker-delete-br-feature-x'));

    await waitFor(() => {
      expect(deleted).toBe('br-feature-x');
    });
    await waitFor(() => {
      expect(
        screen.queryByTestId('branch-picker-option-br-feature-x'),
      ).toBeNull();
    });
  });

  it('does not call DELETE when the confirm is dismissed', async () => {
    let deleteCount = 0;
    server.use(
      http.get('/api/v2/ontologies/foundry/branches', () =>
        HttpResponse.json({ data: [branchObj('br-feature-x', 'feature-x')] }),
      ),
      http.delete(
        '/api/v2/ontologies/foundry/branches/br-feature-x',
        () => {
          deleteCount += 1;
          return new HttpResponse(null, { status: 204 });
        },
      ),
    );
    vi.spyOn(window, 'confirm').mockReturnValue(false);

    const user = userEvent.setup();
    renderPicker();

    await user.click(screen.getByTestId('branch-picker-trigger'));
    await screen.findByTestId('branch-picker-option-br-feature-x');
    await user.click(screen.getByTestId('branch-picker-delete-br-feature-x'));

    await new Promise((r) => setTimeout(r, 30));
    expect(deleteCount).toBe(0);
  });
});
