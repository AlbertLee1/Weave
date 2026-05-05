import {
  describe,
  it,
  expect,
  vi,
  beforeAll,
  afterAll,
  afterEach,
} from 'vitest';
import { createElement } from 'react';
import {
  render,
  screen,
  fireEvent,
  waitFor,
} from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';

// Mock the heavy diff viewer the same way FunctionDiffPage's test suite
// does: jsdom doesn't need the full react-diff-viewer-continued render
// path, just the prop pass-through so we can assert which strings the
// diff sees.
vi.mock('react-diff-viewer-continued', () => ({
  default: (props: {
    oldValue: string;
    newValue: string;
    leftTitle?: string;
    rightTitle?: string;
  }) =>
    createElement(
      'div',
      { 'data-testid': 'rdv-mock' },
      createElement(
        'div',
        { 'data-testid': 'rdv-titles' },
        `${props.leftTitle ?? ''}|${props.rightTitle ?? ''}`,
      ),
      createElement('pre', { 'data-testid': 'rdv-old' }, props.oldValue),
      createElement('pre', { 'data-testid': 'rdv-new' }, props.newValue),
    ),
  DiffMethod: { LINES: 'diffLines' },
}));

import { FunctionCodePage } from '../FunctionCodePage';

const server = setupServer();
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => {
  server.resetHandlers();
  vi.clearAllMocks();
});
afterAll(() => server.close());

function renderRoute(initialPath: string) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(
    createElement(
      QueryClientProvider,
      { client: qc },
      createElement(
        MemoryRouter,
        { initialEntries: [initialPath] },
        createElement(
          Routes,
          null,
          createElement(Route, {
            path: '/functions/:ontology/:functionRid/code',
            element: createElement(FunctionCodePage, null),
          }),
        ),
      ),
    ),
  );
}

const ROUTE = '/functions/northwind/hello/code';
const FUNCTIONS_URL = '/api/v2/ontologies/northwind/functions/hello';
const LOG_URL = '/api/v2/ontologies/northwind/functions/hello/log';
const COMMITS_URL = '/api/v2/ontologies/northwind/functions/hello/commits/:hash';
const POST_COMMITS_URL =
  '/api/v2/ontologies/northwind/functions/hello/commits';

function fnHandler(sourceCode = 'function v() { return 1; }') {
  return http.get(FUNCTIONS_URL, () =>
    HttpResponse.json({
      rid: 'ri.ontology.main.function.f1',
      ontologyRid: 'ri.ontology.main.ontology.o1',
      name: 'hello',
      version: '1.2.0',
      sourceCode,
      runtime: 'goja',
    }),
  );
}

function logHandler(
  commits: Array<{ hash: string; message: string; author?: string }>,
) {
  return http.get(LOG_URL, () =>
    HttpResponse.json({
      data: commits.map((c) => ({
        hash: c.hash,
        message: c.message,
        author: c.author ?? 'alice',
        email: 'alice@example.com',
        authorDate: '2026-05-04T10:00:00Z',
      })),
    }),
  );
}

function commitHandler(sources: Record<string, string>) {
  return http.get(COMMITS_URL, ({ params }) => {
    const hash = String(params.hash);
    const src = sources[hash];
    if (src === undefined) {
      return HttpResponse.json(
        { errorCode: 'NOT_FOUND', errorName: 'FunctionRepoCommitNotFound' },
        { status: 404 },
      );
    }
    return HttpResponse.json({
      hash,
      message: 'commit msg',
      author: 'alice',
      email: 'alice@example.com',
      authorDate: '2026-05-04T10:00:00Z',
      sourceCode: src,
    });
  });
}

