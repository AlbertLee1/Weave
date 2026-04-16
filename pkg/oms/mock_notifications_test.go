package oms_test

import (
	"context"

	"github.com/liyang/weave/pkg/oms"
)

// Functional notification mock implementations on mockRepo.

func (m *mockRepo) CreateNotification(_ context.Context, n *oms.Notification) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.notifications = append(m.notifications, *n)
	return nil
}

func (m *mockRepo) ListNotifications(_ context.Context, userID string, unreadOnly bool) ([]oms.Notification, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var result []oms.Notification
	for _, n := range m.notifications {
		if n.UserID == userID {
			if unreadOnly && n.Read {
				continue
			}
			result = append(result, n)
		}
	}
	return result, nil
}

func (m *mockRepo) MarkNotificationRead(_ context.Context, id string) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	for i := range m.notifications {
		if m.notifications[i].ID == id {
			m.notifications[i].Read = true
			return nil
		}
	}
	return oms.ErrNotFound
}

// Notification stubs on noopRepo to satisfy oms.Repository.

func (n *noopRepo) CreateNotification(_ context.Context, _ *oms.Notification) error { return nil }
func (n *noopRepo) ListNotifications(_ context.Context, _ string, _ bool) ([]oms.Notification, error) {
	return nil, nil
}
func (n *noopRepo) MarkNotificationRead(_ context.Context, _ string) error { return nil }
