import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AipToolsPage } from '../AipToolsPage';
import type { ToolRecord } from '../../../api/aipTools';

// Captures which tool the backend was asked to delete so the BDD assertions can
// prove the destructive call only fires after explicit confirmation.
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
  http.delete('/api/v2/aip/tools/:toolName', ({ params }) => {
    capturedDeleteName = String(params.toolName);
    tools = tools.filter((t) => t.name !== capturedDeleteName);
    return new HttpResponse(null, { status: 204 });
  }),
);

beforeAll(() => server.listen());
afterEach(() => {
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

describe('BDD: AipToolsPage styled delete confirmation (replace window.confirm)', () => {
  it('Given a tool exists, When Delete is clicked, Then a styled Modal opens and window.confirm is never called', async () => {
    tools = [echoTool];
    const user = userEvent.setup();
    // Trip-wire: a styled-Modal flow must never reach the native dialog. If the
    // implementation still calls window.confirm, this throws and fails the test
    // — without us mocking it to a benign no-op.
    const originalConfirm = window.confirm;
    let confirmCalls = 0;
    window.confirm = () => {
      confirmCalls += 1;
      throw new Error('window.confirm must not be used for delete confirmation');
    };
    try {
      renderPage();

      const row = await screen.findByTestId('aip-tool-row');
      await user.click(within(row).getByTestId('aip-tool-delete-btn'));

      // A styled confirmation Modal appears (role=dialog / overlay), naming the
      // tool to be deleted. No native confirm was invoked.
      const dialog = await screen.findByTestId('aip-tool-delete-confirm');
      expect(within(dialog).getByText(/echo_tool/)).toBeInTheDocument();
      expect(confirmCalls).toBe(0);
      // Nothing was deleted just by opening the dialog.
      expect(capturedDeleteName).toBeNull();
    } finally {
      window.confirm = originalConfirm;
    }
  });

  it('Given the confirm Modal is open, When Cancel is clicked, Then nothing is deleted and the dialog closes', async () => {
    tools = [echoTool];
    const user = userEvent.setup();
    renderPage();

    const row = await screen.findByTestId('aip-tool-row');
    await user.click(within(row).getByTestId('aip-tool-delete-btn'));

    const dialog = await screen.findByTestId('aip-tool-delete-confirm');
    await user.click(
      within(dialog).getByTestId('aip-tool-delete-confirm-cancel'),
    );

    // The confirmation dialog closes…
    await waitFor(() =>
      expect(
        screen.queryByTestId('aip-tool-delete-confirm'),
      ).not.toBeInTheDocument(),
    );

    // …the tool is still listed and no DELETE went out.
    expect(screen.getByTestId('aip-tool-row')).toBeInTheDocument();
    expect(capturedDeleteName).toBeNull();
  });

  it('Given the confirm Modal is open, When Delete is confirmed, Then DELETE fires and the tool disappears', async () => {
    tools = [echoTool];
    const user = userEvent.setup();
    renderPage();

    const row = await screen.findByTestId('aip-tool-row');
    await user.click(within(row).getByTestId('aip-tool-delete-btn'));

    const dialog = await screen.findByTestId('aip-tool-delete-confirm');
    await user.click(
      within(dialog).getByTestId('aip-tool-delete-confirm-submit'),
    );

    // The destructive call targets the right tool…
    await waitFor(() => expect(capturedDeleteName).toBe('echo_tool'));
    // …and after the list refetch the row is gone.
    await waitFor(() =>
      expect(screen.queryByTestId('aip-tool-row')).not.toBeInTheDocument(),
    );
  });
});
