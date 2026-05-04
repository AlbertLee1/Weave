package contract

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func samplePact() *Pact {
	return &Pact{
		Consumer: Participant{Name: "weave-python-sdk"},
		Provider: Participant{Name: "weave-server"},
		Interactions: []Interaction{{
			Description: "GET /health",
			Request:     Request{Method: "GET", Path: "/health"},
			Response:    Response{Status: 200, Body: json.RawMessage(`{"status":"alive"}`)},
		}},
	}
}

// fakeBroker captures every request the BrokerClient sends so the wire
// shape can be asserted without a real Pact Broker instance.
type fakeBroker struct {
	t        *testing.T
	mu       sync.Mutex
	requests []recordedBrokerRequest
	server   *httptest.Server
	// fail is non-nil to short-circuit the next request to a 4xx/5xx.
	fail func(*http.Request) (int, string)
	// pacts indexed by (provider, consumer) → raw JSON the latest GET
	// should return.
	pacts map[string][]byte
}

type recordedBrokerRequest struct {
	method      string
	path        string
	body        []byte
	contentType string
	auth        string
}

func newFakeBroker(t *testing.T) *fakeBroker {
	t.Helper()
	fb := &fakeBroker{t: t, pacts: map[string][]byte{}}
	fb.server = httptest.NewServer(http.HandlerFunc(fb.handle))
	t.Cleanup(fb.server.Close)
	return fb
}

