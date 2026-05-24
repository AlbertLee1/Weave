package oss_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/liyang/weave/pkg/oss"
)

// TestBDD_OSS_TraceSpans_PRD_Gap_O2 covers PRD-V2 §4.6 Gap-O2: the
// `tracing` package has a clean StartSpan helper that's been used to
// wrap actions.Apply and oss.GetObject, but the other five OSS
// service entry points (ListObjects, SearchObjects, CountObjects,
// ListLinkedObjects, GetLinkedObject) were never wired through it.
// As a result an OTel trace of a Foundry-shape query session
// showed a gap between the HTTP middleware span and any downstream
// Bleve / OMS spans — operators chasing latency had to guess which
// service method took how long.
//
// This BDD installs a recording SpanRecorder via the global tracer
// provider, calls each service entry point through its handler-level
// API, and asserts the expected span exists with the right
// (ontology.rid, object_type.api_name, …) attributes. The span name
// convention mirrors oss.GetObject (the only pre-existing wrap):
// `oss.<Method>` in PascalCase so operators can sort by method
// straight from the span panel.
func TestBDD_OSS_TraceSpans_PRD_Gap_O2(t *testing.T) {
	// install a recording tracer provider and restore on cleanup.
	rec := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(trace.WithSpanProcessor(rec))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = tp.Shutdown(context.Background())
	})

	hasSpan := func(name string) bool {
		for _, s := range rec.Ended() {
			if s.Name() == name {
				return true
			}
		}
		return false
	}

	t.Run("ListObjects emits oss.ListObjects span", func(t *testing.T) {
		svc, _, _, _ := setupOSSTest(t)
		_, _ = svc.ListObjects(context.Background(), oss.ListObjectsRequest{
			OntologyRID: testOntologyRID,
			ObjectType:  "employee",
		})
		if !hasSpan("oss.ListObjects") {
			names := []string{}
			for _, s := range rec.Ended() {
				names = append(names, s.Name())
			}
			t.Fatalf("expected oss.ListObjects span, saw %v", names)
		}
	})

	t.Run("SearchObjects emits oss.SearchObjects span", func(t *testing.T) {
		svc, _, _, _ := setupOSSTest(t)
		_, _ = svc.SearchObjects(context.Background(), oss.SearchObjectsRequest{
			OntologyRID: testOntologyRID,
			ObjectType:  "employee",
		})
		if !hasSpan("oss.SearchObjects") {
			t.Fatalf("expected oss.SearchObjects span")
		}
	})

	t.Run("CountObjects emits oss.CountObjects span", func(t *testing.T) {
		svc, _, _, _ := setupOSSTest(t)
		_, _ = svc.CountObjects(context.Background(), oss.CountObjectsRequest{
			OntologyRID: testOntologyRID,
			ObjectType:  "employee",
		})
		if !hasSpan("oss.CountObjects") {
			t.Fatalf("expected oss.CountObjects span")
		}
	})

	t.Run("ListLinkedObjects emits oss.ListLinkedObjects span", func(t *testing.T) {
		svc, _, _, _ := setupOSSTest(t)
		_, _ = svc.ListLinkedObjects(context.Background(), oss.LinkedObjectsRequest{
			OntologyRID: testOntologyRID,
			ObjectType:  "employee",
			PrimaryKey:  "emp1",
			LinkType:    "manager",
		})
		if !hasSpan("oss.ListLinkedObjects") {
			t.Fatalf("expected oss.ListLinkedObjects span")
		}
	})

	t.Run("GetLinkedObject emits oss.GetLinkedObject span", func(t *testing.T) {
		svc, _, _, _ := setupOSSTest(t)
		_, _ = svc.GetLinkedObject(context.Background(), oss.GetLinkedObjectRequest{
			OntologyRID:            testOntologyRID,
			ObjectType:             "employee",
			PrimaryKey:             "emp1",
			LinkType:               "manager",
			LinkedObjectPrimaryKey: "emp2",
		})
		if !hasSpan("oss.GetLinkedObject") {
			t.Fatalf("expected oss.GetLinkedObject span")
		}
	})

	t.Run("Span attributes carry ontology.rid + object_type.api_name", func(t *testing.T) {
		// Sanity-check the attribute convention so operators can group
		// by these dimensions in the span panel. We re-use SearchObjects
		// since it appears in every realistic query session.
		recLocal := tracetest.NewSpanRecorder()
		tpLocal := trace.NewTracerProvider(trace.WithSpanProcessor(recLocal))
		prevLocal := otel.GetTracerProvider()
		otel.SetTracerProvider(tpLocal)
		t.Cleanup(func() {
			otel.SetTracerProvider(prevLocal)
			_ = tpLocal.Shutdown(context.Background())
		})

		svc, _, _, _ := setupOSSTest(t)
		_, _ = svc.SearchObjects(context.Background(), oss.SearchObjectsRequest{
			OntologyRID: testOntologyRID,
			ObjectType:  "employee",
		})
		var got trace.ReadOnlySpan
		for _, s := range recLocal.Ended() {
			if s.Name() == "oss.SearchObjects" {
				got = s
				break
			}
		}
		if got == nil {
			t.Fatal("oss.SearchObjects span missing")
		}
		want := map[string]string{
			"ontology.rid":         testOntologyRID,
			"object_type.api_name": "employee",
		}
		for _, a := range got.Attributes() {
			if v, ok := want[string(a.Key)]; ok && a.Value.AsString() == v {
				delete(want, string(a.Key))
			}
		}
		if len(want) != 0 {
			t.Errorf("missing expected span attributes: %v", want)
		}
	})
}
