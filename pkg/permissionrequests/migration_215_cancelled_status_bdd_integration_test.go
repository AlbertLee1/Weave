//go:build integration

package permissionrequests_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/permissionrequests"
)

// TestBDD_Migration215_PermissionRequestsCancelledStatus proves the
// round-64 migration 000215 — without it, the round-63 Cancel
// endpoint's UPDATE … SET status='CANCELLED' would trip the
// permission_requests_status_enum CHECK constraint (only allowed
// PENDING / APPROVED / REJECTED before round 64) and the call would
// fail with 23514 check_violation in production PG-backed deployments.
//
// This test runs against a real Postgres testcontainer with the full
// migration ladder applied so it can assert the constraint actually
// admits CANCELLED end-to-end.
//
// Scenarios:
//   - INSERT a row with status='CANCELLED' succeeds (no constraint
//     violation). Round-64 migration is what makes this possible.
//   - UPDATE an existing PENDING row to status='CANCELLED' succeeds
//     (this is the exact path the round-63 Cancel handler runs).
//   - INSERT with an unknown status ('BOGUS') still fails — the
//     constraint remains in force; we only widened the allowed set,
//     not removed the guard entirely.
//   - The constant exported by the Go side matches the wire value.
func TestBDD_Migration215_PermissionRequestsCancelledStatus(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("RunMigrationsUp: %v", err)
	}
	ctx := context.Background()

	insertRow := func(t *testing.T, status string) (string, error) {
		t.Helper()
		id := uuid.NewString()
		_, err := pg.Pool.Exec(ctx, `
			INSERT INTO permission_requests
			    (id, target_rid, requested_by, reason, status,
			     decided_by, decision_note, created_at, updated_at, decided_at)
			VALUES ($1, $2, $3, '', $4, '', '', NOW(), NOW(), NULL)`,
			id, "ri.objects.main.Customer.42", "u-test", status,
		)
		return id, err
	}

	t.Run("Const matches wire value", func(t *testing.T) {
		if permissionrequests.StatusCancelled != "CANCELLED" {
			t.Fatalf("StatusCancelled = %q, want \"CANCELLED\" — wire format must match migration enum",
				permissionrequests.StatusCancelled)
		}
	})

	t.Run("INSERT with status=CANCELLED succeeds", func(t *testing.T) {
		id, err := insertRow(t, permissionrequests.StatusCancelled)
		if err != nil {
			t.Fatalf("INSERT CANCELLED failed (migration 000215 not applied?): %v", err)
		}
		var got string
		if err := pg.Pool.QueryRow(ctx,
			`SELECT status FROM permission_requests WHERE id = $1`, id).Scan(&got); err != nil {
			t.Fatalf("read-back: %v", err)
		}
		if got != "CANCELLED" {
			t.Errorf("status read back = %q, want CANCELLED", got)
		}
	})

	t.Run("UPDATE PENDING → CANCELLED succeeds (round-63 Cancel path)", func(t *testing.T) {
		id, err := insertRow(t, permissionrequests.StatusPending)
		if err != nil {
			t.Fatalf("seed PENDING failed: %v", err)
		}
		now := time.Now().UTC()
		_, err = pg.Pool.Exec(ctx, `
			UPDATE permission_requests
			   SET status = $1, decided_by = $2, decided_at = $3, updated_at = $3
			 WHERE id = $4`,
			permissionrequests.StatusCancelled, "u-test", now, id,
		)
		if err != nil {
			t.Fatalf("UPDATE → CANCELLED failed (constraint still narrow?): %v", err)
		}
		var status, decidedBy string
		if err := pg.Pool.QueryRow(ctx,
			`SELECT status, decided_by FROM permission_requests WHERE id = $1`, id).
			Scan(&status, &decidedBy); err != nil {
			t.Fatalf("read-back: %v", err)
		}
		if status != "CANCELLED" || decidedBy != "u-test" {
			t.Errorf("status/decided_by = %q/%q, want CANCELLED/u-test", status, decidedBy)
		}
	})

	t.Run("INSERT with unknown status BOGUS still rejected", func(t *testing.T) {
		_, err := insertRow(t, "BOGUS")
		if err == nil {
			t.Fatal("INSERT BOGUS succeeded — constraint must still reject unknown statuses")
		}
		// pgx surfaces SQLSTATE 23514 (check_violation) in the error message.
		if !strings.Contains(err.Error(), "permission_requests_status_enum") &&
			!strings.Contains(err.Error(), "check") {
			t.Errorf("error %v does not look like a CHECK constraint violation", err)
		}
	})

	t.Run("Existing PENDING/APPROVED/REJECTED inserts still work", func(t *testing.T) {
		// Round-64 must NOT break the legacy three-value path.
		for _, status := range []string{
			permissionrequests.StatusPending,
			permissionrequests.StatusApproved,
			permissionrequests.StatusRejected,
		} {
			if _, err := insertRow(t, status); err != nil {
				t.Errorf("legacy status %q insert failed: %v", status, err)
			}
		}
	})
}
