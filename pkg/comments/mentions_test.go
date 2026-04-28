package comments

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"sync"
	"testing"
)

func TestExtractMentions(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"no mentions", "hello world", nil},
		{"single email", "hey @alice@example.com please review", []string{"alice@example.com"}},
		{
			"multiple distinct",
			"cc @alice@example.com and @bob@example.com",
			[]string{"alice@example.com", "bob@example.com"},
		},
		{
			"deduped case-insensitive",
			"@Alice@Example.com and @alice@example.com",
			[]string{"alice@example.com"},
		},
		{
			"adjacent punctuation",
			"thanks @bob@example.com! also @carol+team@example.co.uk?",
			[]string{"bob@example.com", "carol+team@example.co.uk"},
		},
		{
			"leading email without @ prefix is ignored",
			"contact alice@example.com directly",
			nil,
		},
		{
			"plain @handle with no domain is ignored",
			"hi @alice",
			nil,
		},
		{
			"escape inside code fence not handled — extracted regardless",
			"`@bob@example.com`",
			[]string{"bob@example.com"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractMentions(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ExtractMentions(%q): got %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// stubDirectory implements MentionUserDirectory for tests.
type stubDirectory struct {
	byEmail map[string]MentionUser
	search  []MentionUser
	err     error
}

func (s *stubDirectory) LookupUserByEmail(_ context.Context, email string) (MentionUser, error) {
	if s.err != nil {
		return MentionUser{}, s.err
	}
	u, ok := s.byEmail[email]
	if !ok {
		return MentionUser{}, ErrMentionUserNotFound
	}
	return u, nil
}

func (s *stubDirectory) SearchMentionUsers(_ context.Context, _ string, _ int) ([]MentionUser, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.search, nil
}

// stubNotifier records every NotifyMention call.
type stubNotifier struct {
	mu     sync.Mutex
	events []MentionEvent
	err    error
}

func (n *stubNotifier) NotifyMention(_ context.Context, ev MentionEvent) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.err != nil {
		return n.err
	}
	n.events = append(n.events, ev)
	return nil
}

func (n *stubNotifier) snapshot() []MentionEvent {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]MentionEvent, len(n.events))
	copy(out, n.events)
	sort.Slice(out, func(i, j int) bool { return out[i].RecipientID < out[j].RecipientID })
	return out
}

func TestProcessMentions_FiresOnePerUser(t *testing.T) {
	dir := &stubDirectory{
		byEmail: map[string]MentionUser{
			"alice@example.com": {ID: "user:alice@example.com", Email: "alice@example.com", Name: "Alice"},
			"bob@example.com":   {ID: "user:bob@example.com", Email: "bob@example.com", Name: "Bob"},
		},
	}
	notif := &stubNotifier{}
	c := &Comment{
		ID:        "c1",
		TargetRID: "ri.ontology.main.object.t1",
		Body:      "cc @alice@example.com and @bob@example.com",
		Author:    "user:carol@example.com",
	}
	processMentions(context.Background(), dir, notif, c)
	got := notif.snapshot()
	if len(got) != 2 {
		t.Fatalf("want 2 notifications, got %d (%+v)", len(got), got)
	}
	if got[0].RecipientID != "user:alice@example.com" || got[1].RecipientID != "user:bob@example.com" {
		t.Fatalf("unexpected recipients: %+v", got)
	}
	if got[0].AuthorID != c.Author || got[0].CommentID != c.ID || got[0].TargetRID != c.TargetRID {
		t.Fatalf("event missing fields: %+v", got[0])
	}
}

func TestProcessMentions_SkipsAuthorSelfMention(t *testing.T) {
	dir := &stubDirectory{
		byEmail: map[string]MentionUser{
			"alice@example.com": {ID: "user:alice@example.com", Email: "alice@example.com"},
		},
	}
	notif := &stubNotifier{}
	c := &Comment{
		ID:        "c1",
		TargetRID: "ri.ontology.main.object.t1",
		Body:      "note to self @alice@example.com",
		Author:    "user:alice@example.com",
	}
	processMentions(context.Background(), dir, notif, c)
	if got := notif.snapshot(); len(got) != 0 {
		t.Fatalf("self-mention should not notify, got %+v", got)
	}
}

func TestProcessMentions_IgnoresUnknownUsers(t *testing.T) {
	dir := &stubDirectory{
		byEmail: map[string]MentionUser{
			"alice@example.com": {ID: "user:alice@example.com", Email: "alice@example.com"},
		},
	}
	notif := &stubNotifier{}
	c := &Comment{
		ID:        "c1",
		TargetRID: "ri.ontology.main.object.t1",
		Body:      "@alice@example.com @ghost@nowhere.invalid",
		Author:    "user:bob@example.com",
	}
	processMentions(context.Background(), dir, notif, c)
	got := notif.snapshot()
	if len(got) != 1 || got[0].RecipientID != "user:alice@example.com" {
		t.Fatalf("expected one notification for alice, got %+v", got)
	}
}

func TestProcessMentions_SwallowsNotifierError(t *testing.T) {
	// A notifier failure must not panic or block — operators monitor
	// notification health out-of-band. processMentions just logs and
	// continues, mirroring the audit-tee + propagation patterns.
	dir := &stubDirectory{
		byEmail: map[string]MentionUser{
			"alice@example.com": {ID: "user:alice@example.com", Email: "alice@example.com"},
		},
	}
	notif := &stubNotifier{err: errors.New("boom")}
	c := &Comment{
		ID:        "c1",
		TargetRID: "ri.ontology.main.object.t1",
		Body:      "@alice@example.com",
		Author:    "user:bob@example.com",
	}
	// Must not panic.
	processMentions(context.Background(), dir, notif, c)
}

func TestProcessMentions_NoOpWithoutCollaborators(t *testing.T) {
	c := &Comment{Body: "@alice@example.com", Author: "user:bob@example.com"}
	// Must be safe to call with nil directory or nil notifier — degraded
	// mode (no PG) leaves both unset.
	processMentions(context.Background(), nil, nil, c)
	processMentions(context.Background(), &stubDirectory{}, nil, c)
	processMentions(context.Background(), nil, &stubNotifier{}, c)
}
