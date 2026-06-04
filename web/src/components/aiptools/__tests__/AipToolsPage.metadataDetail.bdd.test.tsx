import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AipToolsPage } from '../AipToolsPage';
import type { ToolRecord } from '../../../api/aipTools';

// US-285 follow-up: the catalog list already carries the audit metadata
// (createdBy/createdAt/updatedAt) and the read-only GET /tools/{name} endpoint
// exists, but the page dropped both. These BDD scenarios pin (1) the muted
// metadata line on each row and (2) a read-only "View" modal that fetches the
// full record via getAipTool and renders the parameters JSON.

const echoTool: ToolRecord = {
  name: 'echo_tool',
  description: 'Echoes its input back',
  parameters: {
    type: 'object',
    properties: { text: { type: 'string' } },
    required: ['text'],
  },
  handlerFunctionRid: 'ri.function.main.function.echo',
  enabled: true,
  createdBy: 'user:alice',
  createdAt: '2026-06-01T12:00:00Z',
  updatedAt: '2026-06-02T09:30:00Z',
};

// A second tool with no metadata at all, to prove the existence guards: the
// metadata line is omitted entirely rather than rendering "Created by  ·".
const bareTool: ToolRecord = {
  name: 'bare_tool',
  enabled: false,
};

let tools: ToolRecord[] = [];
// Records the name the read-only View fetched, so we can prove getAipTool ran.
let capturedGetName: string | null = null;

const server = setupServer(
  http.get('/api/v2/aip/tools', () => HttpResponse.json({ tools })),
  http.get('/api/v2/aip/tools/:toolName', ({ params }) => {
    capturedGetName = String(params.toolName);
    const found = tools.find((t) => t.name === capturedGetName);
    if (!found) {
      return HttpResponse.json(
        { errorCode: 'NOT_FOUND', errorName: 'AIPToolNotFound' },
        { status: 404 },
      );
    }
    return HttpResponse.json(found);
  }),
);

beforeAll(() => server.listen());
afterEach(() => {
  tools = [];
  capturedGetName = null;
  server.resetHandlers();
});
afterAll(() => server.close());

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <AipToolsPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BDD: AipToolsPage audit metadata line', () => {
  it('Given a tool with audit metadata, When the list loads, Then the row shows createdBy and a created time', async () => {
    tools = [echoTool];
    renderPage();

    const row = await screen.findByTestId('aip-tool-row');
    const meta = within(row).getByTestId('aip-tool-row-meta');
    expect(meta).toHaveTextContent(/user:alice/);
    // The created instant is rendered as a localized string, not the raw ISO.
    expect(meta).toHaveTextContent(
      new Date('2026-06-01T12:00:00Z').toLocaleString(),
    );
    // updatedAt is present and distinct from createdAt, so it shows too.
    expect(meta).toHaveTextContent(/Updated/);
  });

  it('Given a tool with no audit metadata, When the list loads, Then no metadata line is rendered', async () => {
    tools = [bareTool];
    renderPage();

    const row = await screen.findByTestId('aip-tool-row');
    expect(within(row).queryByTestId('aip-tool-row-meta')).toBeNull();
  });
});

describe('BDD: AipToolsPage read-only View detail', () => {
  it('Given a tool, When View is clicked, Then getAipTool runs and a read-only detail modal shows the full record', async () => {
    tools = [echoTool];
    renderPage();

    const row = await screen.findByTestId('aip-tool-row');
    fireEvent.click(within(row).getByTestId('aip-tool-view-btn'));

    // The read-only detail panel renders after the getAipTool fetch resolves.
    const detail = await screen.findByTestId('aip-tool-detail');
    await waitFor(() => expect(capturedGetName).toBe('echo_tool'));

    expect(within(detail).getByText('echo_tool')).toBeInTheDocument();
    expect(
      within(detail).getByText('Echoes its input back'),
    ).toBeInTheDocument();
    expect(
      within(detail).getByText('ri.function.main.function.echo'),
    ).toBeInTheDocument();
    // createdBy surfaces inside the audit metadata line.
    expect(within(detail).getByTestId('aip-tool-row-meta')).toHaveTextContent(
      /Created by user:alice/,
    );

    // Parameters are pretty-printed JSON, so the schema text is visible.
    const params = within(detail).getByTestId('aip-tool-detail-parameters');
    expect(params).toHaveTextContent('"type": "object"');
    expect(params).toHaveTextContent('"required"');

    // It is read-only: no editable inputs / submit button live in the detail.
    expect(within(detail).queryByTestId('aip-tool-submit-btn')).toBeNull();
  });
});
