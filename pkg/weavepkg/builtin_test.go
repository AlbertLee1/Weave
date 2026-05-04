package weavepkg_test

import (
	"encoding/json"
	"testing"
	"testing/fstest"

	examplepkgs "github.com/liyang/weave/examples/packages"
	"github.com/liyang/weave/pkg/weavepkg"
)

func TestLoadBuiltinPackages_EmbeddedExamples(t *testing.T) {
	got, err := weavepkg.LoadBuiltinPackages(examplepkgs.FS)
	if err != nil {
		t.Fatalf("LoadBuiltinPackages: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 builtin packages, got %d", len(got))
	}

	wantSlugs := []string{"chinook", "iot-demo", "northwind"}
	for i, p := range got {
		if p.Slug != wantSlugs[i] {
			t.Errorf("slug[%d] = %q, want %q", i, p.Slug, wantSlugs[i])
		}
		if p.Manifest.Name == "" {
			t.Errorf("%s: manifest.name is empty", p.Slug)
		}
		if p.Manifest.Version == "" {
			t.Errorf("%s: manifest.version is empty", p.Slug)
		}
		if len(p.Ontology) == 0 {
			t.Errorf("%s: ontology body is empty", p.Slug)
		}
		// Sanity-check that the ontology body has the expected wire shape.
		var env struct {
			Ontology    map[string]any   `json:"ontology"`
			ObjectTypes []map[string]any `json:"objectTypes"`
		}
		if err := json.Unmarshal(p.Ontology, &env); err != nil {
			t.Fatalf("%s: ontology not parseable: %v", p.Slug, err)
		}
		if env.Ontology["apiName"] == nil {
			t.Errorf("%s: ontology.apiName missing", p.Slug)
		}
		if len(env.ObjectTypes) == 0 {
			t.Errorf("%s: no objectTypes declared", p.Slug)
		}
	}
}

func TestLoadBuiltinPackages_MissingManifest(t *testing.T) {
	fsys := fstest.MapFS{
		"broken/ontology.json": &fstest.MapFile{Data: []byte(`{"ontology":{"apiName":"x"}}`)},
	}
	if _, err := weavepkg.LoadBuiltinPackages(fsys); err == nil {
		t.Fatal("expected error for missing manifest.json")
	}
}

func TestLoadBuiltinPackages_MissingOntology(t *testing.T) {
	fsys := fstest.MapFS{
		"broken/manifest.json": &fstest.MapFile{Data: []byte(`{"name":"x","version":"1.0.0"}`)},
	}
	if _, err := weavepkg.LoadBuiltinPackages(fsys); err == nil {
		t.Fatal("expected error for missing ontology.json")
	}
}

func TestLoadBuiltinPackages_InvalidOntologyJSON(t *testing.T) {
	fsys := fstest.MapFS{
		"broken/manifest.json": &fstest.MapFile{Data: []byte(`{"name":"x","version":"1.0.0"}`)},
		"broken/ontology.json": &fstest.MapFile{Data: []byte(`not-json`)},
	}
	if _, err := weavepkg.LoadBuiltinPackages(fsys); err == nil {
		t.Fatal("expected error for invalid ontology.json")
	}
}

func TestLoadBuiltinPackages_OptionalSubdirs(t *testing.T) {
	fsys := fstest.MapFS{
		"demo/manifest.json":               &fstest.MapFile{Data: []byte(`{"name":"demo","version":"1.0.0"}`)},
		"demo/ontology.json":               &fstest.MapFile{Data: []byte(`{"ontology":{"apiName":"demo"}}`)},
		"demo/actions/createX.json":        &fstest.MapFile{Data: []byte(`{"apiName":"createX"}`)},
		"demo/functions/compute.js":        &fstest.MapFile{Data: []byte("module.exports = function(){};")},
		"demo/migrations/0001_init.up.sql": &fstest.MapFile{Data: []byte(`SELECT 1;`)},
	}
	got, err := weavepkg.LoadBuiltinPackages(fsys)
	if err != nil {
		t.Fatalf("LoadBuiltinPackages: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 package, got %d", len(got))
	}
	p := got[0]
	if len(p.Actions) != 1 || p.Actions[0].APIName != "createX" {
		t.Errorf("actions: %+v", p.Actions)
	}
	if len(p.Functions) != 1 || p.Functions[0].Name != "compute" {
		t.Errorf("functions: %+v", p.Functions)
	}
	if len(p.Migrations) != 1 || p.Migrations[0].Filename != "0001_init.up.sql" {
		t.Errorf("migrations: %+v", p.Migrations)
	}
}
