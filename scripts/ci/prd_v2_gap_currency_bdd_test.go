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
