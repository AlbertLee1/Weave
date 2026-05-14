package optimistic

import (
	"errors"
	"testing"
)

func TestCheckIfMatch_Given_MatchingVersion_When_Check_Then_NoError(t *testing.T) {
	err := CheckIfMatch(5, 5)
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestCheckIfMatch_Given_StaleClient_When_Check_Then_ErrVersionConflict(t *testing.T) {
	err := CheckIfMatch(5, 6)
	if !errors.Is(err, ErrVersionConflict) {
		t.Errorf("expected ErrVersionConflict, got %v", err)
	}
}

func TestCheckIfMatch_Given_ClientAheadOfServer_When_Check_Then_ErrVersionConflict(t *testing.T) {
	// If client somehow has a future version, treat as conflict — server
	// is the source of truth.
	err := CheckIfMatch(10, 5)
	if !errors.Is(err, ErrVersionConflict) {
		t.Errorf("expected ErrVersionConflict, got %v", err)
	}
}

func TestParseIfMatchHeader_Given_PlainInt_Then_OK(t *testing.T) {
	v, err := ParseIfMatchHeader("5")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v != 5 {
		t.Errorf("expected 5, got %d", v)
	}
}

func TestParseIfMatchHeader_Given_QuotedETag_Then_StripsQuotes(t *testing.T) {
	v, err := ParseIfMatchHeader(`"5"`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v != 5 {
		t.Errorf("expected 5, got %d", v)
	}
}

func TestParseIfMatchHeader_Given_WeakETag_Then_StripsWAndQuotes(t *testing.T) {
	v, err := ParseIfMatchHeader(`W/"5"`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v != 5 {
		t.Errorf("expected 5, got %d", v)
	}
}

func TestParseIfMatchHeader_Given_Empty_Then_Error(t *testing.T) {
	_, err := ParseIfMatchHeader("")
	if !errors.Is(err, ErrInvalidIfMatch) {
		t.Errorf("expected ErrInvalidIfMatch, got %v", err)
	}
}

func TestParseIfMatchHeader_Given_NonNumeric_Then_Error(t *testing.T) {
	_, err := ParseIfMatchHeader("not-an-int")
	if !errors.Is(err, ErrInvalidIfMatch) {
		t.Errorf("expected ErrInvalidIfMatch, got %v", err)
	}
}

func TestParseIfMatchHeader_Given_NegativeVersion_Then_Error(t *testing.T) {
	_, err := ParseIfMatchHeader("-1")
	if !errors.Is(err, ErrInvalidIfMatch) {
		t.Errorf("expected ErrInvalidIfMatch, got %v", err)
	}
}
