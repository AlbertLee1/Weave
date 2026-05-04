package reports

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/audit"
)

func sampleEvents() []audit.AuditEvent {
	base := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	return []audit.AuditEvent{
		{ActorID: "user:a", Action: "login", Timestamp: base},
		{ActorID: "user:a", Action: "login_failed", Timestamp: base.Add(time.Minute)},
		{ActorID: "user:b", Action: "logout", Timestamp: base.Add(2 * time.Minute)},
		{ActorID: "admin", Action: "role_assigned", Timestamp: base.Add(3 * time.Minute)},
		{ActorID: "admin", Action: "marking_grant", Timestamp: base.Add(4 * time.Minute)},
		{ActorID: "admin", Action: "ontology_create", Timestamp: base.Add(5 * time.Minute)},
		{ActorID: "admin", Action: "ontology_update", Timestamp: base.Add(6 * time.Minute)},
		{ActorID: "system", Action: "audit_inspect", Timestamp: base.Add(7 * time.Minute)},
		{ActorID: "user:c", Action: "gdpr_export", Timestamp: base.Add(8 * time.Minute)},
		{ActorID: "user:d", Action: "totally_unknown_action", Timestamp: base.Add(9 * time.Minute)},
		{ActorID: "user:d", Action: "another_unknown", Timestamp: base.Add(10 * time.Minute)},
	}
}

func findControl(report *SOC2Report, id string) *ControlEvidence {
	for i := range report.Controls {
		if report.Controls[i].Control.ID == id {
			return &report.Controls[i]
		}
	}
	return nil
}

func TestClassifier_ClassifiesAcrossControls(t *testing.T) {
	cls := NewClassifier(nil)
	cases := []struct {
		action string
		want   string
	}{
		{"login", "CC6.1"},
		{"logout", "CC6.1"},
		{"sso_redirect", "CC6.1"},
		{"role_assigned", "CC6.3"},
		{"permission_grant", "CC6.3"},
		{"marking_grant", "CC6.3"}, // longer match wins over CC6.7's "marking_"
		{"marking_create", "CC6.7"},
		{"policy_update", "CC6.7"},
		{"audit_redact", "CC7.2"},
		{"compliance_report_generated", "CC7.2"},
		{"ontology_create", "CC8.1"},
		{"function_publish", "CC8.1"},
		{"gdpr_export", "CC9.2"},
	}
	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			idx := cls.Classify(tc.action)
			if idx < 0 {
				t.Fatalf("Classify(%q) returned -1; want %s", tc.action, tc.want)
			}
			got := cls.Controls()[idx].ID
			if got != tc.want {
				t.Errorf("Classify(%q): want %s, got %s", tc.action, tc.want, got)
			}
		})
	}
}

func TestClassifier_UnknownActionReturnsMinusOne(t *testing.T) {
	cls := NewClassifier(nil)
	if got := cls.Classify("totally_unknown_action"); got != -1 {
		t.Errorf("unknown action: want -1, got %d", got)
	}
	if got := cls.Classify(""); got != -1 {
		t.Errorf("empty action: want -1, got %d", got)
	}
}

func TestClassifier_LongestPrefixWins(t *testing.T) {
	// Custom mapping where one control's prefix is a strict prefix of
	// another's. The longer prefix MUST win regardless of declaration
	// order.
	custom := []ControlMapping{
		{ID: "GENERIC", Title: "General", ActionPrefixes: []string{"function"}},
		{ID: "SPECIFIC", Title: "Specific", ActionPrefixes: []string{"function_publish"}},
	}
	cls := NewClassifier(custom)
	idx := cls.Classify("function_publish_v2")
	if idx < 0 {
		t.Fatalf("Classify returned -1")
	}
	if cls.Controls()[idx].ID != "SPECIFIC" {
		t.Errorf("longest prefix should win; got %s", cls.Controls()[idx].ID)
	}
	idx = cls.Classify("function_other")
	if idx < 0 {
		t.Fatalf("Classify returned -1")
	}
	if cls.Controls()[idx].ID != "GENERIC" {
		t.Errorf("non-matching specific prefix should fall through to generic; got %s",
			cls.Controls()[idx].ID)
	}
}

