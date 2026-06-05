import {
  describe,
  it,
  expect,
  beforeAll,
  afterAll,
  afterEach,
  vi,
} from 'vitest';
import { createElement } from 'react';
import { render, screen, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { ActionImpactPage } from '../ActionImpactPage';
import { ActionHistoryPage } from '../ActionHistoryPage';
import * as ontologiesApi from '../../../api/ontologies';

// Backend contract: GET /api/v2/actions/{rid}/impact ->
//   { actionRid, actionLog?, objects: ImpactObject[], truncated }
// where {rid} = ri.actions.main.action-log.<id>. These scenarios drive the
// new Action Impact view through MSW so the full request -> render path is
// exercised (not just the component in isolation).

const ACTION_LOG_ID = 42;
const IMPACT_RID = `ri.actions.main.action-log.${ACTION_LOG_ID}`;

type JsonValue =
  | null
  | boolean
  | number
  | string
  | JsonValue[]
  | { [key: string]: JsonValue };

type ImpactBody = {
  status?: number;
  body?: JsonValue;
};

// Mutable handler config so each scenario can dictate the impact response.
let impactResponse: ImpactBody = { body: null };

const server = setupServer(
  http.get('/api/v2/actions/:rid/impact', ({ params }) => {
    // Only respond for the RID the page derives from the route param; this
    // guards against the page accidentally addressing a different resource.
    if (params.rid !== IMPACT_RID) {
      return HttpResponse.json({ errorName: 'NotFound' }, { status: 404 });
    }
    if (impactResponse.status && impactResponse.status >= 400) {
      return HttpResponse.json(
        impactResponse.body ?? { errorName: 'Internal' },
        { status: impactResponse.status },
      );
    }
    return HttpResponse.json(impactResponse.body);
  }),
);

beforeAll(() => server.listen({ onUnhandledRequest: 'bypass' }));
afterEach(() => {
  server.resetHandlers();
  vi.restoreAllMocks();
  impactResponse = { body: null };
});
afterAll(() => server.close());

function renderImpactPage() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(
    createElement(
      QueryClientProvider,
      { client: qc },
      createElement(
        MemoryRouter,
        { initialEntries: [`/actions/northwind/impact/${ACTION_LOG_ID}`] },
        createElement(
          Routes,
          null,
          createElement(Route, {
            path: '/actions/:ontology/impact/:actionLogId',
            element: createElement(ActionImpactPage, null),
          }),
        ),
      ),
    ),
  );
}

describe('BDD: ActionImpactPage', () => {
  it('Given an action affected objects, Then the impact table lists each with an operation badge', async () => {
    impactResponse = {
      body: {
        actionRid: IMPACT_RID,
        truncated: false,
        objects: [
          {
            rid: 'ri.objects.main.object.Customer.ALFKI',
            objectType: 'Customer',
            primaryKey: 'ALFKI',
            operation: 'MODIFY',
            timestamp: '2026-06-01T10:00:00Z',
          },
          {
            rid: 'ri.objects.main.object.Order.10248',
            objectType: 'Order',
            primaryKey: '10248',
            operation: 'CREATE',
            timestamp: '2026-06-01T10:00:01Z',
          },
        ],
      },
    };

    renderImpactPage();

    const table = await screen.findByTestId('action-impact-table');
    const rows = within(table).getAllByTestId('action-impact-row');
    expect(rows).toHaveLength(2);

    // Each object's identity + operation badge is visible.
    expect(
      within(table).getByText('ri.objects.main.object.Customer.ALFKI'),
    ).toBeInTheDocument();
    expect(within(table).getByText('Customer')).toBeInTheDocument();
    expect(within(table).getByText('ALFKI')).toBeInTheDocument();

    const modifyBadge = within(table).getByTestId('impact-operation-MODIFY');
    const createBadge = within(table).getByTestId('impact-operation-CREATE');
    expect(modifyBadge).toHaveTextContent('MODIFY');
    expect(createBadge).toHaveTextContent('CREATE');
  });

  it('Given the action affected no objects, Then an empty state is shown', async () => {
    impactResponse = {
      body: { actionRid: IMPACT_RID, truncated: false, objects: [] },
    };

    renderImpactPage();

    expect(await screen.findByTestId('action-impact-empty')).toHaveTextContent(
      /did not affect any objects/i,
    );
    expect(screen.queryByTestId('action-impact-table')).toBeNull();
  });

  it('Given the impact list is truncated, Then a warning banner is shown', async () => {
    impactResponse = {
      body: {
        actionRid: IMPACT_RID,
        truncated: true,
        objects: [
          {
            rid: 'ri.objects.main.object.Order.10248',
            objectType: 'Order',
            primaryKey: '10248',
            operation: 'DELETE',
            timestamp: '2026-06-01T10:00:01Z',
          },
        ],
      },
    };

    renderImpactPage();

    const banner = await screen.findByTestId('action-impact-truncated');
    expect(banner).toHaveTextContent(/truncated/i);
    // The table still renders the partial set alongside the warning.
    expect(screen.getByTestId('action-impact-table')).toBeInTheDocument();
    expect(screen.getByTestId('impact-operation-DELETE')).toHaveTextContent(
      'DELETE',
    );
  });

  it('Given the impact request fails, Then an error state is shown', async () => {
    impactResponse = { status: 500, body: { errorName: 'Internal' } };

    renderImpactPage();

    expect(await screen.findByTestId('action-impact-error')).toBeInTheDocument();
  });

  it('Then the page renders an h1 and a link back to action history', async () => {
    impactResponse = {
      body: { actionRid: IMPACT_RID, truncated: false, objects: [] },
    };

    renderImpactPage();

    // Heading present.
    const heading = await screen.findByRole('heading', {
      level: 1,
      name: /action impact/i,
    });
    expect(heading).toBeInTheDocument();

    // Back link routes to the action history page for this ontology.
    const back = screen.getByTestId('action-impact-back-link');
    expect(back).toHaveAttribute('href', '/actions/northwind/history');
  });
});

describe('BDD: ActionHistoryPage exposes a View impact link per row', () => {
  it('Given history rows, Then each row links to its impact view by id', async () => {
    vi.spyOn(ontologiesApi, 'listActionTypes').mockResolvedValue([]);
    // Register the history handler on the shared MSW server (reset in
    // afterEach) rather than spinning up a second interceptor.
    server.use(
      http.get('/api/v2/ontologies/:ontology/actions/history', () =>
        HttpResponse.json({
          data: [
            {
              id: 7,
              actionTypeRid: 'ri.action.main.action-type.update',
              userId: 'user:alice',
              status: 'SUCCESS',
              createdAt: '2026-06-01T10:00:00Z',
            },
          ],
        }),
      ),
    );

    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: 0 } },
    });
    render(
      createElement(
        QueryClientProvider,
        { client: qc },
        createElement(
          MemoryRouter,
          { initialEntries: ['/actions/northwind/history'] },
          createElement(
            Routes,
            null,
            createElement(Route, {
              path: '/actions/:ontology/history',
              element: createElement(ActionHistoryPage, null),
            }),
          ),
        ),
      ),
    );

    const link = await screen.findByTestId('view-impact-link');
    expect(link).toHaveAttribute('href', '/actions/northwind/impact/7');
  });
});
