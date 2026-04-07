package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubServer returns an httptest.Server that maps "METHOD path" -> body.
func stubServer(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		if body, ok := routes[key]; ok {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func runCLIWith(t *testing.T, configDir string, args ...string) (stdout, stderr string, exit int) {
	t.Helper()
	t.Setenv("WEAVE_CONFIG_DIR", configDir)
	var stdoutBuf, stderrBuf bytes.Buffer
	exit = run(args, &stdoutBuf, &stderrBuf)
	return stdoutBuf.String(), stderrBuf.String(), exit
}

func TestCLIWithoutArgsShowsUsage(t *testing.T) {
	tmp := t.TempDir()
	stdout, stderr, exit := runCLIWith(t, tmp)
	if exit != 2 {
		t.Fatalf("exit = %d, want 2", exit)
	}
	combined := stdout + stderr
	if !strings.Contains(combined, "weave") || !strings.Contains(combined, "ontology") {
		t.Fatalf("usage missing keywords: %q", combined)
	}
}

func TestCLIUnknownSubcommandPrintsHelp(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runCLIWith(t, tmp, "doesnotexist")
	if exit == 0 {
		t.Fatalf("expected non-zero exit")
	}
	if !strings.Contains(stderr, "unknown") {
		t.Fatalf("stderr should mention unknown command: %q", stderr)
	}
}

func TestConfigSetThenGetRoundTrips(t *testing.T) {
	tmp := t.TempDir()

	if _, _, exit := runCLIWith(t, tmp, "config", "set", "base_url", "http://api.example"); exit != 0 {
		t.Fatalf("set exit = %d", exit)
	}
	stdout, _, exit := runCLIWith(t, tmp, "config", "get", "base_url")
	if exit != 0 {
		t.Fatalf("get exit = %d", exit)
	}
	if strings.TrimSpace(stdout) != "http://api.example" {
		t.Fatalf("get stdout = %q", stdout)
	}
}

func TestConfigGetWithNoValueShowsAll(t *testing.T) {
	tmp := t.TempDir()
	if _, _, exit := runCLIWith(t, tmp, "config", "set", "base_url", "http://x"); exit != 0 {
		t.Fatalf("set exit")
	}
	if _, _, exit := runCLIWith(t, tmp, "config", "set", "access_token", "tok"); exit != 0 {
		t.Fatalf("set exit")
	}
	stdout, _, exit := runCLIWith(t, tmp, "config", "get")
	if exit != 0 {
		t.Fatalf("exit = %d", exit)
	}
	if !strings.Contains(stdout, "base_url") || !strings.Contains(stdout, "http://x") {
		t.Fatalf("config dump missing values: %q", stdout)
	}
	if !strings.Contains(stdout, "access_token") || !strings.Contains(stdout, "tok") {
		t.Fatalf("config dump missing token: %q", stdout)
	}
}

func TestConfigFileWrittenToExpectedLocation(t *testing.T) {
	tmp := t.TempDir()
	if _, _, exit := runCLIWith(t, tmp, "config", "set", "base_url", "http://here"); exit != 0 {
		t.Fatalf("exit")
	}
	want := filepath.Join(tmp, "config.toml")
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	if !strings.Contains(string(data), `base_url = "http://here"`) {
		t.Fatalf("config file body = %q", string(data))
	}
}

func TestOntologyListUsesConfiguredBaseURL(t *testing.T) {
	srv := stubServer(t, map[string]string{
		"GET /api/v2/ontologies": `{"data":[
			{"rid":"ri.ontology.main.ontology.northwind","apiName":"northwind","displayName":"Northwind","currentVersion":3},
			{"rid":"ri.ontology.main.ontology.chinook","apiName":"chinook","displayName":"Chinook","currentVersion":1}
		]}`,
	})

	tmp := t.TempDir()
	if _, _, exit := runCLIWith(t, tmp, "config", "set", "base_url", srv.URL); exit != 0 {
		t.Fatalf("set exit")
	}
	if _, _, exit := runCLIWith(t, tmp, "config", "set", "access_token", "abc"); exit != 0 {
		t.Fatalf("set exit")
	}

	stdout, stderr, exit := runCLIWith(t, tmp, "ontology", "list")
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
	if !strings.Contains(stdout, "northwind") || !strings.Contains(stdout, "chinook") {
		t.Fatalf("listing missing entries: %q", stdout)
	}
}

func TestOntologyListJSONFormat(t *testing.T) {
	srv := stubServer(t, map[string]string{
		"GET /api/v2/ontologies": `{"data":[{"rid":"ri.x","apiName":"a","displayName":"A","currentVersion":1}]}`,
	})
	tmp := t.TempDir()
	_, _, _ = runCLIWith(t, tmp, "config", "set", "base_url", srv.URL)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "access_token", "tok")
	stdout, stderr, exit := runCLIWith(t, tmp, "ontology", "list", "--json")
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("output not valid JSON: %q (err %v)", stdout, err)
	}
	if len(got) != 1 || got[0]["apiName"] != "a" {
		t.Fatalf("unexpected json: %+v", got)
	}
}

