package userprefs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestMemoryStore_GetMissing(t *testing.T) {
	s := NewMemoryStore()
	_, err := s.Get(context.Background(), "alice")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get on empty store: want ErrNotFound, got %v", err)
	}
}

func TestMemoryStore_UpsertInsertThenGet(t *testing.T) {
	s := NewMemoryStore()
	theme := "dark"
	lang := "zh-CN"
	notif := json.RawMessage(`{"mentions":true}`)
	got, err := s.Upsert(context.Background(), "alice", Update{
		Theme:         &theme,
		Language:      &lang,
		Notifications: &notif,
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if got.Theme != "dark" {
		t.Errorf("Theme: want 'dark', got %q", got.Theme)
	}
	if got.Language != "zh-CN" {
		t.Errorf("Language: want 'zh-CN', got %q", got.Language)
	}
	if string(got.Notifications) != `{"mentions":true}` {
		t.Errorf("Notifications: want '{\"mentions\":true}', got %s", got.Notifications)
	}
	if string(got.Hotkeys) != "{}" {
		t.Errorf("Hotkeys default: want '{}', got %s", got.Hotkeys)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Errorf("timestamps not stamped")
	}
	round, err := s.Get(context.Background(), "alice")
	if err != nil {
		t.Fatalf("Get after upsert: %v", err)
	}
	if round.Theme != "dark" || round.Language != "zh-CN" {
		t.Errorf("round-trip lost values: %+v", round)
	}
}

func TestMemoryStore_UpsertPartialPreservesOtherFields(t *testing.T) {
	s := NewMemoryStore()
	theme := "dark"
	lang := "en"
	if _, err := s.Upsert(context.Background(), "bob", Update{Theme: &theme, Language: &lang}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	newLang := "zh-CN"
	got, err := s.Upsert(context.Background(), "bob", Update{Language: &newLang})
	if err != nil {
		t.Fatalf("partial upsert: %v", err)
	}
	if got.Theme != "dark" {
		t.Errorf("Theme should be preserved when upd.Theme is nil; got %q", got.Theme)
	}
	if got.Language != "zh-CN" {
		t.Errorf("Language not updated: %q", got.Language)
	}
}

func TestMemoryStore_UpsertRejectsInvalidTheme(t *testing.T) {
	s := NewMemoryStore()
	bad := "purple"
	_, err := s.Upsert(context.Background(), "alice", Update{Theme: &bad})
	if err == nil {
		t.Fatal("expected error on invalid theme")
	}
}

func TestMemoryStore_UpsertEmptyEnvelopeBecomesDefault(t *testing.T) {
	s := NewMemoryStore()
	empty := json.RawMessage(nil)
	got, err := s.Upsert(context.Background(), "alice", Update{Notifications: &empty})
	if err != nil {
		t.Fatalf("upsert empty notif: %v", err)
	}
	if string(got.Notifications) != "{}" {
		t.Errorf("empty notif should become '{}', got %s", got.Notifications)
	}
}

func TestMemoryStore_GetReturnsCloneNotAlias(t *testing.T) {
	s := NewMemoryStore()
	notif := json.RawMessage(`{"a":1}`)
	if _, err := s.Upsert(context.Background(), "alice", Update{Notifications: &notif}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got1, _ := s.Get(context.Background(), "alice")
	got1.Notifications[1] = 'X'
	got2, _ := s.Get(context.Background(), "alice")
	if string(got2.Notifications) != `{"a":1}` {
		t.Errorf("Get returned aliased slice; second Get sees mutation: %s", got2.Notifications)
	}
}

func TestValidateTheme(t *testing.T) {
	for _, ok := range []string{"", "dark", "light", "system"} {
		if err := ValidateTheme(ok); err != nil {
			t.Errorf("ValidateTheme(%q) unexpected error: %v", ok, err)
		}
	}
	if err := ValidateTheme("emerald"); err == nil {
		t.Errorf("ValidateTheme(emerald) should error")
	}
}

func TestValidateLanguage(t *testing.T) {
	if err := ValidateLanguage(""); err != nil {
		t.Errorf("empty language should be allowed: %v", err)
	}
	if err := ValidateLanguage("zh-CN"); err != nil {
		t.Errorf("standard tag should be allowed: %v", err)
	}
	long := ""
	for i := 0; i < MaxLanguageLength+1; i++ {
		long += "a"
	}
	if err := ValidateLanguage(long); err == nil {
		t.Errorf("over-length language should error")
	}
}
