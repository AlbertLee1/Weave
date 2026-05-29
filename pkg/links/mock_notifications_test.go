package links_test

import (
	"context"

	"github.com/liyang/weave/pkg/oms"
)

// Notification stubs on mockRepo to satisfy oms.Repository.

func (m *mockRepo) CreateNotification(_ context.Context, _ *oms.Notification) error { return nil }
func (m *mockRepo) ListNotifications(_ context.Context, _ string, _ bool) ([]oms.Notification, error) {
	return nil, nil
}
func (m *mockRepo) MarkNotificationRead(_ context.Context, _ string) error { return nil }
func (m *mockRepo) CountNotifications(_ context.Context, _ string, _ bool) (int, error) {
	return 0, nil
}
