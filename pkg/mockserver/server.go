package mockserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
)

// Override pins the response for one (method, path) tuple. Path uses the
// same `{paramName}` template as the OpenAPI spec.
type Override struct {
	Method      string            `json:"method"`
	Path        string            `json:"path"`
	Status      int               `json:"status,omitempty"`
	ContentType string            `json:"contentType,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Body        json.RawMessage   `json:"body,omitempty"`
}

// Options shapes the mock-server handler beyond the spec itself.
//
//   - Overrides: file-loaded or programmatic per-route pins applied at
//     boot time. Runtime registration via /__mock/overrides composes on
//     top with last-write-wins semantics.
//   - EnableAdmin: when true, mounts POST/DELETE/GET /__mock/overrides
//     for runtime tweaking. Off by default — admin endpoints leak the
//     mock's existence and should be opt-in for hostile environments.
//   - DefaultStatus: status code to use when an operation declares no
//     responses at all. Default 200.
type Options struct {
	Overrides     []Override
	EnableAdmin   bool
	DefaultStatus int
}

// NewHandler builds an http.Handler that serves mock responses for every
// operation in spec. Routes are mounted via chi using the OpenAPI path
// templates verbatim — chi treats {param} the same way OpenAPI does.
func NewHandler(spec *Spec, opts Options) (http.Handler, error) {
	if spec == nil {
		return nil, fmt.Errorf("mockserver: spec is nil")
	}

	store := newOverrideStore()
	for _, o := range opts.Overrides {
		if err := store.Set(o); err != nil {
			return nil, fmt.Errorf("seed override %s %s: %w", o.Method, o.Path, err)
		}
	}

	defaultStatus := opts.DefaultStatus
	if defaultStatus == 0 {
		defaultStatus = http.StatusOK
	}

	r := chi.NewRouter()
	if opts.EnableAdmin {
		mountAdmin(r, store)
	}

	// Snapshot baseline (synthesized) responses so each request only
	// pays a JSON marshal cost, not a full schema walk.
	for i := range spec.Operations {
		op := spec.Operations[i]
		baseline := buildBaselineBody(spec, op)
		r.Method(op.Method, op.Path, makeHandler(op, baseline, store, defaultStatus))
	}

	return r, nil
}

// LoadSpecFile reads an OpenAPI document from disk and parses it.
func LoadSpecFile(path string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read spec %s: %w", path, err)
	}
	return ParseSpec(data)
}

// DecodeOverrides parses a JSON document containing either a single
// Override or an array of Overrides into a slice. Empty document =
// empty slice (acceptable input — admins may iterate a config).
func DecodeOverrides(data []byte) ([]Override, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var out []Override
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("decode overrides array: %w", err)
		}
		return out, nil
	}
	var single Override
	if err := json.Unmarshal(data, &single); err != nil {
		return nil, fmt.Errorf("decode override: %w", err)
	}
	return []Override{single}, nil
}

// LoadOverridesFile reads + decodes a JSON overrides file.
func LoadOverridesFile(path string) ([]Override, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read overrides %s: %w", path, err)
	}
	return DecodeOverrides(data)
}

// buildBaselineBody synthesises the default response body for one
// operation. Marshalled to JSON once so the hot path is a copy.
func buildBaselineBody(spec *Spec, op Operation) []byte {
	if op.Schema == nil {
		return nil
	}
	syn := newSynthesizer(spec.Schemas, spec.Responses)
	value := syn.synthesize(op.Schema)
	if value == nil {
		return nil
	}
	out, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return out
}

func makeHandler(op Operation, baseline []byte, store *overrideStore, defaultStatus int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ov, ok := store.Get(op.Method, op.Path); ok {
			writeOverride(w, ov)
			return
		}
		writeBaseline(w, op, baseline, defaultStatus)
	})
}

func writeOverride(w http.ResponseWriter, ov Override) {
	status := ov.Status
	if status == 0 {
		status = http.StatusOK
	}
	contentType := ov.ContentType
	if contentType == "" && len(ov.Body) > 0 {
		contentType = "application/json"
	}
	for k, v := range ov.Headers {
		w.Header().Set(k, v)
	}
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.WriteHeader(status)
	if len(ov.Body) > 0 {
		_, _ = w.Write(ov.Body)
	}
}

func writeBaseline(w http.ResponseWriter, op Operation, baseline []byte, defaultStatus int) {
	status := op.Status
	if status == 0 {
		status = defaultStatus
	}
	if len(baseline) == 0 {
		w.WriteHeader(status)
		return
	}
	contentType := op.ContentType
	if contentType == "" {
		contentType = "application/json"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_, _ = w.Write(baseline)
}

// overrideStore is the concurrency-safe per-route override map.
type overrideStore struct {
	mu  sync.RWMutex
	by  map[string]Override // key: "<METHOD> <path>"
}

func newOverrideStore() *overrideStore {
	return &overrideStore{by: map[string]Override{}}
}

func (s *overrideStore) key(method, path string) string {
	return strings.ToUpper(method) + " " + path
}

func (s *overrideStore) Set(o Override) error {
	if o.Method == "" || o.Path == "" {
		return fmt.Errorf("override requires method and path")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.by[s.key(o.Method, o.Path)] = o
	return nil
}

func (s *overrideStore) Get(method, path string) (Override, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	o, ok := s.by[s.key(method, path)]
	return o, ok
}

func (s *overrideStore) Delete(method, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.by, s.key(method, path))
}

func (s *overrideStore) List() []Override {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Override, 0, len(s.by))
	for _, o := range s.by {
		out = append(out, o)
	}
	return out
}

func mountAdmin(r chi.Router, store *overrideStore) {
	r.Get("/__mock/overrides", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(store.List())
	})

	r.Post("/__mock/overrides", func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ovs, err := DecodeOverrides(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for _, o := range ovs {
			if err := store.Set(o); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
	})

	r.Delete("/__mock/overrides", func(w http.ResponseWriter, req *http.Request) {
		var target Override
		if err := json.NewDecoder(req.Body).Decode(&target); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if target.Method == "" || target.Path == "" {
			http.Error(w, "method and path required", http.StatusBadRequest)
			return
		}
		store.Delete(target.Method, target.Path)
		w.WriteHeader(http.StatusOK)
	})
}
