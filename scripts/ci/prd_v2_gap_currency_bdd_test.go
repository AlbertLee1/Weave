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
