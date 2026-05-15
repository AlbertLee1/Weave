package scenarios_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/scenarios"
)

func TestShouldArchive_Given_VariousAgeStatusCombos_When_Asked_Then_OnlyAppliedOrFailedPastWindowQualify(t *testing.T) {
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	old := now.Add(-scenarios.RetentionWindow - time.Hour)
	young := now.Add(-scenarios.RetentionWindow + time.Hour)

	cases := []struct {
		name   string
		s      scenarios.Scenario
		expect bool
	}{
		{"applied + old", scenarios.Scenario{Status: "applied", CreatedAt: old}, true},
		{"failed + old", scenarios.Scenario{Status: "failed", CreatedAt: old}, true},
		{"applied + young", scenarios.Scenario{Status: "applied", CreatedAt: young}, false},
		{"draft + old", scenarios.Scenario{Status: "draft", CreatedAt: old}, false},
		{"frozen + old", scenarios.Scenario{Status: "frozen", CreatedAt: old}, false},
		{"applied + zero CreatedAt", scenarios.Scenario{Status: "applied"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := scenarios.ShouldArchive(c.s, now)
			if got != c.expect {
				t.Errorf("ShouldArchive(%+v) = %v, want %v", c.s, got, c.expect)
			}
		})
	}
}

func TestCompressArchivePayload_RoundTrip(t *testing.T) {
	in := scenarios.ArchivePayload{
		Edits: []scenarios.ScenarioEdit{
			{Seq: 1, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: json.RawMessage(`150`)},
		},
		Overrides: []scenarios.ScenarioOverride{
			{ModelRID: "ri.vertex.main.model.m1", Parameter: "capacity", ObjectID: "JFK", Value: json.RawMessage(`0.5`)},
		},
	}
	blob, err := scenarios.CompressArchivePayload(in)
	if err != nil {
		t.Fatalf("CompressArchivePayload: %v", err)
	}
	if len(blob) == 0 {
		t.Fatal("expected non-empty blob")
	}
	got, err := scenarios.DecompressArchivePayload(blob)
	if err != nil {
		t.Fatalf("DecompressArchivePayload: %v", err)
	}
	// AppliedAt / CreatedAt zero-value on both sides; reflect.DeepEqual is fine.
	if !reflect.DeepEqual(got.Edits, in.Edits) {
		t.Errorf("Edits round-trip mismatch\n got=%+v\nwant=%+v", got.Edits, in.Edits)
	}
	if !reflect.DeepEqual(got.Overrides, in.Overrides) {
		t.Errorf("Overrides round-trip mismatch\n got=%+v\nwant=%+v", got.Overrides, in.Overrides)
	}
}

func TestDecompressArchivePayload_OnInvalidBlob_ReturnsError(t *testing.T) {
	_, err := scenarios.DecompressArchivePayload([]byte("not-gzip"))
	if err == nil {
		t.Fatal("expected error on malformed blob")
	}
}

func TestErrArchived_Error(t *testing.T) {
	e := scenarios.ErrArchived{ScenarioRID: "ri.vertex.main.scenario.s1"}
	if e.Error() == "" || !contains(e.Error(), "s1") {
		t.Errorf("Error() = %q", e.Error())
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
