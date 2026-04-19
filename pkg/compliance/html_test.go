package compliance

import (
	"strings"
	"testing"
	"time"
)

func sampleReport() *Report {
	return &Report{
		GeneratedAt: time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC),
		WindowFrom:  time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		WindowTo:    time.Date(2026, 4, 19, 0, 0, 0, 0, time.UTC),
		Access: AccessStatistics{
			Total:        42,
			UniqueActors: 7,
			ByAction: []ActionCount{
				{Action: "login", Count: 30},
				{Action: "read", Count: 12},
			},
			TopActors: []ActorCount{
				{ActorID: "user:admin@example.com", Count: 20},
			},
		},
		Markings: MarkingDistribution{
			Total: 1,
			Markings: []MarkingSummary{
				{Name: "PII", DisplayName: "PII", GrantCount: 3},
			},
		},
		Policies: PolicyCoverage{
			ObjectTypesTotal:   4,
			CoveredObjectTypes: 2,
			CoverageRatio:      0.5,
			RowPolicies:        PolicySurface{Total: 1, CoveredObjectTypes: 1},
			ColumnMasks:        PolicySurface{Total: 2, CoveredObjectTypes: 2},
			CellMasks:          PolicySurface{Total: 0, CoveredObjectTypes: 0},
		},
	}
}

func TestRenderHTML_ContainsAllSections(t *testing.T) {
	b, err := RenderHTMLBytes(sampleReport())
	if err != nil {
		t.Fatalf("RenderHTMLBytes: %v", err)
	}
	html := string(b)
	for _, want := range []string{
		"Weave Compliance Report",
		"Access Statistics",
		"Marking Distribution",
		"Policy Coverage",
		"login",
		"PII",
		"50.0%",
		"user:admin@example.com",
		"2026-04-19",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered HTML missing %q", want)
		}
	}
	if !strings.HasPrefix(strings.TrimSpace(html), "<!DOCTYPE html>") {
		t.Error("rendered HTML should start with <!DOCTYPE html>")
	}
}

func TestRenderHTML_NilReport(t *testing.T) {
	if _, err := RenderHTMLBytes(nil); err == nil {
		t.Fatal("expected error for nil report")
	}
}

func TestRenderHTML_EmptySectionsRenderNoDataMessage(t *testing.T) {
	empty := &Report{
		GeneratedAt: time.Now(),
		Access:      AccessStatistics{ByAction: []ActionCount{}, TopActors: []ActorCount{}},
		Markings:    MarkingDistribution{Markings: []MarkingSummary{}},
	}
	b, err := RenderHTMLBytes(empty)
	if err != nil {
		t.Fatalf("RenderHTMLBytes: %v", err)
	}
	html := string(b)
	if !strings.Contains(html, "No events recorded") {
		t.Errorf("expected empty-events message, got %s", html)
	}
	if !strings.Contains(html, "No markings defined") {
		t.Errorf("expected empty-markings message, got %s", html)
	}
}
