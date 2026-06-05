// US-046 a11y BDD — focus management for the self-drawn replay drawer in
// web/src/components/functions/FunctionRepoPage.tsx (the `ReplayDrawer`).
//
// The replay drawer is a plain Tailwind full-screen overlay with
// role="dialog" aria-modal="true" — NOT the shared common/Modal (which
// already traps + restores focus). So focus management must live in the
// drawer itself, mirroring the merged VertexShareLinkPanel (#229).
//
// These scenarios pin the keyboard-user contract from the outside (what a
// keyboard / screen-reader user can observe):
//
//   Given the replay drawer is opened from the "Replay this commit" button
//     Then it is announced as a modal dialog (aria-modal="true")
//     And initial focus lands inside the drawer (not on the page behind it)
//   Given focus on the last focusable element, When Tab is pressed
//     Then focus wraps to the first (stays trapped — never escapes background)
//   Given focus on the first focusable element, When Shift+Tab is pressed
//     Then focus wraps to the last (stays trapped)
//   Given an open drawer, When Escape is pressed
//     Then the drawer closes (onClose fires, drawer unmounts)
//   Given a drawer opened from the trigger button, When it closes
//     Then focus returns to the trigger ("Replay this commit") button
//
// The function repo endpoints are stubbed at MSW so the page's data effects
// resolve and the drawer's form controls render as focusable targets. The
// heavy react-diff-viewer is mocked the same way the sibling FunctionCodePage
// / FunctionDiffPage suites do (jsdom doesn't need the real diff render path).

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
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';

vi.mock('react-diff-viewer-continued', () => ({
  default: () => createElement('div', { 'data-testid': 'rdv-mock' }),
  DiffMethod: { LINES: 'diffLines' },
}));

import { FunctionRepoPage } from '../FunctionRepoPage';

const ROUTE = '/functions/northwind/hello/repo';
const FUNCTIONS_URL = '/api/v2/ontologies/northwind/functions/hello';
const LOG_URL = '/api/v2/ontologies/northwind/functions/hello/log';
const COMMITS_URL = '/api/v2/ontologies/northwind/functions/hello/commits/:hash';
const VERSIONS_URL = '/api/v2/ontologies/northwind/functions/hello/versions';
const REPLAY_URL = '/api/v2/ontologies/northwind/functions/hello/replay';

const HASH_A = 'a'.repeat(40);

const server = setupServer();
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => {
  server.resetHandlers();
  vi.clearAllMocks();
});
afterAll(() => server.close());

function installHandlers() {
  server.use(
    http.get(FUNCTIONS_URL, () =>
      HttpResponse.json({
        rid: 'ri.ontology.main.function.f1',
        ontologyRid: 'ri.ontology.main.ontology.o1',
        name: 'hello',
        version: '1.2.0',
        sourceCode: 'function v() { return 1; }',
        runtime: 'goja',
      }),
    ),
    http.get(LOG_URL, () =>
      HttpResponse.json({
        data: [
          {
            hash: HASH_A,
            message: 'first',
            author: 'alice',
            email: 'alice@example.com',
            authorDate: '2026-05-04T10:00:00Z',
          },
        ],
      }),
    ),
    http.get(COMMITS_URL, ({ params }) =>
      HttpResponse.json({
        hash: String(params.hash),
        message: 'first',
        author: 'alice',
        email: 'alice@example.com',
        authorDate: '2026-05-04T10:00:00Z',
        sourceCode: 'function v() { return 1; }',
      }),
    ),
    http.get(VERSIONS_URL, () =>
      HttpResponse.json({ name: 'hello', data: [] }),
    ),
    http.post(REPLAY_URL, () =>
      HttpResponse.json({
        functionRid: 'ri.ontology.main.function.f1',
        functionVersion: '1.2.0',
        replayHash: 'deadbeef0000',
        match: true,
        result: { ok: true },
      }),
    ),
  );
}

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(
    createElement(
      QueryClientProvider,
      { client: qc },
      createElement(
        MemoryRouter,
        { initialEntries: [ROUTE] },
        createElement(
          Routes,
          null,
          createElement(Route, {
            path: '/functions/:ontology/:functionRid/repo',
            element: createElement(FunctionRepoPage, null),
          }),
        ),
      ),
    ),
  );
}

