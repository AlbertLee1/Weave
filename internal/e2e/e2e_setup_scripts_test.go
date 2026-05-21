package e2e_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBDD_E2ESetupVerifiesNorthwindObjectTypesAfterSeed(t *testing.T) {
	script := mustRead(t, scriptPath(t, "e2e-setup.sh"))
	source := string(script)

	for _, required := range []string{
		"verify_seeded_object_types",
		"/api/v2/ontologies/northwind/objectTypes",
		`"apiName"`,
		"customer",
		"order",
		"product",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("scripts/e2e-setup.sh must contain %q for the post-seed objectTypes probe", required)
		}
	}

	seedCall := strings.Index(source, "test/fixtures/e2e_seed.sh")
	verifyCall := strings.LastIndex(source, "verify_seeded_object_types")
	if seedCall == -1 {
		t.Fatal("scripts/e2e-setup.sh must invoke test/fixtures/e2e_seed.sh")
	}
	if verifyCall == -1 {
		t.Fatal("scripts/e2e-setup.sh must invoke verify_seeded_object_types after seeding")
	}
	if verifyCall < seedCall {
		t.Fatal("scripts/e2e-setup.sh must verify objectTypes after e2e_seed.sh completes")
	}
}

func TestE2ESetupScript_BashParse(t *testing.T) {
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("bash not in PATH: %v", err)
	}

	var stderr bytes.Buffer
	cmd := exec.Command(bashPath, "-n", scriptPath(t, "e2e-setup.sh"))
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Errorf("bash -n e2e-setup.sh failed: %v\n%s", err, stderr.String())
	}
}

func scriptPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "scripts", name)
}

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

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return data
}
