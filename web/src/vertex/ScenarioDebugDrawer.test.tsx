import { afterEach, describe, it, expect, vi } from 'vitest';
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

const scenarioRid = 'ri.vertex.main.scenario.s1';

describe('ScenarioDebugDrawer (VTX-102)', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('renders nothing when no rid is selected', () => {
    render(<ScenarioDebugDrawer scenarioRunRid={null} onClose={() => {}} />);
    expect(screen.queryByTestId('scenario-debug-drawer')).toBeNull();
  });

  it('Given a scenario run is selected, When the default fetcher loads it, Then it uses the mounted documented run record route', async () => {
    const fetchSpy = vi.fn<typeof fetch>(async () =>
      new Response(
        JSON.stringify({
          rid: sample.scenarioRunRid,
          scenarioRid,
          status: 'failed',
          error: 'model scorer failed',
          checkpoint: {
            runRid: sample.scenarioRunRid,
            scenarioRid,
            status: 'failed',
            completed: ['load-inputs'],
            lastActivity: 'score',
            attemptsById: { score: 3 },
            error: 'model scorer failed',
            updatedAt: '2026-05-20T00:00:00Z',
          },
          startedAt: '2026-05-20T00:00:00Z',
          createdAt: '2026-05-20T00:00:00Z',
        }),
        { status: 200, headers: { 'content-type': 'application/json' } },
      ),
    );
    vi.stubGlobal('fetch', fetchSpy);

    render(
      <ScenarioDebugDrawer
        scenarioRid={scenarioRid}
        scenarioRunRid={sample.scenarioRunRid}
        onClose={() => {}}
      />,
    );

    await waitFor(() => {
      expect(fetchSpy).toHaveBeenCalledWith(
        `/api/vertex/v1/scenarios/${encodeURIComponent(scenarioRid)}/runs/${encodeURIComponent(sample.scenarioRunRid)}`,
      );
    });
    expect(screen.getByTestId('scenario-debug-input').textContent).toContain('"status": "failed"');
    expect(screen.getByTestId('scenario-debug-logs').textContent).toContain('score attempts=3');
    expect(screen.getByTestId('scenario-debug-edits').textContent).toContain('No partial edits in run record');
  });

  it('Given a scenario run is missing, When the default fetcher receives 404, Then the drawer shows a typed not-found state', async () => {
    const fetchSpy = vi.fn<typeof fetch>(async () =>
      new Response(JSON.stringify({ code: 'ScenarioRunNotFound' }), {
        status: 404,
        headers: { 'content-type': 'application/json' },
      }),
    );
    vi.stubGlobal('fetch', fetchSpy);

    render(
      <ScenarioDebugDrawer
        scenarioRid={scenarioRid}
        scenarioRunRid={sample.scenarioRunRid}
        onClose={() => {}}
      />,
    );

    await waitFor(() => {
      expect(screen.getByTestId('scenario-debug-error').textContent).toContain('Scenario run not found');
    });
    expect(fetchSpy).toHaveBeenCalledTimes(1);
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
