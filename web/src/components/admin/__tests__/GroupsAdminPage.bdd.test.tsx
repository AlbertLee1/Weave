import {
  describe,
  it,
  expect,
  beforeAll,
  afterAll,
  afterEach,
} from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { GroupsAdminPage } from '../GroupsAdminPage';
import { Toaster } from '../../common/Toaster';
import { useToastStore } from '../../../stores/toastStore';

// ---------------------------------------------------------------------------
// In-memory group store backing the MSW handlers. Each test reseeds it in
// afterEach via server.resetHandlers + a fresh seed call so scenarios stay
// isolated.
// ---------------------------------------------------------------------------

interface StoredGroup {
  id: string;
  name: string;
  description: string;
  createdAt: string;
  updatedAt: string;
}

let groups: StoredGroup[] = [];
let members: Record<string, string[]> = {};
const postBodies: Array<Record<string, unknown>> = [];
const deletedIds: string[] = [];

function seed(initial: StoredGroup[]) {
  groups = initial.map((g) => ({ ...g }));
  members = {};
  postBodies.length = 0;
  deletedIds.length = 0;
}

const ISO = '2026-05-01T00:00:00Z';

function makeGroup(id: string, name: string, description = ''): StoredGroup {
  return { id, name, description, createdAt: ISO, updatedAt: ISO };
}

const server = setupServer(
  http.get('/api/admin/groups', () => HttpResponse.json({ groups })),

  http.post('/api/admin/groups', async ({ request }) => {
    const body = (await request.json()) as Record<string, unknown>;
    postBodies.push(body);
    const g = makeGroup(
      `grp-${groups.length + 1}`,
      String(body.name ?? ''),
      String(body.description ?? ''),
    );
    groups = [...groups, g];
    return HttpResponse.json(g, { status: 201 });
  }),

  http.delete('/api/admin/groups/:id', ({ params }) => {
    const id = String(params.id);
    deletedIds.push(id);
    groups = groups.filter((g) => g.id !== id);
    return new HttpResponse(null, { status: 204 });
  }),

  http.get('/api/admin/groups/:id/members', ({ params }) => {
    const id = String(params.id);
    return HttpResponse.json({ members: members[id] ?? [] });
  }),

  http.post('/api/admin/groups/:id/members', async ({ params, request }) => {
    const id = String(params.id);
    const body = (await request.json()) as { userId: string };
    members[id] = [...(members[id] ?? []), body.userId];
    return new HttpResponse(null, { status: 200 });
  }),

  http.delete('/api/admin/groups/:id/members/:userId', ({ params }) => {
    const id = String(params.id);
    const userId = decodeURIComponent(String(params.userId));
    members[id] = (members[id] ?? []).filter((u) => u !== userId);
    return new HttpResponse(null, { status: 200 });
  }),
);

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchInterval: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/admin/groups']}>
        <GroupsAdminPage />
        <Toaster />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => {
  server.resetHandlers();
  useToastStore.getState().clear();
});
afterAll(() => server.close());