describe('FunctionCodePage', () => {
  it('seeds the editor from the working-copy sourceCode', async () => {
    server.use(
      fnHandler('function v() { return 42; }'),
      logHandler([
        { hash: 'b'.repeat(40), message: 'second' },
        { hash: 'a'.repeat(40), message: 'first' },
      ]),
      commitHandler({
        ['a'.repeat(40)]: 'function v() { return 1; }',
        ['b'.repeat(40)]: 'function v() { return 2; }',
      }),
    );

    renderRoute(ROUTE);

    const editor = (await screen.findByTestId(
      'function-code-editor',
    )) as HTMLTextAreaElement;
    expect(editor.value).toBe('function v() { return 42; }');
    expect(screen.getByTestId('function-code-subject').textContent).toContain(
      'hello',
    );
  });

  it('shows the empty state when no commits exist yet', async () => {
    server.use(fnHandler(''), logHandler([]));

    renderRoute(ROUTE);

    await waitFor(() =>
      expect(
        screen.getByTestId('function-code-commit-list-empty'),
      ).toBeInTheDocument(),
    );
    // No commit selected yet → diff pane shows the placeholder.
    expect(
      screen.getByTestId('function-code-diff-placeholder'),
    ).toBeInTheDocument();
  });

  it('renders a diff between the selected commit and the editor', async () => {
    server.use(
      fnHandler('function v() { return 99; }'),
      logHandler([
        { hash: 'b'.repeat(40), message: 'second' },
        { hash: 'a'.repeat(40), message: 'first' },
      ]),
      commitHandler({
        ['a'.repeat(40)]: 'function v() { return 1; }',
        ['b'.repeat(40)]: 'function v() { return 2; }',
      }),
    );

    renderRoute(ROUTE);

    // Default selection is the most recent commit (b).
    await waitFor(() =>
      expect(
        screen.getByTestId('function-code-diff-viewer'),
      ).toBeInTheDocument(),
    );
    expect(screen.getByTestId('rdv-old').textContent).toBe(
      'function v() { return 2; }',
    );
    expect(screen.getByTestId('rdv-new').textContent).toBe(
      'function v() { return 99; }',
    );

    // Pick the older commit — diff base flips to 'a'.
    fireEvent.click(
      screen.getByTestId(`function-code-commit-row-${'a'.repeat(40)}`),
    );
    await waitFor(() =>
      expect(screen.getByTestId('rdv-old').textContent).toBe(
        'function v() { return 1; }',
      ),
    );
    // The right side keeps the editor draft.
    expect(screen.getByTestId('rdv-new').textContent).toBe(
      'function v() { return 99; }',
    );
  });

  it('loads the selected commit into the editor on demand', async () => {
    server.use(
      fnHandler('function v() { return 99; }'),
      logHandler([
        { hash: 'a'.repeat(40), message: 'first' },
      ]),
      commitHandler({
        ['a'.repeat(40)]: 'function v() { return 1; }',
      }),
    );

    renderRoute(ROUTE);

    const editor = (await screen.findByTestId(
      'function-code-editor',
    )) as HTMLTextAreaElement;
    expect(editor.value).toBe('function v() { return 99; }');

    // Wait for the commit source to be fetched so the load button gates open.
    await waitFor(() =>
      expect(screen.getByTestId('rdv-old').textContent).toBe(
        'function v() { return 1; }',
      ),
    );

    fireEvent.click(screen.getByTestId('function-code-load-into-editor'));
    await waitFor(() => expect(editor.value).toBe('function v() { return 1; }'));
  });

  it('disables the Commit button when message or draft is empty', async () => {
    server.use(
      fnHandler('start'),
      logHandler([{ hash: 'a'.repeat(40), message: 'first' }]),
      commitHandler({ ['a'.repeat(40)]: 'start' }),
    );

    renderRoute(ROUTE);

    const button = (await screen.findByTestId(
      'function-code-commit-button',
    )) as HTMLButtonElement;
    // Empty message → still disabled even when draft is non-empty.
    expect(button.disabled).toBe(true);

    fireEvent.change(screen.getByTestId('function-code-commit-message'), {
      target: { value: 'work in progress' },
    });
    await waitFor(() => expect(button.disabled).toBe(false));
  });

  it('POSTs a new commit and refetches the log on success', async () => {
    let postedBody: { message?: string; sourceCode?: string } | null = null;
    let logCalls = 0;
    server.use(
      fnHandler('seed'),
      http.get(LOG_URL, () => {
        logCalls += 1;
        if (logCalls === 1) {
          return HttpResponse.json({
            data: [
              {
                hash: 'a'.repeat(40),
                message: 'first',
                author: 'alice',
                email: 'a@x',
                authorDate: '2026-05-04T10:00:00Z',
              },
            ],
          });
        }
        return HttpResponse.json({
          data: [
            {
              hash: 'c'.repeat(40),
              message: 'work in progress',
              author: 'alice',
              email: 'a@x',
              authorDate: '2026-05-04T11:00:00Z',
            },
            {
              hash: 'a'.repeat(40),
              message: 'first',
              author: 'alice',
              email: 'a@x',
              authorDate: '2026-05-04T10:00:00Z',
            },
          ],
        });
      }),
      commitHandler({
        ['a'.repeat(40)]: 'seed',
        ['c'.repeat(40)]: 'seed-with-edit',
      }),
      http.post(POST_COMMITS_URL, async ({ request }) => {
        postedBody = (await request.json()) as {
          message?: string;
          sourceCode?: string;
        };
        return HttpResponse.json(
          {
            hash: 'c'.repeat(40),
            message: postedBody?.message ?? '',
            author: 'alice',
            email: 'a@x',
            authorDate: '2026-05-04T11:00:00Z',
          },
          { status: 201 },
        );
      }),
    );

    renderRoute(ROUTE);

    const editor = (await screen.findByTestId(
      'function-code-editor',
    )) as HTMLTextAreaElement;
    fireEvent.change(editor, { target: { value: 'seed-with-edit' } });
    fireEvent.change(screen.getByTestId('function-code-commit-message'), {
      target: { value: 'work in progress' },
    });
    fireEvent.click(screen.getByTestId('function-code-commit-button'));

    await waitFor(() =>
      expect(
        screen.getByTestId(`function-code-commit-row-${'c'.repeat(40)}`),
      ).toBeInTheDocument(),
    );
    expect(postedBody).toEqual({
      message: 'work in progress',
      sourceCode: 'seed-with-edit',
    });
    // Message input cleared after the successful commit so a re-press
    // doesn't accidentally double-submit.
    expect(
      (screen.getByTestId('function-code-commit-message') as HTMLInputElement)
        .value,
    ).toBe('');
  });

  it('surfaces a friendly error when the commit POST fails', async () => {
    server.use(
      fnHandler('seed'),
      logHandler([{ hash: 'a'.repeat(40), message: 'first' }]),
      commitHandler({ ['a'.repeat(40)]: 'seed' }),
      http.post(POST_COMMITS_URL, () =>
        HttpResponse.json(
          {
            errorCode: 'INVALID_ARGUMENT',
            errorName: 'InvalidCommitMessage',
            errorInstanceId: 'eid',
          },
          { status: 400 },
        ),
      ),
    );

    renderRoute(ROUTE);

    fireEvent.change(
      await screen.findByTestId('function-code-commit-message'),
      {
        target: { value: 'broken' },
      },
    );
    fireEvent.click(screen.getByTestId('function-code-commit-button'));

    await waitFor(() =>
      expect(
        screen.getByTestId('function-code-commit-error'),
      ).toBeInTheDocument(),
    );
    expect(
      screen.getByTestId('function-code-commit-error').textContent,
    ).toContain('INVALID_ARGUMENT');
  });

  it('surfaces a friendly error when the function lookup fails', async () => {
    server.use(
      http.get(FUNCTIONS_URL, () =>
        HttpResponse.json(
          { errorCode: 'NOT_FOUND', errorName: 'FunctionNotFound' },
          { status: 404 },
        ),
      ),
    );

    renderRoute(ROUTE);

    await waitFor(() =>
      expect(screen.getByTestId('function-code-error')).toBeInTheDocument(),
    );
  });
});
