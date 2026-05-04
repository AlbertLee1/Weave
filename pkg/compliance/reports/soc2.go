// Package reports renders compliance evidence bundles. soc2.go classifies
// audit events against SOC2 / ISO27001 control families and emits a
// printable PDF (US-442). The classifier is data-driven via the
// Classifier value so an operator deploying with custom action vocabulary
// can override the default mapping without touching the rendering code.
package reports

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/jung-kurt/gofpdf"

	"github.com/liyang/weave/pkg/audit"
)

// ControlMapping is one row in the SOC2 / ISO27001 control taxonomy used
// to bucket audit events. ID is the canonical control identifier (e.g.
// "CC6.1"). ActionPrefixes match the Action field of an AuditEvent
// case-insensitively; the longest matching prefix wins so a generic prefix
// like "function" does not steal traffic from a specific "function_publish".
type ControlMapping struct {
	// ID is the canonical control identifier (e.g. "CC6.1" for SOC2 or
	// "A.5.15" for ISO27001).
	ID string
	// Title is the human-readable control name.
	Title string
	// Description is a one-paragraph explanation of what the control
	// covers; rendered into the PDF beneath the ID + Title.
	Description string
	// Standards lists the standards this row maps to (e.g. "SOC2",
	// "ISO27001"). Multiple standards can share one mapping when a
	// single Weave action provides evidence for both.
	Standards []string
	// ActionPrefixes is the set of audit Action prefixes that count as
	// evidence for this control. Matched case-insensitively.
	ActionPrefixes []string
}

// MaxSampleEventsPerControl caps how many sample events the report
// embeds in each control's evidence section. Auditors want a couple of
// witnesses, not the full event log.
const MaxSampleEventsPerControl = 5

// UnclassifiedControlID is the synthesised bucket for audit events whose
// Action does not match any ControlMapping prefix. The PDF emits this
// section last and the SDK clients can detect it for quality alerting.
const UnclassifiedControlID = "OTHER"

// DefaultSOC2Controls is the canonical SOC2 / ISO27001 control taxonomy
// shipped with Weave. It maps the SOC2 Trust Services Common Criteria
// (CC1..CC9) to action-prefix witnesses; the ISO27001 column is the
// closest Annex A control reference for cross-mapping. Operators with
// custom action vocabulary can supply their own []ControlMapping to
// NewClassifier without modifying this list.
//
// Sorted longest-prefix-first within each control's prefix list so the
// classifier's stable longest-prefix tiebreak is independent of slice
// order.
var DefaultSOC2Controls = []ControlMapping{
	{
		ID:          "CC6.1",
		Title:       "Logical and Physical Access — Authentication",
		Description: "Authentication events including login, logout, multi-factor, and OAuth/SSO flows. Demonstrates that the system requires authenticated identity before granting access.",
		Standards:   []string{"SOC2", "ISO27001:A.5.15", "ISO27001:A.5.16"},
		ActionPrefixes: []string{
			"login", "logout", "sso_", "oauth_", "mfa_", "session_",
			"token_issued", "token_revoked",
		},
	},
	{
		ID:          "CC6.2",
		Title:       "Logical and Physical Access — User Provisioning",
		Description: "User account creation, modification, and deactivation. Establishes that access to the system is granted, modified, and revoked through controlled processes.",
		Standards:   []string{"SOC2", "ISO27001:A.5.16", "ISO27001:A.5.18"},
		ActionPrefixes: []string{
			"user_created", "user_updated", "user_deleted", "user_disabled",
			"user_invite", "user_password",
		},
	},
	{
		ID:          "CC6.3",
		Title:       "Logical and Physical Access — Authorization",
		Description: "Role assignment, permission grants, and group membership changes. Demonstrates that authorization decisions are based on documented role / group definitions.",
		Standards:   []string{"SOC2", "ISO27001:A.5.18"},
		ActionPrefixes: []string{
			"role_", "permission_", "group_", "marking_grant", "marking_revoke",
		},
	},
	{
		ID:          "CC6.7",
		Title:       "Logical and Physical Access — Information Protection",
		Description: "Data masking, row-level policies, marking definitions, and column-level redaction. Demonstrates protection of confidential data in transit, at rest, and in use.",
		Standards:   []string{"SOC2", "ISO27001:A.5.12", "ISO27001:A.5.13"},
		ActionPrefixes: []string{
			"marking_", "policy_", "mask_", "row_policy_", "column_mask_",
			"cell_mask_",
		},
	},
	{
		ID:          "CC7.2",
		Title:       "System Operations — Detection and Monitoring",
		Description: "Audit log inspection, compliance report generation, and anomaly investigation. Demonstrates ongoing detection of security events.",
		Standards:   []string{"SOC2", "ISO27001:A.5.25", "ISO27001:A.5.28"},
		ActionPrefixes: []string{
			"audit_", "compliance_", "alert_", "anomaly_",
		},
	},
	{
		ID:          "CC8.1",
		Title:       "Change Management",
		Description: "Ontology, ActionType, Function, and pipeline changes. Establishes that changes to the production system follow a controlled and audited process.",
		Standards:   []string{"SOC2", "ISO27001:A.8.32"},
		ActionPrefixes: []string{
			"ontology_", "object_type_", "action_type_", "function_",
			"pipeline_", "branch_", "merge_", "schema_",
		},
	},
	{
		ID:          "CC9.2",
		Title:       "Risk Mitigation — Data Subject Rights",
		Description: "GDPR-style data subject access, export, and deletion flows. Demonstrates the operator's ability to honour regulatory requests within a defined window.",
		Standards:   []string{"SOC2", "ISO27001:A.5.34", "GDPR:Art.15-17"},
		ActionPrefixes: []string{
			"gdpr_", "data_export", "data_delete", "subject_request",
		},
	},
}

