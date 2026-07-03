package where

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blevesearch/bleve/v2"
	// register the English analyzer used by pkg/index for standard text
	_ "github.com/blevesearch/bleve/v2/analysis/lang/en"
)

// --- "interval" operator tests (Foundry SearchJsonQueryV2 parity) ---
//
// Foundry IntervalQuery contract: {"type":"interval","field":"<prop>",
// "rule":{...}} matches the ANALYZED form of text fields with a sub-rule
// tree. The rule union (IntervalQueryRule) is discriminated by "type":
//
//	match             {query, ordered, maxGaps?}  terms of the query
//	allOf             {rules, ordered, maxGaps?}  all sub-rules
//	anyOf             {rules}                     any sub-rule
//	prefixOnLastToken {query}                     ordered terms, last is a prefix
//	fuzzy             {term, fuzziness? (0-2, default 2)}
//
// Weave approximates interval position semantics on Bleve: maxGaps maps to
// the phrase-slop budget, and ordered=true without maxGaps uses the
// MaxPhraseSlop budget. Set/term membership semantics are exact.

func intervalClause(t *testing.T, payload string) *WhereClause {
	t.Helper()
	var clause WhereClause
	if err := json.Unmarshal([]byte(payload), &clause); err != nil {
		t.Fatalf("unmarshal clause: %v", err)
	}
	return &clause
}

// Seed docs (setupTestIndex descriptions):
//
//	1: "software engineer at acme"
//	2: "product manager at globex"
//	3: "senior software engineer"
func TestInterval_RuleFamilies(t *testing.T) {
	idx := setupTestIndex(t)

	tests := []struct {
		name    string
		payload string
		want    []string
	}{
		{
			name: "match ordered finds adjacent terms",
			payload: `{"type":"interval","field":"description",
				"rule":{"type":"match","query":"software engineer","ordered":true}}`,
			want: []string{"1", "3"},
		},
		{
			name: "match ordered without maxGaps tolerates gaps between terms",
			payload: `{"type":"interval","field":"description",
				"rule":{"type":"match","query":"software acme","ordered":true}}`,
			want: []string{"1"},
		},
		{
			name: "match unordered without maxGaps requires all terms anywhere",
			payload: `{"type":"interval","field":"description",
				"rule":{"type":"match","query":"engineer software","ordered":false}}`,
			want: []string{"1", "3"},
		},
		{
			name: "match maxGaps=0 requires adjacency",
			payload: `{"type":"interval","field":"description",
				"rule":{"type":"match","query":"senior engineer","ordered":true,"maxGaps":0}}`,
			want: []string{},
		},
		{
			name: "match maxGaps=1 admits a single-position gap",
			payload: `{"type":"interval","field":"description",
				"rule":{"type":"match","query":"senior engineer","ordered":true,"maxGaps":1}}`,
			want: []string{"3"},
		},
		{
			name: "match single term behaves like an analyzed term match",
			payload: `{"type":"interval","field":"description",
				"rule":{"type":"match","query":"globex","ordered":false}}`,
			want: []string{"2"},
		},
		{
			name: "match query is analyzed (case-insensitive)",
			payload: `{"type":"interval","field":"description",
				"rule":{"type":"match","query":"SOFTWARE Engineer","ordered":true}}`,
			want: []string{"1", "3"},
		},
		{
			name: "anyOf unions its sub-rules",
			payload: `{"type":"interval","field":"description",
				"rule":{"type":"anyOf","rules":[
					{"type":"match","query":"acme","ordered":false},
					{"type":"match","query":"globex","ordered":false}]}}`,
			want: []string{"1", "2"},
		},
		{
			name: "allOf intersects its sub-rules",
			payload: `{"type":"interval","field":"description",
				"rule":{"type":"allOf","ordered":false,"rules":[
					{"type":"match","query":"software","ordered":false},
					{"type":"match","query":"engineer","ordered":false}]}}`,
			want: []string{"1", "3"},
		},
		{
			name: "prefixOnLastToken prefix-matches the final term",
			payload: `{"type":"interval","field":"description",
				"rule":{"type":"prefixOnLastToken","query":"software eng"}}`,
			want: []string{"1", "3"},
		},
		{
			name: "prefixOnLastToken single token is a bare prefix",
			payload: `{"type":"interval","field":"description",
				"rule":{"type":"prefixOnLastToken","query":"prod"}}`,
			want: []string{"2"},
		},
		{
			name: "fuzzy defaults to fuzziness 2",
			payload: `{"type":"interval","field":"description",
				"rule":{"type":"fuzzy","term":"softwar"}}`,
			want: []string{"1", "3"},
		},
		{
			name: "fuzzy with fuzziness 0 requires an exact term",
			payload: `{"type":"interval","field":"description",
				"rule":{"type":"fuzzy","term":"softwar","fuzziness":0}}`,
			want: []string{},
		},
		{
			name: "match empty query matches nothing",
			payload: `{"type":"interval","field":"description",
				"rule":{"type":"match","query":"   ","ordered":false}}`,
			want: []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clause := intervalClause(t, tc.payload)
			ids := searchWithWhere(t, idx, clause)
			assertIDs(t, ids, tc.want)
		})
	}
}

