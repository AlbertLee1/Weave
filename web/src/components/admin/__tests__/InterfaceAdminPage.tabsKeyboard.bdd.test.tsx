import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { InterfaceAdminPage } from '../InterfaceAdminPage';

// BDD (a11y / WAI-ARIA tabs keyboard contract):
// The Interface edit modal renders a role="tablist" with 2 role="tab"
// buttons (Definition / Methods). Per the WAI-ARIA Authoring Practices
// "Tabs" pattern with automatic activation, a keyboard user focused on the
// active tab must be able to:
//   - ArrowRight / ArrowLeft (+ ArrowDown / ArrowUp mirrors) → move to the
//     next / previous tab (wrapping), activating it and moving DOM focus.
//   - Home / End → jump to the first / last tab.
//   - roving tabindex: only the selected tab is tabIndex 0; the rest are -1.
// This file is the executable contract for that keyboard behavior. It must
// not disturb mouse clicks or the existing tab-switching state.

interface MockInterface {
  rid: string;
  apiName: string;
  displayName: string;
  extendsRid?: string;
  sharedProperties: Array<{ apiName: string; baseType: string; isArray: boolean }>;
  outgoingLinkTypes: Array<{
    apiName: string;
    displayName: string;
    linkedEntityTypeApiName: string;
    cardinality: 'ONE' | 'MANY';
    required?: boolean;
  }>;
}

const OBJECT_TYPES = [
  {
    rid: 'ri.ontology.main.object-type.emp',
    apiName: 'Employee',
    displayName: 'Employee',
    primaryKey: 'employeeId',
    status: 'ACTIVE',
    visibility: 'PROMINENT',
  },
];

const INTERFACE: MockInterface = {
  rid: 'ri.ontology.main.interface.addr',
  apiName: 'Addressable',
  displayName: 'Addressable',
  sharedProperties: [{ apiName: 'address', baseType: 'string', isArray: false }],
  outgoingLinkTypes: [],
};

interface StubState {
  interfaces: MockInterface[];
}

