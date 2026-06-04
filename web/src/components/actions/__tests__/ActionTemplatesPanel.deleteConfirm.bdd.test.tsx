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
import { createElement } from 'react';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { ActionTemplatesPanel } from '../ActionTemplatesPanel';
import type { ActionTemplate } from '../../../api/actionTemplates';

// UX consistency — action-template deletion confirm.
//
// The panel previously gated DELETE behind a native `window.confirm`,
// which does not honour the dark theme, breaks visually with the rest of
// the app's styled Modal dialogs, and is awkward to assert against. The
// codebase has standardised on the shared `common/Modal` (see
// DashboardEditorPage's note about deliberately avoiding the unstylable
// window.confirm). This contract pins the styled two-step confirm flow:
// click Delete → styled Modal naming the template → Cancel aborts (no
// DELETE), Confirm deletes and removes the row. The native window.confirm
// must never be invoked.

function templateFixture(overrides: Partial<ActionTemplate>): ActionTemplate {
  return {
    id: 'tmpl-x',
    name: 'Daily Reorder',
    ontology: 'main',
    actionType: 'createOrder',
    createdBy: 'user:alice',
    scope: 'PRIVATE',
    shared: false,
    parameters: { qty: 1, sku: 'WIDGET' },
    createdAt: '2026-04-28T00:00:00Z',
    updatedAt: '2026-04-28T00:00:00Z',
    ...overrides,
  };
}

let templates: ActionTemplate[] = [];
let deleteCalls: string[] = [];

const server = setupServer(
  http.get('/api/v2/action-templates', () =>
    HttpResponse.json({ actionTemplates: templates }),
  ),
  http.delete('/api/v2/action-templates/:id', ({ params }) => {
    const id = String(params.id);
    deleteCalls.push(id);
    templates = templates.filter((t) => t.id !== id);
    return new HttpResponse(null, { status: 204 });
  }),
);

function renderPanel() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
  const Wrapper = ({ children }: { children: React.ReactNode }) =>
    createElement(QueryClientProvider, { client: queryClient }, children);
  return render(
    <ActionTemplatesPanel
      ontology="main"
      actionType="createOrder"
      currentParameters={{}}
      hasCurrentState={false}
      onLoad={() => {}}
      currentUserId="user:alice"
    />,
    { wrapper: Wrapper },
  );
}

describe('ActionTemplatesPanel styled delete-confirm Modal (UX consistency)', () => {
  beforeAll(() => server.listen());
  beforeEach(() => {
    templates = [];
    deleteCalls = [];
  });
  afterEach(() => {
    server.resetHandlers();
    vi.restoreAllMocks();
  });
  afterAll(() => server.close());

  it('Given a template, When Delete is clicked, Then window.confirm is NOT called and a styled Modal naming the template appears', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm');
    templates = [templateFixture({ id: 'tmpl-a', name: 'Daily Reorder' })];

    renderPanel();
    const user = userEvent.setup();

    const delBtn = await screen.findByTestId('action-template-delete-tmpl-a');
    await user.click(delBtn);

    // No native, unstylable confirm.
    expect(confirmSpy).not.toHaveBeenCalled();

    // A styled shared-Modal dialog appears, naming the template.
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText(/Daily Reorder/)).toBeInTheDocument();

    // Nothing deleted just by opening the confirm.
    expect(deleteCalls).toEqual([]);
  });

  it('Given the confirm Modal is open, When Cancel is clicked, Then the template is NOT deleted and the Modal closes', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm');
    templates = [templateFixture({ id: 'tmpl-a', name: 'Daily Reorder' })];

    renderPanel();
    const user = userEvent.setup();

    await user.click(await screen.findByTestId('action-template-delete-tmpl-a'));

    const dialog = await screen.findByRole('dialog');
    await user.click(within(dialog).getByRole('button', { name: /cancel/i }));

    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument(),
    );
    expect(confirmSpy).not.toHaveBeenCalled();
    expect(deleteCalls).toEqual([]);
    // Row still present.
    expect(
      await screen.findByTestId('action-template-load-tmpl-a'),
    ).toBeInTheDocument();
  });

  it('Given the confirm Modal is open, When the destructive Delete is clicked, Then the template is deleted and disappears', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm');
    templates = [
      templateFixture({ id: 'tmpl-a', name: 'Daily Reorder' }),
      templateFixture({ id: 'tmpl-b', name: 'Express Ship' }),
    ];

    renderPanel();
    const user = userEvent.setup();

    await user.click(await screen.findByTestId('action-template-delete-tmpl-a'));

    const dialog = await screen.findByRole('dialog');
    // The destructive confirm button (distinct from Cancel).
    await user.click(
      within(dialog).getByTestId('action-template-delete-confirm-btn'),
    );

    // Real DELETE fired for the chosen template only.
    await waitFor(() => expect(deleteCalls).toEqual(['tmpl-a']));
    expect(confirmSpy).not.toHaveBeenCalled();

    // Row gone after the list refetches; the other template survives.
    await waitFor(() =>
      expect(
        screen.queryByTestId('action-template-load-tmpl-a'),
      ).not.toBeInTheDocument(),
    );
    expect(
      screen.getByTestId('action-template-load-tmpl-b'),
    ).toBeInTheDocument();

    // Modal closed.
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });
});