func (fb *fakeBroker) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	fb.mu.Lock()
	fb.requests = append(fb.requests, recordedBrokerRequest{
		method:      r.Method,
		path:        r.URL.Path,
		body:        body,
		contentType: r.Header.Get("Content-Type"),
		auth:        r.Header.Get("Authorization"),
	})
	fail := fb.fail
	fb.mu.Unlock()
	if fail != nil {
		code, msg := fail(r)
		http.Error(w, msg, code)
		return
	}
	switch r.Method {
	case http.MethodPut:
		// /pacts/provider/{p}/consumer/{c}/version/{v}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) >= 6 && parts[0] == "pacts" {
			provider, consumer := parts[2], parts[4]
			fb.mu.Lock()
			fb.pacts[provider+"|"+consumer] = body
			fb.mu.Unlock()
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"_links":{}}`))
	case http.MethodGet:
		// /pacts/provider/{p}/latest → list-of-latest-pacts envelope.
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) == 4 && parts[0] == "pacts" && parts[3] == "latest" {
			fb.mu.Lock()
			defer fb.mu.Unlock()
			refs := []map[string]any{}
			for key := range fb.pacts {
				toks := strings.SplitN(key, "|", 2)
				refs = append(refs, map[string]any{
					"href": fb.server.URL + "/pacts/provider/" + toks[0] +
						"/consumer/" + toks[1] + "/latest",
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"_links": map[string]any{"pacts": refs}})
			return
		}
		// /pacts/provider/{p}/consumer/{c}/latest
		if len(parts) == 6 && parts[0] == "pacts" && parts[5] == "latest" {
			provider, consumer := parts[2], parts[4]
			fb.mu.Lock()
			body, ok := fb.pacts[provider+"|"+consumer]
			fb.mu.Unlock()
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
			return
		}
		http.NotFound(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (fb *fakeBroker) snapshot() []recordedBrokerRequest {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	out := make([]recordedBrokerRequest, len(fb.requests))
	copy(out, fb.requests)
	return out
}

func TestBroker_PublishPact_PutsCanonicalURL(t *testing.T) {
	fb := newFakeBroker(t)
	client := NewBrokerClient(fb.server.URL, BrokerClientOptions{})

	if err := client.PublishPact(samplePact(), "1.2.3"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	reqs := fb.snapshot()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 broker request, got %d", len(reqs))
	}
	got := reqs[0]
	wantPath := "/pacts/provider/weave-server/consumer/weave-python-sdk/version/1.2.3"
	if got.method != http.MethodPut {
		t.Errorf("method = %q, want PUT", got.method)
	}
	if got.path != wantPath {
		t.Errorf("path = %q, want %q", got.path, wantPath)
	}
	if got.contentType != "application/json" {
		t.Errorf("content-type = %q, want application/json", got.contentType)
	}
	// Body must round-trip back to the same Pact.
	var got2 Pact
	if err := json.Unmarshal(got.body, &got2); err != nil {
		t.Fatalf("decode published body: %v", err)
	}
	if got2.Consumer.Name != "weave-python-sdk" || got2.Provider.Name != "weave-server" {
		t.Errorf("decoded consumer/provider mismatch: %+v / %+v", got2.Consumer, got2.Provider)
	}
}

func TestBroker_PublishPact_PropagatesAuthHeader(t *testing.T) {
	fb := newFakeBroker(t)
	client := NewBrokerClient(fb.server.URL, BrokerClientOptions{
		AuthHeader: "Bearer test-token",
	})
	if err := client.PublishPact(samplePact(), "1.0.0"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	reqs := fb.snapshot()
	if len(reqs) != 1 || reqs[0].auth != "Bearer test-token" {
		t.Errorf("auth header missing: %+v", reqs)
	}
}

func TestBroker_PublishPact_NormalisesBaseURL(t *testing.T) {
	fb := newFakeBroker(t)
	client := NewBrokerClient(fb.server.URL+"/", BrokerClientOptions{})
	if err := client.PublishPact(samplePact(), "1.0.0"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	reqs := fb.snapshot()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request: %+v", reqs)
	}
	// Must not have produced a path with a doubled slash.
	if strings.Contains(reqs[0].path, "//") {
		t.Errorf("publish doubled the slash: %s", reqs[0].path)
	}
}

func TestBroker_PublishPact_HTTPErrorSurfaces(t *testing.T) {
	fb := newFakeBroker(t)
	fb.fail = func(*http.Request) (int, string) { return 409, "conflict" }
	client := NewBrokerClient(fb.server.URL, BrokerClientOptions{})
	err := client.PublishPact(samplePact(), "1.0.0")
	if err == nil {
		t.Fatal("expected error on 409 from broker, got nil")
	}
	var be *BrokerError
	if !errors.As(err, &be) || be.Status != 409 {
		t.Errorf("expected BrokerError(409), got %v", err)
	}
}

func TestBroker_PublishPact_ValidatesInput(t *testing.T) {
	client := NewBrokerClient("http://example.invalid", BrokerClientOptions{})
	tests := []struct {
		name    string
		pact    *Pact
		version string
		want    string
	}{
		{"nil", nil, "1.0.0", "pact is required"},
		{"missing consumer", &Pact{Provider: Participant{Name: "p"}}, "1.0.0", "consumer.name"},
		{"missing provider", &Pact{Consumer: Participant{Name: "c"}}, "1.0.0", "provider.name"},
		{"missing version", samplePact(), "", "version is required"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := client.PublishPact(tc.pact, tc.version)
			if err == nil {
				t.Fatalf("expected error mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err.Error(), tc.want)
			}
		})
	}
}

func TestBroker_FetchProviderPacts_ReturnsAllConsumers(t *testing.T) {
	fb := newFakeBroker(t)
	client := NewBrokerClient(fb.server.URL, BrokerClientOptions{})

	for _, consumer := range []string{"weave-python-sdk", "weave-ts-sdk", "weave-go-sdk"} {
		pact := samplePact()
		pact.Consumer.Name = consumer
		if err := client.PublishPact(pact, "1.0.0"); err != nil {
			t.Fatalf("publish %s: %v", consumer, err)
		}
	}
	got, err := client.FetchProviderPacts("weave-server")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 pacts, got %d", len(got))
	}
	consumers := map[string]bool{}
	for _, p := range got {
		consumers[p.Consumer.Name] = true
	}
	for _, want := range []string{"weave-python-sdk", "weave-ts-sdk", "weave-go-sdk"} {
		if !consumers[want] {
			t.Errorf("missing consumer %s in fetched pacts: %+v", want, consumers)
		}
	}
}

func TestBroker_FetchProviderPacts_PropagatesHTTPError(t *testing.T) {
	fb := newFakeBroker(t)
	fb.fail = func(*http.Request) (int, string) { return 500, "broken" }
	client := NewBrokerClient(fb.server.URL, BrokerClientOptions{})
	_, err := client.FetchProviderPacts("weave-server")
	if err == nil {
		t.Fatal("expected error on 500 from broker")
	}
}
