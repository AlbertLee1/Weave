package rag

import (
	"fmt"
	"strings"
)

// Placeholders recognised by PromptTemplate.Render. Both are simple
// literal strings — there is no escaping or expression syntax. Anything
// more sophisticated (loops, conditionals) belongs in the higher-level
// pkg/aip/logic template engine.
const (
	PlaceholderContext = "{{context}}"
	PlaceholderQuery   = "{{query}}"
)

// DefaultPromptTemplate is the canonical RAG prompt body callers can use
// when they have no custom prompt of their own. Mirrors the shape every
// well-known RAG cookbook ships: anchor the model to the supplied
// context, then ask the question. Override at any point by constructing
// a fresh PromptTemplate.
const DefaultPromptTemplate PromptTemplate = `You are answering using only the context provided below.

Context:
{{context}}

Question: {{query}}

If the context does not contain the answer, say "I don't know."`

// PromptTemplate is a tiny string-substitution template with two known
// placeholders ({{context}} + {{query}}). Both placeholders are
// optional; absent ones simply pass the template body through unchanged
// to the rendered output.
type PromptTemplate string

// String returns the raw template body.
func (t PromptTemplate) String() string { return string(t) }

// Render substitutes both placeholders with the supplied values. Each
// placeholder may appear any number of times in the template body
// (including zero) — every occurrence is replaced.
func (t PromptTemplate) Render(query, context string) string {
	body := string(t)
	body = strings.ReplaceAll(body, PlaceholderQuery, query)
	body = strings.ReplaceAll(body, PlaceholderContext, context)
	return body
}

// RenderFromMatches is the common-case shortcut: format the matches into
// the canonical context block via FormatContext, then substitute both
// placeholders. Equivalent to t.Render(query, FormatContext(matches)).
func (t PromptTemplate) RenderFromMatches(query string, matches []Match) string {
	return t.Render(query, FormatContext(matches))
}

// FormatContext renders matches as a numbered block suitable for
// substitution into the {{context}} placeholder. Each entry is laid out
// as:
//
//	[N] <Title> (<objectType>:<primaryKey>)
//	<text>
//
// The Title segment is omitted when empty so callers that don't carry a
// per-document headline still get a clean handle line. Empty input
// returns an empty string so prompts with no retrieved context degrade
// gracefully (the surrounding template body still renders).
func FormatContext(matches []Match) string {
	if len(matches) == 0 {
		return ""
	}
	var b strings.Builder
	for i, m := range matches {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "[%d] ", i+1)
		title := strings.TrimSpace(m.Document.Title)
		if title != "" {
			b.WriteString(title)
			b.WriteString(" ")
		}
		fmt.Fprintf(&b, "(%s:%s)\n", m.Document.ObjectType, m.Document.PrimaryKey)
		b.WriteString(m.Document.Text)
	}
	return b.String()
}