// Open the replay drawer from the "Replay this commit" trigger and wait for
// it to mount. Returns the trigger element so callers can assert focus
// restoration to it on close.
async function openDrawer(user: ReturnType<typeof userEvent.setup>) {
  installHandlers();
  renderPage();

  // Wait for the commit list to populate so the Replay trigger is enabled.
  const trigger = await screen.findByTestId('function-repo-replay-btn');
  await waitFor(() => expect(trigger).not.toBeDisabled());

  await user.click(trigger);
  await screen.findByTestId('function-repo-replay-drawer');
  return trigger;
}

describe('BDD: FunctionRepo replay drawer focus management', () => {
  it('Given the drawer is opened, Then it is announced as a modal dialog', async () => {
    const user = userEvent.setup();
    await openDrawer(user);

    const dialog = screen.getByTestId('function-repo-replay-drawer');
    expect(dialog).toHaveAttribute('role', 'dialog');
    expect(dialog).toHaveAttribute('aria-modal', 'true');
  });

  it('Given the drawer is opened, Then initial focus lands inside the drawer', async () => {
    const user = userEvent.setup();
    await openDrawer(user);

    const dialog = screen.getByTestId('function-repo-replay-drawer');
    expect(dialog.contains(document.activeElement)).toBe(true);
    expect(document.activeElement).not.toBe(document.body);
  });

  it('Given focus on the last focusable element, When Tab is pressed, Then focus wraps to the first (stays trapped)', async () => {
    const user = userEvent.setup();
    await openDrawer(user);

    const dialog = screen.getByTestId('function-repo-replay-drawer');
    const focusables = within(dialog).getAllByRole('button');
    const first = focusables[0];
    const last = focusables[focusables.length - 1];

    last.focus();
    expect(document.activeElement).toBe(last);

    await user.tab();

    expect(dialog.contains(document.activeElement)).toBe(true);
    expect(document.activeElement).toBe(first);
  });

  it('Given focus on the first focusable element, When Shift+Tab is pressed, Then focus wraps to the last (stays trapped)', async () => {
    const user = userEvent.setup();
    await openDrawer(user);

    const dialog = screen.getByTestId('function-repo-replay-drawer');
    const focusables = within(dialog).getAllByRole('button');
    const first = focusables[0];
    const last = focusables[focusables.length - 1];

    first.focus();
    expect(document.activeElement).toBe(first);

    await user.tab({ shift: true });

    expect(dialog.contains(document.activeElement)).toBe(true);
    expect(document.activeElement).toBe(last);
  });

  it('Given an open drawer, When Escape is pressed, Then the drawer closes', async () => {
    const user = userEvent.setup();
    await openDrawer(user);

    expect(screen.getByTestId('function-repo-replay-drawer')).toBeInTheDocument();

    await user.keyboard('{Escape}');

    await waitFor(() =>
      expect(
        screen.queryByTestId('function-repo-replay-drawer'),
      ).not.toBeInTheDocument(),
    );
  });

  it('Given a drawer opened from the trigger, When it closes via Escape, Then focus returns to the trigger', async () => {
    const user = userEvent.setup();
    const trigger = await openDrawer(user);

    const dialog = screen.getByTestId('function-repo-replay-drawer');
    expect(dialog.contains(document.activeElement)).toBe(true);

    await user.keyboard('{Escape}');
    await waitFor(() =>
      expect(
        screen.queryByTestId('function-repo-replay-drawer'),
      ).not.toBeInTheDocument(),
    );

    expect(document.activeElement).toBe(trigger);
  });
});
