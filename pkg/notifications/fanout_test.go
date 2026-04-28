package notifications

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
)

type fakeWatcherLister struct {
	mu  sync.Mutex
	got [][]string
	out map[string][]string
	err error
}

func (f *fakeWatcherLister) WatchersFor(_ context.Context, targets []string) (map[string][]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := append([]string(nil), targets...)
	f.got = append(f.got, cp)
	if f.err != nil {
		return nil, f.err
	}
	return f.out, nil
}

type fakeNotificationCreator struct {
	mu    sync.Mutex
	calls []notificationCall
	err   error
}

type notificationCall struct {
	UserID, Title, Body, Type, Link string
}

func (f *fakeNotificationCreator) CreateNotificationForUser(_ context.Context, userID, title, body, nType, link string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, notificationCall{userID, title, body, nType, link})
	return f.err
}

type fakeMailer struct {
	mu    sync.Mutex
	calls []mailCall
	err   error
}

type mailCall struct {
	To, Subject, Body string
}

func (f *fakeMailer) Send(_ context.Context, to, subject, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, mailCall{to, subject, body})
	return f.err
}

type fakeEmailResolver struct {
	emails map[string]string
	err    error
}

func (f *fakeEmailResolver) ResolveEmail(_ context.Context, userID string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if e, ok := f.emails[userID]; ok {
		return e, nil
	}
	return "", ErrEmailNotFound
}

func TestActivityFanout_CreatesNotificationsForEachWatcher(t *testing.T) {
	wl := &fakeWatcherLister{
		out: map[string][]string{
			"ri.phonograph2-objects.main.object.42": {"user:alice", "user:bob"},
		},
	}
	nc := &fakeNotificationCreator{}
	f := New(wl, nc)

	err := f.HandleActivity(context.Background(), Activity{
		ObjectType: "Employee",
		PrimaryKey: "42",
		EditType:   "MODIFY",
		ActorID:    "user:dave",
		Properties: map[string]interface{}{"name": "Alice"},
	})
	if err != nil {
		t.Fatalf("HandleActivity: %v", err)
	}

	if len(nc.calls) != 2 {
		t.Fatalf("want 2 notification calls, got %d (%+v)", len(nc.calls), nc.calls)
	}
	got := []string{nc.calls[0].UserID, nc.calls[1].UserID}
	sort.Strings(got)
	if got[0] != "user:alice" || got[1] != "user:bob" {
		t.Fatalf("want alice+bob notifications, got %v", got)
	}
	for _, c := range nc.calls {
		if c.Type != "watch" {
			t.Errorf("notification type = %q want watch", c.Type)
		}
		if !strings.Contains(c.Title, "Employee") {
			t.Errorf("title should mention objectType, got %q", c.Title)
		}
		if c.Link == "" {
			t.Errorf("notification should carry a deep link")
		}
	}
}

func TestActivityFanout_SkipsActor(t *testing.T) {
	wl := &fakeWatcherLister{
		out: map[string][]string{
			"ri.phonograph2-objects.main.object.42": {"user:alice", "user:bob"},
		},
	}
	nc := &fakeNotificationCreator{}
	f := New(wl, nc)

	err := f.HandleActivity(context.Background(), Activity{
		ObjectType: "Employee",
		PrimaryKey: "42",
		EditType:   "MODIFY",
		ActorID:    "user:alice", // alice is both watcher AND author of the edit
	})
	if err != nil {
		t.Fatalf("HandleActivity: %v", err)
	}

	for _, c := range nc.calls {
		if c.UserID == "user:alice" {
			t.Fatalf("actor should NOT receive a self-notification: %+v", c)
		}
	}
	if len(nc.calls) != 1 {
		t.Fatalf("want 1 notification (bob), got %d (%+v)", len(nc.calls), nc.calls)
	}
}

func TestActivityFanout_NoWatchersIsNoop(t *testing.T) {
	wl := &fakeWatcherLister{out: map[string][]string{}}
	nc := &fakeNotificationCreator{}
	f := New(wl, nc)

	err := f.HandleActivity(context.Background(), Activity{
		ObjectType: "Employee",
		PrimaryKey: "42",
		EditType:   "CREATE",
	})
	if err != nil {
		t.Fatalf("HandleActivity: %v", err)
	}
	if len(nc.calls) != 0 {
		t.Fatalf("expected zero notifications, got %d", len(nc.calls))
	}
}

func TestActivityFanout_LookupErrorBubblesUp(t *testing.T) {
	wl := &fakeWatcherLister{err: errors.New("db is sad")}
	nc := &fakeNotificationCreator{}
	f := New(wl, nc)

	err := f.HandleActivity(context.Background(), Activity{
		ObjectType: "Employee",
		PrimaryKey: "42",
		EditType:   "MODIFY",
	})
	if err == nil {
		t.Fatalf("expected error from WatchersFor failure")
	}
	if len(nc.calls) != 0 {
		t.Fatalf("no notifications should be created on lookup failure")
	}
}

// TestActivityFanout_NotificationFailureDoesNotAbort verifies one bad
// recipient doesn't poison the rest of the fan-out — same shape every
// other "best effort" hook (mention notifier, embeddings) follows.
func TestActivityFanout_NotificationFailureDoesNotAbort(t *testing.T) {
	wl := &fakeWatcherLister{
		out: map[string][]string{
			"ri.phonograph2-objects.main.object.42": {"user:alice", "user:bob"},
		},
	}
	nc := &fakeNotificationCreator{err: errors.New("insert failed")}
	f := New(wl, nc)

	err := f.HandleActivity(context.Background(), Activity{
		ObjectType: "Employee",
		PrimaryKey: "42",
		EditType:   "MODIFY",
	})
	if err != nil {
		t.Fatalf("per-recipient failure should be logged, not surfaced: %v", err)
	}
	if len(nc.calls) != 2 {
		t.Fatalf("both recipients should be attempted even after one fails, got %d", len(nc.calls))
	}
}

