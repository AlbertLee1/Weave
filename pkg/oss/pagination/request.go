package pagination

import (
	"fmt"
	"net/http"
	"strconv"
)

const (
	DefaultPageSize = 100
	MaxPageSize     = 1000
)

// PageRequest holds the parsed pagination parameters from an HTTP request.
type PageRequest struct {
	PageSize int
	Cursor   *Cursor
}

// ParsePageRequest extracts pagination parameters from query string.
// Supports: pageSize (default 100, max 1000) and pageToken (cursor string).
func ParsePageRequest(r *http.Request) (*PageRequest, error) {
	pr := &PageRequest{
		PageSize: DefaultPageSize,
	}

	if ps := r.URL.Query().Get("pageSize"); ps != "" {
		size, err := strconv.Atoi(ps)
		if err != nil {
			return nil, fmt.Errorf("invalid pageSize: %w", err)
		}
		if size <= 0 {
			return nil, fmt.Errorf("invalid pageSize: must be positive")
		}
		if size > MaxPageSize {
			size = MaxPageSize
		}
		pr.PageSize = size
	}

	cursor, err := DecodeCursor(r.URL.Query().Get("pageToken"))
	if err != nil {
		return nil, err
	}
	pr.Cursor = cursor

	return pr, nil
}
