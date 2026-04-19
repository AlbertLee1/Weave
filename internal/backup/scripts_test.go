// Package backup_test verifies the operator scripts shipped under
// scripts/{backup,restore,test-restore}.sh exist, are executable, parse
// cleanly under bash, and contain the key invariants the PRD demands —
// pg_dump in backup, pg_restore in restore, the round-trip drop/create
// in test-restore. The tests are pure-static (no Postgres required), so
// they run as part of `go test ./...` on every CI lane.
package backup_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// repoRoot walks up from the test file until it finds go.mod. Mirrors the
// helper in internal/dashboards so both infrastructure-style packages share
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

// scriptPath returns the absolute path to a file under scripts/.
func scriptPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "scripts", name)
}

// TestBackupScripts_Exist verifies all three operator scripts are present.
func TestBackupScripts_Exist(t *testing.T) {
	for _, name := range []string{"backup.sh", "restore.sh", "test-restore.sh"} {
		p := scriptPath(t, name)
		info, err := os.Stat(p)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if info.IsDir() {
			t.Errorf("%s is a directory, want a file", name)
		}
	}
}

// TestBackupScripts_Executable verifies each script has the executable bit
// set so operators can `./scripts/backup.sh` directly. Skipped on Windows
// where unix mode bits are not meaningful.
func TestBackupScripts_Executable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits not meaningful on windows")
	}
	for _, name := range []string{"backup.sh", "restore.sh", "test-restore.sh"} {
		p := scriptPath(t, name)
		info, err := os.Stat(p)
		if err != nil {
			t.Errorf("stat %s: %v", name, err)
			continue
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Errorf("%s mode %v missing executable bit", name, info.Mode().Perm())
		}
	}
}

// TestBackupScripts_BashParse runs `bash -n` on each script to catch syntax
// errors at test time rather than at 3am during a real restore. Skipped if
// bash is not available (Windows CI).
func TestBackupScripts_BashParse(t *testing.T) {
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("bash not in PATH: %v", err)
	}
	for _, name := range []string{"backup.sh", "restore.sh", "test-restore.sh"} {
		p := scriptPath(t, name)
		var stderr bytes.Buffer
		cmd := exec.Command(bashPath, "-n", p)
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Errorf("bash -n %s failed: %v\n%s", name, err, stderr.String())
		}
	}
}

// TestBackupScripts_HaveShebang verifies each script starts with a bash
// shebang so PATH-mediated invocation picks the right interpreter.
func TestBackupScripts_HaveShebang(t *testing.T) {
	want := "#!/usr/bin/env bash"
	for _, name := range []string{"backup.sh", "restore.sh", "test-restore.sh"} {
		data, err := os.ReadFile(scriptPath(t, name))
		if err != nil {
			t.Errorf("read %s: %v", name, err)
			continue
		}
		first := firstLine(data)
		if first != want {
			t.Errorf("%s first line = %q, want %q", name, first, want)
		}
	}
}

// TestBackupScripts_BackupContainsPgDump asserts backup.sh actually invokes
// pg_dump (the PRD acceptance criterion) and tars data/media. A future
// "refactor" that replaces pg_dump with a half-broken pgbackrest wrapper
// would trip this test before it ships.
func TestBackupScripts_BackupContainsPgDump(t *testing.T) {
	data := mustRead(t, scriptPath(t, "backup.sh"))
	mustContain(t, "backup.sh", data, "pg_dump")
	mustContain(t, "backup.sh", data, "media")
	mustContain(t, "backup.sh", data, "WAL_ARCHIVE_DIR")
	mustContain(t, "backup.sh", data, "manifest.json")
}

// TestBackupScripts_RestoreContainsPgRestore asserts restore.sh invokes
// pg_restore and accepts a timestamp argument that defaults to "latest".
func TestBackupScripts_RestoreContainsPgRestore(t *testing.T) {
	data := mustRead(t, scriptPath(t, "restore.sh"))
	mustContain(t, "restore.sh", data, "pg_restore")
	mustContain(t, "restore.sh", data, "PITR_TARGET_TIME")
	mustContain(t, "restore.sh", data, "media.tar.gz")
	mustContain(t, "restore.sh", data, `TARGET="${1:-latest}"`)
}

// TestBackupScripts_TestRestoreCallsBoth asserts test-restore.sh actually
// drives backup.sh + restore.sh end-to-end (per PRD: "scripts/test-restore.sh
// 验证脚本"). String-matching is intentionally loose — any path layout that
// invokes the two scripts via $ROOT/scripts/* satisfies the test.
func TestBackupScripts_TestRestoreCallsBoth(t *testing.T) {
	data := mustRead(t, scriptPath(t, "test-restore.sh"))
	mustContain(t, "test-restore.sh", data, "scripts/backup.sh")
	mustContain(t, "test-restore.sh", data, "scripts/restore.sh")
	mustContain(t, "test-restore.sh", data, "CREATE DATABASE")
	mustContain(t, "test-restore.sh", data, "DROP DATABASE")
}

// TestBackupScripts_SetEuoPipefail asserts each script uses the strict
// shell-error mode. Without it, a failing pg_dump silently produces an
// empty backup file and the operator finds out at restore time.
func TestBackupScripts_SetEuoPipefail(t *testing.T) {
	for _, name := range []string{"backup.sh", "restore.sh", "test-restore.sh"} {
		data := mustRead(t, scriptPath(t, name))
		mustContain(t, name, data, "set -euo pipefail")
	}
}

func firstLine(b []byte) string {
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return data
}

func mustContain(t *testing.T, name string, data []byte, needle string) {
	t.Helper()
	if !bytes.Contains(data, []byte(needle)) {
		t.Errorf("%s does not contain %q", name, needle)
	}
}
