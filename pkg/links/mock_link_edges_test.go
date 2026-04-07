package links_test

import (
	"context"

	"github.com/liyang/weave/pkg/oms"
)

// Stub link-edge methods on mockRepo to satisfy oms.Repository. The fk
// resolver tests do not exercise the M2M write path, so these return nil.
func (m *mockRepo) UpsertLinkEdge(_ context.Context, _ *oms.LinkEdge) error {
	return nil
}

func (m *mockRepo) DeleteLinkEdge(_ context.Context, _, _, _ string) error {
	return nil
}

func (m *mockRepo) DeleteAllLinkEdgesForSource(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockRepo) GetLinkTypeByAPIName(_ context.Context, _, _ string) (*oms.LinkType, error) {
	return nil, oms.ErrNotFound
}

// Object history stubs — tier 2.3 added these interface methods. The
// resolver tests do not exercise the history path, so all three are no-ops.
func (m *mockRepo) InsertObjectHistory(_ context.Context, _ *oms.ObjectHistory) error {
	return nil
}

func (m *mockRepo) ListObjectHistory(_ context.Context, _, _ string, _ int) ([]oms.ObjectHistory, error) {
	return nil, nil
}

func (m *mockRepo) GetObjectVersionCount(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}
