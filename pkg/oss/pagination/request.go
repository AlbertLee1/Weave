package pagination

import (
	"fmt"
	"net/http"
	"strconv"
)

const (
	// DefaultPageSize is the shared cursor page size used by ParsePageRequest
	// and by the search / linked-object / interface list paths. Kept at 100 so
	// those endpoints are unchanged.
	DefaultPageSize = 100
	// ListDefaultPageSize is the Foundry list default applied ONLY by the
	// GET .../objects/{objectType} ListObjects path when the caller sends no
	// pageSize. Foundry's list endpoint defaults to 1000 (== MaxPageSize),
	// whereas the shared DefaultPageSize stays 100 so other endpoints keep
	// their smaller default (no global blast radius).
	ListDefaultPageSize = 1000
	MaxPageSize         = 1000
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
