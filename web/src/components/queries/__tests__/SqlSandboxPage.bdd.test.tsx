import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import { SqlSandboxPage } from '../SqlSandboxPage';

// BDD: the SQL Sandbox page drives the new raw-SQL execute endpoint
// (POST /api/v2/sqlQueries/execute) and renders the inlined result set.
//
//   Given a SELECT that returns rows
//   When the user runs it
//   Then a result table with columns + rows is shown.
//
//   Given a query the engine rejects (failed status / 400 validation)
//   When the user runs it
//   Then the failureReason + error detail are surfaced (no result table).
//
// Each scenario asserts the request the page emits AND the rendered output
// so both the wire contract and the user-visible behaviour are locked.

interface Call {
  method: string;
  path: string;
  body: Record<string, unknown> | null;
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

type Responder = (call: Call) => Response;

function installFetch(calls: Call[], responder: Responder) {
  vi.stubGlobal(
    'fetch',
    vi.fn(
      async (
        input: RequestInfo | URL,
        init?: RequestInit,
      ): Promise<Response> => {
        const raw = typeof input === 'string' ? input : input.toString();
        const path = raw.replace(/^https?:\/\/[^/]+/, '');
        const method = (init?.method ?? 'GET').toUpperCase();
        const body = init?.body
          ? (JSON.parse(init.body as string) as Record<string, unknown>)
          : null;
        const call = { method, path, body };
        calls.push(call);
        return responder(call);
      },
    ),
  );
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/developer/sql']}>
      <SqlSandboxPage />
    </MemoryRouter>,
  );
}

describe('SqlSandboxPage', () => {
  let calls: Call[];

  beforeEach(() => {
    calls = [];
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('Given a SELECT returning rows, When the user runs it, Then a result table is rendered', async () => {
    installFetch(calls, (call) => {
      if (
        call.method === 'POST' &&
        call.path === '/api/v2/sqlQueries/execute'
      ) {
        return jsonResponse({
          type: 'succeeded',
          queryId: 'q-1',
          columns: ['id', 'name'],
          rows: [
            [1, 'alice'],
            [2, 'bob'],
          ],
        });
      }
      return jsonResponse({}, 200);
    });

    const user = userEvent.setup();
    renderPage();

    const input = screen.getByTestId('sql-input');
    await user.clear(input);
    await user.type(input, 'SELECT id, name FROM people');
    await user.click(screen.getByTestId('sql-run'));

    // Result table shows columns + rows.
    const table = await screen.findByTestId('sql-result-table');
    expect(within(table).getByText('id')).toBeInTheDocument();
    expect(within(table).getByText('name')).toBeInTheDocument();
    expect(within(table).getByText('alice')).toBeInTheDocument();
    expect(within(table).getByText('bob')).toBeInTheDocument();
    expect(within(table).getByText('1')).toBeInTheDocument();
    expect(within(table).getByText('2')).toBeInTheDocument();

    // The page POSTed the query the user typed.
    const exec = calls.find(
      (c) => c.path === '/api/v2/sqlQueries/execute' && c.method === 'POST',
    );
    expect(exec).toBeTruthy();
    expect(exec?.body?.query).toBe('SELECT id, name FROM people');

    // No error surface is shown on success.
    expect(screen.queryByTestId('sql-error')).not.toBeInTheDocument();
  });

  it('Given a zero-row SELECT, When the user runs it, Then the header renders with no rows', async () => {
    installFetch(calls, () =>
      jsonResponse({
        type: 'succeeded',
        queryId: 'q-2',
        columns: ['only_col'],
        rows: [],
      }),
    );

    const user = userEvent.setup();
    renderPage();

    await user.click(screen.getByTestId('sql-run'));

    const table = await screen.findByTestId('sql-result-table');
    expect(within(table).getByText('only_col')).toBeInTheDocument();
    expect(screen.getByTestId('sql-result-empty')).toBeInTheDocument();
  });

  it('Given a failed execution status, When the user runs it, Then the failureReason is surfaced', async () => {
    installFetch(calls, () =>
      jsonResponse({
        type: 'failed',
        queryId: 'q-3',
        failureReason: 'MaxRowsExceeded',
        errorMessage: 'query produced more rows than the configured cap',
        columns: null,
        rows: null,
      }),
    );

    const user = userEvent.setup();
    renderPage();

    const input = screen.getByTestId('sql-input');
    await user.clear(input);
    await user.type(input, 'SELECT generate_series(1, 1000000)');
    await user.click(screen.getByTestId('sql-run'));

    const err = await screen.findByTestId('sql-error');
    expect(within(err).getByTestId('sql-error-reason')).toHaveTextContent(
      'MaxRowsExceeded',
    );
    expect(within(err).getByTestId('sql-error-detail')).toHaveTextContent(
      'query produced more rows than the configured cap',
    );
    // No result table on failure.
    expect(screen.queryByTestId('sql-result-table')).not.toBeInTheDocument();
  });

  it('Given a non-SELECT rejected with HTTP 400, When the user runs it, Then the validation reason is surfaced', async () => {
    // Matches the real wire body for a validation rejection:
    // apierror.NewInvalidParameter("NonSelectQuery", …) serialises as
    // errorCode="INVALID_ARGUMENT", errorName="NonSelectQuery".
    installFetch(calls, () =>
      jsonResponse(
        {
          errorCode: 'INVALID_ARGUMENT',
          errorName: 'NonSelectQuery',
          errorInstanceId: 'i-1',
          parameters: {
            reason: 'only single-statement SELECT queries are allowed',
          },
        },
        400,
      ),
    );

    const user = userEvent.setup();
    renderPage();

    const input = screen.getByTestId('sql-input');
    await user.clear(input);
    await user.type(input, 'DROP TABLE users');
    await user.click(screen.getByTestId('sql-run'));

    const err = await screen.findByTestId('sql-error');
    expect(within(err).getByTestId('sql-error-reason')).toHaveTextContent(
      'NonSelectQuery',
    );
    expect(screen.queryByTestId('sql-result-table')).not.toBeInTheDocument();
  });
});
