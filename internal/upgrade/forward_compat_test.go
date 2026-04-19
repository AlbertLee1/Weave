// Package upgrade_test verifies the operator-facing rolling-upgrade
// guarantees for the Weave server: forward-compatible migrations
// (running v(N) and v(N+1) side-by-side never breaks v(N) at the
// schema layer) and the rolling-upgrade drill script that exercises
// the dual-instance handoff. Both surfaces are checked as pure-static
// repo invariants — no Postgres or live Weave instance required, so
// `go test ./internal/upgrade/...` runs in every CI lane.
//
// US-275: Zero-Downtime Upgrade.
package upgrade_test

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// repoRoot walks up from the test file until it finds go.mod. Mirrors the
// helper in internal/backup so both infrastructure-style packages share
// the same anchoring strategy.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := wd
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not locate repo root from %s", wd)
	return ""
}

// listUpMigrations returns the absolute paths of every `*.up.sql` file
// under migrations/, sorted by file name (= migration order).
func listUpMigrations(t *testing.T) []string {
	t.Helper()
	root := repoRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "migrations"))
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		out = append(out, filepath.Join(root, "migrations", e.Name()))
	}
	sort.Strings(out)
	return out
}

// stripSQLComments removes `-- ...` line comments and `/* ... */` block
// comments so the regex scans don't accidentally match the documentation
// inside migration headers (e.g. an explanatory comment that mentions
// `DROP TABLE` would otherwise trip the policy guard).
func stripSQLComments(src []byte) []byte {
	// Strip block comments first. Migrations don't nest /*...*/.
	var buf bytes.Buffer
	i := 0
	for i < len(src) {
		if i+1 < len(src) && src[i] == '/' && src[i+1] == '*' {
			end := bytes.Index(src[i+2:], []byte("*/"))
			if end < 0 {
				break
			}
			i += end + 4
			continue
		}
		buf.WriteByte(src[i])
		i++
	}
	// Strip `-- line comment` segments.
	var out bytes.Buffer
	for _, line := range bytes.Split(buf.Bytes(), []byte{'\n'}) {
		if idx := bytes.Index(line, []byte("--")); idx >= 0 {
			line = line[:idx]
		}
		out.Write(line)
		out.WriteByte('\n')
	}
	return out.Bytes()
}

// allowedNonForwardCompat lists migrations that intentionally introduce
// a breaking schema change and were already rolled out at a deliberate
// downtime window. New entries here MUST be paired with an operator
// runbook entry in docs/upgrade.md.
//
// 000041: functions.version migrated INTEGER → TEXT under a guard. v(N)
// readers fail to scan the new TEXT shape into INTEGER, so this was a
// one-shot breaking deploy (US-217).
var allowedNonForwardCompat = map[string]string{
	"000041_function_semver_version.up.sql": "INTEGER→TEXT type change documented in docs/upgrade.md",
}

// TestForwardCompat_AddColumnUsesIfNotExists asserts every `ADD COLUMN`
// in an up migration carries `IF NOT EXISTS`. Re-running migrate up is
// the simplest forward-compat invariant: an idempotent migration is
// safe to ship in v(N+1) even when v(N) already exists in the cluster.
func TestForwardCompat_AddColumnUsesIfNotExists(t *testing.T) {
	addColumn := regexp.MustCompile(`(?i)ADD\s+COLUMN(\s+IF\s+NOT\s+EXISTS)?`)
	for _, p := range listUpMigrations(t) {
		base := filepath.Base(p)
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("read %s: %v", base, err)
			continue
		}
		body := stripSQLComments(raw)
		matches := addColumn.FindAllSubmatch(body, -1)
		for _, m := range matches {
			if len(m[1]) == 0 {
				t.Errorf("%s: ADD COLUMN without IF NOT EXISTS — break-forward-compat (cannot re-apply migration cleanly).",
					base)
				break
			}
		}
	}
}

