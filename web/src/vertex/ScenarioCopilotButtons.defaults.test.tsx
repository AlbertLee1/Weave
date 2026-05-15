import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { ScenarioCopilotButtons } from './ScenarioCopilotButtons';
import '../i18n';

// VTX-122 — cover the default fetch-based suggester/explainer paths.
// The happy-path suite ScenarioCopilotButtons.test.tsx injects stubs,
// so the production fetch wrappers stay uncovered without these
// network-mocking cases.

const originalFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = originalFetch;
  vi.restoreAllMocks();
});

describe('ScenarioCopilotButtons — default fetch suggester/explainer', () => {
  it('default suggester POSTs to suggest-overrides and renders rows', async () => {
    const fetchMock = vi.fn(async (url: string) => {
      if (url.includes('suggest-overrides')) {
        return {
          ok: true,
          status: 200,
          json: async () => [
            { parameter: 'rate', recommendedRange: [1, 5], rationale: 'because' },
          ],
        } as Response;
      }
      throw new Error('unexpected url: ' + url);
    });
    globalThis.fetch = fetchMock as unknown as typeof fetch;

    render(<ScenarioCopilotButtons scenarioRid="ri.vertex.main.scenario.s1" hasResult />);
    fireEvent.click(screen.getByTestId('copilot-suggest'));
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/vertex/v1/scenarios/ri.vertex.main.scenario.s1/copilot/suggest-overrides',
        expect.objectContaining({ method: 'POST' }),
      );
    });
    await waitFor(() => {
      expect(screen.getByTestId('copilot-suggestions').textContent).toContain('rate');
    });
  });

  it('default suggester surfaces HTTP errors via error banner', async () => {
    const fetchMock = vi.fn(async () =>
      ({ ok: false, status: 500, json: async () => ({}) } as Response),
    );
    globalThis.fetch = fetchMock as unknown as typeof fetch;

    render(<ScenarioCopilotButtons scenarioRid="ri.vertex.main.scenario.s1" hasResult />);
    fireEvent.click(screen.getByTestId('copilot-suggest'));
    await waitFor(() => {
      expect(screen.getByTestId('copilot-error').textContent).toContain('500');
    });
  });

  it('default explainer POSTs to explain-result and renders summary + bullets', async () => {
    const fetchMock = vi.fn(async (url: string) => {
      if (url.includes('explain-result')) {
        return {
          ok: true,
          status: 200,
          json: async () => ({ summary: 'overall up 4%', bullets: ['a', 'b'] }),
        } as Response;
      }
      throw new Error('unexpected url: ' + url);
    });
    globalThis.fetch = fetchMock as unknown as typeof fetch;

    render(<ScenarioCopilotButtons scenarioRid="ri.vertex.main.scenario.s1" hasResult />);
    fireEvent.click(screen.getByTestId('copilot-explain'));
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/vertex/v1/scenarios/ri.vertex.main.scenario.s1/copilot/explain-result',
        expect.objectContaining({ method: 'POST' }),
      );
    });
    await waitFor(() => {
      expect(screen.getByTestId('copilot-explanation').textContent).toContain('overall up 4%');
    });
  });

  it('default explainer surfaces HTTP errors via error banner', async () => {
    const fetchMock = vi.fn(async () =>
      ({ ok: false, status: 502, json: async () => ({}) } as Response),
    );
    globalThis.fetch = fetchMock as unknown as typeof fetch;

    render(<ScenarioCopilotButtons scenarioRid="ri.vertex.main.scenario.s1" hasResult />);
    fireEvent.click(screen.getByTestId('copilot-explain'));
    await waitFor(() => {
      expect(screen.getByTestId('copilot-error').textContent).toContain('502');
    });
  });
});
