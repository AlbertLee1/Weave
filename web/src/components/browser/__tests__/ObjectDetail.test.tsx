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
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { ObjectDetail } from '../ObjectDetail';
import type { ActionType, ObjectType, WireObject } from '../../../api/types';

// React-diff-viewer-continued is a heavy import pulled by ObjectDiffPanel via
// ObjectDetail; stub it so jsdom can render without the worker bundle.
vi.mock('react-diff-viewer-continued', () => ({
  __esModule: true,
  default: () => null,
  DiffMethod: { LINES: 'diffLines' },
}));

// Mock the markdown editor too (same pattern as US-313 / US-314 tests).
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
    createElement(QueryClientProvider, { client }, children);
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

const modifyAction: ActionType = {
  rid: 'ri.at.modifyEmployee',
  apiName: 'modifyEmployee',
  displayName: 'Modify Employee',
  status: 'ACTIVE',
  parameters: {
    primaryKey: { dataType: { type: 'string' }, required: true },
    name: { dataType: { type: 'string' }, required: false },
  },
  rules: [
    {
      type: 'modifyObject',
      objectType: 'Employee',
      propertyBindings: { name: { type: 'parameter', value: 'name' } },
    },
  ],
};

function mountActionTypes(actions: ActionType[]) {
  server.use(
    http.get('/api/v2/ontologies/:ontology/actionTypes', () =>
      HttpResponse.json({ data: actions }),
    ),
    // useProperties + outgoing link types fan-out — return empty payloads so
    // their queries resolve cleanly.
    http.get('/api/v2/ontologies/:ontology/properties', () =>
      HttpResponse.json({ data: [] }),
    ),
    http.get(
      '/api/v2/ontologies/:ontology/objectTypes/:objectType/outgoingLinkTypes',
      () => HttpResponse.json({ data: [] }),
    ),
  );
}

describe('ObjectDetail inline editing (US-315)', () => {
  it('renders an InlineEditField for string properties when a modifyObject action exists', async () => {
    mountActionTypes([modifyAction]);
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

    await waitFor(() =>
      expect(screen.getByTestId('inline-edit-name-display')).toBeInTheDocument(),
    );
    expect(screen.getByTestId('inline-edit-name-display')).toHaveTextContent(
      'Alice',
    );
  });

  it('falls back to plain span when no modifyObject action is registered', async () => {
    mountActionTypes([]);
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

    // Detail panel renders synchronously; the action types are still loading,
    // so the fallback is the read-only span.
    await waitFor(() => {
      expect(
        screen.queryByTestId('inline-edit-name-display'),
      ).not.toBeInTheDocument();
    });
  });

  it('applies the action with primaryKey + new value on Enter', async () => {
    mountActionTypes([modifyAction]);
    let captured: unknown = null;
    server.use(
      http.post(
        '/api/v2/ontologies/:ontology/actions/:action/apply',
        async ({ request, params }) => {
          captured = {
            action: params.action,
            body: await request.json(),
          };
          return HttpResponse.json({});
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

    await waitFor(() =>
      expect(screen.getByTestId('inline-edit-name-display')).toBeInTheDocument(),
    );

    fireEvent.click(screen.getByTestId('inline-edit-name-display'));
    const input = screen.getByTestId(
      'inline-edit-name-input',
    ) as HTMLInputElement;
    fireEvent.change(input, { target: { value: 'Bob' } });
    await act(async () => {
      fireEvent.keyDown(input, { key: 'Enter' });
      await new Promise((r) => setTimeout(r, 0));
    });

    await waitFor(() => {
      expect(captured).not.toBeNull();
    });
    const c = captured as { action: string; body: { parameters: Record<string, unknown> } };
    expect(c.action).toBe('modifyEmployee');
    expect(c.body.parameters).toMatchObject({ primaryKey: 'emp1', name: 'Bob' });
  });

  it('rolls back optimistic value when the apply call rejects', async () => {
    mountActionTypes([modifyAction]);
    server.use(
      http.post(
        '/api/v2/ontologies/:ontology/actions/:action/apply',
        () =>
          new HttpResponse(
            JSON.stringify({
              errorName: 'BadRequest',
              errorCode: 'WEAVE_BAD_REQUEST',
              message: 'rejected by server',
            }),
            { status: 400, headers: { 'content-type': 'application/json' } },
          ),
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

    await waitFor(() =>
      expect(screen.getByTestId('inline-edit-name-display')).toBeInTheDocument(),
    );

    fireEvent.click(screen.getByTestId('inline-edit-name-display'));
    const input = screen.getByTestId(
      'inline-edit-name-input',
    ) as HTMLInputElement;
    fireEvent.change(input, { target: { value: 'Bob' } });
    await act(async () => {
      fireEvent.keyDown(input, { key: 'Enter' });
      await new Promise((r) => setTimeout(r, 0));
    });

    await waitFor(() => {
      expect(screen.getByTestId('inline-edit-name-display')).toHaveTextContent(
        'Alice',
      );
    });
    expect(screen.getByTestId('inline-edit-name-error')).toBeInTheDocument();
  });
});
