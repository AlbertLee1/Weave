import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { PlaygroundPage } from '../PlaygroundPage';

const SAMPLE_YAML = `
openapi: 3.0.3
info: { title: Sample, version: "1" }
tags:
  - name: Metadata
paths:
  /api/v2/ontologies:
    get:
      tags: [Metadata]
      operationId: listOntologies
      summary: List ontologies
      responses:
        '200': { description: ok }
`;

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <PlaygroundPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('PlaygroundPage', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url.includes('/api/openapi.yaml')) {
          return new Response(SAMPLE_YAML, {
            status: 200,
            headers: { 'Content-Type': 'application/yaml' },
          });
        }
        return new Response('{}', { status: 200 });
      }),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('renders the heading and loads the spec', async () => {
    renderPage();
    expect(screen.getByText(/API Playground/i)).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.getByText('listOntologies')).toBeInTheDocument();
    });
  });

  it('shows the endpoint search input', async () => {
    renderPage();
    await waitFor(() => {
      expect(screen.getByPlaceholderText(/search endpoints/i)).toBeInTheDocument();
    });
  });

  it('shows tag group header', async () => {
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Metadata')).toBeInTheDocument();
    });
  });
});
