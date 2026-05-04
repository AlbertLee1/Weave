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
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';

// Mock the heavy diff viewer so jsdom doesn't have to render emotion +
// measurement internals; the test only cares about which strings the
// component feeds in. The shape mirrors the ObjectDiffPanel test so the
// same pattern is reused without divergence.
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

import { FunctionDiffPage } from '../FunctionDiffPage';

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
            path: '/functions/:ontology/:functionRid/diff',
            element: createElement(FunctionDiffPage, null),
          }),
        ),
      ),
    ),
  );
}

const ROUTE = '/functions/northwind/hello/diff';
const FUNCTIONS_URL =
  '/api/v2/ontologies/northwind/functions/hello';
const LOG_URL =
  '/api/v2/ontologies/northwind/functions/hello/log';
const COMMITS_URL =
  '/api/v2/ontologies/northwind/functions/hello/commits/:hash';

function fnHandler() {
  return http.get(FUNCTIONS_URL, () =>
    HttpResponse.json({
      rid: 'ri.ontology.main.function.f1',
      ontologyRid: 'ri.ontology.main.ontology.o1',
      name: 'hello',
      version: '1.2.0',
      sourceCode: 'function v2() {}',
      runtime: 'goja',
    }),
  );
}

function logHandler(commits: Array<{ hash: string; message: string; author?: string }>) {
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

describe('FunctionDiffPage', () => {
  it('renders diff between the two latest commits by default', async () => {
    server.use(
      fnHandler(),
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

    await waitFor(() =>
      expect(screen.getByTestId('function-diff-viewer')).toBeInTheDocument(),
    );

    expect(screen.getByTestId('rdv-old').textContent).toBe('function v() { return 1; }');
    expect(screen.getByTestId('rdv-new').textContent).toBe('function v() { return 2; }');
    expect(screen.getByTestId('function-diff-subject').textContent).toContain('hello');
    expect(screen.getByTestId('function-diff-subject').textContent).toContain('1.2.0');
  });

  it('renders the inline-comment placeholder beneath the diff', async () => {
    server.use(
      fnHandler(),
      logHandler([
        { hash: 'b'.repeat(40), message: 'second' },
        { hash: 'a'.repeat(40), message: 'first' },
      ]),
      commitHandler({
        ['a'.repeat(40)]: 'old',
        ['b'.repeat(40)]: 'new',
      }),
    );

    renderRoute(ROUTE);

    await waitFor(() =>
      expect(screen.getByTestId('function-diff-comment-placeholder')).toBeInTheDocument(),
    );
    const submit = screen.getByTestId(
      'function-diff-comment-submit',
    ) as HTMLButtonElement;
    expect(submit.disabled).toBe(true);
  });

  it('shows a "no commits yet" empty state when the repo is empty', async () => {
    server.use(fnHandler(), logHandler([]));

    renderRoute(ROUTE);

    await waitFor(() =>
      expect(screen.getByTestId('function-diff-empty')).toBeInTheDocument(),
    );
    expect(screen.queryByTestId('function-diff-viewer')).toBeNull();
  });

  it('shows a "single commit" state when only one commit exists', async () => {
    server.use(
      fnHandler(),
      logHandler([{ hash: 'a'.repeat(40), message: 'first' }]),
    );

    renderRoute(ROUTE);

    await waitFor(() =>
      expect(screen.getByTestId('function-diff-single')).toBeInTheDocument(),
    );
  });

  it('updates the diff when the user picks different commits', async () => {
    server.use(
      fnHandler(),
      logHandler([
        { hash: 'c'.repeat(40), message: 'third' },
        { hash: 'b'.repeat(40), message: 'second' },
        { hash: 'a'.repeat(40), message: 'first' },
      ]),
      commitHandler({
        ['a'.repeat(40)]: 'one',
        ['b'.repeat(40)]: 'two',
        ['c'.repeat(40)]: 'three',
      }),
    );

    renderRoute(ROUTE);

    await waitFor(() =>
      expect(screen.getByTestId('function-diff-viewer')).toBeInTheDocument(),
    );

    // Default compares second(b) → third(c)
    expect(screen.getByTestId('rdv-old').textContent).toBe('two');
    expect(screen.getByTestId('rdv-new').textContent).toBe('three');

    // Pick "first" as the From commit — diff becomes a → c.
    const leftSelect = screen.getByTestId(
      'function-diff-left-select',
    ) as HTMLSelectElement;
    fireEvent.change(leftSelect, { target: { value: 'a'.repeat(40) } });

    await waitFor(() =>
      expect(screen.getByTestId('rdv-old').textContent).toBe('one'),
    );
    expect(screen.getByTestId('rdv-new').textContent).toBe('three');
  });

  it('shows a "pick distinct commits" hint when both selectors are equal', async () => {
    server.use(
      fnHandler(),
      logHandler([
        { hash: 'b'.repeat(40), message: 'second' },
        { hash: 'a'.repeat(40), message: 'first' },
      ]),
      commitHandler({
        ['a'.repeat(40)]: 'old',
        ['b'.repeat(40)]: 'new',
      }),
    );

    renderRoute(ROUTE);

    await waitFor(() =>
      expect(screen.getByTestId('function-diff-viewer')).toBeInTheDocument(),
    );
    const leftSelect = screen.getByTestId(
      'function-diff-left-select',
    ) as HTMLSelectElement;
    // Pick the right commit on the From dropdown so both selects point at b.
    fireEvent.change(leftSelect, { target: { value: 'b'.repeat(40) } });
    await waitFor(() =>
      expect(screen.getByTestId('function-diff-same')).toBeInTheDocument(),
    );
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
      expect(screen.getByTestId('function-diff-error')).toBeInTheDocument(),
    );
  });
});