// PostureSummary is the optional cover-page summary section. The
// compliance handler builds this from its existing Report value before
// invoking RenderPDF; keeping the type here avoids an import cycle
// between pkg/compliance and pkg/compliance/reports.
type PostureSummary struct {
	AccessTotal        int
	UniqueActors       int
	MarkingsTotal      int
	ObjectTypesTotal   int
	CoveredObjectTypes int
	CoverageRatio      float64
	RowPolicyTotal     int
	ColumnMaskTotal    int
	CellMaskTotal      int
}

// SOC2Report is the per-control evidence bundle assembled from a slice of
// audit events plus an optional PostureSummary. Auditors get the per-
// control breakdown; operators get the summary section for at-a-glance
// posture.
type SOC2Report struct {
	// GeneratedAt is the instant the report was assembled, UTC.
	GeneratedAt time.Time
	// WindowFrom / WindowTo define the inclusive event window. Zero
	// values mean "since beginning" / "as of now" and render as such.
	WindowFrom time.Time
	WindowTo   time.Time
	// Standard is the report's title flavour (default "SOC2"). When the
	// operator wants an ISO27001 cover page they pass "ISO27001" here.
	Standard string
	// Controls is the per-control evidence in the canonical mapping
	// order. Always non-nil; controls with zero matching events still
	// appear so an auditor can see which evidence is missing.
	Controls []ControlEvidence
	// Summary is the optional posture snapshot. nil omits that section
	// from the cover page.
	Summary *PostureSummary
}

// ControlEvidence is one entry in SOC2Report.Controls.
type ControlEvidence struct {
	// Control is a copy of the source mapping (so renderers don't
	// retain a pointer that could mutate underneath them).
	Control ControlMapping
	// EventCount is the total number of audit events that mapped to
	// this control during the window.
	EventCount int
	// UniqueActors is the number of distinct ActorIDs whose events
	// landed in this bucket.
	UniqueActors int
	// SampleEvents is the first MaxSampleEventsPerControl events for
	// this control, sorted by Timestamp ASC (oldest first) so an
	// auditor reading top-to-bottom sees a chronological narrative.
	SampleEvents []audit.AuditEvent
}

// Classifier maps audit events to controls. The default constructor uses
// DefaultSOC2Controls; operators can pass a custom slice to override.
type Classifier struct {
	controls []ControlMapping
	// prefixOrder is the (lowered prefix, control index) tuple list
	// sorted longest-prefix-first so the matcher's tiebreak is
	// "specific over generic" regardless of mapping declaration order.
	prefixes []prefixEntry
}

type prefixEntry struct {
	prefix       string
	controlIndex int
}

