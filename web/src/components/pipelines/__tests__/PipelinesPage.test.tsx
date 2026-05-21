import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import * as pipelinesApi from '../../../api/pipelines';
import type { Pipeline } from '../../../api/pipelines';
import { PipelinesPage } from '../PipelinesPage';

const pipelineAlpha: Pipeline = {
  id: 'pipe_alpha',
  name: 'Alpha pipeline',
  description: 'Reads users, tags them, writes to warehouse.',
  inputs: [
    { name: 'src_users', type: 'objectset', config: { ontology: 'main' } },
  ],
  transforms: [
    {
      name: 'tag_premium',
      type: 'derive',
      inputs: ['src_users'],
      config: { formula: 'isPremium ? "yes" : "no"' },
    },
    {
      name: 'enrich',
      type: 'lookup',
      inputs: ['tag_premium'],
      config: {},
    },
  ],
  outputs: [
    { name: 'sink_warehouse', type: 'jdbc', input: 'enrich', config: { table: 'users' } },
  ],
  schedule: '0 */6 * * *',
  enabled: true,
  createdBy: 'user-1',
  createdAt: '2026-04-28T10:00:00Z',
  updatedAt: '2026-04-28T10:00:00Z',
};

const pipelineBeta: Pipeline = {
  id: 'pipe_beta',
  name: 'Beta pipeline',
  inputs: [{ name: 'csv_in', type: 'csv', config: {} }],
  transforms: [],
  outputs: [{ name: 'sink_kafka', type: 'kafka', input: 'csv_in', config: {} }],
  enabled: false,
  createdBy: 'user-1',
  createdAt: '2026-04-28T11:00:00Z',
  updatedAt: '2026-04-28T11:00:00Z',
};

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

