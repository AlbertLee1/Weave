package controlpanel

// VTX-015 — Vertex Control Panel store + defaults.
//
// BDD acceptance (excerpt):
//   - Given 初始未配置 When GET Then 返回默认值
//     {defaultWindowDays:30, pollingIntervalSec:5,
//      searchAroundMaxNodes:200, searchAroundMaxDepth:3,
//      missingDataWarningHours:24}
//   - Given admin 调 PUT 修改 pollingIntervalSec=10 When 普通用户 GET Then 看到 10
//
// The tests below cover the pure store / defaults shape; HTTP-level concerns
// (auth, status codes) live in handler_test.go.

import (
	"context"
	"testing"
)

// TestDefaultConfig_When_Constructed_Then_MatchesBDDValues anchors the exact
// default values the BDD acceptance demands. Drift here is a wire-contract
// change; the test exists to make that drift explicit.
func TestDefaultConfig_When_Constructed_Then_MatchesBDDValues(t *testing.T) {
	got := DefaultConfig()
	if got.DefaultWindowDays != 30 {
		t.Errorf("DefaultWindowDays = %d, want 30", got.DefaultWindowDays)
	}
	if got.PollingIntervalSec != 5 {
		t.Errorf("PollingIntervalSec = %d, want 5", got.PollingIntervalSec)
	}
	if got.SearchAroundMaxNodes != 200 {
		t.Errorf("SearchAroundMaxNodes = %d, want 200", got.SearchAroundMaxNodes)
	}
	if got.SearchAroundMaxDepth != 3 {
		t.Errorf("SearchAroundMaxDepth = %d, want 3", got.SearchAroundMaxDepth)
	}
	if got.MissingDataWarningHours != 24 {
		t.Errorf("MissingDataWarningHours = %d, want 24", got.MissingDataWarningHours)
	}
}

// TestMemStore_Given_Empty_When_Get_Then_DefaultsReturned anchors the first
// half of BDD acceptance 1: an unconfigured store yields the canonical defaults.
func TestMemStore_Given_Empty_When_Get_Then_DefaultsReturned(t *testing.T) {
	s := NewMemStore()
	got, err := s.Get(context.Background())
	if err != nil {
		t.Fatalf("Get on empty store: %v", err)
	}
	want := DefaultConfig()
	if got != want {
		t.Errorf("Get = %#v, want %#v", got, want)
	}
}

// TestMemStore_Given_Set_When_Get_Then_LatestValueReturned anchors the second
// half of BDD acceptance 2: a PUT (Set) by an admin must be observable to any
// follow-up Get (the handler layer is what gates Set on the admin role).
func TestMemStore_Given_Set_When_Get_Then_LatestValueReturned(t *testing.T) {
	s := NewMemStore()
	updated := DefaultConfig()
	updated.PollingIntervalSec = 10
	if err := s.Set(context.Background(), updated); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := s.Get(context.Background())
	if err != nil {
		t.Fatalf("Get after Set: %v", err)
	}
	if got.PollingIntervalSec != 10 {
		t.Errorf("PollingIntervalSec after Set = %d, want 10", got.PollingIntervalSec)
	}
	// Other fields preserve their values from the Set call (the store keeps
	// the full Config; it does NOT merge with defaults on a partial update —
	// the handler is responsible for merging when callers omit fields).
	if got.DefaultWindowDays != updated.DefaultWindowDays {
		t.Errorf("DefaultWindowDays = %d, want %d", got.DefaultWindowDays, updated.DefaultWindowDays)
	}
}

// TestMemStore_Given_NegativeValue_When_Set_Then_RejectedAsInvalid asserts the
// store-level validator: control-panel knobs are all "positive integers";
// zero or negative values should fail Set so the bad value never reaches Get.
func TestMemStore_Given_NegativeValue_When_Set_Then_RejectedAsInvalid(t *testing.T) {
	s := NewMemStore()
	bad := DefaultConfig()
	bad.PollingIntervalSec = 0
	if err := s.Set(context.Background(), bad); err == nil {
		t.Fatal("Set with PollingIntervalSec=0 returned nil; want validation error")
	}

	bad2 := DefaultConfig()
	bad2.SearchAroundMaxNodes = -1
	if err := s.Set(context.Background(), bad2); err == nil {
		t.Fatal("Set with SearchAroundMaxNodes=-1 returned nil; want validation error")
	}

	// Get after rejected Set still returns defaults — the bad write must not
	// have leaked into observable state.
	got, _ := s.Get(context.Background())
	want := DefaultConfig()
	if got != want {
		t.Errorf("Get after rejected Set = %#v, want defaults %#v", got, want)
	}
}
