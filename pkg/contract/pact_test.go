package contract

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPact_ParsesMinimal(t *testing.T) {
	raw := []byte(`{
        "consumer": {"name": "weave-python-sdk"},
        "provider": {"name": "weave-server"},
        "interactions": [
            {
                "description": "GET /health returns ok",
                "request": {"method": "GET", "path": "/health"},
                "response": {"status": 200, "body": {"status": "ok"}}
            }
        ]
    }`)
	p, err := LoadPactBytes(raw)
	if err != nil {
		t.Fatalf("LoadPactBytes: %v", err)
	}
	if p.Consumer.Name != "weave-python-sdk" {
		t.Errorf("consumer = %q", p.Consumer.Name)
	}
	if p.Provider.Name != "weave-server" {
		t.Errorf("provider = %q", p.Provider.Name)
	}
	if got := len(p.Interactions); got != 1 {
		t.Fatalf("interactions = %d, want 1", got)
	}
	in := p.Interactions[0]
	if in.Request.Method != "GET" || in.Request.Path != "/health" {
		t.Errorf("request = %+v", in.Request)
	}
	if in.Response.Status != 200 {
		t.Errorf("status = %d", in.Response.Status)
	}
}

func TestLoadPact_FromFile(t *testing.T) {
	path := filepath.Join("testdata", "smoke.pact.json")
	p, err := LoadPact(path)
	if err != nil {
		t.Fatalf("LoadPact: %v", err)
	}
	if len(p.Interactions) == 0 {
		t.Fatal("no interactions loaded")
	}
}

func TestLoadPact_RejectsInvalidJSON(t *testing.T) {
	if _, err := LoadPactBytes([]byte("not json")); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoadPact_RequiresMethodAndPath(t *testing.T) {
	raw := []byte(`{
        "consumer": {"name":"c"}, "provider": {"name":"p"},
        "interactions": [{"description":"x", "request": {}, "response": {"status": 200}}]
    }`)
	if _, err := LoadPactBytes(raw); err == nil || !strings.Contains(err.Error(), "method") {
		t.Fatalf("expected method-required error, got %v", err)
	}
}

func TestMatcherRule_UnmarshalShape(t *testing.T) {
	raw := []byte(`{
        "$.id":   {"match": "type", "value": "string"},
        "$.code": {"match": "regex", "value": "^[A-Z]{2}[0-9]+$"},
        "$.timestamp": {"match": "presence"}
    }`)
	var m map[string]MatcherRule
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["$.id"].Match != "type" || m["$.id"].Value != "string" {
		t.Errorf("type matcher = %+v", m["$.id"])
	}
	if m["$.code"].Match != "regex" {
		t.Errorf("regex matcher = %+v", m["$.code"])
	}
	if m["$.timestamp"].Match != "presence" {
		t.Errorf("presence matcher = %+v", m["$.timestamp"])
	}
}