// TestInterval_MatchAnalyzesTermsAgainstFieldAnalyzer pins the behavior the
// HTTP BDD exercises: Weave's real indexes analyze text with the English
// ("en") analyzer, so indexed tokens are STEMMED ("migration" → "migrat").
// Positional interval rules must analyze their query terms with the field's
// analyzer at search time — raw lowercased terms would never equal a
// stemmed token and the rule would silently match nothing.
func TestInterval_MatchAnalyzesTermsAgainstFieldAnalyzer(t *testing.T) {
	indexMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()
	descMapping := bleve.NewTextFieldMapping()
	descMapping.Analyzer = "en"
	docMapping.AddFieldMappingsAt("description", descMapping)
	indexMapping.DefaultMapping = docMapping

	idx, err := bleve.New(filepath.Join(t.TempDir(), "stemmed"), indexMapping)
	if err != nil {
		t.Fatalf("create index: %v", err)
	}
	t.Cleanup(func() { idx.Close() })

	docs := map[string]string{
		"m1": "database migration completed successfully",
		"m2": "database backup rotated",
	}
	for id, desc := range docs {
		if err := idx.Index(id, map[string]interface{}{"description": desc}); err != nil {
			t.Fatalf("index %s: %v", id, err)
		}
	}

	clause := intervalClause(t, `{"type":"interval","field":"description",
		"rule":{"type":"match","query":"database migration","ordered":true}}`)
	assertIDs(t, searchWithWhere(t, idx, clause), []string{"m1"})

	gapClause := intervalClause(t, `{"type":"interval","field":"description",
		"rule":{"type":"match","query":"migration successfully","ordered":true,"maxGaps":1}}`)
	assertIDs(t, searchWithWhere(t, idx, gapClause), []string{"m1"})
}

func TestInterval_InvalidPayloads(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		wantSub string
	}{
		{
			name:    "missing rule rejected",
			payload: `{"type":"interval","field":"description"}`,
			wantSub: "rule",
		},
		{
			name: "unknown rule type rejected",
			payload: `{"type":"interval","field":"description",
				"rule":{"type":"wildcard","query":"a*"}}`,
			wantSub: "unsupported interval rule type",
		},
		{
			name: "anyOf without sub-rules rejected",
			payload: `{"type":"interval","field":"description",
				"rule":{"type":"anyOf","rules":[]}}`,
			wantSub: "anyOf",
		},
		{
			name: "allOf without sub-rules rejected",
			payload: `{"type":"interval","field":"description",
				"rule":{"type":"allOf","ordered":false,"rules":[]}}`,
			wantSub: "allOf",
		},
		{
			name: "nested invalid sub-rule surfaces its position",
			payload: `{"type":"interval","field":"description",
				"rule":{"type":"anyOf","rules":[{"type":"nope"}]}}`,
			wantSub: "unsupported interval rule type",
		},
		{
			name: "fuzzy without term rejected",
			payload: `{"type":"interval","field":"description",
				"rule":{"type":"fuzzy"}}`,
			wantSub: "term",
		},
		{
			name: "fuzzy fuzziness out of range rejected",
			payload: `{"type":"interval","field":"description",
				"rule":{"type":"fuzzy","term":"softwar","fuzziness":3}}`,
			wantSub: "fuzziness",
		},
		{
			name: "negative maxGaps rejected",
			payload: `{"type":"interval","field":"description",
				"rule":{"type":"match","query":"software engineer","ordered":true,"maxGaps":-1}}`,
			wantSub: "maxGaps",
		},
		{
			name: "missing field rejected",
			payload: `{"type":"interval",
				"rule":{"type":"match","query":"software","ordered":false}}`,
			wantSub: "field",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clause := intervalClause(t, tc.payload)
			_, err := ConvertToBleveQuery(clause)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !errors.Is(err, ErrInvalidWhereClause) {
				t.Errorf("error %v must wrap ErrInvalidWhereClause so the handler returns 400", err)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q must mention %q", err.Error(), tc.wantSub)
			}
		})
	}
}
