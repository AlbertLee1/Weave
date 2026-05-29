package main

import (
	"github.com/liyang/weave/pkg/buildinfo"
)

// detectFeatures builds the round-127 capability manifest from the
// runtime ServerDeps state. Each dep-nil-or-not check turns into one
// Feature entry the SPA can read via GET /api/v2/build-info/features.
//
// Adding a new feature: append one row here naming the dep + a
// human-readable description + a Reason string for the disabled
// case so the operator sees the actionable next step without
// grepping logs.
func detectFeatures(deps *ServerDeps) []buildinfo.Feature {
	out := []buildinfo.Feature{
		// Round 91/117/119: RID @vN parser + 8 Get-endpoint guards.
		// Always enabled because the parser is pure Go code with
		// no external deps. Listed so the SPA can decide whether
		// to surface "version pin" UI affordances.
		{
			Name:        "rid-versioning",
			Enabled:     true,
			Description: "RID @vN parser + Get-endpoint @vN guards (rounds 91, 117, 119); SDK WeaveVersionedLookupError (round 118).",
		},
		// Snapshot lookup itself isn't built — versioned RIDs
		// currently return 501 VersionedLookupNotSupported. Surface
		// this in features so the SPA can disable the picker for
		// "view at version N" workflows.
		{
			Name:        "snapshots",
			Enabled:     false,
			Description: "Historical metadata lookup by RID version.",
			Reason:      "Gap-T4 step-1 not yet implemented; versioned-RID Get endpoints return 501.",
		},
		// MCP server (round before this micro-arc): always mounted
		// in NewFullRouter regardless of deps. Useful for AI agents
		// to know they can hit /mcp.
		{
			Name:        "mcp",
			Enabled:     true,
			Description: "MCP JSON-RPC 2.0 endpoint at /mcp for AI agents.",
		},
	}

	if deps != nil {
		out = append(out,
			buildinfo.Feature{
				Name:        "ontology-metadata",
				Enabled:     deps.OmsRepo != nil,
				Description: "Ontology metadata service (object types, action types, etc.).",
				Reason:      ifDisabled(deps.OmsRepo == nil, "OmsRepo not configured (likely degraded boot without PG)"),
			},
			buildinfo.Feature{
				Name:        "object-storage",
				Enabled:     deps.OssSvc != nil,
				Description: "Object Storage Service (search, list, get by primary key).",
				Reason:      ifDisabled(deps.OssSvc == nil, "OssSvc not configured"),
			},
			buildinfo.Feature{
				Name:        "actions",
				Enabled:     deps.ActionExecutor != nil,
				Description: "Action execution pipeline.",
				Reason:      ifDisabled(deps.ActionExecutor == nil, "ActionExecutor not configured"),
			},
			buildinfo.Feature{
				Name:        "sessions",
				Enabled:     deps.SessionStore != nil,
				Description: "Session inventory + revoke endpoints (rounds 101/102).",
				Reason:      ifDisabled(deps.SessionStore == nil, "SessionStore not configured (no PG); /api/auth/sessions endpoints unmounted"),
			},
			buildinfo.Feature{
				Name:        "transactions",
				Enabled:     deps.TransactionStore != nil,
				Description: "Edit transactions persistence (US-379).",
				Reason:      ifDisabled(deps.TransactionStore == nil, "TransactionStore not configured"),
			},
			buildinfo.Feature{
				Name:        "user-preferences",
				Enabled:     deps.UserPreferencesStore != nil,
				Description: "User-scoped KV preferences store (US-350).",
				Reason:      ifDisabled(deps.UserPreferencesStore == nil, "UserPreferencesStore not configured (no PG); /api/v2/user-preferences endpoints unmounted"),
			},
		)
	}
	return out
}

// ifDisabled returns reason when cond is true (the feature is
// disabled and we should explain why), and "" otherwise so the
// json:omitempty tag drops the field from the enabled wire payload.
func ifDisabled(cond bool, reason string) string {
	if cond {
		return reason
	}
	return ""
}