// NewClassifier returns a classifier over controls. When controls is nil
// or empty the default SOC2 mapping is used so callers don't need a
// special-case for the common path.
func NewClassifier(controls []ControlMapping) *Classifier {
	if len(controls) == 0 {
		controls = DefaultSOC2Controls
	}
	c := &Classifier{controls: controls}
	for i, m := range controls {
		for _, p := range m.ActionPrefixes {
			c.prefixes = append(c.prefixes, prefixEntry{
				prefix:       strings.ToLower(p),
				controlIndex: i,
			})
		}
	}
	sort.Slice(c.prefixes, func(i, j int) bool {
		// Longer prefixes win ties so "function_publish" beats "function_".
		if len(c.prefixes[i].prefix) != len(c.prefixes[j].prefix) {
			return len(c.prefixes[i].prefix) > len(c.prefixes[j].prefix)
		}
		return c.prefixes[i].prefix < c.prefixes[j].prefix
	})
	return c
}

// Classify returns the index of the matching control in the classifier's
// controls slice, or -1 when no prefix matches the action. Empty action
// strings always return -1.
func (c *Classifier) Classify(action string) int {
	if action == "" {
		return -1
	}
	a := strings.ToLower(action)
	for _, p := range c.prefixes {
		if strings.HasPrefix(a, p.prefix) {
			return p.controlIndex
		}
	}
	return -1
}

// Controls returns the underlying mapping. Callers must NOT mutate the
// returned slice.
func (c *Classifier) Controls() []ControlMapping {
	return c.controls
}

// BuildSOC2Report classifies events under the supplied classifier and
// composes the per-control evidence bundle. Pass nil for cls to use the
// default classifier. summary is optional — when non-nil it appears as
// the cover-page posture summary in the rendered PDF.
//
// The report ALWAYS includes every control from the classifier's mapping
// (with zero events when there were no matches) so the PDF is shape-
// stable across calls — auditors comparing two reports can spot missing
// evidence by the control row's count rather than its presence.
func BuildSOC2Report(events []audit.AuditEvent, cls *Classifier, summary *PostureSummary, from, to time.Time) *SOC2Report {
	if cls == nil {
		cls = NewClassifier(nil)
	}
	mapping := cls.Controls()

	// Bucket by control index; maintain a parallel actor-set per bucket.
	bucketEvents := make([][]audit.AuditEvent, len(mapping))
	bucketActors := make([]map[string]struct{}, len(mapping))
	for i := range bucketActors {
		bucketActors[i] = make(map[string]struct{})
	}
	var unclassified []audit.AuditEvent
	unclassifiedActors := make(map[string]struct{})

	for _, evt := range events {
		idx := cls.Classify(evt.Action)
		if idx < 0 {
			unclassified = append(unclassified, evt)
			if evt.ActorID != "" {
				unclassifiedActors[evt.ActorID] = struct{}{}
			}
			continue
		}
		bucketEvents[idx] = append(bucketEvents[idx], evt)
		if evt.ActorID != "" {
			bucketActors[idx][evt.ActorID] = struct{}{}
		}
	}

	controls := make([]ControlEvidence, 0, len(mapping)+1)
	for i, m := range mapping {
		controls = append(controls, ControlEvidence{
			Control:      m,
			EventCount:   len(bucketEvents[i]),
			UniqueActors: len(bucketActors[i]),
			SampleEvents: pickSampleEvents(bucketEvents[i]),
		})
	}
	// Always emit the OTHER bucket so unclassified evidence isn't
	// silently dropped. When there are no unclassified events the row's
	// count is zero — same shape as a control with no matches.
	controls = append(controls, ControlEvidence{
		Control: ControlMapping{
			ID:          UnclassifiedControlID,
			Title:       "Unclassified Events",
			Description: "Audit events whose action did not match any control mapping. A non-zero count here is a hint to extend the classifier with the new vocabulary.",
			Standards:   []string{"-"},
		},
		EventCount:   len(unclassified),
		UniqueActors: len(unclassifiedActors),
		SampleEvents: pickSampleEvents(unclassified),
	})

	return &SOC2Report{
		GeneratedAt: time.Now().UTC(),
		WindowFrom:  from.UTC(),
		WindowTo:    to.UTC(),
		Standard:    "SOC2",
		Controls:    controls,
		Summary:     summary,
	}
}

