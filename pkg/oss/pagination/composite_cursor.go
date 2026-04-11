package pagination

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// CompositeCursor represents a pagination position inside a single
// implementing ObjectType while iterating across a multi-type ObjectSet
// (e.g. loadObjectsOrInterfaces). A multi-type cursor carries one of these
// per surviving sub-stream; an empty InnerCursor marks the sub-stream as
// exhausted and ready to be dropped from the heap merge.
type CompositeCursor struct {
	ObjectType  string `json:"objectType"`
	InnerCursor string `json:"innerCursor"`
}

// IsExhausted reports whether this sub-cursor should be removed from a
// composite iteration. A cursor is exhausted when its InnerCursor is empty,
// mirroring the "no more pages" convention used by *Cursor.Encode().
func (c CompositeCursor) IsExhausted() bool {
	return c.InnerCursor == ""
}

// Encode serializes the composite cursor to a URL-safe base64 JSON string.
func (c *CompositeCursor) Encode() string {
	if c == nil {
		return ""
	}
	data, _ := json.Marshal(c)
	return base64.URLEncoding.EncodeToString(data)
}

// DecodeCompositeCursor parses a base64 composite cursor string. An empty
// input yields a zero-valued cursor (exhausted). Malformed base64 or JSON
// produce a non-nil error without leaking the raw payload.
func DecodeCompositeCursor(s string) (*CompositeCursor, error) {
	if s == "" {
		return &CompositeCursor{}, nil
	}
	data, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid composite cursor: %w", err)
	}
	var c CompositeCursor
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("invalid composite cursor: %w", err)
	}
	return &c, nil
}

// MultiTypeCursor wraps a set of per-sub-type CompositeCursor entries. It is
// the wire format for paging a polymorphic (interface) ObjectSet that spans
// multiple implementing ObjectTypes. Exhausted sub-cursors are dropped from
// SubCursors before encoding, so an empty slice (or empty MultiTypeCursor)
// means "no more pages".
type MultiTypeCursor struct {
	SubCursors []CompositeCursor `json:"subCursors"`
}

// IsExhausted reports whether the cursor carries any live sub-cursors.
func (m *MultiTypeCursor) IsExhausted() bool {
	if m == nil {
		return true
	}
	for _, sc := range m.SubCursors {
		if !sc.IsExhausted() {
			return false
		}
	}
	return true
}

// Encode serializes the multi-type cursor to a URL-safe base64 JSON string.
// Returns the empty string if no live sub-cursors remain so callers can use
// the empty string as the "last page" sentinel.
func (m *MultiTypeCursor) Encode() string {
	if m == nil || len(m.SubCursors) == 0 {
		return ""
	}
	data, _ := json.Marshal(m)
	return base64.URLEncoding.EncodeToString(data)
}

// DecodeMultiTypeCursor parses a base64 multi-type cursor string. An empty
// input yields a zero-valued cursor (all sub-streams exhausted). Malformed
// base64 or JSON produce a non-nil error without leaking the raw payload.
func DecodeMultiTypeCursor(s string) (*MultiTypeCursor, error) {
	if s == "" {
		return &MultiTypeCursor{}, nil
	}
	data, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid multi-type cursor: %w", err)
	}
	var m MultiTypeCursor
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("invalid multi-type cursor: %w", err)
	}
	return &m, nil
}
