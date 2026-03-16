package pagination

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// Cursor represents a pagination cursor that encodes position info.
type Cursor struct {
	Offset int `json:"o"`
}

// Encode serializes the cursor to a base64 string.
func (c *Cursor) Encode() string {
	data, _ := json.Marshal(c)
	return base64.URLEncoding.EncodeToString(data)
}

// DecodeCursor parses a base64 cursor string back into a Cursor.
func DecodeCursor(s string) (*Cursor, error) {
	if s == "" {
		return &Cursor{Offset: 0}, nil
	}
	data, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor: %w", err)
	}
	var c Cursor
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("invalid cursor: %w", err)
	}
	if c.Offset < 0 {
		return nil, fmt.Errorf("invalid cursor: negative offset")
	}
	return &c, nil
}
