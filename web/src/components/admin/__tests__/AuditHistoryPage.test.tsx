import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { AuditHistoryPage } from '../AuditHistoryPage';
import type { AuditEvent } from '../../../api/audit';

function mkEvent(partial: Partial<AuditEvent>, i: number): AuditEvent {
  return {
    id: `e${i}`,
    actor_id: 'user-1',
    action: 'CREATE',
    resource_type: 'ObjectType',
    resource_rid: `ri.ontology.main.object-type.${i}`,
    ip: '127.0.0.1',
    user_agent: 'vitest',
    ts: new Date(2026, 0, 1, 12, i).toISOString(),
    ...partial,
  };
}

const PAGE1: AuditEvent[] = [
  mkEvent(
    {
      id: 'evt-create',
      action: 'CREATE',
      resource_type: 'ObjectType',
      actor_id: 'alice',
    },
    1,
  ),
  mkEvent(
    {
      id: 'evt-update',
      action: 'UPDATE',
      resource_type: 'Property',
      actor_id: 'bob',
      diff_json: { before: { name: 'old' }, after: { name: 'new' } },
    },
    2,
  ),
  mkEvent(
    {
      id: 'evt-delete',
      action: 'DELETE',
      resource_type: 'LinkType',
      actor_id: 'alice',
    },
    3,
  ),
];

const PAGE2: AuditEvent[] = [
  mkEvent(
    {
      id: 'evt-page2',
      action: 'UPDATE',
      resource_type: 'ActionType',
      actor_id: 'bob',
    },
    4,
  ),
];

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

interface FetchCall {
  url: string;
  params: URLSearchParams;
}

function installFetch(responder: (call: FetchCall) => Response) {
  const calls: FetchCall[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL): Promise<Response> => {
      const url = typeof input === 'string' ? input : input.toString();
      const qStart = url.indexOf('?');
      const params = new URLSearchParams(qStart >= 0 ? url.slice(qStart + 1) : '');
      const call = { url, params };
      calls.push(call);
      return responder(call);
    }),
  );
  return calls;
}

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchInterval: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/admin/northwind/history']}>
        <Routes>
          <Route path="/admin/:ontology/history" element={<AuditHistoryPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('AuditHistoryPage', () => {
  beforeEach(() => {});

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('renders heading and ontology name', async () => {
    installFetch(() => jsonResponse({ data: [] }));
    renderPage();
    expect(
      screen.getByRole('heading', { name: /Ontology Manager — Audit History/i }),
    ).toBeInTheDocument();
    expect(screen.getByText(/northwind/i)).toBeInTheDocument();
  });

  it('renders a timeline row for each audit event with actor, action and resource', async () => {
    installFetch(() => jsonResponse({ data: PAGE1 }));
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByTestId('audit-row')).toHaveLength(3);
    });
    expect(screen.getAllByText('alice').length).toBe(2);
    expect(screen.getByText('bob')).toBeInTheDocument();
    // action badges
    expect(screen.getByText('CREATE')).toBeInTheDocument();
    expect(screen.getByText('UPDATE')).toBeInTheDocument();
    expect(screen.getByText('DELETE')).toBeInTheDocument();
    // resource type labels (present in both dropdown options AND row content)
    expect(screen.getAllByText('ObjectType').length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText('Property').length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText('LinkType').length).toBeGreaterThanOrEqual(1);
  });

  it('shows empty state when no events match', async () => {
    installFetch(() => jsonResponse({ data: [] }));
    renderPage();
    await waitFor(() => {
      expect(screen.getByText(/No audit events match/i)).toBeInTheDocument();
    });
  });

  it('expands a row to show the before/after diff', async () => {
    const user = userEvent.setup();
    installFetch(() => jsonResponse({ data: PAGE1 }));
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByTestId('audit-row')).toHaveLength(3);
    });
    // Diff exists on the "UPDATE" Property row (evt-update)
    const updateRowButton = screen.getByLabelText('Audit event evt-update');
    expect(screen.queryByTestId('audit-diff')).not.toBeInTheDocument();
    await user.click(updateRowButton);
    const diff = await screen.findByTestId('audit-diff');
    expect(diff).toBeInTheDocument();
    expect(diff.textContent).toContain('Before');
    expect(diff.textContent).toContain('After');
    expect(diff.textContent).toContain('old');
    expect(diff.textContent).toContain('new');
  });

  it('applies entity-type filter by sending resource_type query param', async () => {
    const calls = installFetch((c) => {
      const rt = c.params.get('resource_type') ?? '';
      if (rt === 'Property') {
        return jsonResponse({ data: [PAGE1[1]] });
      }
      return jsonResponse({ data: PAGE1 });
    });
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByTestId('audit-row')).toHaveLength(3);
    });
    await user.selectOptions(
      screen.getByLabelText('Entity type'),
      'Property',
    );
    await waitFor(() => {
      expect(screen.getAllByTestId('audit-row')).toHaveLength(1);
    });
    const filtered = calls.filter((c) => c.params.get('resource_type') === 'Property');
    expect(filtered.length).toBeGreaterThan(0);
  });

  it('applies actor filter by sending actor query param', async () => {
    const calls = installFetch((c) => {
      const actor = c.params.get('actor') ?? '';
      if (actor === 'alice') {
        return jsonResponse({
          data: PAGE1.filter((e) => e.actor_id === 'alice'),
        });
      }
      return jsonResponse({ data: PAGE1 });
    });
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByTestId('audit-row')).toHaveLength(3);
    });
    await user.type(screen.getByLabelText('Actor'), 'alice');
    await waitFor(() => {
      expect(screen.getAllByTestId('audit-row')).toHaveLength(2);
    });
    const filtered = calls.filter((c) => c.params.get('actor') === 'alice');
    expect(filtered.length).toBeGreaterThan(0);
  });

  it('loads more pages via the load-more button', async () => {
    installFetch((c) => {
      const pageToken = c.params.get('pageToken') ?? '';
      if (pageToken === 'tok-2') {
        return jsonResponse({ data: PAGE2 });
      }
      return jsonResponse({ data: PAGE1, nextPageToken: 'tok-2' });
    });
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByTestId('audit-row')).toHaveLength(3);
    });
    const loadMore = screen.getByRole('button', { name: /Load more/i });
    await user.click(loadMore);
    await waitFor(() => {
      expect(screen.getAllByTestId('audit-row')).toHaveLength(4);
    });
    // Exhausted — load more hidden, "End of history" shown
    expect(screen.getByText(/End of history/i)).toBeInTheDocument();
  });

  it('renders API errors', async () => {
    installFetch(() =>
      jsonResponse(
        {
          errorCode: 'Internal',
          errorName: 'AuditListFailed',
          errorInstanceId: 'x',
        },
        500,
      ),
    );
    renderPage();
    await waitFor(() => {
      expect(screen.getByText(/Failed to load audit events/i)).toBeInTheDocument();
    });
  });

  it('clears filters via the Clear button', async () => {
    installFetch(() => jsonResponse({ data: PAGE1 }));
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByTestId('audit-row')).toHaveLength(3);
    });
    const actorInput = screen.getByLabelText('Actor') as HTMLInputElement;
    await user.type(actorInput, 'alice');
    expect(actorInput.value).toBe('alice');
    const entitySelect = screen.getByLabelText(
      'Entity type',
    ) as HTMLSelectElement;
    await user.selectOptions(entitySelect, 'Property');
    expect(entitySelect.value).toBe('Property');

    await user.click(screen.getByRole('button', { name: /Clear/i }));
    expect(actorInput.value).toBe('');
    expect(entitySelect.value).toBe('');
  });
});
