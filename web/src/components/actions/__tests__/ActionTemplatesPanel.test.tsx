import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createElement } from 'react';
import {
  render,
  screen,
  fireEvent,
  waitFor,
  act,
} from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ActionTemplatesPanel } from '../ActionTemplatesPanel';
import * as api from '../../../api/actionTemplates';
import type { ActionTemplate } from '../../../api/actionTemplates';

function makeWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
  return ({ children }: { children: React.ReactNode }) =>
    createElement(QueryClientProvider, { client: queryClient }, children);
}

function renderWithProviders(ui: React.ReactElement) {
  const Wrapper = makeWrapper();
  return render(ui, { wrapper: Wrapper });
}

const OWN_ROW: ActionTemplate = {
  id: 'tmpl-1',
  name: 'Daily Reorder',
  ontology: 'main',
  actionType: 'createOrder',
  createdBy: 'user:alice',
  scope: 'PRIVATE',
  shared: false,
  parameters: { qty: 1, sku: 'WIDGET' },
  createdAt: '2026-04-28T00:00:00Z',
  updatedAt: '2026-04-28T00:00:00Z',
};

const SHARED_ROW: ActionTemplate = {
  id: 'tmpl-2',
  name: 'Team Default',
  ontology: 'main',
  actionType: 'createOrder',
  createdBy: 'user:bob',
  scope: 'PUBLIC',
  shared: true,
  parameters: { qty: 5 },
  createdAt: '2026-04-28T00:00:00Z',
  updatedAt: '2026-04-28T00:00:00Z',
};

const TEAM_ROW: ActionTemplate = {
  id: 'tmpl-3',
  name: 'Team Reorder',
  ontology: 'main',
  actionType: 'createOrder',
  createdBy: 'user:bob',
  scope: 'TEAM',
  shared: true,
  parameters: { qty: 10 },
  createdAt: '2026-04-28T00:00:00Z',
  updatedAt: '2026-04-28T00:00:00Z',
};