// pickSampleEvents returns the first MaxSampleEventsPerControl events
// from xs sorted by Timestamp ASC (oldest first). Returns nil when xs is
// empty so the JSON envelope marshals as null/[] consistently.
func pickSampleEvents(xs []audit.AuditEvent) []audit.AuditEvent {
	if len(xs) == 0 {
		return nil
	}
	sorted := make([]audit.AuditEvent, len(xs))
	copy(sorted, xs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.Before(sorted[j].Timestamp)
	})
	if len(sorted) > MaxSampleEventsPerControl {
		sorted = sorted[:MaxSampleEventsPerControl]
	}
	return sorted
}

// RenderPDF writes the SOC2 evidence PDF for r to w. Errors propagate
// from gofpdf; the writer is left in an indeterminate state on error.
func RenderPDF(w io.Writer, r *SOC2Report) error {
	if r == nil {
		return fmt.Errorf("reports: nil SOC2Report")
	}
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(15, 15, 15)
	pdf.SetAutoPageBreak(true, 15)
	pdf.SetTitle(fmt.Sprintf("Weave %s Evidence Report", reportStandard(r)), false)
	pdf.SetCreator("Weave Compliance Generator", false)

	renderCoverPage(pdf, r)
	renderControlSections(pdf, r)

	return pdf.Output(w)
}

// reportStandard returns the report's titleable standard name, defaulting
// to "SOC2" when unset so renderers don't have to nil-check.
func reportStandard(r *SOC2Report) string {
	if r.Standard == "" {
		return "SOC2"
	}
	return r.Standard
}

func renderCoverPage(pdf *gofpdf.Fpdf, r *SOC2Report) {
	pdf.AddPage()
	pdf.SetFont("Helvetica", "B", 22)
	pdf.SetTextColor(20, 20, 20)
	pdf.CellFormat(0, 14, fmt.Sprintf("Weave %s Evidence Report", reportStandard(r)),
		"", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 11)
	pdf.SetTextColor(80, 80, 80)
	pdf.CellFormat(0, 8,
		fmt.Sprintf("Generated %s",
			r.GeneratedAt.UTC().Format("2006-01-02 15:04:05 UTC")),
		"", 1, "L", false, 0, "")
	pdf.CellFormat(0, 8, "Window: "+formatWindow(r.WindowFrom, r.WindowTo),
		"", 1, "L", false, 0, "")
	pdf.Ln(4)

	pdf.SetTextColor(20, 20, 20)
	pdf.SetFont("Helvetica", "B", 13)
	pdf.CellFormat(0, 8, "Control Coverage Summary", "", 1, "L", false, 0, "")
	pdf.Ln(2)

	// Summary table: ID | Title | Events | Actors
	pdf.SetFillColor(240, 240, 240)
	pdf.SetFont("Helvetica", "B", 10)
	pdf.CellFormat(22, 7, "Control", "1", 0, "L", true, 0, "")
	pdf.CellFormat(110, 7, "Title", "1", 0, "L", true, 0, "")
	pdf.CellFormat(25, 7, "Events", "1", 0, "R", true, 0, "")
	pdf.CellFormat(23, 7, "Actors", "1", 1, "R", true, 0, "")
	pdf.SetFont("Helvetica", "", 9)
	for _, c := range r.Controls {
		pdf.CellFormat(22, 6, c.Control.ID, "1", 0, "L", false, 0, "")
		pdf.CellFormat(110, 6, truncate(c.Control.Title, 70), "1", 0, "L", false, 0, "")
		pdf.CellFormat(25, 6, fmt.Sprintf("%d", c.EventCount), "1", 0, "R", false, 0, "")
		pdf.CellFormat(23, 6, fmt.Sprintf("%d", c.UniqueActors), "1", 1, "R", false, 0, "")
	}

	if r.Summary != nil {
		renderPostureSummary(pdf, r.Summary)
	}
}

