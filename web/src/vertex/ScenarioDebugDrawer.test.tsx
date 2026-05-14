import { describe, it, expect } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { ScenarioDebugDrawer, type ScenarioRunDebug } from './ScenarioDebugDrawer';

const sample: ScenarioRunDebug = {
  scenarioRunRid: 'ri.vertex.main.scenario-run.abc',
  inputSnapshot: { airport: 'JFK', delayPct: 0.5 },
  functionLogs: ['fn:start', 'fn:JFK capacity=50', 'fn:err connection reset'],
  partialEdits: [
    { op: 'modifyProperty', objectId: 'JFK', property: 'capacity', newValue: 50 },
  ],
};

describe('ScenarioDebugDrawer (VTX-102)', () => {
  it('renders nothing when no rid is selected', () => {
    render(<ScenarioDebugDrawer scenarioRunRid={null} onClose={() => {}} />);
    expect(screen.queryByTestId('scenario-debug-drawer')).toBeNull();
  });

  it('fetches and renders input / logs / partial edits', async () => {
    render(
      <ScenarioDebugDrawer
        scenarioRunRid={sample.scenarioRunRid}
        onClose={() => {}}
        fetcher={async () => sample}
      />,
    );
    await waitFor(() => {
      expect(screen.getByTestId('scenario-debug-input').textContent).toContain('JFK');
    });
    expect(screen.getByTestId('scenario-debug-logs').textContent).toContain('fn:err connection reset');
    expect(screen.getByTestId('scenario-debug-edits').textContent).toContain('modifyProperty');
    expect(screen.getByTestId('scenario-debug-edits').textContent).toContain('capacity');
  });

  it('surfaces fetch errors instead of silently hiding them', async () => {
    render(
      <ScenarioDebugDrawer
        scenarioRunRid={sample.scenarioRunRid}
        onClose={() => {}}
        fetcher={async () => {
          throw new Error('boom');
        }}
      />,
    );
    await waitFor(() => {
      expect(screen.getByTestId('scenario-debug-error').textContent).toContain('boom');
    });
  });

  it('invokes onClose when the close button is clicked', () => {
    let closed = false;
    render(
      <ScenarioDebugDrawer
        scenarioRunRid={sample.scenarioRunRid}
        onClose={() => {
          closed = true;
        }}
        fetcher={async () => sample}
      />,
    );
    fireEvent.click(screen.getByTestId('scenario-debug-close'));
    expect(closed).toBe(true);
  });
});
