package pagination

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// resetPaginationState clears the package-level signing key and expiry
// configuration so tests are independent. Defer this in any test that
// mutates SetSigningKey / SetMaxAge.
func resetPaginationState() {
	SetSigningKey(nil)
	SetMaxAge(0)
}

// --- HMAC integrity ---

func TestCursor_HMAC(t *testing.T) {
	defer resetPaginationState()

	cases := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "no_key_back_compat_round_trip",
			run: func(t *testing.T) {
				resetPaginationState()
				c := &Cursor{Offset: 7}
				encoded := c.Encode()
				// Without a signing key the encoded body must not contain a sig field;
				// existing v2 callers rely on the "{\"o\":N}" shape.
				raw, err := base64.URLEncoding.DecodeString(encoded)
				if err != nil {
					t.Fatalf("base64 decode: %v", err)
				}
				if strings.Contains(string(raw), `"sig"`) {
					t.Errorf("unsigned cursor must not include sig field, got %s", raw)
				}
				dec, err := DecodeCursor(encoded)
				if err != nil {
					t.Fatalf("decode unsigned cursor: %v", err)
				}
				if dec.Offset != 7 {
					t.Errorf("Offset round trip: got %d, want 7", dec.Offset)
				}
			},
		},
		{
			name: "with_key_round_trip",
			run: func(t *testing.T) {
				resetPaginationState()
				SetSigningKey([]byte("test-key-do-not-use-in-prod"))
				c := &Cursor{Offset: 100}
				encoded := c.Encode()
				raw, _ := base64.URLEncoding.DecodeString(encoded)
				if !strings.Contains(string(raw), `"sig"`) {
					t.Errorf("signed cursor must include sig field, got %s", raw)
				}
				dec, err := DecodeCursor(encoded)
				if err != nil {
					t.Fatalf("decode signed cursor: %v", err)
				}
				if dec.Offset != 100 {
					t.Errorf("Offset round trip: got %d, want 100", dec.Offset)
				}
			},
		},
		{
			name: "tampered_offset_rejected",
			run: func(t *testing.T) {
				resetPaginationState()
				SetSigningKey([]byte("k1"))
				encoded := (&Cursor{Offset: 50}).Encode()
				raw, _ := base64.URLEncoding.DecodeString(encoded)
				var m map[string]any
				_ = json.Unmarshal(raw, &m)
				m["o"] = 9999 // tamper but keep the original sig
				tampered, _ := json.Marshal(m)
				token := base64.URLEncoding.EncodeToString(tampered)
				_, err := DecodeCursor(token)
				if !errors.Is(err, ErrTamperedCursor) {
					t.Fatalf("want ErrTamperedCursor, got %v", err)
				}
			},
		},
		{
			name: "missing_sig_when_key_set_rejected",
			run: func(t *testing.T) {
				resetPaginationState()
				// First encode without a key (so no sig is present), then
				// rotate the verifier into key-required mode.
				encoded := (&Cursor{Offset: 1}).Encode()
				SetSigningKey([]byte("k2"))
				_, err := DecodeCursor(encoded)
				if !errors.Is(err, ErrTamperedCursor) {
					t.Fatalf("want ErrTamperedCursor for unsigned cursor under signing mode, got %v", err)
				}
			},
		},
		{
			name: "key_rotation_invalidates_old_signatures",
			run: func(t *testing.T) {
				resetPaginationState()
				SetSigningKey([]byte("old-key"))
				token := (&Cursor{Offset: 5}).Encode()
				SetSigningKey([]byte("new-key"))
				_, err := DecodeCursor(token)
				if !errors.Is(err, ErrTamperedCursor) {
					t.Fatalf("after key rotation, want ErrTamperedCursor, got %v", err)
				}
			},
		},
		{
			name: "encode_does_not_mutate_receiver",
			run: func(t *testing.T) {
				resetPaginationState()
				SetSigningKey([]byte("k3"))
				c := &Cursor{Offset: 9}
				_ = c.Encode()
				if c.Sig != "" {
					t.Errorf("Encode must not mutate caller cursor Sig, got %q", c.Sig)
				}
				if c.IssuedAt != 0 {
					t.Errorf("Encode must not mutate caller cursor IssuedAt, got %d", c.IssuedAt)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

// --- Expiry ---

func TestCursor_Expiry(t *testing.T) {
	defer resetPaginationState()

	t.Run("no_max_age_no_iat_field", func(t *testing.T) {
		resetPaginationState()
		token := (&Cursor{Offset: 3}).Encode()
		raw, _ := base64.URLEncoding.DecodeString(token)
		if strings.Contains(string(raw), `"iat"`) {
			t.Errorf("no MaxAge configured, iat must not be set, got %s", raw)
		}
	})

	t.Run("fresh_cursor_decodes", func(t *testing.T) {
		resetPaginationState()
		SetMaxAge(time.Hour)
		token := (&Cursor{Offset: 3}).Encode()
		dec, err := DecodeCursor(token)
		if err != nil {
			t.Fatalf("fresh cursor must decode, got %v", err)
		}
		if dec.Offset != 3 {
			t.Errorf("Offset round trip: got %d, want 3", dec.Offset)
		}
	})

	t.Run("expired_cursor_returns_err_expired", func(t *testing.T) {
		resetPaginationState()
		SetMaxAge(time.Hour)
		// Forge an old IssuedAt by encoding manually.
		c := Cursor{Offset: 3, IssuedAt: time.Now().Add(-2 * time.Hour).Unix()}
		body, _ := json.Marshal(c)
		token := base64.URLEncoding.EncodeToString(body)
		_, err := DecodeCursor(token)
		if !errors.Is(err, ErrExpiredCursor) {
			t.Fatalf("want ErrExpiredCursor, got %v", err)
		}
	})

	t.Run("expiry_works_with_signing_key", func(t *testing.T) {
		resetPaginationState()
		SetSigningKey([]byte("sig-key"))
		SetMaxAge(time.Millisecond)
		token := (&Cursor{Offset: 1}).Encode()
		time.Sleep(20 * time.Millisecond)
		// Wait beyond MaxAge so DecodeCursor should report expired.
		_, err := DecodeCursor(token)
		if !errors.Is(err, ErrExpiredCursor) {
			t.Fatalf("want ErrExpiredCursor under signed+expired, got %v", err)
		}
	})
}

// --- Edge windows: empty / single / last page ---

func TestApply_WindowEdges(t *testing.T) {
	cases := []struct {
		name     string
		items    []int
		offset   int
		pageSize int
		want     []int
	}{
		{"empty_input", []int{}, 0, 10, []int{}},
		{"single_element_first_page", []int{42}, 0, 10, []int{42}},
		{"exact_last_page_full", []int{1, 2, 3, 4}, 2, 2, []int{3, 4}},
		{"last_partial_page", []int{1, 2, 3, 4, 5}, 4, 10, []int{5}},
		{"offset_equals_len_returns_empty", []int{1, 2, 3}, 3, 5, []int{}},
		{"offset_past_end_returns_empty", []int{1, 2, 3}, 99, 5, []int{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pr := &PageRequest{PageSize: tc.pageSize, Cursor: &Cursor{Offset: tc.offset}}
			got := Apply(tc.items, pr)
			if len(got) != len(tc.want) {
				t.Fatalf("Apply len: got %d, want %d (got=%v)", len(got), len(tc.want), got)
			}
			for i, v := range tc.want {
				if got[i] != v {
					t.Errorf("Apply[%d] = %d, want %d", i, got[i], v)
				}
			}
		})
	}
}

// --- PageResponse next-token semantics on edges ---

func TestNewPageResponse_NextTokenEdges(t *testing.T) {
	defer resetPaginationState()

	t.Run("first_page_with_more_data", func(t *testing.T) {
		resetPaginationState()
		pr := &PageRequest{PageSize: 10, Cursor: &Cursor{Offset: 0}}
		resp := NewPageResponse([]string{"a"}, 50, pr)
		if resp.NextPageToken == "" {
			t.Fatal("expected next token on first page when more data exists")
		}
		dec, err := DecodeCursor(resp.NextPageToken)
		if err != nil {
			t.Fatalf("decode next token: %v", err)
		}
		if dec.Offset != 10 {
			t.Errorf("next offset got %d, want 10", dec.Offset)
		}
	})

	t.Run("exact_boundary_no_more_pages", func(t *testing.T) {
		resetPaginationState()
		pr := &PageRequest{PageSize: 10, Cursor: &Cursor{Offset: 40}}
		resp := NewPageResponse([]string{}, 50, pr)
		if resp.NextPageToken != "" {
			t.Errorf("at exact boundary (offset+pageSize == total) next token must be empty, got %q", resp.NextPageToken)
		}
	})

	t.Run("empty_dataset_no_next_token", func(t *testing.T) {
		resetPaginationState()
		pr := &PageRequest{PageSize: 10, Cursor: &Cursor{Offset: 0}}
		resp := NewPageResponse([]string{}, 0, pr)
		if resp.NextPageToken != "" {
			t.Errorf("empty dataset must yield no next token, got %q", resp.NextPageToken)
		}
		if resp.TotalCount != "0" {
			t.Errorf("totalCount must be \"0\", got %q", resp.TotalCount)
		}
	})

	t.Run("single_record_dataset_no_next_token", func(t *testing.T) {
		resetPaginationState()
		pr := &PageRequest{PageSize: 100, Cursor: &Cursor{Offset: 0}}
		resp := NewPageResponse([]string{"only"}, 1, pr)
		if resp.NextPageToken != "" {
			t.Errorf("single-element dataset must yield no next token, got %q", resp.NextPageToken)
		}
	})
}

// --- ParsePageRequest validation surface (used by HTTP handlers to map to 400) ---

func TestParsePageRequest_Errors(t *testing.T) {
	defer resetPaginationState()

	t.Run("non_numeric_page_size_rejected", func(t *testing.T) {
		resetPaginationState()
		r := httptest.NewRequest("GET", "/items?pageSize=abc", nil)
		if _, err := ParsePageRequest(r); err == nil {
			t.Fatal("want error for non-numeric pageSize, got nil")
		}
	})

	t.Run("zero_page_size_rejected", func(t *testing.T) {
		resetPaginationState()
		r := httptest.NewRequest("GET", "/items?pageSize=0", nil)
		if _, err := ParsePageRequest(r); err == nil {
			t.Fatal("want error for pageSize=0, got nil")
		}
	})

	t.Run("negative_page_size_rejected", func(t *testing.T) {
		resetPaginationState()
		r := httptest.NewRequest("GET", "/items?pageSize=-1", nil)
		if _, err := ParsePageRequest(r); err == nil {
			t.Fatal("want error for negative pageSize, got nil")
		}
	})

	t.Run("invalid_base64_page_token_rejected", func(t *testing.T) {
		resetPaginationState()
		// "!!!" is not in the URL-safe base64 alphabet, but is URL-safe
		// enough to round-trip through the query parser unchanged so the
		// failure is in DecodeCursor, not net/url.
		r := httptest.NewRequest("GET", "/items?pageToken=!!!notbase64!!!", nil)
		if _, err := ParsePageRequest(r); err == nil {
			t.Fatal("want error for malformed pageToken base64, got nil")
		}
	})

	t.Run("tampered_page_token_returns_err_tampered", func(t *testing.T) {
		resetPaginationState()
		SetSigningKey([]byte("k"))
		// Encode a valid signed cursor, then flip a byte in the JSON body.
		valid := (&Cursor{Offset: 10}).Encode()
		raw, _ := base64.URLEncoding.DecodeString(valid)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		m["o"] = 11
		bad, _ := json.Marshal(m)
		token := base64.URLEncoding.EncodeToString(bad)
		r := httptest.NewRequest("GET", "/items?pageToken="+token, nil)
		_, err := ParsePageRequest(r)
		if !errors.Is(err, ErrTamperedCursor) {
			t.Fatalf("want ErrTamperedCursor wrapped from ParsePageRequest, got %v", err)
		}
	})
}

// --- CompositeCursor sort keys (NULL ordering, multi-column) ---

func TestCompositeCursor_SortKeys(t *testing.T) {
	t.Run("back_compat_no_sort_keys", func(t *testing.T) {
		c := &CompositeCursor{ObjectType: "Customer", InnerCursor: "x"}
		token := c.Encode()
		raw, _ := base64.URLEncoding.DecodeString(token)
		if strings.Contains(string(raw), `"sortKeys"`) {
			t.Errorf("nil SortKeys must not appear in encoded body, got %s", raw)
		}
		dec, err := DecodeCompositeCursor(token)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(dec.SortKeys) != 0 {
			t.Errorf("decoded SortKeys must be empty, got %v", dec.SortKeys)
		}
	})

	t.Run("single_sort_key_nulls_last", func(t *testing.T) {
		c := &CompositeCursor{
			ObjectType:  "Customer",
			InnerCursor: "page-1",
			SortKeys: []SortKey{
				{Field: "name", Value: "Alice", Direction: "asc", NullOrder: "last"},
			},
		}
		token := c.Encode()
		dec, err := DecodeCompositeCursor(token)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(dec.SortKeys) != 1 {
			t.Fatalf("got %d sort keys, want 1", len(dec.SortKeys))
		}
		got := dec.SortKeys[0]
		if got.Field != "name" || got.Direction != "asc" || got.NullOrder != "last" {
			t.Errorf("sort key round trip mismatch: %+v", got)
		}
	})

	t.Run("multi_column_sort_with_nulls_first", func(t *testing.T) {
		c := &CompositeCursor{
			ObjectType:  "Order",
			InnerCursor: "p2",
			SortKeys: []SortKey{
				{Field: "priority", Value: 5.0, Direction: "desc", NullOrder: "first"},
				{Field: "createdAt", Value: "2026-05-12T10:00:00Z", Direction: "desc", NullOrder: "last"},
			},
		}
		token := c.Encode()
		dec, err := DecodeCompositeCursor(token)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(dec.SortKeys) != 2 {
			t.Fatalf("got %d sort keys, want 2", len(dec.SortKeys))
		}
		if dec.SortKeys[0].NullOrder != "first" || dec.SortKeys[1].NullOrder != "last" {
			t.Errorf("NullOrder ordering mismatch: %+v", dec.SortKeys)
		}
		// Order must be preserved (compound sort is order-sensitive).
		if dec.SortKeys[0].Field != "priority" || dec.SortKeys[1].Field != "createdAt" {
			t.Errorf("sort key order not preserved: %+v", dec.SortKeys)
		}
	})

	t.Run("nil_value_sort_key_round_trip", func(t *testing.T) {
		// A NULL marker (Value=nil) at the cursor boundary is the common
		// "scan past the NULL bucket" case for nulls-first paging.
		c := &CompositeCursor{
			ObjectType:  "Product",
			InnerCursor: "p1",
			SortKeys:    []SortKey{{Field: "discontinuedAt", Value: nil, Direction: "asc", NullOrder: "first"}},
		}
		dec, err := DecodeCompositeCursor(c.Encode())
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(dec.SortKeys) != 1 {
			t.Fatalf("got %d sort keys, want 1", len(dec.SortKeys))
		}
		if dec.SortKeys[0].Value != nil {
			t.Errorf("nil sort value must round-trip as nil, got %v", dec.SortKeys[0].Value)
		}
	})
}

// --- MultiTypeCursor exhaustion + decode boundary ---

func TestMultiTypeCursor(t *testing.T) {
	t.Run("nil_receiver_is_exhausted", func(t *testing.T) {
		var m *MultiTypeCursor
		if !m.IsExhausted() {
			t.Error("nil MultiTypeCursor must be exhausted")
		}
		if got := m.Encode(); got != "" {
			t.Errorf("nil MultiTypeCursor.Encode must return empty, got %q", got)
		}
	})

	t.Run("empty_sub_cursors_is_exhausted_and_encodes_empty", func(t *testing.T) {
		m := &MultiTypeCursor{}
		if !m.IsExhausted() {
			t.Error("empty SubCursors must be exhausted")
		}
		if got := m.Encode(); got != "" {
			t.Errorf("empty MultiTypeCursor.Encode must return empty, got %q", got)
		}
	})

	t.Run("all_inner_empty_is_exhausted", func(t *testing.T) {
		m := &MultiTypeCursor{SubCursors: []CompositeCursor{
			{ObjectType: "A", InnerCursor: ""},
			{ObjectType: "B", InnerCursor: ""},
		}}
		if !m.IsExhausted() {
			t.Error("all-empty inner cursors must report exhausted")
		}
	})

	t.Run("at_least_one_live_sub_not_exhausted", func(t *testing.T) {
		m := &MultiTypeCursor{SubCursors: []CompositeCursor{
			{ObjectType: "A", InnerCursor: ""},
			{ObjectType: "B", InnerCursor: "next"},
		}}
		if m.IsExhausted() {
			t.Error("a live sub-cursor must keep the multi cursor non-exhausted")
		}
	})

	t.Run("encode_decode_round_trip", func(t *testing.T) {
		m := &MultiTypeCursor{SubCursors: []CompositeCursor{
			{ObjectType: "Customer", InnerCursor: "c1"},
			{ObjectType: "Order", InnerCursor: "o1", SortKeys: []SortKey{{Field: "id", Value: 42.0}}},
		}}
		token := m.Encode()
		dec, err := DecodeMultiTypeCursor(token)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(dec.SubCursors) != 2 {
			t.Fatalf("got %d sub cursors, want 2", len(dec.SubCursors))
		}
		if dec.SubCursors[0].ObjectType != "Customer" || dec.SubCursors[1].ObjectType != "Order" {
			t.Errorf("sub cursor order not preserved: %+v", dec.SubCursors)
		}
		if len(dec.SubCursors[1].SortKeys) != 1 || dec.SubCursors[1].SortKeys[0].Field != "id" {
			t.Errorf("sort key not preserved through MultiTypeCursor round trip: %+v", dec.SubCursors[1])
		}
	})

	t.Run("decode_empty_returns_exhausted_zero", func(t *testing.T) {
		dec, err := DecodeMultiTypeCursor("")
		if err != nil {
			t.Fatalf("decode empty: %v", err)
		}
		if !dec.IsExhausted() {
			t.Error("decoded empty MultiTypeCursor must be exhausted")
		}
	})

	t.Run("decode_invalid_base64_rejected", func(t *testing.T) {
		_, err := DecodeMultiTypeCursor("!!!notbase64!!!")
		if err == nil {
			t.Fatal("want error for malformed base64, got nil")
		}
	})

	t.Run("decode_invalid_json_rejected", func(t *testing.T) {
		bad := base64.URLEncoding.EncodeToString([]byte("not-json"))
		_, err := DecodeMultiTypeCursor(bad)
		if err == nil {
			t.Fatal("want error for malformed JSON, got nil")
		}
	})
}