func TestActivityFanout_EmailDeliveryWhenMailerWired(t *testing.T) {
	wl := &fakeWatcherLister{
		out: map[string][]string{
			"ri.phonograph2-objects.main.object.42": {"user:alice", "user:bob"},
		},
	}
	nc := &fakeNotificationCreator{}
	mailer := &fakeMailer{}
	resolver := &fakeEmailResolver{emails: map[string]string{
		"user:alice": "alice@example.com",
		"user:bob":   "bob@example.com",
	}}
	f := New(wl, nc).WithMailer(mailer, resolver)

	err := f.HandleActivity(context.Background(), Activity{
		ObjectType: "Employee",
		PrimaryKey: "42",
		EditType:   "MODIFY",
	})
	if err != nil {
		t.Fatalf("HandleActivity: %v", err)
	}
	if len(mailer.calls) != 2 {
		t.Fatalf("want 2 email sends, got %d (%+v)", len(mailer.calls), mailer.calls)
	}
	got := []string{mailer.calls[0].To, mailer.calls[1].To}
	sort.Strings(got)
	if got[0] != "alice@example.com" || got[1] != "bob@example.com" {
		t.Fatalf("want alice+bob emails, got %v", got)
	}
}

// TestActivityFanout_SkipsRecipientWithoutEmail verifies that a watcher
// with no resolvable email still gets the in-app notification but is
// silently skipped from email delivery (no error).
func TestActivityFanout_SkipsRecipientWithoutEmail(t *testing.T) {
	wl := &fakeWatcherLister{
		out: map[string][]string{
			"ri.phonograph2-objects.main.object.42": {"user:alice", "user:bob"},
		},
	}
	nc := &fakeNotificationCreator{}
	mailer := &fakeMailer{}
	resolver := &fakeEmailResolver{emails: map[string]string{
		"user:alice": "alice@example.com",
		// bob deliberately missing
	}}
	f := New(wl, nc).WithMailer(mailer, resolver)

	err := f.HandleActivity(context.Background(), Activity{
		ObjectType: "Employee",
		PrimaryKey: "42",
		EditType:   "MODIFY",
	})
	if err != nil {
		t.Fatalf("HandleActivity: %v", err)
	}
	if len(nc.calls) != 2 {
		t.Fatalf("in-app should fire for both, got %d", len(nc.calls))
	}
	if len(mailer.calls) != 1 {
		t.Fatalf("only alice has an email, got %d sends", len(mailer.calls))
	}
	if mailer.calls[0].To != "alice@example.com" {
		t.Fatalf("unexpected recipient %q", mailer.calls[0].To)
	}
}

func TestActivityFanout_SkipsLinkEdits(t *testing.T) {
	wl := &fakeWatcherLister{
		out: map[string][]string{
			"ri.phonograph2-objects.main.object.42": {"user:alice"},
		},
	}
	nc := &fakeNotificationCreator{}
	f := New(wl, nc)

	for _, et := range []string{"LINK_CREATE", "LINK_DELETE", "", "UNKNOWN"} {
		if err := f.HandleActivity(context.Background(), Activity{
			ObjectType: "Employee",
			PrimaryKey: "42",
			EditType:   et,
		}); err != nil {
			t.Fatalf("HandleActivity(%q): %v", et, err)
		}
	}
	if len(nc.calls) != 0 {
		t.Fatalf("link/unknown edits should not notify, got %d", len(nc.calls))
	}
	if len(wl.got) != 0 {
		t.Fatalf("link/unknown edits should short-circuit BEFORE the watcher lookup")
	}
}

func TestComputeTargetRID(t *testing.T) {
	got := ComputeTargetRID("Employee", "42")
	want := "ri.phonograph2-objects.main.object.42"
	if got != want {
		t.Fatalf("ComputeTargetRID = %q want %q", got, want)
	}
}

func TestActivityFanout_TitleAndBodyShape(t *testing.T) {
	wl := &fakeWatcherLister{
		out: map[string][]string{
			"ri.phonograph2-objects.main.object.42": {"user:alice"},
		},
	}
	nc := &fakeNotificationCreator{}
	f := New(wl, nc)

	cases := []struct {
		editType string
		want     string
	}{
		{"CREATE", "created"},
		{"MODIFY", "updated"},
		{"DELETE", "deleted"},
	}
	for _, tc := range cases {
		nc.calls = nil
		if err := f.HandleActivity(context.Background(), Activity{
			ObjectType: "Employee",
			PrimaryKey: "42",
			EditType:   tc.editType,
			ActorID:    "user:dave",
		}); err != nil {
			t.Fatalf("HandleActivity(%s): %v", tc.editType, err)
		}
		if len(nc.calls) != 1 {
			t.Fatalf("HandleActivity(%s): want 1 call, got %d", tc.editType, len(nc.calls))
		}
		if !strings.Contains(strings.ToLower(nc.calls[0].Body), tc.want) {
			t.Errorf("body for %s should contain %q, got %q", tc.editType, tc.want, nc.calls[0].Body)
		}
	}
}
