package pagination

import (
	"net/http/httptest"
	"testing"
)

// --- Cursor tests ---

func TestCursor_EncodeRoundTrip(t *testing.T) {
	original := &Cursor{Offset: 42}
	encoded := original.Encode()

	decoded, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeCursor returned error: %v", err)
	}
	if decoded.Offset != original.Offset {
		t.Errorf("got offset %d, want %d", decoded.Offset, original.Offset)
	}
}

func TestCursor_DecodeEmpty(t *testing.T) {
	cursor, err := DecodeCursor("")
	if err != nil {
		t.Fatalf("DecodeCursor returned error: %v", err)
	}
	if cursor.Offset != 0 {
		t.Errorf("got offset %d, want 0", cursor.Offset)
	}
}

func TestCursor_DecodeInvalid(t *testing.T) {
	_, err := DecodeCursor("%%%not-valid-base64%%%")
	if err == nil {
		t.Fatal("expected error for invalid base64, got nil")
	}
}

func TestCursor_DecodeNegativeOffset(t *testing.T) {
	// Manually encode a cursor with a negative offset.
	c := &Cursor{Offset: -5}
	encoded := c.Encode()

	_, err := DecodeCursor(encoded)
	if err == nil {
		t.Fatal("expected error for negative offset, got nil")
	}
}

// --- PageRequest tests ---

func TestParsePageRequest_Defaults(t *testing.T) {
	r := httptest.NewRequest("GET", "/items", nil)
	pr, err := ParsePageRequest(r)
	if err != nil {
		t.Fatalf("ParsePageRequest returned error: %v", err)
	}
	if pr.PageSize != DefaultPageSize {
		t.Errorf("got pageSize %d, want %d", pr.PageSize, DefaultPageSize)
	}
	if pr.Cursor.Offset != 0 {
		t.Errorf("got offset %d, want 0", pr.Cursor.Offset)
	}
}

func TestParsePageRequest_CustomSize(t *testing.T) {
	r := httptest.NewRequest("GET", "/items?pageSize=50", nil)
	pr, err := ParsePageRequest(r)
	if err != nil {
		t.Fatalf("ParsePageRequest returned error: %v", err)
	}
	if pr.PageSize != 50 {
		t.Errorf("got pageSize %d, want 50", pr.PageSize)
	}
}

func TestParsePageRequest_MaxSize(t *testing.T) {
	r := httptest.NewRequest("GET", "/items?pageSize=5000", nil)
	pr, err := ParsePageRequest(r)
	if err != nil {
		t.Fatalf("ParsePageRequest returned error: %v", err)
	}
	if pr.PageSize != MaxPageSize {
		t.Errorf("got pageSize %d, want %d (capped)", pr.PageSize, MaxPageSize)
	}
}

// --- PageResponse tests ---

func TestPageResponse_FirstPage(t *testing.T) {
	pr := &PageRequest{
		PageSize: 10,
		Cursor:   &Cursor{Offset: 0},
	}
	resp := NewPageResponse([]string{"a", "b"}, 25, pr)

	if resp.NextPageToken == "" {
		t.Error("expected nextPageToken to be set, got empty string")
	}
	// Decode and verify the next cursor points to offset 10.
	next, err := DecodeCursor(resp.NextPageToken)
	if err != nil {
		t.Fatalf("failed to decode nextPageToken: %v", err)
	}
	if next.Offset != 10 {
		t.Errorf("got next offset %d, want 10", next.Offset)
	}
}

func TestPageResponse_LastPage(t *testing.T) {
	pr := &PageRequest{
		PageSize: 10,
		Cursor:   &Cursor{Offset: 20},
	}
	resp := NewPageResponse([]string{"a", "b"}, 25, pr)

	if resp.NextPageToken != "" {
		t.Errorf("expected empty nextPageToken on last page, got %q", resp.NextPageToken)
	}
}

func TestPageResponse_IncludesTotalCount(t *testing.T) {
	pr := &PageRequest{
		PageSize: 10,
		Cursor:   &Cursor{Offset: 0},
	}
	resp := NewPageResponse([]string{}, 42, pr)

	if resp.TotalCount == "" {
		t.Fatal("expected totalCount to be set, got empty")
	}
	if resp.TotalCount != "42" {
		t.Errorf("got totalCount %s, want 42", resp.TotalCount)
	}
}

// --- Apply tests ---

func TestApply_FirstPage(t *testing.T) {
	items := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	pr := &PageRequest{
		PageSize: 3,
		Cursor:   &Cursor{Offset: 0},
	}
	result := Apply(items, pr)

	if len(result) != 3 {
		t.Fatalf("got %d items, want 3", len(result))
	}
	for i, want := range []int{1, 2, 3} {
		if result[i] != want {
			t.Errorf("result[%d] = %d, want %d", i, result[i], want)
		}
	}
}

func TestApply_BeyondEnd(t *testing.T) {
	items := []int{1, 2, 3}
	pr := &PageRequest{
		PageSize: 10,
		Cursor:   &Cursor{Offset: 100},
	}
	result := Apply(items, pr)

	if len(result) != 0 {
		t.Errorf("got %d items, want 0", len(result))
	}
}
