package pagination

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestCompositeCursor_EncodeRoundTrip(t *testing.T) {
	original := &CompositeCursor{
		ObjectType:  "Customer",
		InnerCursor: "eyJvIjoxMH0=",
	}
	encoded := original.Encode()
	if encoded == "" {
		t.Fatal("Encode returned empty string")
	}

	decoded, err := DecodeCompositeCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeCompositeCursor returned error: %v", err)
	}
	if decoded.ObjectType != original.ObjectType {
		t.Errorf("got ObjectType %q, want %q", decoded.ObjectType, original.ObjectType)
	}
	if decoded.InnerCursor != original.InnerCursor {
		t.Errorf("got InnerCursor %q, want %q", decoded.InnerCursor, original.InnerCursor)
	}
}

func TestCompositeCursor_EncodeIsBase64JSON(t *testing.T) {
	c := &CompositeCursor{ObjectType: "Employee", InnerCursor: "abc"}
	encoded := c.Encode()
	raw, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("encoded payload is not valid base64: %v", err)
	}
	var payload map[string]string
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decoded payload is not valid JSON: %v", err)
	}
	if payload["objectType"] != "Employee" {
		t.Errorf("JSON payload objectType = %q, want Employee", payload["objectType"])
	}
	if payload["innerCursor"] != "abc" {
		t.Errorf("JSON payload innerCursor = %q, want abc", payload["innerCursor"])
	}
}

func TestDecodeCompositeCursor_Empty(t *testing.T) {
	c, err := DecodeCompositeCursor("")
	if err != nil {
		t.Fatalf("DecodeCompositeCursor(\"\") returned error: %v", err)
	}
	if c == nil {
		t.Fatal("DecodeCompositeCursor(\"\") returned nil cursor")
	}
	if c.ObjectType != "" || c.InnerCursor != "" {
		t.Errorf("empty cursor should have zero fields, got %+v", c)
	}
	if !c.IsExhausted() {
		t.Error("empty cursor should be reported as exhausted")
	}
}

func TestCompositeCursor_IsExhausted(t *testing.T) {
	cases := []struct {
		name string
		c    CompositeCursor
		want bool
	}{
		{"empty inner", CompositeCursor{ObjectType: "Customer", InnerCursor: ""}, true},
		{"non-empty inner", CompositeCursor{ObjectType: "Customer", InnerCursor: "xyz"}, false},
		{"both empty", CompositeCursor{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.IsExhausted(); got != tc.want {
				t.Errorf("IsExhausted() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDecodeCompositeCursor_InvalidBase64(t *testing.T) {
	_, err := DecodeCompositeCursor("%%%not-valid-base64%%%")
	if err == nil {
		t.Fatal("expected error for invalid base64, got nil")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("error should mention 'invalid', got %v", err)
	}
}

func TestDecodeCompositeCursor_InvalidJSON(t *testing.T) {
	bad := base64.URLEncoding.EncodeToString([]byte("not-json"))
	_, err := DecodeCompositeCursor(bad)
	if err == nil {
		t.Fatal("expected error for malformed JSON payload, got nil")
	}
}

func TestCompositeCursor_EncodeEmptyInnerRoundTrip(t *testing.T) {
	// An exhausted sub-cursor must still round-trip cleanly.
	c := &CompositeCursor{ObjectType: "Supplier", InnerCursor: ""}
	encoded := c.Encode()
	decoded, err := DecodeCompositeCursor(encoded)
	if err != nil {
		t.Fatalf("unexpected decode error: %v", err)
	}
	if decoded.ObjectType != "Supplier" {
		t.Errorf("ObjectType lost in round trip: got %q", decoded.ObjectType)
	}
	if !decoded.IsExhausted() {
		t.Error("expected decoded cursor with empty inner to be exhausted")
	}
}