describe('GroupsAdminPage (RBAC) BDD', () => {
  // (a) Given the backend returns two groups, When the page renders,
  //     Then both group names are displayed.
  it('lists every group returned by the backend', async () => {
    seed([
      makeGroup('grp-1', 'Engineers', 'Builds the product'),
      makeGroup('grp-2', 'Analysts', 'Reads the data'),
    ]);

    renderPage();

    expect(await screen.findByText('Engineers')).toBeInTheDocument();
    expect(screen.getByText('Analysts')).toBeInTheDocument();
  });

  // (b) Given the backend returns no groups, Then an empty-state hint is shown.
  it('shows an empty state with a creation hint when there are no groups', async () => {
    seed([]);

    renderPage();

    expect(await screen.findByTestId('groups-empty')).toBeInTheDocument();
    expect(screen.getByTestId('groups-empty')).toHaveTextContent(/no groups/i);
  });

  // (c) Given an empty backend, When the operator fills the create form and
  //     submits, Then a POST is issued and the new group appears in the list.
  it('creates a group and refreshes the list', async () => {
    seed([]);

    renderPage();

    // Wait for the initial (empty) load to settle.
    await screen.findByTestId('groups-empty');

    fireEvent.click(screen.getByTestId('groups-new-btn'));

    const nameInput = await screen.findByTestId('group-create-name');
    fireEvent.change(nameInput, { target: { value: 'Auditors' } });
    fireEvent.change(screen.getByTestId('group-create-description'), {
      target: { value: 'Compliance team' },
    });

    fireEvent.click(screen.getByTestId('group-create-submit'));

    // The new group shows up once the list query is invalidated + refetched.
    expect(await screen.findByText('Auditors')).toBeInTheDocument();
    expect(postBodies).toHaveLength(1);
    expect(postBodies[0]).toMatchObject({
      name: 'Auditors',
      description: 'Compliance team',
    });
  });

  // (d) Given a group exists, When the operator clicks Delete, Then a
  //     confirmation modal appears and confirming issues a DELETE.
  it('requires a second confirmation before deleting a group', async () => {
    seed([makeGroup('grp-1', 'Engineers')]);

    renderPage();

    await screen.findByText('Engineers');

    fireEvent.click(screen.getByTestId('group-delete-btn-grp-1'));

    // Confirmation dialog is shown; no DELETE has fired yet.
    const dialog = await screen.findByTestId('group-delete-modal');
    expect(deletedIds).toHaveLength(0);

    fireEvent.click(within(dialog).getByTestId('group-delete-confirm'));

    await waitFor(() => expect(deletedIds).toEqual(['grp-1']));
    await waitFor(() =>
      expect(screen.queryByText('Engineers')).not.toBeInTheDocument(),
    );
  });

  // (e) Given the create endpoint fails, When the operator submits, Then an
  //     error toast surfaces and the modal stays open.
  it('surfaces an error toast when creation fails', async () => {
    seed([]);
    server.use(
      http.post('/api/admin/groups', () =>
        HttpResponse.json(
          {
            errorCode: 'CONFLICT',
            errorName: 'GroupAlreadyExists',
            errorInstanceId: 'abc',
            parameters: { reason: 'name taken' },
          },
          { status: 409 },
        ),
      ),
    );

    renderPage();

    await screen.findByTestId('groups-empty');
    fireEvent.click(screen.getByTestId('groups-new-btn'));

    const nameInput = await screen.findByTestId('group-create-name');
    fireEvent.change(nameInput, { target: { value: 'Dupes' } });
    fireEvent.click(screen.getByTestId('group-create-submit'));

    // Toast region surfaces the server-provided reason.
    expect(await screen.findByText(/GroupAlreadyExists/)).toBeInTheDocument();
    expect(screen.getByText(/name taken/)).toBeInTheDocument();
    // Modal remains open so the operator can correct + retry.
    expect(screen.getByTestId('group-create-form')).toBeInTheDocument();
  });

  // Members management: selecting a group lists its members, and adding one
  // issues a POST then refreshes the member list.
  it('lists, adds, and removes group members', async () => {
    seed([makeGroup('grp-1', 'Engineers')]);
    members['grp-1'] = ['user:alice'];

    renderPage();

    fireEvent.click(await screen.findByTestId('group-row-grp-1'));

    // Existing member renders.
    expect(await screen.findByText('user:alice')).toBeInTheDocument();

    // Add a new member.
    fireEvent.change(screen.getByTestId('group-member-input'), {
      target: { value: 'user:bob' },
    });
    fireEvent.click(screen.getByTestId('group-member-add'));

    expect(await screen.findByText('user:bob')).toBeInTheDocument();

    // Remove the original member.
    fireEvent.click(screen.getByTestId('group-member-remove-user:alice'));
    await waitFor(() =>
      expect(screen.queryByText('user:alice')).not.toBeInTheDocument(),
    );
  });
});