// TestForwardCompat_NotNullAddColumnHasDefault asserts every ADD COLUMN
// declared NOT NULL also carries a DEFAULT clause. Without a default the
// migration succeeds against an empty table but fails the moment v(N)
// pods (still running, unaware of the new column) try to INSERT a row
// and PG rejects with `null value in column "<x>" violates not-null`.
func TestForwardCompat_NotNullAddColumnHasDefault(t *testing.T) {
	// Match each `ADD COLUMN ...` clause up to the next comma at the
	// matching parenthesis depth or the closing semicolon. Migrations in
	// this repo declare one column per ADD COLUMN line and end the line
	// with `,` or `;`, so a coarse `[^,;]*` scope captures the column
	// definition reliably.
	addCol := regexp.MustCompile(`(?i)ADD\s+COLUMN(?:\s+IF\s+NOT\s+EXISTS)?\s+\w+[^,;]*`)
	for _, p := range listUpMigrations(t) {
		base := filepath.Base(p)
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("read %s: %v", base, err)
			continue
		}
		body := stripSQLComments(raw)
		clauses := addCol.FindAll(body, -1)
		for _, c := range clauses {
			text := string(c)
			upper := strings.ToUpper(text)
			if !strings.Contains(upper, "NOT NULL") {
				continue
			}
			if !strings.Contains(upper, "DEFAULT") {
				t.Errorf("%s: %q is NOT NULL without DEFAULT — break-forward-compat (v(N) INSERT without column trips not-null violation).",
					base, strings.TrimSpace(text))
			}
		}
	}
}

// TestForwardCompat_NoDestructiveSchemaOpsInUp asserts no up migration
// drops or renames a column or table. Both operations would cause v(N)
// readers to fail mid-deploy ("column does not exist", "relation does
// not exist"). Two-phase rollouts MUST land the destructive op in a
// later release after every reader has been upgraded; the down.sql is
// available for that step.
func TestForwardCompat_NoDestructiveSchemaOpsInUp(t *testing.T) {
	dropCol := regexp.MustCompile(`(?i)DROP\s+COLUMN`)
	dropTable := regexp.MustCompile(`(?i)DROP\s+TABLE`)
	renameCol := regexp.MustCompile(`(?i)RENAME\s+COLUMN`)
	renameTable := regexp.MustCompile(`(?i)RENAME\s+TO\b|RENAME\s+TABLE`)

	for _, p := range listUpMigrations(t) {
		base := filepath.Base(p)
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("read %s: %v", base, err)
			continue
		}
		body := stripSQLComments(raw)
		for _, hit := range []struct {
			re   *regexp.Regexp
			name string
		}{
			{dropCol, "DROP COLUMN"},
			{dropTable, "DROP TABLE"},
			{renameCol, "RENAME COLUMN"},
			{renameTable, "RENAME TABLE / RENAME TO"},
		} {
			if hit.re.Find(body) == nil {
				continue
			}
			if reason, ok := allowedNonForwardCompat[base]; ok {
				t.Logf("%s: %s allowed by exception (%s)", base, hit.name, reason)
				continue
			}
			t.Errorf("%s: contains %s in up migration — destructive ops break v(N) readers; defer to a later release once all instances are upgraded.",
				base, hit.name)
		}
	}
}

// TestForwardCompat_ColumnTypeChangeAllowlisted captures the one
// migration that changes a column type inline (000041 INTEGER→TEXT).
// Any future migration that tries the same trick must land in the
// allowlist alongside an operator-runbook entry.
func TestForwardCompat_ColumnTypeChangeAllowlisted(t *testing.T) {
	alterType := regexp.MustCompile(`(?i)ALTER\s+COLUMN\s+\w+\s+TYPE`)
	for _, p := range listUpMigrations(t) {
		base := filepath.Base(p)
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("read %s: %v", base, err)
			continue
		}
		body := stripSQLComments(raw)
		if alterType.Find(body) == nil {
			continue
		}
		if _, ok := allowedNonForwardCompat[base]; !ok {
			t.Errorf("%s: ALTER COLUMN ... TYPE is not forward-compatible. Add an entry to allowedNonForwardCompat in this test AND a runbook in docs/upgrade.md before merging.",
				base)
		}
	}
}

// TestForwardCompat_DocumentedInUpgradeDoc asserts the operator-facing
// runbook exists and mentions every entry in allowedNonForwardCompat.
// Future contributors who add an exception MUST also document it in the
// runbook so SREs running the rolling upgrade have a clear pre-flight
// list of breaking deploys to schedule a maintenance window for.
func TestForwardCompat_DocumentedInUpgradeDoc(t *testing.T) {
	docPath := filepath.Join(repoRoot(t), "docs", "upgrade.md")
	data, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("docs/upgrade.md must exist for US-275: %v", err)
	}
	for migration := range allowedNonForwardCompat {
		if !bytes.Contains(data, []byte(migration)) {
			t.Errorf("docs/upgrade.md does not reference exception %s — every allowedNonForwardCompat entry needs a runbook line.",
				migration)
		}
	}
	// Sanity: the runbook should mention the dual-probe split too.
	for _, want := range []string{"/health/live", "/health/ready", "rolling-upgrade.sh"} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("docs/upgrade.md missing reference to %q", want)
		}
	}
}