function makeStub(): StubState {
  return {
    interfaces: [
      {
        ...INTERFACE,
        sharedProperties: INTERFACE.sharedProperties.map((sp) => ({ ...sp })),
        outgoingLinkTypes: INTERFACE.outgoingLinkTypes.map((lt) => ({ ...lt })),
      },
    ],
  };
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function installFetch(state: StubState) {
  vi.stubGlobal(
    'fetch',
    vi.fn(
      async (
        input: RequestInfo | URL,
        init?: RequestInit,
      ): Promise<Response> => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = (init?.method ?? 'GET').toUpperCase();

        if (
          method === 'GET' &&
          url.endsWith('/api/v2/ontologies/northwind/objectTypes')
        ) {
          return jsonResponse({ data: OBJECT_TYPES });
        }
        if (
          method === 'GET' &&
          url.endsWith('/api/v2/ontologies/northwind/interfacesAdmin')
        ) {
          return jsonResponse({ data: state.interfaces });
        }

        // Implementing-object-types modal queries — return empty.
        const listAttachMatch = url.match(
          /\/api\/v2\/ontologies\/northwind\/objectTypes\/byRid\/([^/]+)\/interfaces$/,
        );
        if (listAttachMatch && method === 'GET') {
          return jsonResponse({ data: [] });
        }

        // Interface Methods list — return empty so the Methods panel renders.
        const methodsMatch = url.match(
          /\/api\/v2\/ontologies\/northwind\/interfaces\/([^/]+)\/methods$/,
        );
        if (methodsMatch && method === 'GET') {
          return jsonResponse({ data: [] });
        }

        return new Response('{}', { status: 200 });
      },
    ),
  );
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
      <MemoryRouter initialEntries={['/admin/northwind/interfaces']}>
        <Routes>
          <Route
            path="/admin/:ontology/interfaces"
            element={<InterfaceAdminPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

// data-testid, in tablist (DOM) order.
const TABS = [
  'interface-edit-tab-definition',
  'interface-edit-tab-methods',
] as const;

function tabEl(testid: string): HTMLButtonElement {
  return screen.getByTestId(testid) as HTMLButtonElement;
}

async function openEditModal() {
  renderPage();
  await waitFor(() => {
    expect(screen.getAllByText('Addressable').length).toBeGreaterThan(0);
  });
  const user = userEvent.setup();
  const row = screen.getByTestId('interface-row');
  await user.click(within(row).getByRole('button', { name: 'Edit' }));
  await screen.findByTestId('interface-edit-tab-definition');
  return user;
}

function expectSelected(testid: string) {
  for (const t of TABS) {
    const el = tabEl(t);
    if (t === testid) {
      expect(el.getAttribute('aria-selected')).toBe('true');
      expect(el.tabIndex).toBe(0);
      expect(el).toHaveFocus();
    } else {
      expect(el.getAttribute('aria-selected')).toBe('false');
      expect(el.tabIndex).toBe(-1);
    }
  }
}

describe('InterfaceAdminPage — edit tabs keyboard navigation (a11y)', () => {
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
    expect(tabEl('interface-edit-tab-definition').tabIndex).toBe(0);
    expect(tabEl('interface-edit-tab-methods').tabIndex).toBe(-1);
  });

  it('ArrowRight moves focus + selection to the next tab', async () => {
    const user = await openEditModal();
    tabEl('interface-edit-tab-definition').focus();

    await user.keyboard('{ArrowRight}');
    expectSelected('interface-edit-tab-methods');
  });

  it('ArrowLeft moves focus + selection to the previous tab', async () => {
    const user = await openEditModal();
    // Move to the second tab first.
    tabEl('interface-edit-tab-definition').focus();
    await user.keyboard('{ArrowRight}');
    expectSelected('interface-edit-tab-methods');

    await user.keyboard('{ArrowLeft}');
    expectSelected('interface-edit-tab-definition');
  });

  it('ArrowDown / ArrowUp mirror ArrowRight / ArrowLeft', async () => {
    const user = await openEditModal();
    tabEl('interface-edit-tab-definition').focus();

    await user.keyboard('{ArrowDown}');
    expectSelected('interface-edit-tab-methods');

    await user.keyboard('{ArrowUp}');
    expectSelected('interface-edit-tab-definition');
  });

  it('ArrowRight wraps from the last tab to the first', async () => {
    const user = await openEditModal();
    tabEl('interface-edit-tab-definition').focus();
    await user.keyboard('{ArrowRight}'); // → Methods (last)
    expectSelected('interface-edit-tab-methods');

    await user.keyboard('{ArrowRight}');
    expectSelected('interface-edit-tab-definition');
  });

  it('ArrowLeft wraps from the first tab to the last', async () => {
    const user = await openEditModal();
    tabEl('interface-edit-tab-definition').focus();

    await user.keyboard('{ArrowLeft}');
    expectSelected('interface-edit-tab-methods');
  });

  it('Home jumps to the first tab, End jumps to the last', async () => {
    const user = await openEditModal();
    tabEl('interface-edit-tab-definition').focus();

    await user.keyboard('{End}');
    expectSelected('interface-edit-tab-methods');

    await user.keyboard('{Home}');
    expectSelected('interface-edit-tab-definition');
  });

  it('keyboard activation switches the rendered tab panel', async () => {
    const user = await openEditModal();
    // Definition panel renders the edit form.
    expect(screen.getByTestId('interface-edit-form')).toBeTruthy();

    tabEl('interface-edit-tab-definition').focus();
    await user.keyboard('{End}'); // → Methods
    expectSelected('interface-edit-tab-methods');
    // The Definition form is no longer rendered once Methods is active.
    expect(screen.queryByTestId('interface-edit-form')).toBeNull();
    // The Methods editor panel is rendered instead.
    expect(screen.getByTestId('interface-methods-editor')).toBeTruthy();
  });

  it('still switches tabs on mouse click', async () => {
    const user = await openEditModal();
    await user.click(tabEl('interface-edit-tab-methods'));
    expect(
      tabEl('interface-edit-tab-methods').getAttribute('aria-selected'),
    ).toBe('true');
    expect(
      tabEl('interface-edit-tab-definition').getAttribute('aria-selected'),
    ).toBe('false');
  });
});