func TestClassifier_NilMappingDefaultsToSOC2(t *testing.T) {
	cls := NewClassifier(nil)
	if len(cls.Controls()) != len(DefaultSOC2Controls) {
		t.Errorf("nil mapping should default to DefaultSOC2Controls; got len=%d",
			len(cls.Controls()))
	}
}

func TestBuildSOC2Report_BucketEventsByControl(t *testing.T) {
	events := sampleEvents()
	r := BuildSOC2Report(events, nil, nil, time.Time{}, time.Time{})
	if r == nil {
		t.Fatal("nil report")
	}
	if r.Standard != "SOC2" {
		t.Errorf("Standard: want SOC2, got %q", r.Standard)
	}
	// Every default control + the synthesised OTHER bucket.
	if len(r.Controls) != len(DefaultSOC2Controls)+1 {
		t.Errorf("controls len: want %d, got %d",
			len(DefaultSOC2Controls)+1, len(r.Controls))
	}

	cc6_1 := findControl(r, "CC6.1")
	if cc6_1 == nil {
		t.Fatal("CC6.1 missing")
	}
	if cc6_1.EventCount != 3 {
		t.Errorf("CC6.1 event count: want 3, got %d", cc6_1.EventCount)
	}
	if cc6_1.UniqueActors != 2 {
		t.Errorf("CC6.1 unique actors: want 2, got %d", cc6_1.UniqueActors)
	}

	cc8_1 := findControl(r, "CC8.1")
	if cc8_1 == nil {
		t.Fatal("CC8.1 missing")
	}
	if cc8_1.EventCount != 2 {
		t.Errorf("CC8.1 event count: want 2 (ontology_create + ontology_update), got %d",
			cc8_1.EventCount)
	}

	other := findControl(r, UnclassifiedControlID)
	if other == nil {
		t.Fatal("OTHER bucket missing")
	}
	if other.EventCount != 2 {
		t.Errorf("OTHER count: want 2, got %d", other.EventCount)
	}
}

func TestBuildSOC2Report_EmptyEventsStillEmitsAllControls(t *testing.T) {
	r := BuildSOC2Report(nil, nil, nil, time.Time{}, time.Time{})
	if len(r.Controls) != len(DefaultSOC2Controls)+1 {
		t.Errorf("empty events: controls len: want %d, got %d",
			len(DefaultSOC2Controls)+1, len(r.Controls))
	}
	for _, c := range r.Controls {
		if c.EventCount != 0 {
			t.Errorf("control %s: want 0 events, got %d", c.Control.ID, c.EventCount)
		}
		if c.UniqueActors != 0 {
			t.Errorf("control %s: want 0 actors, got %d", c.Control.ID, c.UniqueActors)
		}
		if c.SampleEvents != nil {
			t.Errorf("control %s: want nil sample events, got %d", c.Control.ID, len(c.SampleEvents))
		}
	}
}

func TestBuildSOC2Report_SampleEventCappedAndChronological(t *testing.T) {
	base := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	// Build 10 login events with monotone timestamps but in REVERSE
	// order so we can assert the sort.
	var events []audit.AuditEvent
	for i := 9; i >= 0; i-- {
		events = append(events, audit.AuditEvent{
			ActorID:   "user:" + string(rune('a'+i)),
			Action:    "login",
			Timestamp: base.Add(time.Duration(i) * time.Minute),
		})
	}
	r := BuildSOC2Report(events, nil, nil, time.Time{}, time.Time{})
	cc := findControl(r, "CC6.1")
	if cc == nil {
		t.Fatal("CC6.1 missing")
	}
	if cc.EventCount != 10 {
		t.Errorf("EventCount: want 10, got %d", cc.EventCount)
	}
	if len(cc.SampleEvents) != MaxSampleEventsPerControl {
		t.Errorf("SampleEvents: want %d, got %d",
			MaxSampleEventsPerControl, len(cc.SampleEvents))
	}
	for i := 1; i < len(cc.SampleEvents); i++ {
		if cc.SampleEvents[i].Timestamp.Before(cc.SampleEvents[i-1].Timestamp) {
			t.Errorf("samples not in chronological order at %d: %v / %v",
				i, cc.SampleEvents[i-1].Timestamp, cc.SampleEvents[i].Timestamp)
		}
	}
}

