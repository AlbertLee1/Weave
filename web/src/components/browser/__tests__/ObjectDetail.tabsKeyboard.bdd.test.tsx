import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createElement } from 'react';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { ObjectDetail } from '../ObjectDetail';
import { AuthContext, type AuthContextValue } from '../../../auth/AuthContext';
import type { ObjectType, WireObject } from '../../../api/types';

// BDD: WAI-ARIA keyboard navigation for the ObjectDetail detail-tabs tablist.
// The tablist (Properties / Relationships / Activity / Diff / Comments /
// TimeSeries) already has role="tablist" + role="tab" + aria-selected but
// shipped with onClick only. This scenario locks the keyboard contract
// (Arrow keys + Home/End + roving tabindex with automatic activation) so it
// matches the AggregationPage/MetricsPage tabs and is not regressed.

// ObjectDiffPanel pulls react-diff-viewer-continued (heavy worker bundle);
// stub it so the Diff tab can mount in jsdom.
vi.mock('react-diff-viewer-continued', () => ({
  __esModule: true,
  default: () => null,
  DiffMethod: { LINES: 'diffLines' },
}));

// Markdown editor stub mirrors the existing ObjectDetail suite.
vi.mock('@uiw/react-md-editor', () => {
  const Editor = () => null;
  (Editor as unknown as { Markdown: () => null }).Markdown = () => null;
  return { default: Editor };
});

const server = setupServer(
  http.get('/api/v2/ontologies/:ontology/actionTypes', () =>
    HttpResponse.json({ data: [] }),
  ),
  http.get('/api/v2/ontologies/:ontology/properties', () =>
    HttpResponse.json({ data: [] }),
  ),
  http.get(
    '/api/v2/ontologies/:ontology/objectTypes/:objectType/outgoingLinkTypes',
    () => HttpResponse.json({ data: [] }),
  ),
);

beforeEach(() => {
  server.listen({ onUnhandledRequest: 'bypass' });
});

afterEach(() => {
  server.resetHandlers();
  server.close();
  vi.clearAllMocks();
});

const objectType: ObjectType = {
  rid: 'ri.ontology.main.object-type.employee',
  apiName: 'Employee',
  displayName: 'Employee',
  primaryKey: 'id',
  status: 'ACTIVE',
  visibility: 'NORMAL',
  properties: {
    id: { dataType: { type: 'string' }, rid: 'ri.p.id' },
    name: { dataType: { type: 'string' }, rid: 'ri.p.name' },
  },
};

const wireObject: WireObject = {
  __rid: 'ri.o.1',
  __apiName: 'Employee',
  __primaryKey: 'emp1',
  id: 'emp1',
  name: 'Alice',
};

// Minimal AuthContext value — the Comments tab calls useAuth(), which throws
// outside an AuthProvider. We only need a stable shape here, not a live /me
// fetch, so the click-to-activate-Comments scenario can mount cleanly.
const authValue: AuthContextValue = {
  user: { id: 'user:test', email: 't@e', name: 'Tester', roles: [], ontologyRoles: {}, permissions: [] },
  loading: false,
  error: null,
  can: () => true,
  canOnOntology: () => true,
  refresh: async () => {},
};

function makeWrapper() {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
  return ({ children }: { children: React.ReactNode }) =>
    createElement(
      MemoryRouter,
      null,
      createElement(
        QueryClientProvider,
        { client },
        createElement(AuthContext.Provider, { value: authValue }, children),
      ),
    );
}

function renderDetail() {
  return render(
    <ObjectDetail
      object={wireObject}
      objectType={objectType}
      open
      onClose={vi.fn()}
      ontologyApiName="main"
    />,
    { wrapper: makeWrapper() },
  );
}

/** Detail tabs in DOM order. */
async function getTabs() {
  const tablist = await screen.findByRole('tablist', {
    name: 'Object detail tabs',
  });
  return {
    tablist,
    properties: screen.getByTestId(
      'object-detail-tab-properties',
    ) as HTMLButtonElement,
    relationships: screen.getByTestId(
      'object-detail-tab-relationships',
    ) as HTMLButtonElement,
    activity: screen.getByTestId(
      'object-detail-tab-activity',
    ) as HTMLButtonElement,
    diff: screen.getByTestId('object-detail-tab-diff') as HTMLButtonElement,
    comments: screen.getByTestId(
      'object-detail-tab-comments',
    ) as HTMLButtonElement,
    timeseries: screen.getByTestId(
      'object-detail-tab-timeseries',
    ) as HTMLButtonElement,
  };
}