describe('ActionTemplatesPanel', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('shows empty state when there are no templates', async () => {
    vi.spyOn(api, 'listActionTemplates').mockResolvedValue({
      actionTemplates: [],
    });

    renderWithProviders(
      <ActionTemplatesPanel
        ontology="main"
        actionType="createOrder"
        currentParameters={{}}
        hasCurrentState={false}
        onLoad={() => {}}
        currentUserId="user:alice"
      />,
    );

    await waitFor(() =>
      expect(screen.getByTestId('action-templates-empty')).toBeInTheDocument(),
    );
  });

  it('renders own + shared rows; loading delivers parameters to onLoad', async () => {
    vi.spyOn(api, 'listActionTemplates').mockResolvedValue({
      actionTemplates: [OWN_ROW, SHARED_ROW, TEAM_ROW],
    });
    const onLoad = vi.fn();
    renderWithProviders(
      <ActionTemplatesPanel
        ontology="main"
        actionType="createOrder"
        currentParameters={{}}
        hasCurrentState={false}
        onLoad={onLoad}
        currentUserId="user:alice"
      />,
    );

    // Own row: load button + delete button visible.
    await screen.findByTestId(`action-template-load-${OWN_ROW.id}`);
    expect(
      screen.queryByTestId(`action-template-delete-${OWN_ROW.id}`),
    ).toBeInTheDocument();
    // Private row gets no scope badge.
    expect(
      screen.queryByTestId(`action-template-scope-badge-${OWN_ROW.id}`),
    ).toBeNull();

    // Shared (PUBLIC) row: load button visible, delete button hidden, scope
    // badge says "public".
    expect(
      screen.getByTestId(`action-template-load-${SHARED_ROW.id}`),
    ).toBeInTheDocument();
    expect(
      screen.queryByTestId(`action-template-delete-${SHARED_ROW.id}`),
    ).toBeNull();
    expect(
      screen.getByTestId(`action-template-scope-badge-${SHARED_ROW.id}`),
    ).toHaveTextContent('public');

    // TEAM row badge says "team".
    expect(
      screen.getByTestId(`action-template-scope-badge-${TEAM_ROW.id}`),
    ).toHaveTextContent('team');

    // Click load on a shared row → onLoad gets bob's parameters.
    fireEvent.click(screen.getByTestId(`action-template-load-${SHARED_ROW.id}`));
    expect(onLoad).toHaveBeenCalledWith({ qty: 5 });
  });

  it('save button is disabled when no parameters are filled in', async () => {
    vi.spyOn(api, 'listActionTemplates').mockResolvedValue({
      actionTemplates: [],
    });
    renderWithProviders(
      <ActionTemplatesPanel
        ontology="main"
        actionType="createOrder"
        currentParameters={{}}
        hasCurrentState={false}
        onLoad={() => {}}
        currentUserId="user:alice"
      />,
    );
    const btn = await screen.findByTestId('action-template-save');
    expect(btn).toBeDisabled();
  });

  it('save flow opens dialog, posts to API with selected scope', async () => {
    vi.spyOn(api, 'listActionTemplates').mockResolvedValue({
      actionTemplates: [],
    });
    const created: ActionTemplate = {
      ...OWN_ROW,
      id: 'tmpl-3',
      name: 'Express',
      scope: 'TEAM',
      shared: true,
    };
    const createSpy = vi
      .spyOn(api, 'createActionTemplate')
      .mockResolvedValue(created);

    renderWithProviders(
      <ActionTemplatesPanel
        ontology="main"
        actionType="createOrder"
        currentParameters={{ qty: 2 }}
        hasCurrentState={true}
        onLoad={() => {}}
        currentUserId="user:alice"
      />,
    );

    const saveBtn = await screen.findByTestId('action-template-save');
    fireEvent.click(saveBtn);

    const nameInput = await screen.findByTestId('action-template-name-input');
    await act(async () => {
      fireEvent.change(nameInput, { target: { value: 'Express' } });
    });
    const teamRadio = screen.getByTestId('action-template-scope-input-TEAM');
    await act(async () => {
      fireEvent.click(teamRadio);
    });

    const confirm = screen.getByTestId('action-template-confirm');
    await act(async () => {
      fireEvent.click(confirm);
    });

    await waitFor(() => expect(createSpy).toHaveBeenCalled());
    expect(createSpy.mock.calls[0][0]).toMatchObject({
      name: 'Express',
      ontology: 'main',
      actionType: 'createOrder',
      scope: 'TEAM',
      parameters: { qty: 2 },
    });
  });

  it('owner can change scope inline via select', async () => {
    vi.spyOn(api, 'listActionTemplates').mockResolvedValue({
      actionTemplates: [OWN_ROW],
    });
    const updated: ActionTemplate = { ...OWN_ROW, scope: 'PUBLIC', shared: true };
    const updateSpy = vi.spyOn(api, 'updateActionTemplate').mockResolvedValue(updated);

    renderWithProviders(
      <ActionTemplatesPanel
        ontology="main"
        actionType="createOrder"
        currentParameters={{}}
        hasCurrentState={false}
        onLoad={() => {}}
        currentUserId="user:alice"
      />,
    );

    const select = await screen.findByTestId(
      `action-template-scope-select-${OWN_ROW.id}`,
    );
    await act(async () => {
      fireEvent.change(select, { target: { value: 'PUBLIC' } });
    });
    await waitFor(() =>
      expect(updateSpy).toHaveBeenCalledWith({ id: OWN_ROW.id, scope: 'PUBLIC' }),
    );
  });

  it('delete button opens styled Modal confirm and calls API only on confirm', async () => {
    vi.spyOn(api, 'listActionTemplates').mockResolvedValue({
      actionTemplates: [OWN_ROW],
    });
    const delSpy = vi
      .spyOn(api, 'deleteActionTemplate')
      .mockResolvedValue(undefined as void);
    // Deletion no longer uses the native, unstylable window.confirm.
    const confirmSpy = vi.spyOn(window, 'confirm');

    renderWithProviders(
      <ActionTemplatesPanel
        ontology="main"
        actionType="createOrder"
        currentParameters={{}}
        hasCurrentState={false}
        onLoad={() => {}}
        currentUserId="user:alice"
      />,
    );

    const delBtn = await screen.findByTestId(
      `action-template-delete-${OWN_ROW.id}`,
    );
    await act(async () => {
      fireEvent.click(delBtn);
    });

    // No native confirm; a styled Modal opens instead and nothing is
    // deleted until the destructive button is pressed.
    expect(confirmSpy).not.toHaveBeenCalled();
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(delSpy).not.toHaveBeenCalled();

    const confirmBtn = screen.getByTestId('action-template-delete-confirm-btn');
    await act(async () => {
      fireEvent.click(confirmBtn);
    });
    expect(confirmSpy).not.toHaveBeenCalled();
    await waitFor(() => expect(delSpy).toHaveBeenCalledWith(OWN_ROW.id));
  });

  it('hides delete affordance when currentUserId is undefined', async () => {
    vi.spyOn(api, 'listActionTemplates').mockResolvedValue({
      actionTemplates: [OWN_ROW],
    });
    renderWithProviders(
      <ActionTemplatesPanel
        ontology="main"
        actionType="createOrder"
        currentParameters={{}}
        hasCurrentState={false}
        onLoad={() => {}}
      />,
    );
    await screen.findByTestId(`action-template-load-${OWN_ROW.id}`);
    expect(
      screen.queryByTestId(`action-template-delete-${OWN_ROW.id}`),
    ).toBeNull();
    expect(
      screen.queryByTestId(`action-template-scope-select-${OWN_ROW.id}`),
    ).toBeNull();
  });
});
