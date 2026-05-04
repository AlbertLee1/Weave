// Multi-language SDK contract test (US-423).
//
// Boots an in-process weave-mock backed by api/openapi.yaml + a small set
// of overrides, then runs each available quickstart (py / ts / go / java)
// against it via WEAVE_BASE_URL and asserts they all surface the same
// canonical lines from the same wire payload.
//
// Each language gates on its toolchain so this stays runnable on a minimal
// laptop: the Go quickstart always runs (Go is the test runner's own
// toolchain); TypeScript needs tsc + node; Python needs python3 + the
// in-tree weave_client SDK; Java needs javac + java. The test fails if no
// language could be exercised — at least one quickstart must run for the
// contract to be meaningful.
package examples_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/mockserver"
)

// canonicalFixture is the wire payload pinned for every endpoint the
// quickstarts exercise. Written once, shared across all four language
// runs — that's the whole point of the contract test.
type canonicalFixture struct {
	OntologyAPIName   string
	OntologyDisplay   string
	ObjectTypeAPIName string
	ObjectTypeDisplay string
	ObjectPrimaryKey  string
	ObjectExtraField  string
	ObjectExtraValue  string
}

func defaultFixture() canonicalFixture {
	return canonicalFixture{
		OntologyAPIName:   "northwind",
		OntologyDisplay:   "Northwind",
		ObjectTypeAPIName: "customers",
		ObjectTypeDisplay: "Customers",
		ObjectPrimaryKey:  "ALFKI",
		ObjectExtraField:  "companyName",
		ObjectExtraValue:  "Alfreds Futterkiste",
	}
}

