# Vertex Quickstart (VTX-111)

Eight steps that take you from a blank Weave install to a Vertex
Scenario Run with overlaid reads. Every snippet runs against
`http://localhost:9117` (the default `make dev` endpoint). Replace
`$API_KEY` with the value from `weave admin api-key issue`.

The end-to-end flow:

1. Boot Weave
2. Create an ontology + a CaseStudy
3. Create a Scenario (fork of the ontology)
4. Append an edit to the Scenario
5. Read the object with `X-Scenario-Id` (overlaid view)
6. Aggregate across the Scenario
7. Run the Scenario (start + poll)
8. Apply the Scenario to main (or discard)

Companion code lives at [`vertex-quickstart.py`](./vertex-quickstart.py).

---

## 1. Boot Weave

```bash
make dev
# Weave is now serving on http://localhost:9117
```

## 2. Create an ontology + CaseStudy

```bash
# 2a. Create an ontology (one-time per workspace)
curl -X POST http://localhost:9117/api/admin/ontologies \
  -H "Authorization: Bearer $API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"apiName":"aviation","displayName":"Aviation"}'

# Capture the rid:
export ONTOLOGY_RID="ri.weave.main.ontology.<uuid>"

# 2b. Create a CaseStudy attached to that ontology
curl -X POST http://localhost:9117/api/vertex/v1/case-studies \
  -H "Authorization: Bearer $API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"name":"JFK Operations","ontologyRid":"'"$ONTOLOGY_RID"'"}'

export CASE_STUDY_RID="ri.vertex.main.case-study.<uuid>"
```

## 3. Create a Scenario

```bash
curl -X POST http://localhost:9117/api/vertex/v1/scenarios \
  -H "Authorization: Bearer $API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{
    "caseStudyRid": "'"$CASE_STUDY_RID"'",
    "name": "snowstorm",
    "parentOntologyCommit": "head"
  }'

export SCENARIO_RID="ri.vertex.main.scenario.<uuid>"
```

## 4. Append an edit

Cut JFK's capacity in half inside the Scenario. The underlying ontology
stays untouched — the edit lives in `scenario_edits`.

```bash
curl -X POST "http://localhost:9117/api/vertex/v1/scenarios/$SCENARIO_RID/edits" \
  -H "Authorization: Bearer $API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{
    "op": "modifyProperty",
    "objectType": "Airport",
    "objectId": "JFK",
    "property": "capacity",
    "newValue": 50
  }'
```

## 5. Read with X-Scenario-Id

Same URL as a plain read; one header flips the response to the overlaid
view.

```bash
# Base (untouched)
curl "http://localhost:9117/api/v2/ontologies/aviation/objects/Airport/JFK" \
  -H "Authorization: Bearer $API_KEY"
# → { "id": "JFK", "properties": { "capacity": 100, ... } }

# Overlaid
curl "http://localhost:9117/api/v2/ontologies/aviation/objects/Airport/JFK" \
  -H "Authorization: Bearer $API_KEY" \
  -H "X-Scenario-Id: $SCENARIO_RID"
# → { "id": "JFK", "properties": { "capacity": 50, ... } }
```

## 6. Aggregate across the Scenario

```bash
curl -X POST "http://localhost:9117/api/v2/ontologies/aviation/objects/Airport/aggregate?scenarioId=$SCENARIO_RID" \
  -H "Authorization: Bearer $API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{ "aggregations": [{"type":"sum","field":"capacity","name":"sumCap"}] }'
```

## 7. Start the Scenario Run

```bash
curl -X POST "http://localhost:9117/api/vertex/v1/scenarios/$SCENARIO_RID/runs" \
  -H "Authorization: Bearer $API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{}'
# Start returns:
#   {"runRid":"ri.vertex.main.scenario-run.<uuid>","status":"pending"}

RUN_RID="ri.vertex.main.scenario-run.<uuid>"
# Poll until status is succeeded, failed, or canceled:
curl "http://localhost:9117/api/vertex/v1/scenarios/$SCENARIO_RID/runs/$RUN_RID" \
  -H "Authorization: Bearer $API_KEY"
```

## 8. Apply (or discard)

If the run looks good, fold the Scenario back into main:

```bash
curl -X POST "http://localhost:9117/api/vertex/v1/scenarios/$SCENARIO_RID/apply" \
  -H "Authorization: Bearer $API_KEY"
```

Or just abandon it — Scenarios are scoped to the CaseStudy and auto-
archive after the retention window (see VTX-116).

---

## Python (one-shot)

```python
from weave_client.client import Client

c = Client("http://localhost:9117", api_key="$API_KEY")

cs = c._request("POST", "/api/vertex/v1/case-studies",
                json_body={"name": "JFK Operations", "ontologyRid": ONTOLOGY_RID})
scen = c.vertex.scenarios.create(
    case_study_rid=cs["rid"],
    name="snowstorm",
    parent_ontology_commit="head",
)

# Step 4 — edit (handler from VTX-044)
c._request("POST", f"/api/vertex/v1/scenarios/{scen['rid']}/edits", json_body={
    "op": "modifyProperty", "objectType": "Airport", "objectId": "JFK",
    "property": "capacity", "newValue": 50,
})

# Step 5 — overlay read
overlaid = c.objects.get("aviation", "Airport", "JFK", scenario_id=scen["rid"])
assert overlaid["properties"]["capacity"] == 50

# Step 7 — run and wait for the terminal record
run = c.vertex.scenarios.run(scen["rid"])
print(run)

# Step 8 — apply
c.vertex.scenarios.apply_to_main(scen["rid"])
```

## TypeScript (one-shot)

```ts
import { VertexClient } from '@weave/sdk';

const v = new VertexClient({ baseUrl: 'http://localhost:9117' });
const scen = await v.scenarios.create({
  caseStudyRid: CASE_STUDY_RID,
  name: 'snowstorm',
  parentOntologyCommit: 'head',
});

const run = await v.scenarios.run(scen.rid);
console.log(run.status);

await v.scenarios.applyToMain({ scenarioRid: scen.rid });
```

## Go (one-shot)

```go
c := weavesdk.New("http://localhost:9117", os.Getenv("API_KEY"))
scen, _ := c.Vertex.Scenarios.Create(ctx, weavesdk.ScenarioCreateInput{
    CaseStudyRID: caseStudyRID, Name: "snowstorm", ParentOntologyCommit: "head",
})
run, _ := c.Vertex.Scenarios.Run(ctx, scen.RID, nil)
fmt.Println(run.Status)
c.Vertex.Scenarios.ApplyToMain(ctx, scen.RID)
```

---

## Troubleshooting

| Symptom                                   | Cause                                                  | Fix                                                  |
| ----------------------------------------- | ------------------------------------------------------ | ---------------------------------------------------- |
| `404 ScenarioNotFound` on read with overlay | The Scenario RID does not exist or was archived       | Re-create or unarchive; or hit base without header   |
| `409 ScenarioOntologyMismatch`              | `parentOntologyCommit` is from a different ontology    | Re-create the Scenario against the right ontology    |
| Run stays `pending` or `running`             | Scenario execution is still in flight                  | Keep polling `/runs/{runRid}` or use SDK `run()`     |
| `403` on `/apply`                           | Caller lacks `vertex.scenario.apply` permission        | Grant the role or have an admin apply on your behalf |
