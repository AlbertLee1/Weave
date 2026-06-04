import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createElement } from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { ObjectDetail } from '../ObjectDetail';
import type { LinkType, ObjectType, WireObject } from '../../../api/types';

// React-diff-viewer-continued is a heavy import pulled by ObjectDiffPanel via
// ObjectDetail; stub it so jsdom can render without the worker bundle.
vi.mock('react-diff-viewer-continued', () => ({
  __esModule: true,
  default: () => null,
  DiffMethod: { LINES: 'diffLines' },
}));

// Mock the markdown editor too (same pattern as the sibling ObjectDetail test).
vi.mock('@uiw/react-md-editor', () => {
  const Editor = () => null;
  (Editor as unknown as { Markdown: () => null }).Markdown = () => null;
  return { default: Editor };
});

const server = setupServer();

beforeEach(() => {
  server.listen({ onUnhandledRequest: 'bypass' });
});

afterEach(() => {
  server.resetHandlers();
  server.close();
  vi.clearAllMocks();
});

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
      createElement(QueryClientProvider, { client }, children),
    );
}

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

const ordersLink: LinkType = {
  rid: 'ri.link.employee-orders',
  apiName: 'orders',
  displayName: 'Orders',
  objectTypeApiName: 'Employee',
  linkedObjectTypeApiName: 'Order',
  cardinality: 'ONE_TO_MANY',
  required: false,
};

// Mounts the supporting queries ObjectDetail fans out on mount:
//   - actionTypes (inline-edit discovery)
//   - properties (markdown detection)
//   - outgoingLinkTypes (drives the derived-property defs)
// Returns empty actionTypes/properties so those queries resolve cleanly, and
// the caller-supplied link types so the withProperties request has something
// to traverse.
function mountSupportingQueries(linkTypes: LinkType[]) {
  server.use(
    http.get('/api/v2/ontologies/:ontology/actionTypes', () =>
      HttpResponse.json({ data: [] }),
    ),
    http.get('/api/v2/ontologies/:ontology/properties', () =>
      HttpResponse.json({ data: [] }),
    ),
    http.get(
      '/api/v2/ontologies/:ontology/objectTypes/:objectType/outgoingLinkTypes',
      () => HttpResponse.json({ data: linkTypes }),
    ),
  );
}

describe('BDD: ObjectDetail withProperties derived properties', () => {
  it('Given an object with an outgoing link, When the withProperties objectSet returns a derived value, Then the derived properties section renders that value', async () => {
    mountSupportingQueries([ordersLink]);

    let captured: unknown = null;
    server.use(
      http.post(
        '/api/v2/ontologies/:ontology/objectSets/loadObjects',
        async ({ request }) => {
          captured = await request.json();
          // Backend merges withProperties-derived values straight into the
          // object's properties keyed by derived property name.
          return HttpResponse.json({
            data: [
              {
                __rid: 'ri.o.1',
                __apiName: 'Employee',
                __primaryKey: 'emp1',
                id: 'emp1',
                orders_count: 3,
              },
            ],
            totalCount: '1',
          });
        },
      ),
    );

    render(
      <ObjectDetail
        object={wireObject}
        objectType={objectType}
        open
        onClose={vi.fn()}
        ontologyApiName="main"
      />,
      { wrapper: makeWrapper() },
    );

    // Then — the derived properties section renders with the value.
    await waitFor(() =>
      expect(
        screen.getByTestId('object-detail-derived-properties'),
      ).toBeInTheDocument(),
    );
    const row = await screen.findByTestId('derived-property-orders_count');
    expect(row).toHaveTextContent('3');

    // And the request was a withProperties objectSet scoped to this object,
    // selecting only the primary key, with a count metric per outgoing link.
    const body = captured as {
      objectSet: {
        type: string;
        objectSet: { type: string; primaryKeys?: string[] };
        derivedProperties: { name: string; link: string; metric: string }[];
      };
      select: string[];
    };
    expect(body.objectSet.type).toBe('withProperties');
    expect(body.objectSet.objectSet.primaryKeys).toEqual(['emp1']);
    expect(body.objectSet.derivedProperties).toEqual([
      { name: 'orders_count', link: 'orders', metric: 'count' },
    ]);
    expect(body.select).toContain('id');
  });

  it('Given an object type with no outgoing links, Then no derived properties section renders', async () => {
    mountSupportingQueries([]);

    render(
      <ObjectDetail
        object={wireObject}
        objectType={objectType}
        open
        onClose={vi.fn()}
        ontologyApiName="main"
      />,
      { wrapper: makeWrapper() },
    );

    // The Properties tab content settles; with no outgoing links there is
    // nothing to derive, so the section must stay absent.
    await waitFor(() =>
      expect(
        screen.getByTestId('object-detail-properties'),
      ).toBeInTheDocument(),
    );
    expect(
      screen.queryByTestId('object-detail-derived-properties'),
    ).not.toBeInTheDocument();
  });
});
