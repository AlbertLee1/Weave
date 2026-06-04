import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ObjectTypeAdminPage } from '../ObjectTypeAdminPage';

// BDD (a11y / WAI-ARIA tabs keyboard contract):
// The ObjectType edit modal renders a role="tablist" with 5 role="tab"
// buttons (Details / Properties / Bindings / Resolved / History). Per the
// WAI-ARIA Authoring Practices "Tabs" pattern with automatic activation, a
// keyboard user focused on the active tab must be able to:
//   - ArrowRight / ArrowLeft → move to the next / previous tab (wrapping),
//     activating it and moving DOM focus to it.
//   - Home / End → jump to the first / last tab.
//   - roving tabindex: only the selected tab is tabIndex 0; the rest are -1.
// This file is the executable contract for that keyboard behavior. It must
// not disturb mouse clicks or the existing tab-switching state.

const OBJECT_TYPES = [
  {
    rid: 'ri.ontology.main.object-type.flight-delay',
    apiName: 'FlightDelay',
    displayName: 'Flight Delay',
    pluralDisplayName: 'Flight Delays',
    primaryKey: 'id',
    status: 'ACTIVE',
    visibility: 'PROMINENT',
    properties: {
      id: { dataType: { type: 'string' }, rid: 'ri.prop.id' },
    },
  },
];

interface StubState {
  objectTypes: typeof OBJECT_TYPES;
}

function makeStub(): StubState {
  return { objectTypes: OBJECT_TYPES.map((ot) => ({ ...ot })) };
}

function installFetch(state: StubState) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL): Promise<Response> => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.endsWith('/api/v2/ontologies/northwind/objectTypes')) {
        return jsonResponse({ data: state.objectTypes });
      }
      if (url.endsWith('/outgoingLinkTypes')) {
        return jsonResponse({ data: [] });
      }
      if (url.endsWith('/api/v2/ontologies/northwind/actionTypes')) {
        return jsonResponse({ data: [] });
      }
      return new Response('{}', { status: 200 });
    }),
  );
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
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
      <MemoryRouter initialEntries={['/admin/northwind/objectTypes']}>
        <Routes>
          <Route
            path="/admin/:ontology/objectTypes"
            element={<ObjectTypeAdminPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

// data-testid → human label, in tablist (DOM) order.
const TABS = [
  { testid: 'object-type-edit-tab-details', label: 'Details' },
  { testid: 'object-type-edit-tab-properties', label: 'Properties' },
  { testid: 'object-type-edit-tab-bindings', label: 'Bindings' },
  { testid: 'object-type-edit-tab-resolved', label: 'Resolved' },
  { testid: 'object-type-edit-tab-history', label: 'History' },
] as const;

function tabEl(testid: string): HTMLButtonElement {
  return screen.getByTestId(testid) as HTMLButtonElement;
}

async function openEditModal() {
  renderPage();
  await waitFor(() => {
    expect(screen.getAllByText('Flight Delay').length).toBeGreaterThan(0);
  });
  const user = userEvent.setup();
  await user.click(screen.getByRole('button', { name: /^Edit$/i }));
  await screen.findByTestId('object-type-edit-tab-details');
  return user;
}

function expectSelected(testid: string) {
  for (const t of TABS) {
    const el = tabEl(t.testid);
    if (t.testid === testid) {
      expect(el.getAttribute('aria-selected')).toBe('true');
      expect(el.tabIndex).toBe(0);
      expect(el).toHaveFocus();
    } else {
      expect(el.getAttribute('aria-selected')).toBe('false');
      expect(el.tabIndex).toBe(-1);
    }
  }
}

describe('ObjectTypeAdminPage — edit tabs keyboard navigation (a11y)', () => {
  let state: StubState;

  beforeEach(() => {
    state = makeStub();
    installFetch(state);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('exposes a roving tabindex: the selected tab is 0, others -1', async () => {
    await openEditModal();
    expect(tabEl('object-type-edit-tab-details').tabIndex).toBe(0);
    expect(tabEl('object-type-edit-tab-properties').tabIndex).toBe(-1);
    expect(tabEl('object-type-edit-tab-bindings').tabIndex).toBe(-1);
    expect(tabEl('object-type-edit-tab-resolved').tabIndex).toBe(-1);
    expect(tabEl('object-type-edit-tab-history').tabIndex).toBe(-1);
  });

  it('ArrowRight moves focus + selection to the next tab', async () => {
    const user = await openEditModal();
    tabEl('object-type-edit-tab-details').focus();

    await user.keyboard('{ArrowRight}');
    expectSelected('object-type-edit-tab-properties');

    await user.keyboard('{ArrowRight}');
    expectSelected('object-type-edit-tab-bindings');
  });

  it('ArrowLeft moves focus + selection to the previous tab', async () => {
    const user = await openEditModal();
    // Move to the third tab first.
    tabEl('object-type-edit-tab-details').focus();
    await user.keyboard('{ArrowRight}{ArrowRight}');
    expectSelected('object-type-edit-tab-bindings');

    await user.keyboard('{ArrowLeft}');
    expectSelected('object-type-edit-tab-properties');
  });

  it('ArrowRight wraps from the last tab to the first', async () => {
    const user = await openEditModal();
    tabEl('object-type-edit-tab-details').focus();
    // Walk to the last (History) tab.
    await user.keyboard('{ArrowRight}{ArrowRight}{ArrowRight}{ArrowRight}');
    expectSelected('object-type-edit-tab-history');

    await user.keyboard('{ArrowRight}');
    expectSelected('object-type-edit-tab-details');
  });

  it('ArrowLeft wraps from the first tab to the last', async () => {
    const user = await openEditModal();
    tabEl('object-type-edit-tab-details').focus();

    await user.keyboard('{ArrowLeft}');
    expectSelected('object-type-edit-tab-history');
  });

  it('Home jumps to the first tab, End jumps to the last', async () => {
    const user = await openEditModal();
    tabEl('object-type-edit-tab-details').focus();
    await user.keyboard('{ArrowRight}{ArrowRight}');
    expectSelected('object-type-edit-tab-bindings');

    await user.keyboard('{End}');
    expectSelected('object-type-edit-tab-history');

    await user.keyboard('{Home}');
    expectSelected('object-type-edit-tab-details');
  });

  it('keyboard activation switches the rendered tab panel', async () => {
    const user = await openEditModal();
    // Details panel renders the edit form.
    expect(screen.getByTestId('object-type-edit-form')).toBeTruthy();

    tabEl('object-type-edit-tab-details').focus();
    await user.keyboard('{End}'); // → History
    expectSelected('object-type-edit-tab-history');
    // The Details form is no longer rendered once a non-details tab is active.
    expect(screen.queryByTestId('object-type-edit-form')).toBeNull();
  });

  it('still switches tabs on mouse click', async () => {
    const user = await openEditModal();
    await user.click(tabEl('object-type-edit-tab-properties'));
    expect(
      tabEl('object-type-edit-tab-properties').getAttribute('aria-selected'),
    ).toBe('true');
    expect(
      tabEl('object-type-edit-tab-details').getAttribute('aria-selected'),
    ).toBe('false');
  });
});