func TestOntologyGetByName(t *testing.T) {
	srv := stubServer(t, map[string]string{
		"GET /api/v2/ontologies/northwind": `{"rid":"ri.ontology.main.ontology.northwind","apiName":"northwind","displayName":"Northwind","currentVersion":7}`,
	})
	tmp := t.TempDir()
	_, _, _ = runCLIWith(t, tmp, "config", "set", "base_url", srv.URL)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "access_token", "tok")
	stdout, _, exit := runCLIWith(t, tmp, "ontology", "get", "northwind")
	if exit != 0 {
		t.Fatalf("exit = %d", exit)
	}
	if !strings.Contains(stdout, "northwind") || !strings.Contains(stdout, "7") {
		t.Fatalf("get output: %q", stdout)
	}
}

func TestObjectListPrintsRows(t *testing.T) {
	srv := stubServer(t, map[string]string{
		"GET /api/v2/ontologies/nw/objects/Customer": `{"data":[
			{"__primaryKey":"ALFKI","customerId":"ALFKI","companyName":"Alfreds"},
			{"__primaryKey":"ANATR","customerId":"ANATR","companyName":"Ana Trujillo"}
		],"totalCount":"2"}`,
	})
	tmp := t.TempDir()
	_, _, _ = runCLIWith(t, tmp, "config", "set", "base_url", srv.URL)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "access_token", "tok")
	stdout, _, exit := runCLIWith(t, tmp, "object", "list", "--ontology", "nw", "--type", "Customer", "--limit", "10")
	if exit != 0 {
		t.Fatalf("exit = %d", exit)
	}
	if !strings.Contains(stdout, "ALFKI") || !strings.Contains(stdout, "ANATR") {
		t.Fatalf("list output: %q", stdout)
	}
}

func TestObjectGetByPrimaryKey(t *testing.T) {
	srv := stubServer(t, map[string]string{
		"GET /api/v2/ontologies/nw/objects/Customer/ALFKI": `{"__primaryKey":"ALFKI","customerId":"ALFKI","companyName":"Alfreds Futterkiste"}`,
	})
	tmp := t.TempDir()
	_, _, _ = runCLIWith(t, tmp, "config", "set", "base_url", srv.URL)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "access_token", "tok")
	stdout, _, exit := runCLIWith(t, tmp, "object", "get", "--ontology", "nw", "--type", "Customer", "--pk", "ALFKI")
	if exit != 0 {
		t.Fatalf("exit = %d", exit)
	}
	if !strings.Contains(stdout, "Alfreds Futterkiste") {
		t.Fatalf("output: %q", stdout)
	}
}

func TestAuthLoginPersistsAccessToken(t *testing.T) {
	srv := stubServer(t, map[string]string{
		"POST /api/auth/login": `{"access_token":"new-token","refresh_token":"r","token_type":"Bearer","expires_in":900,"user":{"id":"u1","email":"a@b","name":"A","roles":["admin"],"ontologyRoles":{}}}`,
	})
	tmp := t.TempDir()
	_, _, _ = runCLIWith(t, tmp, "config", "set", "base_url", srv.URL)
	stdout, stderr, exit := runCLIWith(t, tmp, "auth", "login", "--email", "a@b", "--password", "pw")
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
	if !strings.Contains(stdout, "logged in") && !strings.Contains(stdout, "Logged in") {
		t.Fatalf("stdout = %q", stdout)
	}
	cfgBytes, err := os.ReadFile(filepath.Join(tmp, "config.toml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(cfgBytes), `access_token = "new-token"`) {
		t.Fatalf("token not persisted: %q", string(cfgBytes))
	}
}

func TestAuthLogoutClearsToken(t *testing.T) {
	srv := stubServer(t, map[string]string{
		"POST /api/auth/logout": ``,
	})
	tmp := t.TempDir()
	_, _, _ = runCLIWith(t, tmp, "config", "set", "base_url", srv.URL)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "access_token", "old")
	_, _, exit := runCLIWith(t, tmp, "auth", "logout")
	if exit != 0 {
		t.Fatalf("exit = %d", exit)
	}
	cfgBytes, _ := os.ReadFile(filepath.Join(tmp, "config.toml"))
	if strings.Contains(string(cfgBytes), `access_token = "old"`) {
		t.Fatalf("token not cleared: %q", string(cfgBytes))
	}
}

func TestAuthStatusShowsTokenPresence(t *testing.T) {
	tmp := t.TempDir()
	stdout, _, _ := runCLIWith(t, tmp, "auth", "status")
	if !strings.Contains(stdout, "not logged in") && !strings.Contains(stdout, "no token") {
		t.Fatalf("status with no token: %q", stdout)
	}
	_, _, _ = runCLIWith(t, tmp, "config", "set", "access_token", "abc")
	stdout, _, _ = runCLIWith(t, tmp, "auth", "status")
	if !strings.Contains(stdout, "logged in") && !strings.Contains(stdout, "token set") {
		t.Fatalf("status with token: %q", stdout)
	}
}

func TestObjectListMissingRequiredFlagsErrors(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runCLIWith(t, tmp, "object", "list")
	if exit == 0 {
		t.Fatalf("expected non-zero exit")
	}
	if !strings.Contains(stderr, "ontology") && !strings.Contains(stderr, "required") {
		t.Fatalf("expected required-flag error, got %q", stderr)
	}
}
