import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { SchemaInferencePage } from '../SchemaInferencePage';

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

interface FetchState {
  inferCalls: Array<{ body: unknown }>;
  failNext: number;
}

function installFetch(state: FetchState) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const url = typeof input === 'string' ? input : input.toString();
      const method = (init?.method ?? 'GET').toUpperCase();

      if (method === 'POST' && url.endsWith('/api/v2/pipelines/schema/infer')) {
        const body = init?.body ? JSON.parse(init.body as string) : {};
        state.inferCalls.push({ body });
        if (state.failNext > 0) {
          state.failNext -= 1;
          return jsonResponse(
            {
              errorCode: 'INVALID_ARGUMENT',
              errorName: 'MissingSample',
              errorInstanceId: 'inst-1',
              parameters: { reason: 'sample content must not be empty' },
            },
            400,
          );
        }
        return jsonResponse({
          format: 'csv',
          rowsScanned: 3,
          sampleRows: 1000,
          hasHeader: true,
          fields: [
            {
              name: 'id',
              baseType: 'integer',
              nullable: false,
              samples: ['1', '2', '3'],
              nonNullCount: 3,
              nullCount: 0,
            },
            {
              name: 'name',
              baseType: 'string',
              nullable: false,
              samples: ['alice', 'bob', 'carol'],
              nonNullCount: 3,
              nullCount: 0,
            },
            {
              name: 'active',
              baseType: 'boolean',
              nullable: true,
              samples: ['true', 'false'],
              nonNullCount: 2,
              nullCount: 1,
            },
          ],
        });
      }

      return new Response('{}', { status: 200 });
    }),
  );
}

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <SchemaInferencePage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('SchemaInferencePage', () => {
  let state: FetchState;

  beforeEach(() => {
    state = { inferCalls: [], failNext: 0 };
    installFetch(state);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('renders the inference controls and dropzone', () => {
    renderPage();
    expect(screen.getByTestId('schema-inference-page')).toBeInTheDocument();
    expect(screen.getByTestId('format-select')).toBeInTheDocument();
    expect(screen.getByTestId('dropzone')).toBeInTheDocument();
    expect(screen.getByTestId('sample-rows')).toHaveValue(1000);
  });

  it('shows an inline error when running without a sample', async () => {
    renderPage();
    const user = userEvent.setup();
    await user.click(screen.getByTestId('run-inference'));
    expect(screen.getByTestId('error-banner')).toHaveTextContent(/paste a sample/i);
    expect(state.inferCalls).toHaveLength(0);
  });

  it('runs inference and renders the field table', async () => {
    renderPage();
    const user = userEvent.setup();
    const textarea = screen.getByTestId('sample-textarea') as HTMLTextAreaElement;
    await user.click(textarea);
    await user.paste('id,name,active\n1,alice,true\n2,bob,false\n3,carol,\n');

    await user.click(screen.getByTestId('run-inference'));

    await waitFor(() => expect(state.inferCalls).toHaveLength(1));
    const sent = state.inferCalls[0].body as {
      format: string;
      sample: string;
      options: { sampleRows: number; hasHeader: boolean; delimiter: string };
    };
    expect(sent.format).toBe('csv');
    expect(sent.sample).toContain('id,name,active');
    expect(sent.options.sampleRows).toBe(1000);
    expect(sent.options.hasHeader).toBe(true);

    await waitFor(() => screen.getByTestId('result-section'));
    expect(screen.getByTestId('rows-scanned')).toHaveTextContent('3 rows scanned');
    expect(screen.getByTestId('field-row-id')).toBeInTheDocument();
    expect(screen.getByTestId('type-id')).toHaveValue('integer');
    expect(screen.getByTestId('type-active')).toHaveValue('boolean');
  });

  it('lets the user override an inferred type', async () => {
    renderPage();
    const user = userEvent.setup();
    const textarea = screen.getByTestId('sample-textarea') as HTMLTextAreaElement;
    await user.click(textarea);
    await user.paste('id\n1\n');
    await user.click(screen.getByTestId('run-inference'));

    await waitFor(() => screen.getByTestId('type-id'));
    const select = screen.getByTestId('type-id') as HTMLSelectElement;
    await user.selectOptions(select, 'long');
    expect(select.value).toBe('long');
  });

  it('surfaces a server validation error in the banner', async () => {
    state.failNext = 1;
    renderPage();
    const user = userEvent.setup();
    const textarea = screen.getByTestId('sample-textarea') as HTMLTextAreaElement;
    await user.click(textarea);
    await user.paste('id\n1\n');
    await user.click(screen.getByTestId('run-inference'));

    await waitFor(() => screen.getByTestId('error-banner'));
    expect(screen.getByTestId('error-banner')).toHaveTextContent(/MissingSample/);
  });
});
