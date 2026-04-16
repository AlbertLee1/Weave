package actions

import (
	"context"

	"github.com/liyang/weave/pkg/oms"
)

// Notification stubs on mockOmsRepo to satisfy oms.Repository.

func (m *mockOmsRepo) CreateNotification(_ context.Context, _ *oms.Notification) error { return nil }
func (m *mockOmsRepo) ListNotifications(_ context.Context, _ string, _ bool) ([]oms.Notification, error) {
	return nil, nil
}
func (m *mockOmsRepo) MarkNotificationRead(_ context.Context, _ string) error { return nil }
