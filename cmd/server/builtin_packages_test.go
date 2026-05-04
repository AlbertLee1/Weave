package main

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

func TestBuiltinPackageProvider_LoadsEmbeddedCatalog(t *testing.T) {
	provider, err := newBuiltinPackageProvider()
	if err != nil {
		t.Fatalf("newBuiltinPackageProvider: %v", err)
	}
	rows := provider.List(context.Background())
	if len(rows) != 3 {
		t.Fatalf("expected 3 builtin packages, got %d: %+v", len(rows), rows)
	}
	wantSlugs := map[string]bool{"chinook": false, "iot-demo": false, "northwind": false}
	for _, r := range rows {
		if _, ok := wantSlugs[r.Slug]; !ok {
			t.Errorf("unexpected slug %q in catalog", r.Slug)
			continue
		}
		wantSlugs[r.Slug] = true
		if r.Name == "" || r.Version == "" {
			t.Errorf("%s: name/version empty: %+v", r.Slug, r)
		}
		if r.OntologyAPIName == "" {
			t.Errorf("%s: ontologyApiName empty", r.Slug)
		}
		if r.ObjectTypeCount == 0 {
			t.Errorf("%s: expected at least one objectType", r.Slug)
		}
	}
	for slug, seen := range wantSlugs {
		if !seen {
			t.Errorf("missing builtin package slug %q", slug)
		}
	}
}

func TestBuiltinPackageProvider_GetReturnsParsedRequest(t *testing.T) {
	provider, err := newBuiltinPackageProvider()
	if err != nil {
		t.Fatalf("newBuiltinPackageProvider: %v", err)
	}
	req, md, ok := provider.Get(context.Background(), "northwind")
	if !ok {
		t.Fatal("expected northwind to be present")
	}
	if req == nil || md == nil {
		t.Fatal("expected request + metadata to be non-nil")
	}
	if req.Manifest.Name != "northwind" {
		t.Errorf("manifest.name = %q, want northwind", req.Manifest.Name)
	}
	if len(req.Ontology) == 0 {
		t.Error("ontology body is empty")
	}
	if !strings.Contains(string(req.Ontology), `"apiName"`) {
		t.Errorf("ontology body missing apiName field: %s", string(req.Ontology)[:min(80, len(req.Ontology))])
	}
}

func TestBuiltinPackageProvider_GetMissingSlug(t *testing.T) {
	provider, err := newBuiltinPackageProvider()
	if err != nil {
		t.Fatalf("newBuiltinPackageProvider: %v", err)
	}
	if _, _, ok := provider.Get(context.Background(), "does-not-exist"); ok {
		t.Fatal("expected missing slug to return ok=false")
	}
}

func TestBuiltinPackageProvider_FromCustomFS(t *testing.T) {
	fsys := fstest.MapFS{
		"toy/manifest.json": &fstest.MapFile{Data: []byte(`{"name":"toy","version":"0.1.0","author":"Test"}`)},
		"toy/ontology.json": &fstest.MapFile{Data: []byte(`{"ontology":{"apiName":"toy"},"objectTypes":[{"apiName":"Item"}]}`)},
	}
	provider, err := newBuiltinPackageProviderFromFS(fsys)
	if err != nil {
		t.Fatalf("newBuiltinPackageProviderFromFS: %v", err)
	}
	rows := provider.List(context.Background())
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %+v", rows)
	}
	if rows[0].Slug != "toy" || rows[0].OntologyAPIName != "toy" {
		t.Fatalf("row drift: %+v", rows[0])
	}
	if rows[0].ObjectTypeCount != 1 {
		t.Errorf("expected 1 objectType, got %d", rows[0].ObjectTypeCount)
	}
}

