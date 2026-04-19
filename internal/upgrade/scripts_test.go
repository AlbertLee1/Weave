// US-275: rolling-upgrade.sh static checks. Mirrors the style used by
// internal/backup/scripts_test.go — pure-static (no live server / PG
// required) so the script invariants are gated at PR time rather than
// discovered during a real upgrade window.

package upgrade_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

const rollupScript = "rolling-upgrade.sh"

func scriptPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "scripts", name)
}

// TestRollingUpgrade_Exists verifies the operator script ships in the repo.
func TestRollingUpgrade_Exists(t *testing.T) {
	info, err := os.Stat(scriptPath(t, rollupScript))
	if err != nil {
		t.Fatalf("scripts/%s missing: %v", rollupScript, err)
	}
	if info.IsDir() {
		t.Fatalf("scripts/%s is a directory, want a file", rollupScript)
	}
}

// TestRollingUpgrade_Executable asserts the chmod +x bit is committed.
// Without this, an `apt-get` fresh checkout will fail to invoke the
// script as `./scripts/rolling-upgrade.sh` and the operator hits a
// "permission denied" right when they need the drill the most.
func TestRollingUpgrade_Executable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits not meaningful on windows")
	}
	info, err := os.Stat(scriptPath(t, rollupScript))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("scripts/%s mode %v missing executable bit", rollupScript, info.Mode().Perm())
	}
}

// TestRollingUpgrade_BashParse runs `bash -n` so syntax errors are
// caught at test time rather than mid-drill.
func TestRollingUpgrade_BashParse(t *testing.T) {
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("bash not in PATH: %v", err)
	}
	var stderr bytes.Buffer
	cmd := exec.Command(bashPath, "-n", scriptPath(t, rollupScript))
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Errorf("bash -n %s failed: %v\n%s", rollupScript, err, stderr.String())
	}
}

// TestRollingUpgrade_HaveShebang asserts the script uses the
// portable `/usr/bin/env bash` shebang (matches backup.sh /
// restore.sh / test-restore.sh).
func TestRollingUpgrade_HaveShebang(t *testing.T) {
	want := "#!/usr/bin/env bash"
	data, err := os.ReadFile(scriptPath(t, rollupScript))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if first := firstLine(data); first != want {
		t.Errorf("%s first line = %q, want %q", rollupScript, first, want)
	}
}

// TestRollingUpgrade_StrictMode asserts `set -euo pipefail` so a half
// of the dual-instance drill failing visibly fails the whole script.
func TestRollingUpgrade_StrictMode(t *testing.T) {
	data, err := os.ReadFile(scriptPath(t, rollupScript))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Contains(data, []byte("set -euo pipefail")) {
		t.Errorf("%s missing `set -euo pipefail`", rollupScript)
	}
}

// TestRollingUpgrade_DualInstanceInvariants asserts the script actually
// embodies the dual-probe / dual-port handoff drill the PRD demands. We
// check for the script text rather than running it because spinning up
// real Weave instances inside a unit test would balloon CI time and
// require Postgres / NATS — both of which the static check renders
// unnecessary. Substring drift is the failure mode worth catching: a
// future "refactor" that drops one of these guards should fail this
// test before it ships.
func TestRollingUpgrade_DualInstanceInvariants(t *testing.T) {
	data, err := os.ReadFile(scriptPath(t, rollupScript))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, want := range []string{
		"BIN_OLD",            //旧实例二进制
		"BIN_NEW",            // 新实例二进制
		"OLD_PORT",           // 旧端口
		"NEW_PORT",           // 新端口
		"/health/live",       // liveness 探针 (separated from ready)
		"/health/ready",      // readiness 探针
		"trap cleanup",       // 防僵尸进程
		"SIGTERM",            // 优雅停机
		"handoff",            // 切换窗口
	} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("%s does not contain %q — invariant lost", rollupScript, want)
		}
	}
}

func firstLine(b []byte) string {
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}
