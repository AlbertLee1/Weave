package links

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
)

// LinkResolver resolves linked objects through link types.
type LinkResolver interface {
	// ResolveLinkedObjects finds objects linked to the given source objects through the specified link type.
	// sourcePKs are the primary keys of the source objects.
	// Returns primary keys of the linked (target) objects.
	ResolveLinkedObjects(ctx context.Context, linkTypeRID string, sourcePKs []string) ([]string, error)

	// ResolveLinkedObjectsByAPIName resolves links using the link type's API name and source object type.
	ResolveLinkedObjectsByAPIName(ctx context.Context, sourceObjectTypeRID, linkTypeAPIName string, sourcePKs []string) ([]string, error)
}

// FKConfig is the foreign key configuration for a link type.
type FKConfig struct {
	SourceProperty string `json:"sourceProperty"`
	TargetProperty string `json:"targetProperty"`
}

// Resolver implements LinkResolver using OMS repository and Bleve indexes.
type Resolver struct {
	repo     oms.Repository
	indexMgr *index.Manager
}

// NewResolver creates a new link resolver.
func NewResolver(repo oms.Repository, indexMgr *index.Manager) *Resolver {
	return &Resolver{
		repo:     repo,
		indexMgr: indexMgr,
	}
}

// ResolveLinkedObjects finds objects linked to the given source objects through the specified link type RID.
func (r *Resolver) ResolveLinkedObjects(ctx context.Context, linkTypeRID string, sourcePKs []string) ([]string, error) {
	lt, err := r.repo.GetLinkType(ctx, linkTypeRID)
	if err != nil {
		return nil, fmt.Errorf("get link type: %w", err)
	}

	return r.resolveFK(ctx, lt, sourcePKs)
}

// ResolveLinkedObjectsByAPIName resolves links using the link type's API name and source object type RID.
func (r *Resolver) ResolveLinkedObjectsByAPIName(ctx context.Context, sourceObjectTypeRID, linkTypeAPIName string, sourcePKs []string) ([]string, error) {
	linkTypes, err := r.repo.ListOutgoingLinkTypes(ctx, sourceObjectTypeRID)
	if err != nil {
		return nil, fmt.Errorf("list link types: %w", err)
	}

	for _, lt := range linkTypes {
		if lt.APIName == linkTypeAPIName {
			return r.resolveFK(ctx, &lt, sourcePKs)
		}
	}

	return nil, fmt.Errorf("link type %q not found for source %q", linkTypeAPIName, sourceObjectTypeRID)
}

// parseFKConfig parses the foreign key configuration JSON.
func parseFKConfig(raw json.RawMessage) (*FKConfig, error) {
	var cfg FKConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
