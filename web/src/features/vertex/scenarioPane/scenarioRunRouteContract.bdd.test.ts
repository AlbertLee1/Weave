import { describe, expect, it } from 'vitest';

import { buildRunScenarioRequest } from './scenarioRun';
import {
  buildAsyncRunScenarioRequest,
  buildCancelScenarioRunRequest,
  buildScenarioRunStreamUrl,
  parseAcceptedScenarioRunResponse,
} from './scenarioRunAsync';

const scenarioRid = 'ri.vertex.main.scenario.s-1';
const runRid = 'ri.vertex.main.scenario-run.r-1';

describe('BDD: Vertex scenario-run route contract (SELF-468)', () => {
  it('Given a scenario RID, When sync and async start requests are built, Then both target the mounted plural runs route', () => {
    const sync = buildRunScenarioRequest({ scenarioRid });
    const async = buildAsyncRunScenarioRequest({ scenarioRid });

    const expected = `/api/vertex/v1/scenarios/${encodeURIComponent(scenarioRid)}/runs`;
    expect(sync).toMatchObject({ method: 'POST', path: expected });
    expect(async).toMatchObject({ method: 'POST', path: expected });
  });

  it('Given a run RID, When stream and cancel helpers are built, Then they use the documented runs/{runRid} route family', () => {
    expect(buildScenarioRunStreamUrl({ scenarioRid, runRid })).toBe(
      `/api/vertex/v1/scenarios/${encodeURIComponent(scenarioRid)}/runs/${encodeURIComponent(runRid)}/stream`,
    );
    expect(buildCancelScenarioRunRequest({ scenarioRid, runRid })).toMatchObject({
      method: 'POST',
      path: `/api/vertex/v1/scenarios/${encodeURIComponent(scenarioRid)}/runs/${encodeURIComponent(runRid)}/cancel`,
    });
  });

  it('Given the server returns an accepted run, When parsed, Then the canonical runRid is preserved', () => {
    expect(parseAcceptedScenarioRunResponse({ status: 'pending', runRid })).toEqual({
      status: 'pending',
      runRid,
    });
  });
});
