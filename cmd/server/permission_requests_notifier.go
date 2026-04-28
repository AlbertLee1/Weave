package main

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/permissionrequests"
	"github.com/liyang/weave/pkg/rid"
)

// permissionRequestApproverLister satisfies
// permissionrequests.ApproverLister by querying user_roles for every user
// granted the admin or ontology-owner role. Approver discovery lives at
// the cmd/server adapter layer so pkg/permissionrequests has no pgx
// import — same dep-direction trick as commentMentionNotifier.
type permissionRequestApproverLister struct {
	pool *pgxpool.Pool
}

func newPermissionRequestApproverLister(pool *pgxpool.Pool) *permissionRequestApproverLister {
	if pool == nil {
		return nil
	}
	return &permissionRequestApproverLister{pool: pool}
}

// ListApproverUserIDs returns every distinct user_id that holds a role
// matching one of the global "approver" roles. Empty result is allowed —
// the handler simply skips the fan-out. The query is bounded by the
// (small) cardinality of the roles table; no pagination is needed.
func (l *permissionRequestApproverLister) ListApproverUserIDs(ctx context.Context) ([]string, error) {
	rows, err := l.pool.Query(ctx,
		`SELECT DISTINCT user_id FROM user_roles
		   WHERE role = ANY($1)`,
		[]string{"admin", "ontology-owner"},
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// permissionRequestNotifier fans created / decided permission requests
// out to the existing OMS notifications table — once per approver on
// create, once back to the requester on decide. Mirrors
// commentMentionNotifier's wire shape (Type=`approval` matches the
// SPA's notification taxonomy) so the existing Notification Center
// dropdown surfaces these without any UI work.
type permissionRequestNotifier struct {
	repo oms.Repository
}

func newPermissionRequestNotifier(repo oms.Repository) *permissionRequestNotifier {
	if repo == nil {
		return nil
	}
	return &permissionRequestNotifier{repo: repo}
}

func (n *permissionRequestNotifier) NotifyApproversNewRequest(ctx context.Context, ev permissionrequests.NewRequestEvent) error {
	if ev.Request == nil || ev.ApproverID == "" {
		return nil
	}
	notif := &oms.Notification{
		ID:        rid.NewNotificationRID(),
		UserID:    ev.ApproverID,
		Title:     buildPermissionRequestTitle(ev.Request),
		Body:      ev.Request.Reason,
		Type:      "approval",
		Link:      buildPermissionRequestLink(ev.Request.ID),
		Read:      false,
		CreatedAt: time.Now().UTC(),
	}
	return n.repo.CreateNotification(ctx, notif)
}

func (n *permissionRequestNotifier) NotifyRequesterDecision(ctx context.Context, ev permissionrequests.DecisionEvent) error {
	if ev.Request == nil || ev.Request.RequestedBy == "" {
		return nil
	}
	notif := &oms.Notification{
		ID:        rid.NewNotificationRID(),
		UserID:    ev.Request.RequestedBy,
		Title:     buildPermissionDecisionTitle(ev.Request),
		Body:      ev.Request.DecisionNote,
		Type:      "approval",
		Link:      buildPermissionRequestLink(ev.Request.ID),
		Read:      false,
		CreatedAt: time.Now().UTC(),
	}
	return n.repo.CreateNotification(ctx, notif)
}

func buildPermissionRequestTitle(r *permissionrequests.Request) string {
	if r == nil {
		return "Permission request"
	}
	if r.RequestedBy == "" {
		return fmt.Sprintf("Access requested for %s", r.TargetRID)
	}
	return fmt.Sprintf("%s requested access to %s", r.RequestedBy, r.TargetRID)
}

func buildPermissionDecisionTitle(r *permissionrequests.Request) string {
	if r == nil {
		return "Permission request decided"
	}
	switch r.Status {
	case permissionrequests.StatusApproved:
		return fmt.Sprintf("Access granted to %s", r.TargetRID)
	case permissionrequests.StatusRejected:
		return fmt.Sprintf("Access denied to %s", r.TargetRID)
	default:
		return fmt.Sprintf("Permission request updated for %s", r.TargetRID)
	}
}

// buildPermissionRequestLink encodes the deep-link the Notification
// Center follows. The SPA reads /permission-requests?id=<id> to scroll
// the relevant row into the inbox view.
func buildPermissionRequestLink(id string) string {
	if id == "" {
		return ""
	}
	q := url.Values{}
	q.Set("id", id)
	return "/permission-requests?" + q.Encode()
}

// Compile-time guards — surface signature drift at adapter boot.
var (
	_ permissionrequests.ApproverLister = (*permissionRequestApproverLister)(nil)
	_ permissionrequests.Notifier       = (*permissionRequestNotifier)(nil)
)
