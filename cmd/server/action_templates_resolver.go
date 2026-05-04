package main

import (
	"context"

	"github.com/liyang/weave/pkg/actiontemplates"
	"github.com/liyang/weave/pkg/auth"
)

// groupTeammateResolver bridges auth.GroupRepository into the
// actiontemplates.TeammateResolver interface for US-427's TEAM scope.
// A teammate is "any user who shares at least one group with the
// caller" — we resolve that as `union(members(g) for g in groups(caller))`
// minus the caller itself. nil group repository (degraded boot)
// returns an empty mate set so TEAM rows from other owners stay
// invisible.
type groupTeammateResolver struct {
	repo auth.GroupRepository
}

func newGroupTeammateResolver(repo auth.GroupRepository) *groupTeammateResolver {
	return &groupTeammateResolver{repo: repo}
}

func (r *groupTeammateResolver) Teammates(ctx context.Context, callerID string) ([]string, error) {
	if r == nil || r.repo == nil || callerID == "" {
		return nil, nil
	}
	groupIDs, err := r.repo.ListUserGroups(ctx, callerID)
	if err != nil {
		return nil, err
	}
	if len(groupIDs) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{})
	for _, gid := range groupIDs {
		members, err := r.repo.ListMembers(ctx, gid)
		if err != nil {
			return nil, err
		}
		for _, uid := range members {
			if uid == callerID {
				continue
			}
			seen[uid] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for uid := range seen {
		out = append(out, uid)
	}
	return out, nil
}

// Compile-time guard.
var _ actiontemplates.TeammateResolver = (*groupTeammateResolver)(nil)
