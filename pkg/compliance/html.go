package compliance

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
)

// RenderHTML writes a single-file printable HTML document for report to
// w. The template has no external dependencies — all styling is inlined
// in a <style> block so the output renders identically offline or in an
// email attachment. Callers that need machine-readable output should
// marshal the Report as JSON instead.
func RenderHTML(w io.Writer, report *Report) error {
	if report == nil {
		return fmt.Errorf("compliance: nil report")
	}
	return reportTemplate.Execute(w, report)
}

// RenderHTMLBytes is a convenience wrapper that returns the document as
// a byte slice. Used by handlers that need to set Content-Length.
func RenderHTMLBytes(report *Report) ([]byte, error) {
	var buf bytes.Buffer
	if err := RenderHTML(&buf, report); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// reportTemplate is the Go html/template for the printable report. Kept
// intentionally simple — tables, no JS, no external CSS — so the output
// is readable in any browser / email client / PDF-printing agent.
var reportTemplate = template.Must(template.New("report").Funcs(template.FuncMap{
	"percent": func(f float64) string { return fmt.Sprintf("%.1f%%", f*100) },
}).Parse(reportTemplateText))

const reportTemplateText = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>Weave Compliance Report</title>
<style>
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; margin: 2em; color: #222; }
  h1 { border-bottom: 2px solid #333; padding-bottom: .3em; }
  h2 { margin-top: 2em; border-bottom: 1px solid #ccc; padding-bottom: .2em; }
  table { border-collapse: collapse; width: 100%; margin-top: .5em; }
  th, td { border: 1px solid #ddd; padding: .4em .6em; text-align: left; }
  th { background: #f4f4f4; }
  td.num { text-align: right; font-variant-numeric: tabular-nums; }
  .meta { color: #666; font-size: .9em; }
  .section { margin-bottom: 1.5em; }
  .metric { display: inline-block; margin-right: 2em; }
  .metric .value { font-size: 1.8em; font-weight: 600; }
  .metric .label { color: #666; font-size: .85em; text-transform: uppercase; }
</style>
</head>
<body>
<h1>Weave Compliance Report</h1>
<p class="meta">Generated {{.GeneratedAt.Format "2006-01-02 15:04:05 UTC"}}
{{if not .WindowFrom.IsZero}} · window {{.WindowFrom.Format "2006-01-02"}} → {{.WindowTo.Format "2006-01-02"}}{{end}}</p>

<section class="section">
  <h2>Access Statistics</h2>
  <div class="metric"><div class="value">{{.Access.Total}}</div><div class="label">Total events</div></div>
  <div class="metric"><div class="value">{{.Access.UniqueActors}}</div><div class="label">Unique actors</div></div>
  <h3>Events by action</h3>
  {{if .Access.ByAction}}
  <table>
    <thead><tr><th>Action</th><th>Count</th></tr></thead>
    <tbody>
    {{range .Access.ByAction}}
      <tr><td>{{.Action}}</td><td class="num">{{.Count}}</td></tr>
    {{end}}
    </tbody>
  </table>
  {{else}}<p class="meta">No events recorded in window.</p>{{end}}
  <h3>Top actors</h3>
  {{if .Access.TopActors}}
  <table>
    <thead><tr><th>Actor</th><th>Count</th></tr></thead>
    <tbody>
    {{range .Access.TopActors}}
      <tr><td>{{.ActorID}}</td><td class="num">{{.Count}}</td></tr>
    {{end}}
    </tbody>
  </table>
  {{else}}<p class="meta">No actor activity in window.</p>{{end}}
</section>

<section class="section">
  <h2>Marking Distribution</h2>
  <div class="metric"><div class="value">{{.Markings.Total}}</div><div class="label">Markings defined</div></div>
  {{if .Markings.Markings}}
  <table>
    <thead><tr><th>Name</th><th>Display name</th><th>Description</th><th>Grants</th></tr></thead>
    <tbody>
    {{range .Markings.Markings}}
      <tr>
        <td>{{.Name}}</td>
        <td>{{.DisplayName}}</td>
        <td>{{.Description}}</td>
        <td class="num">{{.GrantCount}}</td>
      </tr>
    {{end}}
    </tbody>
  </table>
  {{else}}<p class="meta">No markings defined.</p>{{end}}
</section>

<section class="section">
  <h2>Policy Coverage</h2>
  <div class="metric"><div class="value">{{.Policies.ObjectTypesTotal}}</div><div class="label">Object types</div></div>
  <div class="metric"><div class="value">{{.Policies.CoveredObjectTypes}}</div><div class="label">With at least one policy</div></div>
  <div class="metric"><div class="value">{{percent .Policies.CoverageRatio}}</div><div class="label">Coverage ratio</div></div>
  <table>
    <thead><tr><th>Surface</th><th>Total rules</th><th>Covered object types</th></tr></thead>
    <tbody>
      <tr><td>Row-level policies</td><td class="num">{{.Policies.RowPolicies.Total}}</td><td class="num">{{.Policies.RowPolicies.CoveredObjectTypes}}</td></tr>
      <tr><td>Column-level masks</td><td class="num">{{.Policies.ColumnMasks.Total}}</td><td class="num">{{.Policies.ColumnMasks.CoveredObjectTypes}}</td></tr>
      <tr><td>Cell-level masks</td><td class="num">{{.Policies.CellMasks.Total}}</td><td class="num">{{.Policies.CellMasks.CoveredObjectTypes}}</td></tr>
    </tbody>
  </table>
</section>

</body>
</html>
`
