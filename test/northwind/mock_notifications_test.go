package northwind_test

import (
	"context"

	"github.com/liyang/weave/pkg/oms"
)

// Notification stubs on inMemoryOmsRepo to satisfy oms.Repository.

func (m *inMemoryOmsRepo) CreateNotification(_ context.Context, _ *oms.Notification) error {
	return nil
}
func (m *inMemoryOmsRepo) ListNotifications(_ context.Context, _ string, _ bool) ([]oms.Notification, error) {
	return nil, nil
}
func (m *inMemoryOmsRepo) MarkNotificationRead(_ context.Context, _ string) error { return nil }
