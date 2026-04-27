package aip

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newSampleThread(id string) *Thread {
	return &Thread{
		ID:        id,
		Title:     "demo",
		Provider:  ProviderMock,
		CreatedBy: "user:alice",
	}
}

func TestMemoryStore_CreateGetDeleteThread(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	tr := newSampleThread("thr_a")
	if err := s.CreateThread(ctx, tr); err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	if tr.CreatedAt.IsZero() || tr.UpdatedAt.IsZero() {
		t.Fatalf("CreateThread should stamp timestamps; got %#v", tr)
	}

	got, err := s.GetThread(ctx, "thr_a")
	if err != nil {
		t.Fatalf("GetThread: %v", err)
	}
	if got.Title != "demo" || got.Provider != ProviderMock {
		t.Fatalf("GetThread returned %#v", got)
	}

	if _, err := s.GetThread(ctx, "nope"); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("GetThread(nope) err=%v want ErrThreadNotFound", err)
	}

	if err := s.CreateThread(ctx, newSampleThread("thr_a")); !errors.Is(err, ErrThreadAlreadyExists) {
		t.Fatalf("CreateThread duplicate err=%v want ErrThreadAlreadyExists", err)
	}

	if err := s.DeleteThread(ctx, "thr_a"); err != nil {
		t.Fatalf("DeleteThread: %v", err)
	}
	if _, err := s.GetThread(ctx, "thr_a"); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("GetThread after delete err=%v want ErrThreadNotFound", err)
	}
	if err := s.DeleteThread(ctx, "thr_a"); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("DeleteThread(missing) err=%v want ErrThreadNotFound", err)
	}
}

func TestMemoryStore_ListThreads_FiltersByOwner(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	tr1 := newSampleThread("thr_a")
	tr1.CreatedBy = "user:alice"
	tr1.CreatedAt = time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	tr2 := newSampleThread("thr_b")
	tr2.CreatedBy = "user:bob"
	tr2.CreatedAt = time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC)
	tr3 := newSampleThread("thr_c")
	tr3.CreatedBy = "user:alice"
	tr3.CreatedAt = time.Date(2026, 4, 3, 0, 0, 0, 0, time.UTC)
	for _, tr := range []*Thread{tr1, tr2, tr3} {
		if err := s.CreateThread(ctx, tr); err != nil {
			t.Fatalf("CreateThread: %v", err)
		}
	}

	alice, err := s.ListThreads(ctx, "user:alice")
	if err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	if len(alice) != 2 {
		t.Fatalf("expected 2 threads for alice, got %d", len(alice))
	}
	// Newest-first.
	if alice[0].ID != "thr_c" || alice[1].ID != "thr_a" {
		t.Errorf("ListThreads order = %s, %s; want thr_c, thr_a", alice[0].ID, alice[1].ID)
	}

	all, err := s.ListThreads(ctx, "")
	if err != nil {
		t.Fatalf("ListThreads(all): %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 threads with createdBy='', got %d", len(all))
	}
}

func TestMemoryStore_UpdateThread_PartialFields(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	tr := newSampleThread("thr_x")
	tr.Model = "old-model"
	tr.SystemPrompt = "old prompt"
	if err := s.CreateThread(ctx, tr); err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	newTitle := "renamed"
	if err := s.UpdateThread(ctx, "thr_x", ThreadUpdate{Title: &newTitle}); err != nil {
		t.Fatalf("UpdateThread: %v", err)
	}
	got, _ := s.GetThread(ctx, "thr_x")
	if got.Title != "renamed" {
		t.Errorf("Title = %q want renamed", got.Title)
	}
	if got.Model != "old-model" {
		t.Errorf("Model preserved? got %q", got.Model)
	}
	if got.SystemPrompt != "old prompt" {
		t.Errorf("SystemPrompt preserved? got %q", got.SystemPrompt)
	}

	if err := s.UpdateThread(ctx, "missing", ThreadUpdate{Title: &newTitle}); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("UpdateThread missing err=%v want ErrThreadNotFound", err)
	}
}

func TestMemoryStore_AppendListMessages(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	tr := newSampleThread("thr_msg")
	if err := s.CreateThread(ctx, tr); err != nil {
		t.Fatalf("CreateThread: %v", err)
	}

	for i, msg := range []*Message{
		{ThreadID: "thr_msg", Role: RoleSystem, Content: "you are a bot"},
		{ThreadID: "thr_msg", Role: RoleUser, Content: "hi"},
		{ThreadID: "thr_msg", Role: RoleAssistant, Content: "hello"},
	} {
		if err := s.AppendMessage(ctx, msg); err != nil {
			t.Fatalf("AppendMessage[%d]: %v", i, err)
		}
		if msg.ID == 0 {
			t.Errorf("AppendMessage should assign monotonic id; got 0 at index %d", i)
		}
	}

	msgs, err := s.ListMessages(ctx, "thr_msg")
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Role != RoleSystem || msgs[2].Role != RoleAssistant {
		t.Errorf("ordering wrong: got roles %s/%s/%s",
			msgs[0].Role, msgs[1].Role, msgs[2].Role)
	}
	for i := 1; i < len(msgs); i++ {
		if msgs[i].ID <= msgs[i-1].ID {
			t.Errorf("ids should be monotonic: %d, %d", msgs[i-1].ID, msgs[i].ID)
		}
	}

	if err := s.AppendMessage(ctx, &Message{ThreadID: "missing", Role: RoleUser, Content: "x"}); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("AppendMessage(missing thread) err=%v want ErrThreadNotFound", err)
	}
	if _, err := s.ListMessages(ctx, "missing"); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("ListMessages(missing thread) err=%v want ErrThreadNotFound", err)
	}
}

func TestMemoryStore_DeleteThreadCascadesMessages(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	tr := newSampleThread("thr_cascade")
	_ = s.CreateThread(ctx, tr)
	_ = s.AppendMessage(ctx, &Message{ThreadID: "thr_cascade", Role: RoleUser, Content: "x"})
	if err := s.DeleteThread(ctx, "thr_cascade"); err != nil {
		t.Fatalf("DeleteThread: %v", err)
	}
	if _, err := s.ListMessages(ctx, "thr_cascade"); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("ListMessages after delete err=%v want ErrThreadNotFound", err)
	}
}
