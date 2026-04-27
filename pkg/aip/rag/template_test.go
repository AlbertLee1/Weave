package rag

import (
	"strings"
	"testing"
)

func TestPromptTemplate_RenderSubstitutesPlaceholders(t *testing.T) {
	tpl := PromptTemplate("Q: {{query}}\nContext:\n{{context}}\n\nAnswer:")
	got := tpl.Render("who is alice", "doc1\ndoc2")
	want := "Q: who is alice\nContext:\ndoc1\ndoc2\n\nAnswer:"
	if got != want {
		t.Fatalf("Render mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestPromptTemplate_RenderRepeatedPlaceholders(t *testing.T) {
	tpl := PromptTemplate("{{query}} -> {{query}} :: {{context}}")
	got := tpl.Render("X", "Y")
	if got != "X -> X :: Y" {
		t.Fatalf("got %q", got)
	}
}

func TestPromptTemplate_NoPlaceholdersPassesThrough(t *testing.T) {
	tpl := PromptTemplate("hello world")
	if got := tpl.Render("ignored", "ignored"); got != "hello world" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatContext_NumbersAndLabelsMatches(t *testing.T) {
	matches := []Match{
		{Document: Document{ObjectType: "person", PrimaryKey: "alice", Title: "Alice", Text: "is a hero"}, Score: 0.9},
		{Document: Document{ObjectType: "person", PrimaryKey: "bob", Text: "plays bass"}, Score: 0.5},
	}
	got := FormatContext(matches)
	for _, want := range []string{
		"[1]",
		"Alice",
		"person:alice",
		"is a hero",
		"[2]",
		"person:bob",
		"plays bass",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("FormatContext output missing %q\nfull:\n%s", want, got)
		}
	}
}

func TestFormatContext_EmptyMatchesReturnsEmptyString(t *testing.T) {
	if got := FormatContext(nil); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
	if got := FormatContext([]Match{}); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestPromptTemplate_RenderFromMatches(t *testing.T) {
	tpl := PromptTemplate("Q: {{query}}\n{{context}}")
	matches := []Match{
		{Document: Document{ObjectType: "doc", PrimaryKey: "1", Text: "alpha"}, Score: 0.9},
		{Document: Document{ObjectType: "doc", PrimaryKey: "2", Text: "beta"}, Score: 0.5},
	}
	got := tpl.RenderFromMatches("hi", matches)
	if !strings.Contains(got, "Q: hi") {
		t.Fatalf("query not substituted: %s", got)
	}
	if !strings.Contains(got, "alpha") || !strings.Contains(got, "beta") {
		t.Fatalf("context not substituted: %s", got)
	}
}

func TestPromptTemplate_RenderFromMatchesNoMatches(t *testing.T) {
	tpl := PromptTemplate("Q: {{query}}\nContext:\n{{context}}")
	got := tpl.RenderFromMatches("hi", nil)
	want := "Q: hi\nContext:\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPromptTemplate_String(t *testing.T) {
	tpl := PromptTemplate("hello")
	if tpl.String() != "hello" {
		t.Fatalf("String() = %q, want %q", tpl.String(), "hello")
	}
}

func TestDefaultPromptTemplateContainsBothPlaceholders(t *testing.T) {
	body := DefaultPromptTemplate.String()
	if !strings.Contains(body, PlaceholderContext) {
		t.Fatalf("default template missing %q", PlaceholderContext)
	}
	if !strings.Contains(body, PlaceholderQuery) {
		t.Fatalf("default template missing %q", PlaceholderQuery)
	}
}