describe('BDD: ObjectDetail tabs keyboard navigation', () => {
  it('Given the selected tab is focused, When ArrowRight is pressed, Then focus and selection move to the next tab', async () => {
    const user = userEvent.setup();
    renderDetail();
    const { properties, relationships } = await getTabs();

    // Properties is selected by default (roving tabindex 0); others are -1.
    expect(properties).toHaveAttribute('aria-selected', 'true');
    expect(properties).toHaveAttribute('tabindex', '0');
    expect(relationships).toHaveAttribute('tabindex', '-1');

    properties.focus();
    expect(properties).toHaveFocus();

    await user.keyboard('{ArrowRight}');

    expect(relationships).toHaveFocus();
    expect(relationships).toHaveAttribute('aria-selected', 'true');
    expect(relationships).toHaveAttribute('tabindex', '0');
    expect(properties).toHaveAttribute('aria-selected', 'false');
    expect(properties).toHaveAttribute('tabindex', '-1');
    // The relationships panel renders when its tab activates.
    expect(
      screen.getByTestId('object-detail-relationships'),
    ).toBeInTheDocument();
  });

  it('Given the first tab is focused, When ArrowLeft is pressed, Then it wraps to the last tab', async () => {
    const user = userEvent.setup();
    renderDetail();
    const { properties, timeseries } = await getTabs();

    properties.focus();
    await user.keyboard('{ArrowLeft}');

    expect(timeseries).toHaveFocus();
    expect(timeseries).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByTestId('object-detail-timeseries')).toBeInTheDocument();
  });

  it('Given the last tab is focused, When ArrowRight is pressed, Then it wraps to the first tab', async () => {
    const user = userEvent.setup();
    renderDetail();
    const { properties, timeseries } = await getTabs();

    properties.focus();
    await user.keyboard('{ArrowLeft}'); // wrap to TimeSeries (last)
    expect(timeseries).toHaveFocus();

    await user.keyboard('{ArrowRight}');
    expect(properties).toHaveFocus();
    expect(properties).toHaveAttribute('aria-selected', 'true');
  });

  it('ArrowDown/ArrowUp mirror ArrowRight/ArrowLeft', async () => {
    const user = userEvent.setup();
    renderDetail();
    const { properties, relationships } = await getTabs();

    properties.focus();
    await user.keyboard('{ArrowDown}');
    expect(relationships).toHaveFocus();
    expect(relationships).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{ArrowUp}');
    expect(properties).toHaveFocus();
    expect(properties).toHaveAttribute('aria-selected', 'true');
  });

  it('Home jumps to the first tab and End jumps to the last tab', async () => {
    const user = userEvent.setup();
    renderDetail();
    const { properties, relationships, timeseries } = await getTabs();

    properties.focus();
    await user.keyboard('{ArrowRight}');
    expect(relationships).toHaveFocus();

    await user.keyboard('{End}');
    expect(timeseries).toHaveFocus();
    expect(timeseries).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{Home}');
    expect(properties).toHaveFocus();
    expect(properties).toHaveAttribute('aria-selected', 'true');
  });

  it('mouse click still selects a tab (existing behavior preserved)', async () => {
    const user = userEvent.setup();
    renderDetail();
    const { comments } = await getTabs();

    await user.click(comments);

    expect(comments).toHaveAttribute('aria-selected', 'true');
    expect(comments).toHaveAttribute('tabindex', '0');
    expect(screen.getByTestId('object-detail-comments')).toBeInTheDocument();
  });

  it('an unhandled key (Tab) does not preventDefault or change selection', async () => {
    const user = userEvent.setup();
    renderDetail();
    const { properties, relationships } = await getTabs();

    properties.focus();
    // Tab is a browser-native key; the handler must ignore it so native
    // focus movement still works. Selection should be unchanged.
    await user.keyboard('{Tab}');
    expect(properties).toHaveAttribute('aria-selected', 'true');
    expect(relationships).toHaveAttribute('aria-selected', 'false');
  });
});
