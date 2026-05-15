// Unit tests for the parts of VertexService that don't require a PG
// connection: option setters, NewVertexService defaults, and the pure
// SQL helpers aggSQL / pgInterval. The PG-bound Query path is covered
// by the integration tests in vertex_service_test.go.

package timeseries

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNewVertexService_Given_NoOptions_When_Constructed_Then_DefaultsApply(t *testing.T) {
	s := NewVertexService(nil)
	if s == nil {
		t.Fatal("NewVertexService returned nil")
	}
	if s.missingDataWarningHours != 24 {
		t.Errorf("default missingDataWarningHours = %d, want 24", s.missingDataWarningHours)
	}
	if s.overlay != nil {
		t.Errorf("expected nil overlay by default, got %#v", s.overlay)
	}
}

func TestNewVertexService_Given_WithMissingDataWarningHours_When_Set_Then_Override(t *testing.T) {
	s := NewVertexService(nil, WithMissingDataWarningHours(6))
	if s.missingDataWarningHours != 6 {
		t.Errorf("missingDataWarningHours = %d, want 6", s.missingDataWarningHours)
	}
	// Zero disables the warning per documented behaviour.
	s0 := NewVertexService(nil, WithMissingDataWarningHours(0))
	if s0.missingDataWarningHours != 0 {
		t.Errorf("expected 0 to be honoured, got %d", s0.missingDataWarningHours)
	}
}

type fakeOverlay struct{ called bool }

func (f *fakeOverlay) GetWindowedScalarOverride(_ context.Context, _, _, _ string) (*float64, error) {
	f.called = true
	return nil, nil
}

func TestNewVertexService_Given_WithScenarioOverlay_When_Set_Then_Attached(t *testing.T) {
	ov := &fakeOverlay{}
	s := NewVertexService(nil, WithScenarioOverlay(ov))
	if s.overlay == nil {
		t.Fatal("overlay not attached")
	}
	attached, ok := s.overlay.(*fakeOverlay)
	if !ok || attached != ov {
		t.Errorf("overlay mismatch: got %#v, want %#v", s.overlay, ov)
	}
}

func TestAggSQL_Given_KnownAgg_When_Mapped_Then_ExpectedExpression(t *testing.T) {
	cases := map[Agg]string{
		AggAvg:  "AVG(value)",
		AggMin:  "MIN(value)",
		AggMax:  "MAX(value)",
		AggSum:  "SUM(value)",
		AggLast: "last(value, ts)",
	}
	for in, want := range cases {
		got, err := aggSQL(in)
		if err != nil {
			t.Errorf("aggSQL(%q) returned error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("aggSQL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAggSQL_Given_UnsupportedAgg_When_Mapped_Then_Error(t *testing.T) {
	_, err := aggSQL(Agg("HISTOGRAM"))
	if err == nil {
		t.Fatal("expected error on unsupported agg")
	}
	if !strings.Contains(err.Error(), "HISTOGRAM") {
		t.Errorf("error %q should name the unsupported agg", err.Error())
	}
}

func TestPgInterval_Given_KnownDuration_When_Rendered_Then_MicrosecondLiteral(t *testing.T) {
	cases := map[time.Duration]string{
		time.Second:            "1000000 microseconds",
		5 * time.Minute:        "300000000 microseconds",
		2 * time.Hour:          "7200000000 microseconds",
		750 * time.Millisecond: "750000 microseconds",
	}
	for d, want := range cases {
		if got := pgInterval(d); got != want {
			t.Errorf("pgInterval(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestErrScenarioNotFound_Error_NonEmpty(t *testing.T) {
	if ErrScenarioNotFound == nil || ErrScenarioNotFound.Error() == "" {
		t.Errorf("ErrScenarioNotFound sentinel should be set with a message, got %v", ErrScenarioNotFound)
	}
}
