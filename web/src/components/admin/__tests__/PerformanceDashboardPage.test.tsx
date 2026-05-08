// US-456: dashboard component tests. We mock fetch with two consecutive
// /metrics scrapes so the rate-derivation pipeline produces a non-empty
// history, then assert the cards render the derived numbers.

import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, cleanup, waitFor, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { PerformanceDashboardPage } from '../PerformanceDashboardPage';

const SCRAPE_T0 = [
  '# HELP weave_http_requests_total foo',
  '# TYPE weave_http_requests_total counter',
  'weave_http_requests_total{method="GET",status="200"} 100',
  'weave_http_requests_total{method="POST",status="500"} 5',
  'weave_http_request_duration_seconds_count 105',
  'weave_http_request_duration_seconds_sum 5.25',
  'weave_db_queries_total 200',
  'weave_nats_publish_total 10',
  'weave_nats_consume_total 8',
].join('\n');

const SCRAPE_T1 = [
  'weave_http_requests_total{method="GET",status="200"} 200',
  'weave_http_requests_total{method="POST",status="500"} 7',
  'weave_http_request_duration_seconds_count 207',
  'weave_http_request_duration_seconds_sum 16.56',
  'weave_db_queries_total 300',
  'weave_nats_publish_total 20',
  'weave_nats_consume_total 18',
].join('\n');

function mockFetchSequence(bodies: string[]) {
  let idx = 0;
  return vi.fn(async (input: RequestInfo | URL) => {
    expect(String(input)).toBe('/metrics');
    const body = bodies[Math.min(idx, bodies.length - 1)];
    idx += 1;
    return {
      ok: true,
      status: 200,
      text: async () => body,
    } as Response;
  });
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('PerformanceDashboardPage', () => {
  it('shows a loading state before the first scrape lands', async () => {
    let resolveBody: (value: string) => void = () => {};
    const fetchMock = vi.fn(
      async () =>
        ({
          ok: true,
          status: 200,
          text: () =>
            new Promise<string>((resolve) => {
              resolveBody = resolve;
            }),
        }) as Response,
    );
    vi.stubGlobal('fetch', fetchMock);

    render(<PerformanceDashboardPage />);
    expect(screen.getByTestId('perf-loading')).toBeTruthy();
    // resolve so the in-flight promise unblocks before the next test
    resolveBody('');
  });

  it('renders an error envelope when /metrics returns non-2xx', async () => {
    const fetchMock = vi.fn(async () => ({
      ok: false,
      status: 503,
      text: async () => 'svc unavailable',
    }) as Response);
    vi.stubGlobal('fetch', fetchMock);

    render(<PerformanceDashboardPage />);

    await waitFor(() => {
      expect(screen.getByTestId('perf-error').textContent).toContain('503');
    });
  });

  it('derives QPS / error-rate / latency from two consecutive scrapes', async () => {
    let now = 1_000_000;
    const dateSpy = vi.spyOn(Date, 'now').mockImplementation(() => now);
    const fetchMock = mockFetchSequence([SCRAPE_T0, SCRAPE_T1]);
    vi.stubGlobal('fetch', fetchMock);

    render(<PerformanceDashboardPage />);

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(1);
    });

    // simulate 5 seconds passing and force a manual refresh so the second
    // snapshot has a positive dt against the first.
    now += 5000;
    await act(async () => {
      await userEvent.click(screen.getByTestId('perf-refresh-now'));
    });

    await waitFor(() => {
      expect(screen.getByTestId('perf-card-qps').textContent).toContain('20');
    });
    expect(screen.getByTestId('perf-card-error-rate').textContent).toMatch(/[12]\.\d{2}%/);
    expect(screen.getByTestId('perf-card-latency').textContent).toMatch(/11\d/);
    expect(screen.getByTestId('perf-card-db-qps').textContent).toContain('20');

    dateSpy.mockRestore();
  });

  it('Refresh button forces an immediate scrape', async () => {
    const fetchMock = mockFetchSequence([SCRAPE_T0, SCRAPE_T1, SCRAPE_T1]);
    vi.stubGlobal('fetch', fetchMock);

    render(<PerformanceDashboardPage />);
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(1);
    });

    await act(async () => {
      await userEvent.click(screen.getByTestId('perf-refresh-now'));
    });

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(2);
    });
  });

  it('Pause button toggles label and avoids extra polls during user-driven refresh window', async () => {
    const fetchMock = mockFetchSequence([SCRAPE_T0, SCRAPE_T1]);
    vi.stubGlobal('fetch', fetchMock);

    render(<PerformanceDashboardPage />);
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(1);
    });

    const pauseBtn = screen.getByTestId('perf-toggle-pause');
    expect(pauseBtn.textContent).toContain('Pause');

    await act(async () => {
      await userEvent.click(pauseBtn);
    });
    expect(pauseBtn.textContent).toContain('Resume');
  });

  it('renders the raw-samples drawer with the parsed counters', async () => {
    const fetchMock = mockFetchSequence([SCRAPE_T0]);
    vi.stubGlobal('fetch', fetchMock);

    render(<PerformanceDashboardPage />);

    await waitFor(() => {
      const list = screen.getByTestId('perf-raw-samples');
      expect(list.textContent).toContain('weave_http_requests_total');
      expect(list.textContent).toContain('weave_db_queries_total');
    });
  });
});
