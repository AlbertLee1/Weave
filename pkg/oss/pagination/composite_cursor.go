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