// fixtureOverrides emits the three mock-server overrides the quickstarts
// depend on. Field shapes intentionally include every property the
// strictest SDK pydantic models require (Ontology.currentVersion,
// ObjectType.primaryKey/status/visibility) so deserialisation never trips
// before the canonical lines reach stdout.
func fixtureOverrides(f canonicalFixture) []mockserver.Override {
	ontologies := mustJSON(map[string]any{
		"data": []map[string]any{
			{
				"rid":            "ri.ontology.main.ontology.nw",
				"apiName":        f.OntologyAPIName,
				"displayName":    f.OntologyDisplay,
				"description":    "Contract-test fixture",
				"currentVersion": 1,
			},
		},
	})
	objectTypes := mustJSON(map[string]any{
		"data": []map[string]any{
			{
				"rid":               "ri.ontology.main.object-type.cust",
				"apiName":           f.ObjectTypeAPIName,
				"displayName":       f.ObjectTypeDisplay,
				"pluralDisplayName": f.ObjectTypeDisplay,
				"primaryKey":        "customerID",
				"status":            "ACTIVE",
				"visibility":        "NORMAL",
			},
		},
	})
	objects := mustJSON(map[string]any{
		"data": []map[string]any{
			{
				"__primaryKey":     f.ObjectPrimaryKey,
				"__apiName":        f.ObjectTypeAPIName,
				"__rid":            "ri.object.main." + f.ObjectTypeAPIName + ".alfki",
				f.ObjectExtraField: f.ObjectExtraValue,
			},
		},
	})
	return []mockserver.Override{
		{Method: "GET", Path: "/api/v2/ontologies", Body: ontologies},
		{Method: "GET", Path: "/api/v2/ontologies/{ontologyApiName}/objectTypes", Body: objectTypes},
		{Method: "GET", Path: "/api/v2/ontologies/{ontologyApiName}/objects/{objectType}", Body: objects},
	}
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// startMockServer boots the in-process mock backed by api/openapi.yaml.
// The httptest.Server is wrapped here so each test gets a clean port and
// owns its own teardown, regardless of whether other tests ran first.
func startMockServer(t *testing.T, fixture canonicalFixture) *httptest.Server {
	t.Helper()
	specPath := filepath.Join("..", "api", "openapi.yaml")
	spec, err := mockserver.LoadSpecFile(specPath)
	if err != nil {
		t.Fatalf("load spec %s: %v", specPath, err)
	}
	handler, err := mockserver.NewHandler(spec, mockserver.Options{
		Overrides: fixtureOverrides(fixture),
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// TestContract_AllSDKsAgreeOnMockResponses runs every available quickstart
// against the same mock server and asserts they all emit the canonical
// fixture lines. At least one language run is required so an environment
// without any toolchain doesn't quietly pass.
func TestContract_AllSDKsAgreeOnMockResponses(t *testing.T) {
	fixture := defaultFixture()
	srv := startMockServer(t, fixture)

	results := map[string]cmdOutput{}

	if out, ok := runGoQuickstartOnMock(t, srv.URL); ok {
		results["go"] = out
	}
	if out, ok := runPythonQuickstartOnMock(t, srv.URL); ok {
		results["python"] = out
	}
	if out, ok := runTypeScriptQuickstartOnMock(t, srv.URL); ok {
		results["typescript"] = out
	}
	if out, ok := runJavaQuickstartOnMock(t, srv.URL); ok {
		results["java"] = out
	}

	if len(results) == 0 {
		t.Fatal("no language toolchain was reachable — contract suite needs >=1 quickstart to run")
	}

	wantOntologyLine := fmt.Sprintf("- %s\t%s", fixture.OntologyAPIName, fixture.OntologyDisplay)
	wantObjectTypeLine := fmt.Sprintf("- %s\t%s", fixture.ObjectTypeAPIName, fixture.ObjectTypeDisplay)

	languages := sortedKeys(results)
	t.Logf("contract suite ran for: %s", strings.Join(languages, ", "))
	for _, lang := range languages {
		out := results[lang]
		assertContains(t, lang, out.stdout, wantOntologyLine)
		assertContains(t, lang, out.stdout, wantObjectTypeLine)
		// Object PK appears in the third section; format varies per
		// language (each prints the row JSON inline), but every SDK
		// MUST surface the primary key value verbatim.
		assertContains(t, lang, out.stdout, fixture.ObjectPrimaryKey)
	}
}

// TestContract_MockOverridesPinKnownFixture verifies the override
// payloads bound to the canonical fixture path templates actually drive
// the mock server's response. Runs unconditionally — gives CI a fast
// failure signal even when no language toolchain is reachable.
func TestContract_MockOverridesPinKnownFixture(t *testing.T) {
	fixture := defaultFixture()
	srv := startMockServer(t, fixture)

	cases := []struct {
		name string
		path string
		want string
	}{
		{"ontologies", "/api/v2/ontologies", fixture.OntologyAPIName},
		{"objectTypes",
			"/api/v2/ontologies/" + fixture.OntologyAPIName + "/objectTypes",
			fixture.ObjectTypeAPIName,
		},
		{"objects",
			"/api/v2/ontologies/" + fixture.OntologyAPIName + "/objects/" + fixture.ObjectTypeAPIName,
			fixture.ObjectPrimaryKey,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := httpGet(t, srv.URL+tc.path)
			if !strings.Contains(body, tc.want) {
				t.Errorf("response body missing %q\nbody:\n%s", tc.want, body)
			}
		})
	}
}

func httpGet(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

func assertContains(t *testing.T, language, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("%s quickstart output missing %q\n--- stdout ---\n%s\n--- end ---",
			language, needle, haystack)
	}
}

func sortedKeys(m map[string]cmdOutput) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// cmdOutput is a transparent struct alias holding a command's captured
// stdout/stderr. Lifted out so each runner returns the same shape.
type cmdOutput struct {
	stdout string
	stderr string
}

// ---------- per-language runners --------------------------------------------

func runGoQuickstartOnMock(t *testing.T, baseURL string) (cmdOutput, bool) {
	t.Helper()
	dir, err := filepath.Abs("go-quickstart")
	if err != nil {
		t.Fatalf("abs go-quickstart: %v", err)
	}
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	cmd.Env = appendEnv(os.Environ(), "WEAVE_BASE_URL="+baseURL, "WEAVE_TOKEN=")
	out, err := runCapture(cmd)
	if err != nil {
		t.Fatalf("go quickstart failed: %v\nstderr:\n%s", err, out.stderr)
	}
	return out, true
}

func runPythonQuickstartOnMock(t *testing.T, baseURL string) (cmdOutput, bool) {
	t.Helper()
	py, err := exec.LookPath("python3")
	if err != nil {
		py, err = exec.LookPath("python")
	}
	if err != nil {
		t.Log("python: skipping — no python3/python on PATH")
		return cmdOutput{}, false
	}
	sdkPath, err := filepath.Abs(filepath.Join("..", "sdk", "python"))
	if err != nil {
		t.Fatalf("abs sdk path: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(sdkPath, "weave_client", "__init__.py")); statErr != nil {
		t.Logf("python: skipping — sdk/python missing (%v)", statErr)
		return cmdOutput{}, false
	}
	check := exec.Command(py, "-c", "import weave_client")
	check.Env = appendEnv(os.Environ(), "PYTHONPATH="+sdkPath)
	if cout, cerr := runCapture(check); cerr != nil {
		t.Logf("python: skipping — weave_client import failed:\nstdout:%s\nstderr:%s", cout.stdout, cout.stderr)
		return cmdOutput{}, false
	}
	dir, err := filepath.Abs("py-quickstart")
	if err != nil {
		t.Fatalf("abs py-quickstart: %v", err)
	}
	cmd := exec.Command(py, "main.py")
	cmd.Dir = dir
	cmd.Env = appendEnv(os.Environ(),
		"PYTHONPATH="+sdkPath,
		"WEAVE_BASE_URL="+baseURL,
		"WEAVE_TOKEN=",
	)
	out, err := runCapture(cmd)
	if err != nil {
		t.Fatalf("python quickstart failed: %v\nstdout:\n%s\nstderr:\n%s", err, out.stdout, out.stderr)
	}
	return out, true
}

func runTypeScriptQuickstartOnMock(t *testing.T, baseURL string) (cmdOutput, bool) {
	t.Helper()
	tsc := findTSC(t)
	if tsc == "" {
		t.Log("typescript: skipping — no tsc binary reachable")
		return cmdOutput{}, false
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Log("typescript: skipping — no node on PATH")
		return cmdOutput{}, false
	}
	dir, err := filepath.Abs("ts-quickstart")
	if err != nil {
		t.Fatalf("abs ts-quickstart: %v", err)
	}
	distDir := filepath.Join(dir, "dist")
	_ = os.RemoveAll(distDir)
	t.Cleanup(func() { _ = os.RemoveAll(distDir) })

	build := exec.Command(tsc, "--project", filepath.Join(dir, "tsconfig.json"))
	build.Dir = dir
	if buildOut, err := runCapture(build); err != nil {
		t.Fatalf("tsc build failed: %v\nstdout:%s\nstderr:%s", err, buildOut.stdout, buildOut.stderr)
	}
	mainJS := filepath.Join(distDir, "main.js")
	if _, err := os.Stat(mainJS); err != nil {
		t.Fatalf("ts build did not emit %s: %v", mainJS, err)
	}
	cmd := exec.Command(node, mainJS)
	cmd.Dir = dir
	cmd.Env = appendEnv(os.Environ(), "WEAVE_BASE_URL="+baseURL, "WEAVE_TOKEN=")
	out, err := runCapture(cmd)
	if err != nil {
		t.Fatalf("ts quickstart failed: %v\nstdout:\n%s\nstderr:\n%s", err, out.stdout, out.stderr)
	}
	return out, true
}

func runJavaQuickstartOnMock(t *testing.T, baseURL string) (cmdOutput, bool) {
	t.Helper()
	javac, err := exec.LookPath("javac")
	if err != nil {
		t.Log("java: skipping — no javac on PATH")
		return cmdOutput{}, false
	}
	java, err := exec.LookPath("java")
	if err != nil {
		t.Log("java: skipping — no java on PATH")
		return cmdOutput{}, false
	}
	srcDir, err := filepath.Abs("java-quickstart")
	if err != nil {
		t.Fatalf("abs java-quickstart: %v", err)
	}
	classesDir := t.TempDir()
	build := exec.Command(javac, "-d", classesDir, filepath.Join(srcDir, "Main.java"))
	if bOut, bErr := runCapture(build); bErr != nil {
		t.Fatalf("javac failed: %v\nstdout:%s\nstderr:%s", bErr, bOut.stdout, bOut.stderr)
	}
	cmd := exec.Command(java, "-cp", classesDir, "Main")
	cmd.Env = appendEnv(os.Environ(), "WEAVE_BASE_URL="+baseURL, "WEAVE_TOKEN=")
	out, err := runCapture(cmd)
	if err != nil {
		t.Fatalf("java quickstart failed: %v\nstdout:\n%s\nstderr:\n%s", err, out.stdout, out.stderr)
	}
	return out, true
}

// ---------- shared helpers -------------------------------------------------

func runCapture(cmd *exec.Cmd) (cmdOutput, error) {
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return cmdOutput{stdout: stdout.String(), stderr: stderr.String()}, err
}

// appendEnv returns os.Environ with extras appended; later entries win
// because os/exec uses the last value seen for a duplicated key. Used so
// each runner can override WEAVE_BASE_URL/WEAVE_TOKEN without dropping the
// caller's PATH and toolchain env.
func appendEnv(env []string, extras ...string) []string {
	out := make([]string, 0, len(env)+len(extras))
	out = append(out, env...)
	out = append(out, extras...)
	return out
}