func TestBuildSOC2Report_WindowAndStandardPropagate(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	r := BuildSOC2Report(nil, nil, nil, from, to)
	if !r.WindowFrom.Equal(from) {
		t.Errorf("WindowFrom: want %v, got %v", from, r.WindowFrom)
	}
	if !r.WindowTo.Equal(to) {
		t.Errorf("WindowTo: want %v, got %v", to, r.WindowTo)
	}
	if r.GeneratedAt.IsZero() {
		t.Error("GeneratedAt should not be zero")
	}
}

func TestRenderPDF_ProducesValidPDFBytes(t *testing.T) {
	r := BuildSOC2Report(sampleEvents(), nil, &PostureSummary{
		AccessTotal:        11,
		UniqueActors:       5,
		MarkingsTotal:      3,
		ObjectTypesTotal:   10,
		CoveredObjectTypes: 6,
		CoverageRatio:      0.6,
		RowPolicyTotal:     2,
		ColumnMaskTotal:    1,
		CellMaskTotal:      3,
	}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))

	var buf bytes.Buffer
	if err := RenderPDF(&buf, r); err != nil {
		t.Fatalf("RenderPDF: %v", err)
	}

	if !IsValidPDF(buf.Bytes()) {
		t.Fatalf("output does not look like PDF: first bytes = %q",
			truncate(buf.String(), 16))
	}
	if buf.Len() < 1024 {
		t.Errorf("PDF too small (%d bytes); expected substantive content", buf.Len())
	}
}

func TestRenderPDF_NilReportReturnsError(t *testing.T) {
	if err := RenderPDF(&bytes.Buffer{}, nil); err == nil {
		t.Fatal("RenderPDF(nil): want error, got nil")
	}
}

func TestRenderPDF_HandlesUnicodeAndLongStrings(t *testing.T) {
	// Make sure neither ASCII clipping nor multibyte clipping break the
	// renderer. Using a long mixed CJK + ASCII action exercises both
	// truncate() arms and the gofpdf path that emits Cell content.
	long := strings.Repeat("函", 80) + "_publish"
	events := []audit.AuditEvent{
		{
			ActorID:     long,
			Action:      "function_" + long,
			ResourceRID: "ri.functions.main.function." + long,
			Timestamp:   time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
		},
	}
	r := BuildSOC2Report(events, nil, nil, time.Time{}, time.Time{})
	var buf bytes.Buffer
	if err := RenderPDF(&buf, r); err != nil {
		t.Fatalf("RenderPDF unicode: %v", err)
	}
	if !IsValidPDF(buf.Bytes()) {
		t.Fatal("unicode PDF output not valid")
	}
}

func TestTruncate_Boundaries(t *testing.T) {
	cases := []struct {
		s    string
		max  int
		want string
	}{
		{"abc", 5, "abc"},
		{"abc", 3, "abc"},
		{"abcdef", 4, "abc…"},
		{"abc", 0, ""},
		{"abc", 1, "…"},
		{"a你好b", 3, "a你…"},
		{"a你好b", 5, "a你好b"},
	}
	for _, tc := range cases {
		got := truncate(tc.s, tc.max)
		if got != tc.want {
			t.Errorf("truncate(%q, %d): want %q, got %q", tc.s, tc.max, tc.want, got)
		}
	}
}

func TestFormatWindow_HandlesZeroBounds(t *testing.T) {
	got := formatWindow(time.Time{}, time.Time{})
	if got != "— → —" {
		t.Errorf("zero bounds: want '— → —', got %q", got)
	}
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	got = formatWindow(t1, t2)
	if got != "2026-01-01 → 2026-04-01" {
		t.Errorf("populated bounds: got %q", got)
	}
}

func TestIsValidPDF_DetectsHeader(t *testing.T) {
	if !IsValidPDF([]byte("%PDF-1.4\n")) {
		t.Error("expected valid PDF header to be detected")
	}
	if IsValidPDF([]byte("Hello")) {
		t.Error("non-PDF content reported as valid")
	}
	if IsValidPDF([]byte("")) {
		t.Error("empty content reported as valid")
	}
	if IsValidPDF([]byte("%PD")) {
		t.Error("truncated header reported as valid")
	}
}