func renderPostureSummary(pdf *gofpdf.Fpdf, s *PostureSummary) {
	pdf.Ln(6)
	pdf.SetFont("Helvetica", "B", 13)
	pdf.CellFormat(0, 8, "Posture Snapshot", "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	pdf.CellFormat(0, 6, fmt.Sprintf("Audit events in window: %d (across %d unique actors)",
		s.AccessTotal, s.UniqueActors), "", 1, "L", false, 0, "")
	pdf.CellFormat(0, 6, fmt.Sprintf("Markings defined: %d",
		s.MarkingsTotal), "", 1, "L", false, 0, "")
	coverage := s.CoverageRatio * 100
	pdf.CellFormat(0, 6, fmt.Sprintf(
		"Object types: %d total / %d covered by at least one policy (%.1f%%)",
		s.ObjectTypesTotal, s.CoveredObjectTypes, coverage),
		"", 1, "L", false, 0, "")
	pdf.CellFormat(0, 6, fmt.Sprintf(
		"Row policies: %d  Column masks: %d  Cell masks: %d",
		s.RowPolicyTotal, s.ColumnMaskTotal,
		s.CellMaskTotal),
		"", 1, "L", false, 0, "")
}

func renderControlSections(pdf *gofpdf.Fpdf, r *SOC2Report) {
	for _, c := range r.Controls {
		pdf.AddPage()
		pdf.SetFont("Helvetica", "B", 16)
		pdf.SetTextColor(20, 20, 20)
		pdf.CellFormat(0, 10, fmt.Sprintf("%s — %s", c.Control.ID, c.Control.Title),
			"", 1, "L", false, 0, "")

		if len(c.Control.Standards) > 0 {
			pdf.SetFont("Helvetica", "I", 9)
			pdf.SetTextColor(110, 110, 110)
			pdf.CellFormat(0, 6, "Standards: "+strings.Join(c.Control.Standards, ", "),
				"", 1, "L", false, 0, "")
		}
		pdf.Ln(2)

		pdf.SetFont("Helvetica", "", 10)
		pdf.SetTextColor(40, 40, 40)
		pdf.MultiCell(0, 5, c.Control.Description, "", "L", false)
		pdf.Ln(2)

		pdf.SetFont("Helvetica", "B", 11)
		pdf.SetTextColor(20, 20, 20)
		pdf.CellFormat(0, 7, fmt.Sprintf("Evidence: %d events / %d unique actors",
			c.EventCount, c.UniqueActors), "", 1, "L", false, 0, "")

		if len(c.SampleEvents) == 0 {
			pdf.SetFont("Helvetica", "I", 10)
			pdf.SetTextColor(110, 110, 110)
			pdf.CellFormat(0, 6, "No events recorded for this control in the report window.",
				"", 1, "L", false, 0, "")
			continue
		}

		pdf.Ln(1)
		pdf.SetFillColor(240, 240, 240)
		pdf.SetFont("Helvetica", "B", 9)
		pdf.SetTextColor(20, 20, 20)
		pdf.CellFormat(38, 6, "Timestamp (UTC)", "1", 0, "L", true, 0, "")
		pdf.CellFormat(45, 6, "Actor", "1", 0, "L", true, 0, "")
		pdf.CellFormat(45, 6, "Action", "1", 0, "L", true, 0, "")
		pdf.CellFormat(52, 6, "Resource", "1", 1, "L", true, 0, "")

		pdf.SetFont("Helvetica", "", 8)
		for _, evt := range c.SampleEvents {
			ts := evt.Timestamp.UTC().Format("2006-01-02 15:04:05")
			pdf.CellFormat(38, 5, ts, "1", 0, "L", false, 0, "")
			pdf.CellFormat(45, 5, truncate(evt.ActorID, 26), "1", 0, "L", false, 0, "")
			pdf.CellFormat(45, 5, truncate(evt.Action, 26), "1", 0, "L", false, 0, "")
			pdf.CellFormat(52, 5, truncate(evt.ResourceRID, 32), "1", 1, "L", false, 0, "")
		}
	}
}

// formatWindow renders an inclusive [from, to] window for the cover page.
// Zero values become "—" so an audit reader sees the open-ended bound
// rather than a misleading 1970-01-01.
func formatWindow(from, to time.Time) string {
	return fmt.Sprintf("%s → %s", formatWindowBound(from), formatWindowBound(to))
}

func formatWindowBound(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.UTC().Format("2006-01-02")
}

// truncate clips s to max runes, suffixing "…" when clipped. Operates on
// runes rather than bytes so multibyte characters don't get split.
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	return string(rs[:max-1]) + "…"
}

// IsValidPDF reports whether b begins with the PDF magic header. Callers
// asserting "the renderer produced something valid" should use this in
// tests rather than parsing the document.
func IsValidPDF(b []byte) bool {
	return len(b) >= 4 && string(b[:4]) == "%PDF"
}
