import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AipToolsPage } from '../AipToolsPage';
import type { ToolRecord } from '../../../api/aipTools';

// Captured request bodies / params so the BDD assertions can verify the exact
// wire shape the page sends to the mocked backend.
let capturedCreateBody: unknown = null;
let capturedDeleteName: string | null = null;

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
  createdAt: '2026-06-01T00:00:00Z',
  updatedAt: '2026-06-01T00:00:00Z',
};

let tools: ToolRecord[] = [];

const server = setupServer(
  http.get('/api/v2/aip/tools', () => HttpResponse.json({ tools })),
  http.post('/api/v2/aip/tools', async ({ request }) => {
    capturedCreateBody = await request.json();
    const body = capturedCreateBody as ToolRecord;
    const created: ToolRecord = {
      name: body.name,
      description: body.description,
      parameters: body.parameters,
      handlerFunctionRid: body.handlerFunctionRid,
      enabled: body.enabled ?? true,
      createdAt: '2026-06-02T00:00:00Z',
      updatedAt: '2026-06-02T00:00:00Z',
    };
    tools = [...tools, created];
    return HttpResponse.json(created, { status: 201 });
  }),
  http.delete('/api/v2/aip/tools/:toolName', ({ params }) => {
    capturedDeleteName = String(params.toolName);
    tools = tools.filter((t) => t.name !== capturedDeleteName);
    return new HttpResponse(null, { status: 204 });
  }),
);

beforeAll(() => server.listen());
afterEach(() => {
  capturedCreateBody = null;
  capturedDeleteName = null;
  tools = [];
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

describe('BDD: AipToolsPage tool catalog (US-285)', () => {
  it('Given the catalog has a tool, When the page loads, Then the tool is rendered', async () => {
    tools = [echoTool];
    renderPage();

    const row = await screen.findByTestId('aip-tool-row');
    expect(within(row).getByText('echo_tool')).toBeInTheDocument();
    expect(within(row).getByText('Echoes its input back')).toBeInTheDocument();
    expect(
      within(row).getByText('ri.function.main.function.echo'),
    ).toBeInTheDocument();
    // enabled badge
    expect(within(row).getByTestId('aip-tool-enabled-badge')).toHaveTextContent(
      /enabled/i,
    );
  });

  it('Given the create form is filled, When submitted, Then the right body is POSTed', async () => {
    renderPage();

    fireEvent.click(await screen.findByTestId('aip-tool-create-btn'));

    const modal = await screen.findByTestId('aip-tool-modal');
    fireEvent.change(within(modal).getByTestId('aip-tool-name-input'), {
      target: { value: 'my_search' },
    });
    fireEvent.change(within(modal).getByTestId('aip-tool-description-input'), {
      target: { value: 'Search the corpus' },
    });
    fireEvent.change(within(modal).getByTestId('aip-tool-parameters-input'), {
      target: {
        value:
          '{\n  "type": "object",\n  "properties": { "q": { "type": "string" } }\n}',
      },
    });
    fireEvent.change(within(modal).getByTestId('aip-tool-handler-input'), {
      target: { value: 'ri.function.main.function.search' },
    });
    // enabled toggle defaults to on; leave it on.
    fireEvent.click(within(modal).getByTestId('aip-tool-submit-btn'));

    await waitFor(() => expect(capturedCreateBody).not.toBeNull());
    const body = capturedCreateBody as {
      name?: string;
      description?: string;
      parameters?: unknown;
      handlerFunctionRid?: string;
      enabled?: boolean;
    };
    expect(body.name).toBe('my_search');
    expect(body.description).toBe('Search the corpus');
    expect(body.parameters).toEqual({
      type: 'object',
      properties: { q: { type: 'string' } },
    });
    expect(body.handlerFunctionRid).toBe('ri.function.main.function.search');
    expect(body.enabled).toBe(true);

    // The new tool shows up after the list refetch.
    await screen.findByText('my_search');
  });

  it('Given invalid parameters JSON, When submitting, Then the request is blocked with an inline error', async () => {
    renderPage();

    fireEvent.click(await screen.findByTestId('aip-tool-create-btn'));
    const modal = await screen.findByTestId('aip-tool-modal');
    fireEvent.change(within(modal).getByTestId('aip-tool-name-input'), {
      target: { value: 'bad_tool' },
    });
    fireEvent.change(within(modal).getByTestId('aip-tool-parameters-input'), {
      target: { value: '{ not valid json' },
    });
    fireEvent.click(within(modal).getByTestId('aip-tool-submit-btn'));

    // Inline parse error is shown and no POST goes out.
    await screen.findByTestId('aip-tool-parameters-error');
    expect(capturedCreateBody).toBeNull();
  });

  it('Given a tool exists, When delete is confirmed, Then DELETE is called with the tool name', async () => {
    tools = [echoTool];
    const originalConfirm = window.confirm;
    window.confirm = () => true;
    try {
      renderPage();
      const row = await screen.findByTestId('aip-tool-row');
      fireEvent.click(within(row).getByTestId('aip-tool-delete-btn'));

      await waitFor(() => expect(capturedDeleteName).toBe('echo_tool'));
    } finally {
      window.confirm = originalConfirm;
    }
  });
});

describe('BDD: AipToolsPage degraded catalog', () => {
  it('Given the catalog is unavailable (500), When the page loads, Then an unavailable state is shown', async () => {
    server.use(
      http.get('/api/v2/aip/tools', () =>
        HttpResponse.json(
          {
            errorCode: 'INTERNAL',
            errorName: 'AIPToolCatalogUnavailable',
            errorInstanceId: 'x',
          },
          { status: 500 },
        ),
      ),
    );
    renderPage();
    await screen.findByTestId('aip-tools-unavailable');
  });
});
