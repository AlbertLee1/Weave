package ci_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBDD_PRDV2GapEntriesReflectImplementationReality is the round-56
// staleness guard for docs/PRD-Weave-OSv2-深度复刻-V2.md. Rounds
// 24–55 surfaced that several Gap-* entries described work that
// was actually already implemented — the "建议" recommendation
// text was carried over from an earlier survey and never refreshed
// after the implementation landed. Future rounds wasted research
// cycles re-confirming "yes that's already done" before they
// could pick a real target.
//
// This test asserts that each audited Gap-* entry mentions a
// specific implementation marker (file path, function name, or
// round-NN citation). If a future PR reverts the doc to its
// stale state — or if someone deletes the implementation — the
// test fails loudly and the PRD stays trustworthy as a planning
// document.
//
// Each marker pair is (Gap-ID, expected substring). Substrings
// are chosen to be specific enough to fail on rewording but loose
// enough that re-numbering a US-* citation or moving a file does
// not break the test.
func TestBDD_PRDV2GapEntriesReflectImplementationReality(t *testing.T) {
	docPath := filepath.Join(repoRoot(t), "docs", "PRD-Weave-OSv2-深度复刻-V2.md")
	raw, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read PRD: %v", err)
	}
	doc := string(raw)

	cases := []struct {
		gap    string
		marker string
		why    string
	}{
		{
			gap:    "Gap-O1",
			marker: "pkg/metrics/oss.go",
			why:    "Gap-O1 business metrics ARE wired — pkg/metrics/oss.go + funnel.go expose weave_objectset_execute_duration / weave_funnel_lag.",
		},
		{
			gap:    "Gap-O2",
			marker: "pkg/funnel/tracing.go",
			why:    "Gap-O2 cross-process TraceContext propagation IS in place via pkg/funnel/tracing.go natsHeaderCarrier; rounds 52-53 closed the outbound HTTP edges too.",
		},
		{
			gap:    "Gap-O4",
			marker: "ErrFunnelLagDegraded",
			why:    "Gap-O4 health-check lag threshold IS implemented — cmd/server/health.go exposes ErrFunnelLagDegraded so /health/ready returns 200 + status=degraded above the configured lag.",
		},
		{
			gap:    "Gap-T2",
			marker: "validate.go",
			why:    "Gap-T2 Struct + Array deep validation IS implemented — pkg/types/validate.go recurses Struct fields and Array elements with full error path attribution.",
		},
		{
			gap:    "Gap-T3",
			marker: "ValidateConstraints",
			why:    "Gap-T3 ValueType constraint enforcement IS implemented — pkg/types/constraints.go ValidateConstraints called from both pkg/oss/stream_ingest_validation.go and pkg/actions/executor.go on the write path.",
		},
		{
			gap:    "Gap-S5",
			marker: "pkg/sqlqueries",
			why:    "Gap-S5 SQL Query sandbox IS implemented — pkg/sqlqueries/safety.go (30+ forbidden keywords, system-table guard, full tokenizer) + engine.go (5s timeout, 10K row cap, pgx.ReadOnly transactions).",
		},
		// Round 57 — second wave of stale gaps surfaced while
		// auditing Gap-Q* / Gap-A* / Gap-R* / Gap-T* sections.
		{
			gap:    "Gap-Q1",
			marker: "composite_cursor.go",
			why:    "Gap-Q1 multi-type interface paging IS stable — pkg/oss/pagination/composite_cursor.go + pkg/oss/objectset/us463_interface_cursor_stability_test.go drive the {objectTypeApiName, innerCursor} composite cursor.",
		},
		{
			gap:    "Gap-A1",
			marker: "edit_source_test.go",
			why:    "Gap-A1 user-edit-wins IS implemented — pkg/actions/edit_source_test.go covers the EditSource marking; consumer compares last user-edit timestamp before applying funnel writes.",
		},
		{
			gap:    "Gap-A2",
			marker: "optimistic_test.go",
			why:    "Gap-A2 optimistic concurrency IS implemented — pkg/actions/optimistic_test.go + us471_optimistic_multitarget_test.go cover expectedVersion / If-Match wiring with 409 on stale version.",
		},
		{
			gap:    "Gap-A4",
			marker: "pkg/actions/effects.go",
			why:    "Gap-A4 webhook side-effects ARE implemented — pkg/actions/effects.go full retry loop + DLQ + action_logs.side_effect_status; rounds 30/31/33/53 sealed retry/outcomes/DLQ/tracing.",
		},
		{
			gap:    "Gap-R2",
			marker: "pkg/oss/stream_ingest.go",
			why:    "Gap-R2 stream ingest IS implemented — pkg/oss/stream_ingest.go + BDDs in stream_ingest_dog003_bdd_test.go / stream_ingest_self102_bdd_test.go cover the dedicated edit-batch ingestion path that bypasses Action rules.",
		},
		{
			gap:    "Gap-T1",
			marker: "pkg/index/mapping_builder.go",
			why:    "Gap-T1 TypeClass driving Bleve index IS implemented — pkg/index/mapping_builder.go reads property.typeclass to choose analyzer / keyword / skip mapping per US-001 / US-012 / US-040.",
		},
		// Round 137 — Gap-Q3 stale: multi-groupBy + accuracy marker
		// end-to-end coverage IS already in place, the PRD wording
		// just hadn't caught up. Pin the fixture path so a future
		// PR that deletes us015 will fail this guard.
		{
			gap:    "Gap-Q3",
			marker: "us015_multi_groupby.json",
			why:    "Gap-Q3 multi-groupBy + accuracy marker coverage IS implemented — test/foundry_parity/us015_multi_groupby.json (105-doc country×freight×orderDate fixture) drives test/integration/aggregation_multigroupby_test.go::TestMultiGroupBy_NorthwindOrders for the ExactValue × FixedWidth × Duration combo; pkg/oss/aggregation/multi_groupby_test.go covers nested key shape / stable order / null keys; accuracy_test.go::TestAggregationAccuracyMarker asserts ACCURATE vs APPROXIMATE across simple avg / standardDeviation / approximatePercentile / groupBy+truncated leaf / count-only / fits-all-docs.",
		},
		// Round 138 — Gap-A3 stale: parameterCompare + AND/OR/NOT
		// composite group criteria + save-time validation + typed
		// SDK exception were all delivered (commits 9bd0f2b /
		// c8bb4ba / a0a8079 / c0bb215 / c7725c1) but the PRD still
		// said "无法表达 cross-field constraints". Pin the
		// implementation type so a future revert is caught loudly.
		{
			gap:    "Gap-A3",
			marker: "parameterCompareValue",
			why:    "Gap-A3 cross-field submission-criteria expressiveness IS implemented — pkg/actions/criteria.go parameterCompareValue (line 69) drives the `case \"parameterCompare\"` dispatch (line 129) for gt/gte/lt/lte/eq/neq comparisons; AND/OR/NOT composite groups round out boolean algebra; admin save validates the criteria tree structurally; SDK ships typed WeaveValidationError + criteria builders (always/parameterMatch/parameterCompare/and_/or_/not_) — only CEL-lite / Goja-embedded forms remain on the Gap-A5 SHOULD layer.",
		},
		// Round 139 — Gap-D3 stale: docs/cli.md action/aggregate/
		// objectset reference sections + body templates + northwind
		// examples landed in commit fc6ef44 (131 doc lines + the
		// cli_docs_bdd_test.go guard), but the PRD still said
		// "建议补". Pin the BDD guard path so the doc cannot be
		// silently regressed.
		{
			gap:    "Gap-D3",
			marker: "cli_docs_bdd_test.go",
			why:    "Gap-D3 CLI action/aggregate/objectset depth IS implemented — commit fc6ef44 added docs/cli.md sections '### weave action apply' (L141) / '### weave aggregate' (L177) / '### weave objectset <load|create-temporary>' (L220) with command reference + body templates + real northwind examples (131 doc lines), plus scripts/ci/cli_docs_bdd_test.go BDD guard that fails loudly if any of those headings disappears.",
		},
		// Round 140 — Gap-D4 stale: MCP completion/complete was
		// delivered (commits 1a1065e Gap-D4 partial + fb5f90c
		// Gap-D4 follow-up wiring ontology-backed CompletionSource)
		// but the PRD's Gap-D4 paragraph still listed only
		// prompts + resources, omitting completion entirely. Pin
		// the source file so a future revert is caught.
		{
			gap:    "Gap-D4",
			marker: "completion_ontology_source.go",
			why:    "Gap-D4 MCP completion/complete protocol method IS implemented (in addition to prompts/list and resources/* listed in the PRD) — pkg/mcp/completion.go dispatches the completion/complete RPC and pkg/mcp/completion_ontology_source.go wires it to the OMS so AI clients see live ontology/objectType apiName suggestions while typing weave://objecttype/<...>/ URIs; BDD coverage in pkg/mcp/completion_bdd_test.go + completion_ontology_source_bdd_test.go (commits 1a1065e partial + fb5f90c follow-up). Only MCP sampling and production auth remain on the SHOULD layer.",
		},
		// Round 141 — Gap-T4 stale: PRD still read "RID 不含 version,
		// 无 branch 概念" even though ALL four suggestions had
		// landed (ontology_branches table in migration 000024 +
		// 000091 parent_tx, RID @vN parser in pkg/rid + Python SDK
		// mirror, X-Weave-Branch header + ?branch= query, and 8
		// versioned-Get endpoints with typed 501 / typed SDK
		// exception). Pin the RID parser fn so a regression on the
		// suffix is caught.
		{
			gap:    "Gap-T4",
			marker: "splitVersionSuffix",
			why:    "Gap-T4 ontology branching + semantic versioning ALL four PRD suggestions ARE implemented — (1) migration 000024_ontology_branches.up.sql lays branch_id / branch_name / ontology_rid / base_version / is_experimental columns plus 000091_ontology_branches_parent_tx for branch lineage; (2) pkg/rid/rid.go's Version field + splitVersionSuffix parser (commit 72b37ba P91 / mirror 07e304e SDK92) makes 'ri.ontology.main.ontology.xyz@v3' lex; (3) pkg/oms/branch_scope.go::BranchHeader + handlers.go:238 honor ?branch= query and X-Weave-Branch header (commit 3716931 Gap-T4 partial, query wins on conflict); (4) 8 Get endpoints reject @vN with 501 VersionedLookupNotSupported (commits 8bc0005 P117 pilot + ed6f78b P119 family), SDK ships typed WeaveVersionedLookupError (commits 265cffd SDK118 + 61b1d80 SDK120 contract), OpenAPI documents the 501 on 7 Get ops (commit 33a8233 P121). Writes still default to HEAD per the PRD's 'avoid real write branches'.",
		},
		// Round 142 — Gap-S1/S2/S3 triple-stale: pkg/security/
		// policy_engine.go exposes Evaluate / AllowedProperties /
		// SetMarkingsEnabled / AllowedForIngest, wired into the OSS
		// pipeline (pkg/oss/service_impl.go) with CEL evaluator,
		// integration + BDD + bench, but the PRD still said
		// "没挂在 query pipeline" / "无" / "不在主链路". Pin all
		// three implementation tokens.
		{
			gap:    "Gap-S1",
			marker: "policy_engine.go",
			why:    "Gap-S1 row-level security IS implemented and wired into the OSS query pipeline — pkg/security/policy_engine.go exposes Engine.Evaluate(ctx, user, oms.ObjectType) (query.Query, error) which pkg/oss/service_impl.go ANDs into the user where clause; CEL DSL in cel_evaluator.go evaluates row predicates; integration tests row_policy_integration_test.go / policy_engine_integration_test.go / row_policy_cel_integration_test.go, aggregate path handlers_aggregate_policy_test.go, BDD rls_cel_us487_bdd_test.go and rls_bench_test.go cover the matrix.",
		},
		{
			gap:    "Gap-S2",
			marker: "AllowedProperties",
			why:    "Gap-S2 column/property-level security IS implemented — pkg/security/policy_engine.go AllowedProperties(ctx, user, ot) []string returns the user-scoped permitted property set and propertyRuleMatches(Rule, *auth.User) drives per-property rule matching; pkg/oss/service_impl.go uses the set to filter WireObject serialization.",
		},
		{
			gap:    "Gap-S3",
			marker: "SetMarkingsEnabled",
			why:    "Gap-S3 Marking evaluation IS implemented and merged into policy_engine (no separate marking_filter.go) — pkg/security/policy_engine.go SetMarkingsEnabled / MarkingsEnabled / AllowedForIngest take user-context markings (from auth.User.Attributes[MarkingsAttributeKey]) and short-circuit both ingest and row-level decisions; pkg/security/auto_marking_test.go covers inheritance.",
		},
		// Round 143 — Gap-Q4 / D1 / D2 / D5 / R1 marker top-up:
		// these five gaps were already ✅ in the PRD but the
		// staleness CI test had no marker for them. D1 and D2 in
		// fact had stale wording too ("raw dict" / "未暴露") which
		// this round refreshed in the same commit. Pin one stable
		// identifier per gap so a future regression on any of the
		// five surfaces shows up red.
		{
			gap:    "Gap-Q4",
			marker: "fusionStrategy",
			why:    "Gap-Q4 nearestNeighbors hybrid surfaces ARE implemented through the MUST line — filter-then-KNN via CandidatePKs, PropertyIdentifiers multi-vector column (round 49), and fusionStrategy 'min' | 'rrf' RRF rank fusion (round 50, k=60). Pinning fusionStrategy guards against regression on the most subtle of the three.",
		},
		{
			gap:    "Gap-D1",
			marker: "ObjectSetBuilder",
			why:    "Gap-D1 Python SDK ObjectSet builder IS implemented (commit a042fa5) — sdk/python/weave_client/objectsets.py::ObjectSetBuilder chains base / filter / search_around / union / intersect / subtract / withProperties Pythonically; sdk/python/tests/test_objectsets.py + test_builders.py exercise it.",
		},
		{
			gap:    "Gap-D2",
			marker: "TimeSeriesAPI",
			why:    "Gap-D2 Python SDK Aggregation / TimeSeries / Attachment all IS implemented — sdk/python/weave_client/aggregation.py::AggregationAPI (commit 863a19e), timeseries.py::TimeSeriesAPI (commit 751d9dc Gap-D2 partial), attachments.py::AttachmentsAPI (commit 66a675d Gap-D2 close-out); sdk/python/tests/test_aggregation_builders.py + test_timeseries.py + test_attachments.py cover the matrix.",
		},
		{
			gap:    "Gap-D5",
			marker: "http_bridge.go",
			why:    "Gap-D5 weave-mcp stdio bridge IS implemented — cmd/weave-mcp/http_bridge.go forwards local stdio JSON-RPC to a running /mcp when WEAVE_MCP_URL is set, passes through WEAVE_MCP_TOKEN / WEAVE_MCP_API_KEY, and honors WEAVE_MCP_HTTP_TIMEOUT; tests cover empty_response_p2a003 + timeout_p2a003. Local-standalone embed mode is the only remaining sub-task.",
		},
		{
			gap:    "Gap-R1",
			marker: "subscribe_sse.go",
			why:    "Gap-R1 client subscription depth IS implemented — pkg/oss/subscribe_sse.go exposes GET /api/v2/ontologies/{ont}/objectSets/{rid}/subscribe with Last-Event-ID + since replay and per-user connection guard, pkg/subscriptions/ mounts the WebSocket /subscriptions/ws endpoint, web/src/hooks/useObjectSetSubscription.ts drives the realtime mode in BrowserPage and ObjectSetLivePage. Remaining ops items (multi-instance fan-out, replay window metrics, reconnect matrix, end-to-end load) are deferred SHOULD-layer hardening.",
		},
		// Round 144 — Gap-Q2 was the most spectacularly stale gap
		// left: PRD still said "跨 link 的聚合未实现" but
		// withProperties has a full 101-line executor +
		// derived-aware aggregation gate + 12 single-hop subtests +
		// 5 formula subtests + reverse / lineage / multi-hop /
		// M2M coverage. This is a Foundry MUST-line crown jewel
		// that had been left looking like a known gap. Pin the
		// executor entry-point.
		{
			gap:    "Gap-Q2",
			marker: "executeWithProperties",
			why:    "Gap-Q2 withProperties cross-link aggregation IS implemented (Foundry derived-property MUST line) — pkg/oss/objectset/executor.go executeWithProperties + executeWithPropertiesPolymorphic (101-line impl) drive forward / reverse / polymorphic derived paths attaching values onto Result.DerivedValues; handler_aggregate_derived.go aggregationNeedsDerivedPath gates the derived-aware aggregation; withproperties_test.go covers count / sum / avg / min / max / reverse count + 6 edge cases (12 subtests); withproperties_formula_test.go covers FullName / arithmetic / multi-DP / validation (5 subtests); withproperties_reverse_test.go locks reverse semantics; aggregate_derived_us382_test.go locks derived-excluded items; handler_lineage_test.go::TestObjectSetLineage_WithPropertiesAggregation pulls derived into lineage. Multi-hop searchAround (US-366) + M2M traversal (US-210) wire into ErrQueryTooLarge. Only custom reducer DSL remains on the SHOULD layer.",
		},
		// Round 145 — Gap-S4 audit breadth: PRD listed 4
		// suggestions (taxonomy unification / SIEM delivery health
		// / retention evidence dashboard / root-hash runbook) but
		// SIEM delivery and root-hash publication ARE implemented
		// (pkg/audit/export + roothash.go::RootHashPublisher per
		// US-266). Pin the publisher type so US-266's tamper-proof
		// anchor cannot be silently removed.
		{
			gap:    "Gap-S4",
			marker: "RootHashPublisher",
			why:    "Gap-S4 audit breadth + hardening IS implemented at the code level — pkg/audit/audit.go + pg_store.go + chain.go + context.go + redaction.go + oms/audited_repository.go form the core write path; pkg/audit/export/{exporter,syslog,s3,batched,tee}.go ship SIEM delivery with batched retry, syslog/S3 sinks, and TeeStore fan-out so internal store + SIEM both receive events; pkg/audit/roothash.go::RootHashPublisher periodically anchors the previous UTC day's hash chain (US-266 tamper-proof). Admin query + retention / export / redaction hooks exposed via cmd/server/audit_retention.go AuditExportConfig. Only the operator-facing batch audit UX and root-hash runbook prose remain on the SHOULD / runbook layer.",
		},
		// Round 146 — Gap-R3 was a real (not stale) gap: the PRD
		// asked for an end-to-end "wipe the Bleve dir → fresh
		// manager → RebuildWithOptions from source → equivalent
		// query results" scenario, and although RebuildWithOptions
		// itself has been in place since US-408 the disaster-path
		// e2e test did NOT exist. We added
		// rehydrate_disaster_recovery_bdd_test.go this round; pin
		// the file so a future delete is loudly caught.
		{
			gap:    "Gap-R3",
			marker: "rehydrate_disaster_recovery_bdd_test.go",
			why:    "Gap-R3 rehydrate path testing matrix IS now end-to-end covered — pkg/index/rehydrate_disaster_recovery_bdd_test.go::TestBDD_Rehydrate_KillBleveDirAndRebuildFromSource (round 146) drives 'index 3 docs → close mgr → os.RemoveAll(dataDir) → new mgr on the same path → RebuildWithOptions from LatestDocumentSource → country=USA returns 1, country=Mexico returns 2 (equivalent to pre-wipe)'. Complements existing rehydrate_test.go (7 EnsureAllIndexes paths) + rebuild_us408_test.go (RebuildMarker lifecycle + 5 stages) + rebuild_test.go (drop+reindex clears stale docs).",
		},
	}

	for _, c := range cases {
		// Locate the gap header to bound the search so we look at
		// the gap's own paragraph, not somewhere else in the doc.
		gapStart := strings.Index(doc, "**"+c.gap+" ")
		if gapStart < 0 {
			t.Errorf("%s: gap header not found in PRD", c.gap)
			continue
		}
		// Bound by the next "**Gap-" header or by document end.
		searchFrom := gapStart
		gapEnd := strings.Index(doc[searchFrom+1:], "**Gap-")
		var paragraph string
		if gapEnd < 0 {
			paragraph = doc[searchFrom:]
		} else {
			paragraph = doc[searchFrom : searchFrom+1+gapEnd]
		}
		if !strings.Contains(paragraph, c.marker) {
			t.Errorf("%s paragraph does not mention %q — %s\n\nparagraph was:\n%s",
				c.gap, c.marker, c.why, paragraph)
		}
	}
}
