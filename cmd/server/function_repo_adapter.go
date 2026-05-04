package main

import (
	"context"

	"github.com/liyang/weave/pkg/funcrepo"
	"github.com/liyang/weave/pkg/oms"
)

// funcRepoStoreAdapter bridges the concrete *funcrepo.Manager to the narrow
// oms.FunctionRepoStore interface. The two layers use structurally-identical
// commit shapes, declared independently so pkg/funcrepo (go-git heavy) and
// pkg/oms (HTTP / wire types) can evolve without a direct dependency.
type funcRepoStoreAdapter struct {
	mgr *funcrepo.Manager
}

func newFuncRepoStoreAdapter(mgr *funcrepo.Manager) *funcRepoStoreAdapter {
	return &funcRepoStoreAdapter{mgr: mgr}
}

func (a *funcRepoStoreAdapter) Commit(ctx context.Context, rid string, in oms.FunctionRepoCommitInput) (oms.FunctionRepoCommit, error) {
	c, err := a.mgr.Commit(ctx, rid, funcrepo.CommitInput{
		Message:    in.Message,
		SourceCode: in.SourceCode,
		Author:     in.Author,
		Email:      in.Email,
		When:       in.When,
	})
	if err != nil {
		return oms.FunctionRepoCommit{}, err
	}
	return oms.FunctionRepoCommit{
		Hash:       c.Hash,
		Message:    c.Message,
		Author:     c.Author,
		Email:      c.Email,
		AuthorDate: c.AuthorDate,
	}, nil
}

func (a *funcRepoStoreAdapter) Log(ctx context.Context, rid string, limit int) ([]oms.FunctionRepoCommit, error) {
	commits, err := a.mgr.Log(ctx, rid, limit)
	if err != nil {
		return nil, err
	}
	out := make([]oms.FunctionRepoCommit, len(commits))
	for i, c := range commits {
		out[i] = oms.FunctionRepoCommit{
			Hash:       c.Hash,
			Message:    c.Message,
			Author:     c.Author,
			Email:      c.Email,
			AuthorDate: c.AuthorDate,
		}
	}
	return out, nil
}
