import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import * as pipelinesApi from '../../../api/pipelines';
import { PipelinesPage } from '../PipelinesPage';

function jsonResponse(body: unknown) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/pipelines']}>
        <Routes>
          <Route path="/pipelines" element={<PipelinesPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('PipelinesPage accessibility — page-level <h1> (a11y)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.stubGlobal('fetch', vi.fn(async () => jsonResponse({ runs: [] })));
  });

  it('Given the Pipelines page is rendered, Then it exposes exactly one level-1 heading named "Pipelines"', async () => {
    vi.spyOn(pipelinesApi, 'listPipelines').mockResolvedValue({ pipelines: [] });

    renderPage();

    // Wait for the page to settle (empty state resolves).
    await screen.findByTestId('pipelines-page');

    const h1s = screen.getAllByRole('heading', { level: 1 });
    expect(h1s).toHaveLength(1);
    expect(h1s[0]).toHaveAccessibleName(/pipelines/i);
  });
});
