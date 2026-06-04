import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { AuditHistoryPage } from '../AuditHistoryPage';
import type { AuditEvent } from '../../../api/audit';

// BDD: the audit history page exposes resourceRid + action filters that the
// backend (cmd/server/admin_audit.go) and api layer (web/src/api/audit.ts)
// already understand. These scenarios pin the observable contract: typing a
// Resource RID / picking an action re-issues the list request with the
// matching query params, and Clear wipes them.

function mkEvent(partial: Partial<AuditEvent>, i: number): AuditEvent {
  return {
    id: `e${i}`,
    actor_id: 'user-1',
    action: 'create',
    resource_type: 'ObjectType',
    resource_rid: `ri.ontology.main.object-type.${i}`,
    ip: '127.0.0.1',
    user_agent: 'vitest',
    ts: new Date(2026, 0, 1, 12, i).toISOString(),
    ...partial,
  };
}

const PAGE1: AuditEvent[] = [
  mkEvent({ id: 'evt-1', action: 'create' }, 1),
  mkEvent({ id: 'evt-2', action: 'update' }, 2),
  mkEvent({ id: 'evt-3', action: 'delete' }, 3),
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
      const params = new URLSearchParams(
        qStart >= 0 ? url.slice(qStart + 1) : '',
      );
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
          <Route
            path="/admin/:ontology/history"
            element={<AuditHistoryPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BDD: AuditHistoryPage resource-RID & action filters', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('Given the audit page, When an admin types a Resource RID, Then the list request carries resourceRid=', async () => {
    const calls = installFetch(() => jsonResponse({ data: PAGE1 }));
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByTestId('audit-row')).toHaveLength(3);
    });

    const rid = 'ri.ontology.main.object-type.42';
    await user.type(screen.getByLabelText('Resource RID'), rid);

    await waitFor(() => {
      const hit = calls.filter((c) => c.params.get('resourceRid') === rid);
      expect(hit.length).toBeGreaterThan(0);
    });
  });

  it('Given the audit page, When an admin selects an action, Then the list request carries action=', async () => {
    const calls = installFetch(() => jsonResponse({ data: PAGE1 }));
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByTestId('audit-row')).toHaveLength(3);
    });

    await user.selectOptions(screen.getByLabelText('Action'), 'delete');

    await waitFor(() => {
      const hit = calls.filter((c) => c.params.get('action') === 'delete');
      expect(hit.length).toBeGreaterThan(0);
    });
  });

  it('exposes the documented action vocabulary as select options', async () => {
    installFetch(() => jsonResponse({ data: PAGE1 }));
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByTestId('audit-row')).toHaveLength(3);
    });
    const select = screen.getByLabelText('Action') as HTMLSelectElement;
    const values = Array.from(select.options).map((o) => o.value);
    expect(values).toEqual([
      '',
      'create',
      'update',
      'delete',
      'login_success',
      'login_failure',
    ]);
  });

  it('When Clear is clicked, Then resourceRid + action inputs reset and drop from the request', async () => {
    const calls = installFetch(() => jsonResponse({ data: PAGE1 }));
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByTestId('audit-row')).toHaveLength(3);
    });

    const ridInput = screen.getByLabelText('Resource RID') as HTMLInputElement;
    const actionSelect = screen.getByLabelText('Action') as HTMLSelectElement;
    await user.type(ridInput, 'ri.ontology.main.object-type.7');
    await user.selectOptions(actionSelect, 'update');
    expect(ridInput.value).toBe('ri.ontology.main.object-type.7');
    expect(actionSelect.value).toBe('update');

    await user.click(screen.getByRole('button', { name: /Clear/i }));
    expect(ridInput.value).toBe('');
    expect(actionSelect.value).toBe('');

    await waitFor(() => {
      const last = calls[calls.length - 1];
      expect(last.params.get('resourceRid')).toBeNull();
      expect(last.params.get('action')).toBeNull();
    });
  });
});