describe('PipelinesPage (US-297)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.stubGlobal('fetch', vi.fn(async () => jsonResponse({ runs: [] })));
  });

  it('shows the empty state when no pipelines exist', async () => {
    vi.spyOn(pipelinesApi, 'listPipelines').mockResolvedValue({ pipelines: [] });
    renderPage();
    expect(await screen.findByText(/no pipelines yet/i)).toBeInTheDocument();
    expect(screen.getByText(/no pipeline selected/i)).toBeInTheDocument();
  });

  it('renders a "New pipeline" CTA inside the empty state (dogfood #6)', async () => {
    vi.spyOn(pipelinesApi, 'listPipelines').mockResolvedValue({ pipelines: [] });
    renderPage();
    const emptyBlock = await screen.findByTestId('pipeline-list-empty');
    const cta = within(emptyBlock).getByRole('button', { name: /new pipeline/i });
    expect(cta).toBeInTheDocument();
  });

  it('lists pipelines, auto-selects the first, and renders DAG nodes', async () => {
    vi.spyOn(pipelinesApi, 'listPipelines').mockResolvedValue({
      pipelines: [pipelineAlpha, pipelineBeta],
    });
    vi.spyOn(pipelinesApi, 'getPipeline').mockImplementation((id: string) =>
      Promise.resolve(id === pipelineAlpha.id ? pipelineAlpha : pipelineBeta),
    );

    renderPage();

    const items = await screen.findAllByTestId('pipeline-list-item');
    expect(items).toHaveLength(2);
    expect(items[0].getAttribute('data-pipeline-id')).toBe(pipelineAlpha.id);

    // Auto-selected detail loaded → graph renders the four nodes.
    const nodes = await screen.findAllByTestId('pipeline-graph-node');
    const names = nodes.map((n) => n.getAttribute('data-node-name')).sort();
    expect(names).toEqual([
      'enrich',
      'sink_warehouse',
      'src_users',
      'tag_premium',
    ]);
    // Edges: src_users→tag_premium, tag_premium→enrich, enrich→sink_warehouse
    const edges = screen.getAllByTestId('pipeline-graph-edge');
    expect(edges).toHaveLength(3);

    // Schedule + enabled badge in the log panel.
    expect(screen.getByTestId('pipeline-log-schedule').textContent).toBe(
      '0 */6 * * *',
    );
    expect(screen.getByTestId('pipeline-log-enabled').textContent).toBe(
      'enabled',
    );
  });

  it('switches the detail when another pipeline is selected', async () => {
    vi.spyOn(pipelinesApi, 'listPipelines').mockResolvedValue({
      pipelines: [pipelineAlpha, pipelineBeta],
    });
    vi.spyOn(pipelinesApi, 'getPipeline').mockImplementation((id: string) =>
      Promise.resolve(id === pipelineAlpha.id ? pipelineAlpha : pipelineBeta),
    );

    renderPage();
    await screen.findAllByTestId('pipeline-graph-node');

    const items = screen.getAllByTestId('pipeline-list-item');
    fireEvent.click(items[1]);

    await waitFor(() => {
      const names = screen
        .getAllByTestId('pipeline-graph-node')
        .map((n) => n.getAttribute('data-node-name'))
        .sort();
      expect(names).toEqual(['csv_in', 'sink_kafka']);
    });
    // Beta is disabled and has no schedule.
    expect(screen.getByTestId('pipeline-log-schedule').textContent).toBe(
      'on demand',
    );
    expect(screen.getByTestId('pipeline-log-enabled').textContent).toBe(
      'disabled',
    );
  });

  it('shows node config in the log panel when a graph node is clicked', async () => {
    vi.spyOn(pipelinesApi, 'listPipelines').mockResolvedValue({
      pipelines: [pipelineAlpha],
    });
    vi.spyOn(pipelinesApi, 'getPipeline').mockResolvedValue(pipelineAlpha);

    renderPage();
    const nodes = await screen.findAllByTestId('pipeline-graph-node');

    // Initial state: no selection.
    expect(screen.getByTestId('pipeline-log-no-selection')).toBeInTheDocument();

    const tagNode = nodes.find(
      (n) => n.getAttribute('data-node-name') === 'tag_premium',
    )!;
    fireEvent.click(tagNode);

    const selected = await screen.findByTestId('pipeline-log-selected');
    expect(within(selected).getByText('tag_premium')).toBeInTheDocument();
    expect(within(selected).getByText('Transform')).toBeInTheDocument();
    expect(within(selected).getByText('derive')).toBeInTheDocument();
    expect(within(selected).getByText('src_users')).toBeInTheDocument();
    const cfg = screen.getByTestId('pipeline-log-config');
    expect(cfg.textContent).toContain('isPremium');
  });

  it('Given a selected pipeline has recent runs, When the log panel renders, Then it lists run status, trigger, timestamp, and error text (SELF-602)', async () => {
    vi.spyOn(pipelinesApi, 'listPipelines').mockResolvedValue({
      pipelines: [pipelineAlpha],
    });
    vi.spyOn(pipelinesApi, 'getPipeline').mockResolvedValue(pipelineAlpha);
    const fetchSpy = vi.fn(async (url: RequestInfo | URL) => {
      expect(String(url)).toBe('/api/v2/pipelines/pipe_alpha/runs?limit=10');
      return jsonResponse({
        runs: [
          {
            id: 7,
            pipelineId: pipelineAlpha.id,
            status: 'failed',
            startedAt: '2026-05-21T03:12:00Z',
            finishedAt: '2026-05-21T03:12:03Z',
            triggeredBy: 'schedule',
            errorMessage: 'pipeline runtime not configured',
            createdAt: '2026-05-21T03:12:03Z',
          },
        ],
      });
    });
    vi.stubGlobal('fetch', fetchSpy);

    renderPage();

    const history = await screen.findByTestId('pipeline-run-history');
    expect(within(history).getByText('#7')).toBeInTheDocument();
    expect(within(history).getByText('failed')).toBeInTheDocument();
    expect(within(history).getByText('schedule')).toBeInTheDocument();
    expect(within(history).getByText('2026-05-21 03:12:00Z')).toBeInTheDocument();
    expect(
      within(history).getByText('pipeline runtime not configured'),
    ).toBeInTheDocument();
    expect(fetchSpy).toHaveBeenCalledTimes(1);
  });

  it('Given a selected pipeline has no runs, When the log panel renders, Then it shows an empty run-history state (SELF-602)', async () => {
    vi.spyOn(pipelinesApi, 'listPipelines').mockResolvedValue({
      pipelines: [pipelineAlpha],
    });
    vi.spyOn(pipelinesApi, 'getPipeline').mockResolvedValue(pipelineAlpha);

    renderPage();

    const empty = await screen.findByTestId('pipeline-run-history-empty');
    expect(empty).toHaveTextContent(/no runs recorded/i);
    expect(
      screen.queryByText(/live run history will appear here/i),
    ).not.toBeInTheDocument();
  });

  it('surfaces the list error when the pipelines API fails', async () => {
    vi.spyOn(pipelinesApi, 'listPipelines').mockRejectedValue(
      new Error('boom'),
    );
    renderPage();
    expect(await screen.findByRole('alert')).toHaveTextContent(/boom/);
  });
});
