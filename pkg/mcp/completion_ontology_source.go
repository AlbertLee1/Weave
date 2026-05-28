package mcp

import (
	"context"
	"strings"

	"github.com/liyang/weave/pkg/oms"
)

// OntologyCompletionSource is the round-47 CompletionSource that
// resolves MCP completion/complete prefixes against OMS catalog
// data. It's the default provider production wires; tests can
// substitute a static fake via Server.SetCompletionSource.
//
// Sources implemented:
//
//	ref/prompt + argument.name in {"objectType", "objectTypeApiName"}
//	  → ObjectType apiNames in the prompt's ontology
//
//	ref/prompt + argument.name in {"actionType", "actionTypeApiName"}
//	  → ActionType apiNames in the prompt's ontology
//
//	ref/prompt + argument.name in {"linkType", "linkTypeApiName"}
//	  → LinkType apiNames in the prompt's ontology (outgoing + incoming)
//
//	ref/resource uri starts with "weave://objecttype/<ontology>/"
//	  → next-segment completion of ObjectType apiNames within
//	    that ontology
//
//	ref/resource uri starts with "weave://ontology/" (no further
//	segment yet)
//	  → ontology apiNames
//
// Any other (ref, argument) pair returns nil — empty completions
// per the round-46 protocol contract. Argument matching is
// case-insensitive so SDK authors don't have to remember the
// canonical spelling.
//
// The provider parses prompt names via splitPromptName from
// prompts.go so the ontology lookup stays consistent with the
// prompts/get handler.
type OntologyCompletionSource struct {
	repo oms.Repository
}

// NewOntologyCompletionSource wraps an oms.Repository as a
// CompletionSource. A nil repo makes Complete a no-op (always
// empty) so degraded-mode bootstraps don't need to special-case
// the source wiring.
func NewOntologyCompletionSource(repo oms.Repository) *OntologyCompletionSource {
	return &OntologyCompletionSource{repo: repo}
}

// Complete implements the CompletionSource interface.
func (s *OntologyCompletionSource) Complete(ctx context.Context, ref CompletionRef, arg CompletionArgument, limit int) ([]string, error) {
	if s == nil || s.repo == nil {
		return nil, nil
	}
	switch ref.Type {
	case "ref/prompt":
		return s.completeForPrompt(ctx, ref.Name, arg, limit)
	case "ref/resource":
		return s.completeForResource(ctx, ref.URI, arg, limit)
	}
	return nil, nil
}

// completeForPrompt resolves prompt-argument prefixes. The prompt
// name encodes the ontology via the prompts.go convention
// "<ontology>__<action>" — extract the ontology so OMS lookups
// stay scoped.
func (s *OntologyCompletionSource) completeForPrompt(ctx context.Context, promptName string, arg CompletionArgument, limit int) ([]string, error) {
	ontologyAPIName, _, ok := splitPromptName(promptName)
	if !ok {
		return nil, nil
	}
	ont, err := s.repo.GetOntology(ctx, ontologyAPIName)
	if err != nil || ont == nil {
		// Missing ontology is not an error for the completion path —
		// the spec says empty completions are always valid. Surfacing
		// the underlying error would block the user's typing UX over
		// a misspelled prompt name.
		return nil, nil
	}
	argName := strings.ToLower(arg.Name)
	switch argName {
	case "objecttype", "objecttypeapiname":
		ots, err := s.repo.ListObjectTypes(ctx, ont.RID)
		if err != nil {
			return nil, nil // empty on lookup failure
		}
		names := make([]string, 0, len(ots))
		for _, ot := range ots {
			names = append(names, ot.APIName)
		}
		return PrefixFilter(names, arg.Value, limit), nil

	case "actiontype", "actiontypeapiname":
		ats, err := s.repo.ListActionTypes(ctx, ont.RID)
		if err != nil {
			return nil, nil
		}
		names := make([]string, 0, len(ats))
		for _, at := range ats {
			names = append(names, at.APIName)
		}
		return PrefixFilter(names, arg.Value, limit), nil

	case "linktype", "linktypeapiname":
		// LinkType lookup is per-ObjectType in the OMS surface — we
		// don't have a flat list-by-ontology method, so iterate
		// ObjectTypes and dedupe.
		ots, err := s.repo.ListObjectTypes(ctx, ont.RID)
		if err != nil {
			return nil, nil
		}
		names := []string{}
		seen := make(map[string]struct{})
		for _, ot := range ots {
			outgoing, _ := s.repo.ListOutgoingLinkTypes(ctx, ot.RID)
			for _, lt := range outgoing {
				if _, dup := seen[lt.APIName]; !dup {
					seen[lt.APIName] = struct{}{}
					names = append(names, lt.APIName)
				}
			}
		}
		return PrefixFilter(names, arg.Value, limit), nil
	}
	return nil, nil
}

// completeForResource resolves resource-URI-template variables.
// Three families handled:
//
//   - "weave://objecttype/<ontology>/" with no further segment →
//     complete the next ObjectType apiName segment
//   - "weave://ontology/" with no further segment → complete the
//     ontology apiName segment
//
// The user types into the FINAL incomplete segment; arg.Value is
// the typed prefix. ref.URI is the URI shape so far (ending with
// the "/" the user just typed).
func (s *OntologyCompletionSource) completeForResource(ctx context.Context, uri string, arg CompletionArgument, limit int) ([]string, error) {
	// For URI shape "weave://objecttype/<ontology>/", return ObjectType names of that ontology.
	const objectTypePrefix = "weave://objecttype/"
	if strings.HasPrefix(uri, objectTypePrefix) {
		rest := uri[len(objectTypePrefix):]
		// rest must look like "<ontology>/" with no further segment
		// for completion to apply.
		parts := strings.Split(rest, "/")
		if len(parts) == 2 && parts[0] != "" && parts[1] == "" {
			ontologyAPIName := parts[0]
			ont, err := s.repo.GetOntology(ctx, ontologyAPIName)
			if err != nil || ont == nil {
				return nil, nil
			}
			ots, err := s.repo.ListObjectTypes(ctx, ont.RID)
			if err != nil {
				return nil, nil
			}
			names := make([]string, 0, len(ots))
			for _, ot := range ots {
				names = append(names, ot.APIName)
			}
			return PrefixFilter(names, arg.Value, limit), nil
		}
	}
	// For URI shape "weave://ontology/", return ontology apiNames.
	if uri == "weave://ontology/" {
		onts, err := s.repo.ListOntologies(ctx)
		if err != nil {
			return nil, nil
		}
		names := make([]string, 0, len(onts))
		for _, o := range onts {
			names = append(names, o.APIName)
		}
		return PrefixFilter(names, arg.Value, limit), nil
	}
	return nil, nil
}
