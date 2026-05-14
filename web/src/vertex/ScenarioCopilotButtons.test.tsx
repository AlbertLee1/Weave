import { describe, it, expect, vi } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import {
  ScenarioCopilotButtons,
  type OverrideSuggestion,
  type ExplainResult,
} from './ScenarioCopilotButtons';

const suggestions: OverrideSuggestion[] = [
  { parameter: 'capacity', recommendedRange: [40, 60], rationale: 'past 90d throughput is volatile' },
];

const explanation: ExplainResult = {
  summary: 'Halving JFK capacity propagated to 50 downstream flights.',
  bullets: ['avg delay +12 min', 'rebooking impact: $1.2M'],
};

describe('ScenarioCopilotButtons (VTX-113)', () => {
  it('Suggest Override calls the suggester and renders the recommended range', async () => {
    const suggester = vi.fn(async () => suggestions);
    render(
      <ScenarioCopilotButtons
        scenarioRid="ri.vertex.main.scenario.s1"
        hasResult={false}
        suggester={suggester}
      />,
    );
    fireEvent.click(screen.getByTestId('copilot-suggest'));
    await waitFor(() => {
      expect(screen.getByTestId('copilot-suggestions').textContent).toContain('40');
      expect(screen.getByTestId('copilot-suggestions').textContent).toContain('60');
    });
    expect(suggester).toHaveBeenCalledWith('ri.vertex.main.scenario.s1');
  });

  it('Explain Result is disabled until hasResult is true', () => {
    render(
      <ScenarioCopilotButtons
        scenarioRid="ri.vertex.main.scenario.s1"
        hasResult={false}
      />,
    );
    expect((screen.getByTestId('copilot-explain') as HTMLButtonElement).disabled).toBe(true);
  });

  it('Explain Result calls the explainer and renders the summary + bullets', async () => {
    const explainer = vi.fn(async () => explanation);
    render(
      <ScenarioCopilotButtons
        scenarioRid="ri.vertex.main.scenario.s1"
        hasResult={true}
        explainer={explainer}
      />,
    );
    fireEvent.click(screen.getByTestId('copilot-explain'));
    await waitFor(() => {
      expect(screen.getByTestId('copilot-explanation').textContent).toContain('downstream flights');
      expect(screen.getByTestId('copilot-explanation').textContent).toContain('rebooking impact');
    });
  });

  it('surfaces fetch errors on the panel without crashing', async () => {
    const suggester = async () => {
      throw new Error('llm unavailable');
    };
    render(
      <ScenarioCopilotButtons
        scenarioRid="ri.vertex.main.scenario.s1"
        hasResult={false}
        suggester={suggester}
      />,
    );
    fireEvent.click(screen.getByTestId('copilot-suggest'));
    await waitFor(() => {
      expect(screen.getByTestId('copilot-error').textContent).toContain('llm unavailable');
    });
  });
});
