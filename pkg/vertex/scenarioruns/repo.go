package scenarioruns

import "context"

// Repo is the persistence boundary for scenario_runs. The Service uses
// it to record run lifecycle and to discover resumable runs after a
// process restart. The real PG implementation will live in
// pkg/vertex/scenarioruns/pg_repo.go (deferred to the wiring story —
// this story only needs the interface so the in-memory test repo can
// drive the workflow end-to-end).
//
// Repo embeds Persister so a single concrete implementation satisfies
// both — the workflow takes the narrower Persister, the service takes
// the wider Repo.
type Repo interface {
	Persister
	CreateRun(ctx context.Context, r Run) error
	GetRun(ctx context.Context, rid string) (Run, error)
	ListResumable(ctx context.Context) ([]Run, error)
	// ListRunsForScenario returns every run whose ScenarioRID
	// matches, sorted by StartedAt DESC (newest first to match the
	// Foundry "Recent runs" panel ordering). Returns an empty slice
	// (non-nil) when the scenario has no runs — the wire layer
	// guarantees [] not null. Round 68 added it to back the GET
	// /api/vertex/v1/scenarios/{scenarioRid}/runs endpoint.
	ListRunsForScenario(ctx context.Context, scenarioRID string) ([]Run, error)
}

// ScenarioReader resolves a Scenario into the ordered list of
// activities the workflow should execute. Real implementations join
// the scenarios + Vertex Function Action + Model Mesh tables; for the
// v1 in-process service it can be a thin wrapper over the modelmesh
// planner output + the scenario's action rows.
type ScenarioReader interface {
	ListActivities(ctx context.Context, scenarioRID string) ([]Activity, error)
}
